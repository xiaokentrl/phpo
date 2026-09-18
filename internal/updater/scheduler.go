// 升级检查调度（§5.9）：启动时 + 每 24 小时自动检查；网络不可达仅告警不影响使用
package updater

import (
	"context"
	"time"

	"phpo/internal/model"
)

// DefaultInterval 自动检查周期
const DefaultInterval = 24 * time.Hour

// Scheduler 周期性驱动 Checker
type Scheduler struct {
	checker  *Checker
	interval time.Duration
}

func NewScheduler(c *Checker, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Scheduler{checker: c, interval: interval}
}

// Start 立即检查一次，随后按周期检查，直到 ctx 取消；后台运行不阻塞调用方
func (s *Scheduler) Start(ctx context.Context) {
	s.runOnce(ctx)
	go func() {
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runOnce(ctx)
			}
		}
	}()
}

// runOnce 单次检查；失败仅记 dim 日志（不影响使用），不发 error 事件
func (s *Scheduler) runOnce(ctx context.Context) {
	if _, _, err := s.checker.Check(ctx); err != nil {
		s.checker.em.Emit("task:log", model.TaskLogEvent{
			ID: "updater", Level: model.LogDim, Text: "检查更新失败（不影响使用）: " + err.Error(),
		})
	}
}
