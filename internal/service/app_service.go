// AppService：M3 集成验收的服务门面——把 LifecycleService 的原子操作编排进 task.Manager，
// 使所有写操作走三段式（硬红线 5）并经事件总线推送 task:* / cache:* / state:changed（硬红线 4：后端唯一权威）。
// Install 前置缓存优先镜像步（T303）以兑现离线铁律与硬红线 7 门禁；每次任务后按 §5.13.9 触发校准。
package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/pkg/dockerutil"
)

// dockerProbeTimeout 单次 Docker 探测超时：防 Ping 挂起阻塞首启/轮询
const dockerProbeTimeout = 3 * time.Second

// SiteHealer nginx 就绪后补齐降级站点（真实实现：*SiteService.ReconcileServe）。
// 建站已不设 nginx 门禁，降级站点必须由「装好/启动 nginx」这一事件驱动自愈。
type SiteHealer interface {
	ReconcileServe(ctx context.Context) error
}

// AppService 组合注入的编排入口；各写方法产出一个 task.Task 经 task.Manager 单飞执行
type AppService struct {
	lifecycle *LifecycleService
	tasks     *task.Manager
	cache     steps.ImageEnsurer // 缓存优先保证镜像就绪（*cache.Manager 满足）
	probe     engine.Probe       // Docker 可用性门禁（*engine.Client 满足）
	healer    SiteHealer         // nginx 安装/启动后补齐站点 vhost（未注入则跳过）
	env       config.Env
	seq       atomic.Uint64 // 任务 ID 计数
}

func NewAppService(lc *LifecycleService, tm *task.Manager, cache steps.ImageEnsurer, probe engine.Probe, env config.Env) *AppService {
	return &AppService{lifecycle: lc, tasks: tm, cache: cache, probe: probe, env: env}
}

// SetSiteHealer 注入站点补齐器（di 装配期调用）
func (s *AppService) SetSiteHealer(h SiteHealer) { s.healer = h }

// healStep nginx 就绪后的补齐步骤；非 nginx 或未注入返回 nil（调用方跳过）。
// 补齐失败只记日志不判任务失败：nginx 已装好，站点仍可在下次写操作自愈。
func (s *AppService) healStep(kind model.ServiceKind) task.Step {
	if kind != model.KindNginx || s.healer == nil {
		return nil
	}
	return &task.FuncStep{StepName: "补齐站点 vhost", Exec: func(ctx context.Context, log task.StepLog) error {
		if err := s.healer.ReconcileServe(ctx); err != nil {
			log.Log(string(model.LogErr), err.Error())
		}
		return nil
	}}
}

// ---- 读接口 ----

// GetState 前端订阅之外的一次性权威拉取
func (s *AppService) GetState() (*model.Snapshot, error) { return s.lifecycle.Snapshot() }

// Running 是否有任务在执行（前端 preflight 兜底）
func (s *AppService) Running() bool { return s.tasks.Running() }

// Cancel 取消当前运行中任务（T308 语义：中断可取消步骤并回滚 + 清临时目录）
func (s *AppService) Cancel() { s.tasks.Cancel() }

// CancelQueued 撤回排队中的任务（尚未执行，无需回滚）；返回是否命中
func (s *AppService) CancelQueued(id string) bool { return s.tasks.CancelQueued(id) }

// Calibrate 手动校准（§5.13.9 三处触发之一：启动 / 每任务后 / 手动）
func (s *AppService) Calibrate(ctx context.Context) error {
	_, err := s.lifecycle.Calibrate(ctx)
	return err
}

// DockerStatus 探测 Docker 可用性（只读，供首启/轮询门禁；硬红线 7 的判定源，不改 Snapshot 不发事件）
func (s *AppService) DockerStatus(ctx context.Context) model.DockerStatus {
	cctx, cancel := context.WithTimeout(ctx, dockerProbeTimeout)
	defer cancel()
	h := engine.Check(cctx, s.probe)
	return model.DockerStatus{
		Status:   string(h.Status),
		Version:  h.Version,
		CanStart: h.CanStart,
		Warning:  h.Warning,
		Message:  h.Message,
		Hint:     h.Hint,
	}
}

// ---- 写接口（一律经 task.Manager 三段式）----

// Install 缓存优先镜像就绪 → 容器创建并启动（lifecycle 内部幂等三段式）→ 任务后校准
func (s *AppService) Install(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	t := &task.Task{
		ID:    s.newID("install"),
		Label: "安装 " + name,
		Steps: []task.Step{
			steps.NewInstallImageStep("准备镜像", s.cache, s.probe, s.env, string(kind), version),
			&task.FuncStep{StepName: "落盘工作目录与配置", Exec: func(_ context.Context, log task.StepLog) error {
				return prepareService(s.env, kind, version, log)
			}},
			&task.FuncStep{StepName: "创建并启动 " + name, Exec: func(ctx context.Context, log task.StepLog) error {
				if err := s.lifecycle.Install(ctx, kind, version); err != nil {
					return err
				}
				// 校验通过才报成功：lifecycle 的 Post-Verify 已按 Docker 实际态确认在运行
				log.Log(string(model.LogDim), name+" 运行状态校验通过")
				return nil
			}},
		},
	}
	if st := s.healStep(kind); st != nil {
		t.Steps = append(t.Steps, st)
	}
	return s.run(ctx, t)
}

// Start 启动已安装容器
func (s *AppService) Start(ctx context.Context, kind model.ServiceKind, version string) error {
	return s.one("start", "启动 "+dockerutil.ContainerName(string(kind), version),
		func(ctx context.Context) error { return s.lifecycle.Start(ctx, kind, version) }, s.healStep(kind))
}

// Stop 停止容器（保留数据，§5.13.7）
func (s *AppService) Stop(ctx context.Context, kind model.ServiceKind, version string) error {
	return s.one("stop", "停止 "+dockerutil.ContainerName(string(kind), version),
		func(ctx context.Context) error { return s.lifecycle.Stop(ctx, kind, version) })
}

// Remove 卸载容器（保留数据卷）
func (s *AppService) Remove(ctx context.Context, kind model.ServiceKind, version string) error {
	return s.one("remove", "卸载 "+dockerutil.ContainerName(string(kind), version),
		func(ctx context.Context) error { return s.lifecycle.Remove(ctx, kind, version) })
}

// ---- 内部助手 ----

// one 单步任务的便捷构造：把一次 lifecycle 原子操作包成一步，仍经 task.Manager 发事件；extra 追加可选步骤（nil 跳过）
func (s *AppService) one(op, label string, fn func(ctx context.Context) error, extra ...task.Step) error {
	t := &task.Task{
		ID:    s.newID(op),
		Label: label,
		Steps: []task.Step{
			&task.FuncStep{StepName: label, Exec: func(ctx context.Context, _ task.StepLog) error { return fn(ctx) }},
		},
	}
	for _, st := range extra {
		if st != nil {
			t.Steps = append(t.Steps, st)
		}
	}
	return s.run(context.Background(), t)
}

// run 执行任务；失败直接上抛，成功后按 §5.13.9 触发一次校准（漂移时补发 state-drift/changed）
func (s *AppService) run(ctx context.Context, t *task.Task) error {
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return err
	}
	_, cerr := s.lifecycle.Calibrate(context.Background())
	return cerr
}

func (s *AppService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}
