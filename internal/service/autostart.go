// 开机自启：phpo 启动后把用户勾选过的服务补启动一遍。
//
// 这是干什么的——用户在设置页勾了「开机自启」的服务，phpo 一起来就把还没在跑的那些逐个启动，
// 不用他一个个点「启动」。开机时容器引擎（Docker 或 Podman）常常比 phpo 起得晚，所以这里先等引擎就绪再动手。
//
// 错的时候看得见什么——等满两分钟引擎仍没就绪，或某个服务启动失败，都会在任务抽屉里留下一行 err
// 点名是哪个服务、为什么不行；整个自启任务进队列账本，失败会标成「失败」而不是悄悄跳过。
//
// 怎么回来——先把容器引擎起来（Docker：sudo systemctl start docker；Podman：systemctl --user start podman.socket），
// 再在服务页手动点那颗「启动」。自启只在一次进程里跑一遍，重开 phpo 即再试一次。
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/pkg/dockerutil"
)

const opAutoStart = "autostart"

// autoStartProbeEvery 等引擎就绪时的重试间隔；autoStartWaitMax 最多等多久。
// 等待发生在建任务之前：任务队列是单飞的，把等待塞进步骤里等于让后面所有任务排一分钟。
// 写成 var 只为一处用途：用例把这两档缩短成毫秒级来验「等不到就跳过」那条路，运行时取值不变。
var (
	autoStartProbeEvery = 5 * time.Second
	autoStartWaitMax    = 2 * time.Minute
)

// 自启这几种结局的消息码（中文原文只作 fallback，随码发出；见总纲 §5.15）
const (
	logAutoStartSkipAbsent  = "log.autoStartSkipAbsent"  // 库里没记着装过这个版本
	logAutoStartSkipRunning = "log.autoStartSkipRunning" // 已经在跑，不重复启动
	logAutoStartDone        = "log.autoStartDone"        // 补启动成功
	logAutoStartFail        = "log.autoStartFail"        // 补启动失败（点名原因）
	logAutoStartTimeout     = "log.autoStartTimeout"     // 等容器引擎超时
)

// autoStartTarget 一个待补启动的服务版本
type autoStartTarget struct {
	kind    model.ServiceKind
	version string
	name    string
}

// AutoStart 把 refs（"kind/version"，来自 config.yaml 的 autostart 标记）里没在跑的服务启动起来。
// 一次进程只跑一遍：引擎就绪后延迟校准那一步会再调到这里，靠 autostartOnce 挡住重复触发。
// current 回答「这份对象图还是当下那张吗」——等引擎的两分钟里用户可能改过自定义根并换了图，
// 拿旧 env 去动宿主配置等于把数据写进用户已经搬走的那个目录。
func (s *AppService) AutoStart(ctx context.Context, refs []string, current func() bool) error {
	if len(refs) == 0 || !s.autostartOnce.CompareAndSwap(false, true) {
		return nil
	}
	targets := autoStartTargets(refs)
	if len(targets) == 0 {
		return nil
	}
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.name
	}
	joined := strings.Join(names, ", ")
	label := "开机自启 " + joined
	params := map[string]string{"names": joined}
	meta := model.TaskMeta{Type: opAutoStart}

	ready := s.waitEngineReady(ctx)
	if ctx.Err() != nil {
		return nil // 进程正在退出：这时候建一单只会留下一条谁也看不到的失败账
	}
	if !current() {
		return nil // 等的这两分钟里对象图被换过（改过自定义根）：旧图上的自启不再补，宿主配置已不是当下那份
	}
	if !ready {
		waited := strconv.Itoa(int(autoStartWaitMax / time.Second))
		return s.one(meta, label, task.MsgTaskAutoStart, params,
			func(_ context.Context, log task.StepLog) error {
				logfCode(log, model.LogErr, logAutoStartTimeout, map[string]string{"wait": waited},
					"等了 "+waited+" 秒，容器引擎仍未就绪，本次开机自启已跳过：先启动容器引擎，再在服务页手动点「启动」")
				return fmt.Errorf("container engine not ready within %s", waited+"s")
			})
	}

	return s.one(meta, label, task.MsgTaskAutoStart, params,
		func(ctx context.Context, log task.StepLog) error {
			return s.autoStartAll(ctx, targets, log)
		})
}

