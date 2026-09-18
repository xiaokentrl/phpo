// 回滚：任务失败/取消时，按已完成步骤逆序调用 Rollback（§5.13.1 可恢复性）
// 用独立 context，避免被已取消的任务 ctx 影响；单个回滚错误不中断其余回滚
package task

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// rollback 逆序回滚已执行步骤，聚合回滚错误
func (m *Manager) rollback(executed []Step) {
	if len(executed) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []error
	for i := len(executed) - 1; i >= 0; i-- {
		if err := executed[i].Rollback(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		Logf(m.em, m.CurrentID(), "err", "回滚出现 "+strconv.Itoa(len(errs))+" 个错误: "+errors.Join(errs...).Error())
	}
}
