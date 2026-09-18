// 任务引擎（三段式第二段：task）
// §5.13.3 三阶段：Pre-Clean → Execute → Post-Verify；失败/取消回滚已执行步骤
// 后端唯一权威：单飞（同一时刻仅一个运行中任务），完成回调 Apply=applyStateChange
package task

import (
	"context"
	"time"

	"phpo/internal/model"
)

type Task struct {
	ID       string
	Label    string
	Meta     model.TaskMeta
	Steps    []Step
	PreClean func(ctx context.Context) error // Pre-Clean：清理同名/冲突资源
	Verify   func(ctx context.Context) error // Post-Verify：结果核验
	Apply    func() error                    // applyStateChange：落地并触发 state:changed
}

// Run 同步执行一个任务，返回终态（由 Manager 提供单飞与事件发射）
func (m *Manager) Run(parent context.Context, t *Task) (model.TaskStatus, error) {
	ctx, release, err := m.acquire(t.ID)
	defer release()
	if err != nil {
		return "", err
	}

	start := time.Now()
	logger := &stepLogger{em: m.em, id: t.ID}
	Logf(m.em, t.ID, model.LogMeta, "▶ "+label(t))

	status, runErr := m.execute(ctx, t, logger)

	// Cleanup 无论成败/取消必执行（如清空临时目录）
	for _, s := range t.Steps {
		s.Cleanup()
	}

	Donef(m.em, t.ID, status, time.Since(start))
	return status, runErr
}

func (m *Manager) execute(ctx context.Context, t *Task, logger *stepLogger) (model.TaskStatus, error) {
	// Pre-Clean
	if t.PreClean != nil {
		if err := t.PreClean(ctx); err != nil {
			Logf(m.em, t.ID, model.LogErr, "预清理失败: "+err.Error())
			return model.TaskFailed, err
		}
	}

	// Execute
	completed := make([]Step, 0, len(t.Steps))
	total := len(t.Steps)
	for i, s := range t.Steps {
		if ctx.Err() != nil {
			m.rollback(completed)
			return model.TaskCancelled, ctx.Err()
		}
		Logf(m.em, t.ID, model.LogDim, "步骤 "+s.Name())
		if err := s.Execute(ctx, logger); err != nil {
			Logf(m.em, t.ID, model.LogErr, s.Name()+" 失败: "+err.Error())
			m.rollback(append(completed, s))
			return model.TaskFailed, err
		}
		completed = append(completed, s)
		Progressf(m.em, t.ID, i+1, total)
		Logf(m.em, t.ID, model.LogOk, s.Name()+" 完成")
	}

	// Post-Verify
	if t.Verify != nil {
		if err := t.Verify(ctx); err != nil {
			Logf(m.em, t.ID, model.LogErr, "核验失败: "+err.Error())
			m.rollback(completed)
			return model.TaskFailed, err
		}
	}

	// applyStateChange
	if t.Apply != nil {
		if err := t.Apply(); err != nil {
			Logf(m.em, t.ID, model.LogErr, "状态落地失败: "+err.Error())
			m.rollback(completed)
			return model.TaskFailed, err
		}
	}
	return model.TaskSuccess, nil
}

func label(t *Task) string {
	if t.Label != "" {
		return t.Label
	}
	if t.Meta.Type != "" {
		return t.Meta.Type
	}
	return t.ID
}

// stepLogger 实现 StepLog：把步骤内日志转发到 task:log
type stepLogger struct {
	em Emitter
	id string
}

func (l *stepLogger) Log(level, text string) {
	Logf(l.em, l.id, model.LogLevel(level), text)
}