// autoStartAll 逐个补启动：一个失败不影响下一个（自启是「尽力补齐」，不是原子事务），
// 但整单仍以聚合错误收尾——界面要看得见这一单没全成。
func (s *AppService) autoStartAll(ctx context.Context, targets []autoStartTarget, log task.StepLog) error {
	snap, err := s.lifecycle.Snapshot()
	if err != nil {
		return err
	}
	var failed []string
	for _, t := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !snap.HasVersion(string(t.kind), t.version) {
			logfCode(log, model.LogDim, logAutoStartSkipAbsent, map[string]string{"name": t.name},
				t.name+" 未安装，跳过开机自启")
			continue
		}
		running, err := s.lifecycle.Running(ctx, t.kind, t.version)
		if err != nil {
			failed = append(failed, t.name+": "+err.Error())
			logfCode(log, model.LogErr, logAutoStartFail,
				map[string]string{"name": t.name, "reason": err.Error()}, t.name+" 启动失败: "+err.Error())
			continue
		}
		if running {
			logfCode(log, model.LogDim, logAutoStartSkipRunning, map[string]string{"name": t.name},
				t.name+" 已在运行，跳过开机自启")
			continue
		}
		// 与手动「启动」同一套前置修复：旧装机的坏配置不先就地改掉，自启只是把一个必然崩溃的容器再拉起一次
		healPgLogging(s.env, t.kind, t.version, log)
		healPhpAllowedClients(s.env, t.kind, t.version, log)
		confPerm0644(s.env, t.kind, t.version, log)
		if err := s.lifecycle.Start(ctx, t.kind, t.version); err != nil {
			failed = append(failed, t.name+": "+err.Error())
			logfCode(log, model.LogErr, logAutoStartFail,
				map[string]string{"name": t.name, "reason": err.Error()}, t.name+" 启动失败: "+err.Error())
			continue
		}
		logfCode(log, model.LogOk, logAutoStartDone, map[string]string{"name": t.name}, t.name+" 已开机自启")
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s", strings.Join(failed, "; "))
	}
	return nil
}

// waitEngineReady 等容器引擎拨得通：开机时引擎服务比 phpo 的窗口起得晚，这一刻 CanStart=false 不代表一会儿还不行。
// 就绪返回 true；等满 autoStartWaitMax 或 ctx 被取消返回 false。phpo 不代用户拉起引擎服务（§5.13），所以只能等。
func (s *AppService) waitEngineReady(ctx context.Context) bool {
	deadline := time.Now().Add(autoStartWaitMax)
	for {
		cctx, cancel := context.WithTimeout(ctx, dockerProbeTimeout)
		h := engine.Check(cctx, s.probe)
		cancel()
		if h.CanStart {
			return true
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		if left > autoStartProbeEvery {
			left = autoStartProbeEvery
		}
		timer := time.NewTimer(left)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// autoStartTargets 把 "kind/version" 换成待启动清单；形状不对或版本不是路径安全的（手改过的 config.yaml
// 里塞进 `..`、`/` 这类）一律丢掉——这份清单后面要拿去拼容器名和服务数据目录，不能让一个坏键走进宿主路径。
func autoStartTargets(refs []string) []autoStartTarget {
	out := make([]autoStartTarget, 0, len(refs))
	for _, ref := range refs {
		kind, version, ok := config.SplitSvcRef(ref)
		if !ok {
			continue
		}
		vc := config.ValidateVersion(kind, version)
		if !vc.Ok {
			continue
		}
		out = append(out, autoStartTarget{
			kind:    model.ServiceKind(kind),
			version: vc.Value,
			name:    dockerutil.ContainerName(kind, vc.Value),
		})
	}
	return out
}
