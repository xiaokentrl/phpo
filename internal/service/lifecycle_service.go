// 生命周期服务：容器装卸/启停 + 状态校准；校准在启动 / 每任务后 / 手动三处触发（§5.13.9）
// 依赖以最小接口注入（DockerOps/StateStore/Emitter），核心判定复用 T305 的 Op 与 engine.Calibrate，无 Docker 亦可测。
package service

import (
	"context"
	"fmt"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

// DockerOps engine.Client 提供的最小容器操作集（*engine.Client 天然满足）
type DockerOps interface {
	ManagedContainers(ctx context.Context) ([]engine.ActualState, error)
	CreateServiceContainer(ctx context.Context, env config.Env, spec engine.ContainerSpec) (string, error)
	StartContainer(ctx context.Context, name string) error
	StopContainer(ctx context.Context, name string) error
	RemoveContainer(ctx context.Context, name string) error
	PreCleanContainer(ctx context.Context, name string) error
}

// StateStore SQLite 权威视图读写子集（*store.Store 满足）
type StateStore interface {
	BuildSnapshot() (*model.Snapshot, error)
	SetInstalled(kind, version string, installed bool) error
	SetRunning(kind, version string, running bool) error
}

// Emitter §5.6 事件发射最小抽象（app.Emitter 满足）
type Emitter interface {
	Emit(event string, payload any)
}

// Service 一种服务的安装装配策略：产出创建容器所需的 spec
type Service interface {
	Kind() model.ServiceKind
	ContainerSpec(version string, env config.Env) (engine.ContainerSpec, error)
}

// LifecycleService 组合注入依赖；env 为当前派生路径
type LifecycleService struct {
	docker   DockerOps
	store    StateStore
	emitter  Emitter
	env      config.Env
	services map[model.ServiceKind]Service
}

func NewLifecycle(docker DockerOps, store StateStore, emitter Emitter, env config.Env) *LifecycleService {
	svc := &LifecycleService{docker: docker, store: store, emitter: emitter, env: env, services: map[model.ServiceKind]Service{}}
	// PHP / Nginx 无需 env 读取，恒注册；MySQL / PgSQL / Redis 需读明文密码与按版本端口，仅当 store 实现 EnvReader 时注册（M5）
	for _, s := range []Service{PHPService{}, NginxService{}} {
		svc.services[s.Kind()] = s
	}
	if pr, ok := store.(EnvReader); ok {
		for _, s := range []Service{
			NewDBService(model.KindMySQL, pr),
			NewDBService(model.KindPgsql, pr),
			NewRedisService(pr),
		} {
			svc.services[s.Kind()] = s
		}
	}
	return svc
}

// ---- 校准 ----

// Snapshot 读当前权威视图（后端唯一权威，前端经 state:changed 或直接拉取）
func (l *LifecycleService) Snapshot() (*model.Snapshot, error) {
	return l.store.BuildSnapshot()
}

// Calibrate 读 SQLite 期望态与 Docker 实际态比对：
//   - 有运行态修正 → 写回 SetRunning，并依次发 docker:state-drift（漂移详情）与 state:changed（新快照）；
//   - 无变化则静默返回。
//
// 存在性漂移（缺失/孤儿）仅随事件上报，不自动删建，避免误删用户数据（§5.13.13）。
func (l *LifecycleService) Calibrate(ctx context.Context) (*engine.CalibrateResult, error) {
	snap, err := l.store.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	actual, err := l.docker.ManagedContainers(ctx)
	if err != nil {
		return nil, err
	}
	res := engine.Calibrate(refsOf(snap.Installed), refsOf(snap.Running), actual)
	if !res.Changed() {
		return &res, nil
	}

	for _, c := range res.Corrections {
		if err := l.store.SetRunning(c.Ref.Kind, c.Ref.Version, c.Running); err != nil {
			return nil, fmt.Errorf("校准回写运行态失败 %s/%s: %w", c.Ref.Kind, c.Ref.Version, err)
		}
	}
	l.emitter.Emit("docker:state-drift", model.StateDrift{
		Expected: driftView(snap.Installed, snap.Running),
		Actual:   actualView(actual),
	})
	fresh, err := l.store.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	l.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return &res, nil
}

// ---- 生命周期操作（三阶段 Op 包装） ----

// Install 缓存管道（T303）已确保镜像就绪后，创建并启动容器：Pre-Clean 同名 → 建 → 启 → 校验在运行。
// 幂等：Pre-Clean 抹平同名差异；已达成态由 Post-Verify 判定。配置渲染于 T307 补全（此处镜像自带默认即可起）。
func (l *LifecycleService) Install(ctx context.Context, kind model.ServiceKind, version string) error {
	svc, ok := l.services[kind]
	if !ok {
		return fmt.Errorf("未注册的服务种类: %s", kind)
	}
	spec, err := svc.ContainerSpec(version, l.env)
	if err != nil {
		return err
	}
	name := dockerutil.ContainerName(string(kind), version)

	op := engine.Op{
		Name:     "install " + name,
		PreClean: func(ctx context.Context) error { return l.docker.PreCleanContainer(ctx, name) },
		Execute: func(ctx context.Context) error {
			if _, err := l.docker.CreateServiceContainer(ctx, l.env, spec); err != nil {
				return err
			}
			return l.docker.StartContainer(ctx, name)
		},
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, true) },
		Rollback:   func(ctx context.Context) error { return l.docker.RemoveContainer(ctx, name) },
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	return l.commit(ctx, kind, version, true, true)
}

