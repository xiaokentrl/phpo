// 容器装配：按命名规范幂等创建/启停/删除 phpo 托管服务容器（§5.13.2/§5.13.3）
package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"phpo/internal/config"
	"phpo/pkg/dockerutil"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// ContainerSpec 描述一个 phpo 托管服务容器的创建意图
type ContainerSpec struct {
	Kind    string            // php / mysql / pgsql / redis / nginx
	Version string            // 版本号（保留原样）
	Image   string            // repo:tag
	Env     []string          // KEY=VAL
	Command []string          // 覆盖 CMD
	Exposed []string          // 容器内暴露端口，如 "9000/tcp"
	PortMap map[string]string // 发布映射：容器端口 "9000/tcp" → 宿主端口 "9000"
	Labels  map[string]string // 额外标签（合并进归属标签）
}

// CreateServiceContainer 幂等创建服务容器：确保网络 → 清理同名 → 装配命名/挂载/别名 → create
func (c *Client) CreateServiceContainer(ctx context.Context, env config.Env, spec ContainerSpec) (string, error) {
	name := dockerutil.ContainerName(spec.Kind, spec.Version)
	if err := c.EnsureNetwork(ctx); err != nil {
		return "", err
	}
	if c.ContainerExists(ctx, name) {
		// 清同名这一步只要「容器不在了」就算成：引擎报错但实测已没（Podman rootless 收尾那类）不该拦住装/重建
		if err := ToleratedAsNil(c.RemoveContainer(ctx, name)); err != nil {
			return "", err
		}
	}
	cfg, host, netc := buildContainerSpecs(env, spec)
	resp, err := c.cli.ContainerCreate(ctx, cfg, host, netc, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// buildContainerSpecs 纯装配：把 ContainerSpec 翻译为 create 所需的三份配置（不含网络 IO，便于单测）
func buildContainerSpecs(env config.Env, spec ContainerSpec) (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	cfg := &container.Config{
		Image:  spec.Image,
		Env:    spec.Env,
		Cmd:    spec.Command,
		Labels: mergeLabels(OwnershipLabels(spec.Kind, spec.Version), spec.Labels),
	}
	if len(spec.Exposed) > 0 {
		cfg.ExposedPorts = make(map[nat.Port]struct{}, len(spec.Exposed))
		for _, p := range spec.Exposed {
			cfg.ExposedPorts[nat.Port(p)] = struct{}{}
		}
	}

	host := &container.HostConfig{
		Binds:         BuildBinds(env, spec.Kind, spec.Version),
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}
	if len(spec.PortMap) > 0 {
		host.PortBindings = make(nat.PortMap, len(spec.PortMap))
		for cport, hport := range spec.PortMap {
			host.PortBindings[nat.Port(cport)] = []nat.PortBinding{{HostPort: hport}}
		}
	}

	alias := dockerutil.NetworkAlias(spec.Kind, spec.Version)
	netc := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			dockerutil.NetworkName: {Aliases: []string{alias}},
		},
	}
	return cfg, host, netc
}

// 启动就绪等待参数：ContainerStart 返回时进程未必活着，须确认它稳定停在 running 再交棒给 Post-Verify。
// 写成 var 是为让 wire 测试把等待收紧到毫秒级，否则「崩溃循环取证」这条路径每次都要跑满 startWait
const diagLogLines = 5 // 失败时带进日志的容器日志行数

var (
	startWait     = 12 * time.Second       // 最长等待（pgsql 首次 initdb 之外的常规启动远小于此）
	startInterval = 300 * time.Millisecond // 轮询间隔
	startHold     = 2 * time.Second        // 见到 running 后复验前的静默期（避开「起来即崩」窗口）
)

// StartContainer 启动容器并等待其稳定运行（幂等：已在运行则直接返回）。
// 不等就不报错的话，Post-Verify 只会看到一眼状态字符串：崩溃循环（restarting/exited）读成
// 「没在跑」，报「期望 running=true 实际 false」却不给真因（§5.13.1 一致性）。
// 崩溃循环中的容器走 restart 而非 start：多次失败后 Docker 的重启退避已拉到分钟级
// （真机实测两次重试相隔 60s），start 只排进下次自动重试，等待窗口内永远读不到 running。
func (c *Client) StartContainer(ctx context.Context, name string) error {
	status, _, err := c.ContainerStatus(ctx, name)
	if err != nil {
		return err
	}
	switch status {
	case "running":
		return nil
	case "restarting":
		if err := c.cli.ContainerRestart(ctx, name, container.StopOptions{}); err != nil {
			return err
		}
	default:
		if err := c.cli.ContainerStart(ctx, name, container.StartOptions{}); err != nil {
			return err
		}
	}
	return c.waitRunning(ctx, name)
}

// RestartContainer 重启容器并等待其稳定运行（php.ini / fpm 池这类「仅启动期读取」的配置保存后生效用）。
// 与 StartContainer 同一套等待与失败诊断：不等就报成功，调用方只会看到一眼状态字符串而拿不到真因。
func (c *Client) RestartContainer(ctx context.Context, name string) error {
	if err := c.cli.ContainerRestart(ctx, name, container.StopOptions{}); err != nil {
		return err
	}
	return c.waitRunning(ctx, name)
}

