// UpdateService（T604 / §4.1）：把应用升级写操作收进三段式任务（硬红线 5），
// 经 task.Manager 单飞执行并推送 task:* + update:progress/done 事件（硬红线 4：后端唯一权威）。
// 读侧 Check 供前端「检查更新」；写侧 Apply 触发下载→双校验→备份→安装，失败自动回滚。
package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/internal/updater"
)

// UpdateService 组合升级编排器与任务引擎
type UpdateService struct {
	updater *updater.Updater
	tasks   *task.Manager
	seq     atomic.Uint64
}

func NewUpdateService(u *updater.Updater, tm *task.Manager) *UpdateService {
	return &UpdateService{updater: u, tasks: tm}
}

// CurrentVersion 当前应用版本（前端展示比较基准）
func (s *UpdateService) CurrentVersion() string { return s.updater.Current() }

// Check 拉取发布清单，返回是否有新版本及可用信息
func (s *UpdateService) Check(ctx context.Context) (model.UpdateAvailable, bool, error) {
	rel, newer, err := s.updater.Check(ctx)
	if err != nil {
		return model.UpdateAvailable{}, false, err
	}
	if !newer {
		return model.UpdateAvailable{Version: s.updater.Current()}, false, nil
	}
	return model.UpdateAvailable{
		Version:      rel.Version,
		Changelog:    rel.Changelog,
		Size:         rel.Size,
		Source:       rel.Source,
		DownloadPage: rel.DownloadPage,
	}, true, nil
}

// Apply 经任务引擎执行一次升级（串行 FIFO；前面有任务则排队，同标签重复提交返回 ErrQueued）
func (s *UpdateService) Apply(ctx context.Context) error {
	t := &task.Task{
		ID:    fmt.Sprintf("update-%d", s.seq.Add(1)),
		Label: "升级应用",
		Steps: []task.Step{steps.NewUpdateStep(s.updater)},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}
