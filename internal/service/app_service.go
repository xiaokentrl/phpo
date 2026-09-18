// AppService：M3 集成验收的服务门面——把 LifecycleService 的原子操作编排进 task.Manager，
// 使所有写操作走三段式（硬红线 5）并经事件总线推送 task:* / cache:* / state:changed（硬红线 4：后端唯一权威）。
// Install 前置缓存优先镜像步（T303）以兑现离线铁律与硬红线 7 门禁；每次任务后按 §5.13.9 触发校准。
package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/pkg/dockerutil"
)

// AppService 组合注入的编排入口；各写方法产出一个 task.Task 经 task.Manager 单飞执行
type AppService struct {
	lifecycle *LifecycleService
	tasks     *task.Manager
	cache     steps.ImageEnsurer // 缓存优先保证镜像就绪（*cache.Manager 满足）
	probe     engine.Probe       // Docker 可用性门禁（*engine.Client 满足）
	env       config.Env
	seq       atomic.Uint64 // 任务 ID 计数
}

func NewAppService(lc *LifecycleService, tm *task.Manager, cache steps.ImageEnsurer, probe engine.Probe, env config.Env) *AppService {
	return &AppService{lifecycle: lc, tasks: tm, cache: cache, probe: probe, env: env}
}

// ---- 读接口 ----

// GetState 前端订阅之外的一次性权威拉取
func (s *AppService) GetState() (*model.Snapshot, error) { return s.lifecycle.Snapshot() }

// Running 是否有任务在执行（前端 preflight 兜底）
func (s *AppService) Running() bool { return s.tasks.Running() }

// Cancel 取消当前运行中任务（T308 语义：中断可取消步骤并回滚 + 清临时目录）
func (s *AppService) Cancel() { s.tasks.Cancel() }

// Calibrate 手动校准（§5.13.9 三处触发之一：启动 / 每任务后 / 手动）
func (s *AppService) Calibrate(ctx context.Context) error {
	_, err := s.lifecycle.Calibrate(ctx)
	return err
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
			&task.FuncStep{StepName: "落盘工作目录与配置", Exec: func(context.Context, task.StepLog) error {
				return prepareService(s.env, kind, version)
			}},
			&task.FuncStep{StepName: "创建并启动 " + name, Exec: func(ctx context.Context, _ task.StepLog) error {
				return s.lifecycle.Install(ctx, kind, version)
			}},
		},
	}
	return s.run(ctx, t)
}

// Start 启动已安装容器
func (s *AppService) Start(ctx context.Context, kind model.ServiceKind, version string) error {
	return s.one("start", "启动 "+dockerutil.ContainerName(string(kind), version),
		func(ctx context.Context) error { return s.lifecycle.Start(ctx, kind, version) })
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

// one 单步任务的便捷构造：把一次 lifecycle 原子操作包成一步，仍经 task.Manager 发事件
func (s *AppService) one(op, label string, fn func(ctx context.Context) error) error {
	t := &task.Task{
		ID:    s.newID(op),
		Label: label,
		Steps: []task.Step{
			&task.FuncStep{StepName: label, Exec: func(ctx context.Context, _ task.StepLog) error { return fn(ctx) }},
		},
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
