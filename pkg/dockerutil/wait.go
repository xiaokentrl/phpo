// 通用就绪轮询：容器/服务健康检查用，按间隔重试直到条件满足或超时
package dockerutil

import (
	"context"
	"time"
)

// Wait 轮询 cond 直到返回 true 或超时/取消。cond 返回 error 时立即中止。
func Wait(ctx context.Context, timeout, interval time.Duration, cond func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