// waitRunning 轮询到容器稳定 running：running 后静默 startHold 再复验，躲开「起来即崩」窗口；
// 其余状态一律等到超时——崩溃循环里的自愈（改完配置等下次重试）不能被一眼 exited 判死。
func (c *Client) waitRunning(ctx context.Context, name string) error {
	deadline := time.Now().Add(startWait)
	status, exit := "", 0
	for {
		st, ex, err := c.ContainerStatus(ctx, name)
		if err != nil {
			return err
		}
		status, exit = st, ex
		if status == "running" {
			if err := sleepCtx(ctx, startHold); err != nil {
				return err
			}
			st, ex, e := c.ContainerStatus(ctx, name)
			if e != nil {
				return e
			}
			if st == "running" {
				return nil
			}
			status, exit = st, ex
		}
		if !time.Now().Before(deadline) {
			return c.startFailure(ctx, name, status, exit)
		}
		if err := sleepCtx(ctx, startInterval); err != nil {
			return err
		}
	}
}

// startFailure 把「容器没起来」拼成人话：状态 + 退出码 + 日志尾部（§3.2 原则 3）
func (c *Client) startFailure(ctx context.Context, name, status string, exitCode int) error {
	return errors.New(startFailureMsg(name, status, exitCode, c.LogTail(ctx, name, diagLogLines)))
}

// startFailureMsg 纯拼装，便于无 Docker 单测
func startFailureMsg(name, status string, exitCode int, logTail string) string {
	msg := fmt.Sprintf("启动 %s 失败：容器状态 %s", name, status)
	if exitCode != 0 {
		msg += fmt.Sprintf("（退出码 %d）", exitCode)
	}
	if logTail != "" {
		msg += "；容器日志尾部：" + logTail
	}
	return msg
}

// needsStop 崩溃重启中的容器也要停：只认 running 会把它放过后又被 Docker 拉起来，
// 「已停止」的结论下一轮即被推翻（§5.13.1 幂等）
func needsStop(status string) bool { return status == "running" || status == "restarting" }

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ToleratedError 引擎把这一步判成失败，但现读现场发现目标其实已经达成——最典型的是 Podman rootless：
// 容器真的停了，报错来自停止之后的网络收尾（`rootless netns: kill network process: permission denied`）。
// 这一类不该判死已经做成的工作（§1.2 规则 16：能警告不阻止）：调用方照常收尾，只把引擎原文落一行提醒。
type ToleratedError struct {
	Name string // 容器名
	Op   string // 哪一步（stop / remove），只作诊断，不进文案
	Msg  string // 引擎返回的原文，原样带出去给用户看
}

func (e *ToleratedError) Error() string { return e.Name + ": " + e.Msg }

// AsTolerated 取出「实测已达成、引擎收尾报错」那一类错误；不是这一类返回 nil。
// 用 errors.As 而非类型断言：调用方包装过一层（%w）也认得出来。
func AsTolerated(err error) *ToleratedError {
	var t *ToleratedError
	if errors.As(err, &t) {
		return t
	}
	return nil
}

// ToleratedAsNil 把「实测已达成、引擎收尾报错」那一类折成 nil，真失败原样返回。
// 只用在拿不到步骤日志、也无从再提醒的幂等清理路（建容器前清同名、批量删除）；
// 任务路径要用 AsTolerated 取出引擎原文落一行提醒，别用它把抱怨抹平。
func ToleratedAsNil(err error) error {
	if AsTolerated(err) != nil {
		return nil
	}
	return err
}

// StopContainer 停止容器（幂等：已停/不存在视为已停止；restarting 视为仍需停止）。
// 报错后现读一次容器状态：实测已停就按目标达成收尾（返回 *ToleratedError 带引擎原文），
// 实测仍在跑（或现场问不出来）才判失败——不能拿收尾抱怨把一个已经停下的服务说成没停。
func (c *Client) StopContainer(ctx context.Context, name string) error {
	status, _, err := c.ContainerStatus(ctx, name)
	if err != nil {
		return err
	}
	if !needsStop(status) {
		return nil
	}
	if err := c.cli.ContainerStop(ctx, name, container.StopOptions{}); err != nil {
		st, _, e := c.ContainerStatus(ctx, name)
		if e == nil && !needsStop(st) {
			return &ToleratedError{Name: name, Op: "stop", Msg: err.Error()}
		}
		return err
	}
	return nil
}

// RemoveContainer 删除容器（force 停删合一，幂等：不存在视为成功）。
// 不带 removeVolumes：卸载/重装默认保留数据（§0.2 规则 20、§5.13.7），删卷只能走显式二次确认。
// 本项目持久化全在宿主 bind 目录（§5.14.2 MOUNTS），容器不留卷，删卷只可能误伤镜像声明的匿名卷。
// 与 StopContainer 同一把尺子：报错后现读一次现场，容器已经不在了就按达成收尾并带上引擎原文；
// 现读问不出结果（引擎本身没答）或容器还在，一律原判失败——不把「看不见」当成「没了」。
func (c *Client) RemoveContainer(ctx context.Context, name string) error {
	if !c.ContainerExists(ctx, name) {
		return nil
	}
	if err := c.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}); err != nil {
		if st, _, e := c.ContainerStatus(ctx, name); e == nil && st == "" {
			return &ToleratedError{Name: name, Op: "remove", Msg: err.Error()}
		}
		return err
	}
	return nil
}

func mergeLabels(base, extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
