// A · 开机自启遇上「容器引擎晚起」验收（真实引擎，默认跳过）
//
// 这是干什么的——把 phpo 放到一个「此刻拨不通、几秒后才出现」的引擎端点前，看它是否真的等得到：
// 端点是一条转发到本机真实引擎的临时 socket，phpo 起先拨不通，socat 在第 3 秒把它接上。
//
// 错的时候看得见什么——没等就跳过 → 这里直接报错（任务以失败终态收尾）；等满了仍未就绪 → 会看到
// log.autoStartTimeout 那一行；本用例断的是「等得到就照常把这一单跑完」。
//
// 怎么回来——先把容器引擎起来（Docker：sudo systemctl start docker；Podman：systemctl --user start podman.socket），
// 再在服务页手动点那颗「启动」。
//
// 副作用面：勾选项在临时库里没记安装态，所以走「未安装，跳过」那一档——一个容器都不建、不停、不删，
// 不碰本机那份真实 phpo 环境（本机可能正有一个 phpo 在跑）。
package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
)

// liveLogRecorder 记下每条 task:log 的消息码，供断言「等满/跳过/成功」各落在哪一行
type liveLogRecorder struct {
	codes []string
	lines []string
}

func (r *liveLogRecorder) Emit(event string, payload any) {
	if event != "task:log" {
		return
	}
	if l, ok := payload.(model.TaskLogEvent); ok {
		r.codes = append(r.codes, l.Code)
		r.lines = append(r.lines, l.Text)
	}
}

func (r *liveLogRecorder) has(code string) bool {
	for _, c := range r.codes {
		if c == code {
			return true
		}
	}
	return false
}

// TestA_AutoStart_DelayedEngine_Live 引擎晚起也照样自启：真实 store + 真实引擎客户端 + 真实任务队列，
// 唯一假的东西是「端点第 3 秒才出现」这件事（用 socat 转发到本机真实引擎 socket 复现）。
func TestA_AutoStart_DelayedEngine_Live(t *testing.T) {
	if os.Getenv("PHPO_LIVE") != "1" {
		t.Skip("设置 PHPO_LIVE=1 后运行开机自启真实引擎验收")
	}
	// 1) 先拿本机真实引擎端点当转发靶
	real0, err := engine.New()
	if err != nil {
		t.Fatalf("构造引擎客户端失败: %v", err)
	}
	ctx0, cancel0 := context.WithTimeout(context.Background(), 10*time.Second)
	h0 := engine.Check(ctx0, real0)
	cancel0()
	_ = real0.Close()
	if !h0.CanStart {
		t.Skipf("本机引擎不可用，跳过： %s %s", h0.Message, h0.Hint)
	}
	target := strings.TrimPrefix(real0.DockerHost(), "unix://")
	if target == real0.DockerHost() || !strings.HasPrefix(target, "/") {
		t.Skipf("本机端点不是 unix socket（%q），socat 转发路不适用", real0.DockerHost())
	}

	// 2) 临时目录里的真实对象图；端点钉在一条此刻还不存在的 socket 上
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	st, err := store.Open(home + "/phpo.db")
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	defer func() { _ = st.Close() }()
	cfg, err := config.LoadFromPath(home + "/config.yaml")
	if err != nil {
		t.Fatalf("载入 ConfigStore 失败: %v", err)
	}
	st.SetEnvProvider(cfg)

	lateSock := filepath.Join(home, "engine.sock")
	t.Setenv("DOCKER_HOST", "unix://"+lateSock)
	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造延迟端点客户端失败: %v", err)
	}
	defer func() { _ = cli.Close() }()
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	if engine.Check(cctx, cli).CanStart {
		ccancel()
		t.Fatal("端点此刻应拨不通（socket 还没建），否则这个用例没有验到等待")
	}
	ccancel()

	rec := &liveLogRecorder{}
	lc := service.NewLifecycle(cli, st, rec, env, cfg)
	tm := task.NewManager(rec)
	cacheMgr := steps.NewCacheManager(env, rec, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)

	// 3) 第 3 秒才把引擎「装上」：socat 把临时 socket 转发到本机真实引擎
	//    sleep 在 shell 里等，exec 让 socat 顶掉 shell 的 PID——Cleanup 杀的就是监听本体
	cmd := exec.Command("/bin/sh", "-c",
		fmt.Sprintf("sleep 3; exec socat '%s' '%s'", "UNIX-LISTEN:"+lateSock+",fork,reuseaddr", "UNIX-CONNECT:"+target))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("本机没有 socat/sh，无法复现延迟引擎：%v", err)
	}
	go func() { _ = cmd.Wait() }() // 收尸，避免留 zombie
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // 进程组：连同 fork 出的转发子进程
			_ = cmd.Process.Kill()
		}
	})

	// 4) 自启：库里没记 redis/8 → 每一项走「未安装，跳过」，但整单必须在引擎装上之后跑完
	started := time.Now()
	if err := appSvc.AutoStart(context.Background(), []string{"redis/8"}, func() bool { return true }); err != nil {
		t.Fatalf("引擎晚起仍应把自启这一单跑完，实得错误: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 3*time.Second {
		t.Errorf("自启应等过引擎装上那一刻（第 3 秒），却只等了 %s——说明它没等", elapsed)
	}
	if elapsed > 60*time.Second {
		t.Errorf("等得到就不该拖满窗口，实得 %s", elapsed)
	}
	if rec.has("log.autoStartTimeout") {
		t.Errorf("引擎第 3 秒就就绪，不该判成等引擎超时，日志码 %v", rec.codes)
	}
	if !rec.has("log.autoStartSkipAbsent") {
		t.Errorf("应有一行「未安装，跳过开机自启」，实得码 %v / 文本 %v", rec.codes, rec.lines)
	}
	if len(rec.lines) == 0 {
		t.Fatal("自启这一单应留下步骤日志")
	}
}
