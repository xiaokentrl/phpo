// 升级任务步骤（T604 / 硬红线 5）：把 Updater.Apply 包进三段式任务，进度/结果由 update:* 事件直达前端
package steps

import (
	"context"

	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/updater"
)

// NewUpdateStep 生成一步式升级：Execute 内跑完整「下载 → 双校验 → 备份 → 安装」编排；
// 编排器已处理安装同步失败的即时回滚，故此处无需额外 Rollback
func NewUpdateStep(u *updater.Updater) *task.FuncStep {
	return &task.FuncStep{
		StepName: "升级应用到新版本",
		Can:      true,
		Exec: func(ctx context.Context, l task.StepLog) error {
			l.Log(string(model.LogCmd), "检查并下载升级包…")
			done, err := u.Apply(ctx)
			if err != nil {
				l.Log(string(model.LogErr), err.Error())
				return err
			}
			if done.Status == model.TaskSuccess {
				l.Log(string(model.LogOk), "升级完成，目标版本 "+done.Version)
			}
			return nil
		},
	}
}
