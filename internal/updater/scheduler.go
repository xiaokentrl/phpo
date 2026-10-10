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

// Start 按 interval 周期检查更新（首次在启动 interval 后触发），直到 ctx 取消。
// 不做启动即查：应用打开时不发起网络请求（v2.9.16 移除——用户不需要打开就知道有没有新版本，
// 24h 周期 + 手动「检查更新」覆盖了所有场景）。
func (s *Scheduler) Start(ctx context.Context) {
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