// Start 启动已安装容器（幂等：已运行则 Post-Verify 直接通过）
func (l *LifecycleService) Start(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	op := engine.Op{
		Name:       "start " + name,
		Execute:    func(ctx context.Context) error { return l.docker.StartContainer(ctx, name) },
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, true) },
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	return l.commit(ctx, kind, version, true, true)
}

// Stop 停止容器（幂等；保留数据，§5.13.7）
func (l *LifecycleService) Stop(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	op := engine.Op{
		Name:       "stop " + name,
		Execute:    func(ctx context.Context) error { return l.docker.StopContainer(ctx, name) },
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, false) },
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	return l.commit(ctx, kind, version, true, false)
}

// Remove 卸载：停 + 删容器，**保留数据卷/绑定目录**（§0.2 规则 20、§5.13.7），并置未安装
func (l *LifecycleService) Remove(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	op := engine.Op{
		Name:       "remove " + name,
		Execute:    func(ctx context.Context) error { return l.docker.RemoveContainer(ctx, name) },
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, false) },
		Rollback:   func(context.Context) error { return nil }, // 卸载为终结操作，无自动回滚
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	if err := l.store.SetInstalled(string(kind), version, false); err != nil {
		return err
	}
	fresh, err := l.store.BuildSnapshot()
	if err != nil {
		return err
	}
	l.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return nil
}

// ---- 内部助手 ----

// verifyRunning 从 Docker 实际态断言某容器的存在/运行判定，作 Op.PostVerify
func (l *LifecycleService) verifyRunning(ctx context.Context, kind model.ServiceKind, version string, wantRunning bool) error {
	actual, err := l.docker.ManagedContainers(ctx)
	if err != nil {
		return err
	}
	target := engine.ContainerRef{Kind: string(kind), Version: version}.Name()
	for _, a := range actual {
		if a.Ref.Name() == target {
			if a.Running != wantRunning {
				return fmt.Errorf("运行态校验失败：%s 期望 running=%v，实际 %v", target, wantRunning, a.Running)
			}
			return nil
		}
	}
	if wantRunning {
		return fmt.Errorf("校验失败：容器 %s 不存在", target)
	}
	return nil // 停止/删除后不存在即符合预期
}

// commit 落地一次运行态变更：写库 + 发 service:changed 与 state:changed
func (l *LifecycleService) commit(ctx context.Context, kind model.ServiceKind, version string, installed, running bool) error {
	if err := l.store.SetInstalled(string(kind), version, installed); err != nil {
		return err
	}
	if err := l.store.SetRunning(string(kind), version, running); err != nil {
		return err
	}
	l.emitter.Emit("service:changed", map[string]any{"kind": string(kind), "version": version, "running": running})
	fresh, err := l.store.BuildSnapshot()
	if err != nil {
		return err
	}
	l.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return nil
}

func refsOf(byKind map[string][]string) []engine.ContainerRef {
	var out []engine.ContainerRef
	for kind, versions := range byKind {
		for _, v := range versions {
			out = append(out, engine.ContainerRef{Kind: kind, Version: v})
		}
	}
	return out
}

// driftView / actualView 组织 docker:state-drift 的可读载荷
func driftView(installed, running map[string][]string) map[string]any {
	return map[string]any{"installed": installed, "running": running}
}

func actualView(actual []engine.ActualState) map[string]bool {
	out := make(map[string]bool, len(actual))
	for _, a := range actual {
		out[a.Ref.Name()] = a.Running
	}
	return out
}
