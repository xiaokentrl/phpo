// T605 · 清洁三模式 + 回收站 + 审计：孤儿资源全量扫描、按 §5.13.6 三模式删除 Docker 资源、
// 7 天回收站恢复/清空（§5.13.7），每次写操作以 JSON Lines 落审计（§5.13.10）并同步 operations 表。
// 写操作一律经 task.Manager 三段式（硬红线 5）；扫描/列表为纯读。默认保留卷——卷删除仅出现在「激进」模式，
// 涉缓存清理为独立显式动作（复用 cache 三模式）；两者的用户确认由前端 DangerConfirm 承担（§5.14.13 / 规则 20）。
package service

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
)

// StandardCacheAgeDays 缓存「标准」清理的天数阈值（§5.14.6：清理 N 天未用）
const StandardCacheAgeDays = 30

// CleanupDocker 孤儿扫描与资源删除（*engine.Client 满足）
type CleanupDocker interface {
	ScanOrphans(ctx context.Context, installed map[string]bool) (model.OrphanReport, error)
	RemoveContainer(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
	ImageRemove(ctx context.Context, ref string) error
}

// CleanupStore 快照 / 回收站登记簿 / 审计落表（*store.Store 满足）
type CleanupStore interface {
	BuildSnapshot() (*model.Snapshot, error)
	ListTrash() ([]store.TrashItem, error)
	RemoveTrashItem(id int64) error
	ExpiredTrash(now time.Time) ([]store.TrashItem, error)
	AppendOperation(model.Operation) error
	ListOperations(limit int) ([]model.Operation, error)
}

// CleanupTrash 回收站文件动作（*engine.Trash 满足）
type CleanupTrash interface {
	Restore(trashPath, origPath string) error
	Purge(trashPath string) error
}

// CleanupCache 缓存三模式清理（*cache.Manager 满足）
type CleanupCache interface {
	CleanupCache(ctx context.Context, mode model.CleanupMode, inUse map[string]bool, maxAgeDays int) (*model.CleanupResult, error)
}

// CleanupAuditor 审计写盘（*engine.Audit 满足）
type CleanupAuditor interface {
	Log(model.Operation) error
}

// CleanupService 清理门面
type CleanupService struct {
	dock  CleanupDocker
	store CleanupStore
	trash CleanupTrash
	cache CleanupCache
	audit CleanupAuditor
	em    Emitter
	tasks *task.Manager
	now   func() time.Time // 可注入时钟，供到期判定单测
	seq   atomic.Uint64
}

func NewCleanupService(dock CleanupDocker, st CleanupStore, tr CleanupTrash, cc CleanupCache, audit CleanupAuditor, em Emitter, tm *task.Manager) *CleanupService {
	return &CleanupService{dock: dock, store: st, trash: tr, cache: cc, audit: audit, em: em, tasks: tm, now: time.Now}
}

// ---- 读接口 ----

// Scan 全量孤儿扫描并广播 docker:orphan-found（§5.13.5；启动 + 24h + 手动复用同一口径）
func (s *CleanupService) Scan(ctx context.Context) (model.OrphanReport, error) {
	rep, err := s.dock.ScanOrphans(ctx, s.installedNames())
	if err != nil {
		return model.OrphanReport{}, err
	}
	s.em.Emit("docker:orphan-found", model.OrphanFound{Resources: rep.All()})
	return rep, nil
}

// ListTrash 回收站条目（含是否过期）
func (s *CleanupService) ListTrash() ([]model.TrashEntry, error) {
	items, err := s.store.ListTrash()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	out := make([]model.TrashEntry, 0, len(items))
	for _, it := range items {
		out = append(out, model.TrashEntry{
			ID: it.ID, Kind: it.Kind, OrigPath: it.OrigPath, TrashPath: it.TrashPath,
			MovedAt: it.MovedAt, ExpiresAt: it.ExpiresAt, Expired: now.After(it.ExpiresAt),
		})
	}
	return out, nil
}

// ---- 写接口（三段式 + 审计）----

// Clean 按三模式删除孤儿 Docker 资源，并顺带清空到期回收站（§5.13.6）
func (s *CleanupService) Clean(ctx context.Context, mode model.CleanupMode) (*model.CleanupReport, error) {
	rep := &model.CleanupReport{Mode: mode}
	t := &task.Task{
		ID:    s.newID("cleanup"),
		Label: "清理孤儿资源（" + string(mode) + "）",
		Meta:  model.TaskMeta{Type: "cleanup"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "扫描并删除孤儿资源", Exec: func(ctx context.Context, log task.StepLog) error {
				orphan, err := s.dock.ScanOrphans(ctx, s.installedNames())
				if err != nil {
					return err
				}
				for _, r := range planDeletions(mode, orphan) {
					err := s.removeOne(ctx, r)
					item := model.CleanedItem{Type: r.Type, Name: r.Name, OK: err == nil}
					if err != nil {
						item.Error = err.Error()
						rep.Failed++
						log.Log(string(model.LogErr), "删除失败 "+string(r.Type)+" "+r.Name+": "+err.Error())
					} else {
						rep.Removed++
						rep.FreedBytes += r.Size
						log.Log(string(model.LogOk), "已删除 "+string(r.Type)+" "+r.Name)
						s.em.Emit("docker:cleanup", map[string]any{"stage": "removed", "resource": r.Name, "action": string(r.Type)})
					}
					rep.Items = append(rep.Items, item)
				}
				return nil
			}},
			&task.FuncStep{StepName: "清空到期回收站", Exec: func(_ context.Context, log task.StepLog) error {
				n, err := s.purgeExpired(log)
				rep.TrashPurged = n
				return err
			}},
		},
		Apply: func() error {
			s.auditOp("cleanup", map[string]any{"mode": string(mode), "removed": rep.Removed, "trashPurged": rep.TrashPurged})
			s.emitState()
			return nil
		},
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return rep, err
	}
	return rep, nil
}

// CleanCache 缓存三模式清理（§5.14.6）；inUse 由当前已安装服务推导。激进涉缓存删除由前端确认后调用。
func (s *CleanupService) CleanCache(ctx context.Context, mode model.CleanupMode) (*model.CleanupResult, error) {
	inUse := s.cacheInUse()
	var res *model.CleanupResult
	t := &task.Task{
		ID:    s.newID("cache-cleanup"),
		Label: "清理离线缓存（" + string(mode) + "）",
		Meta:  model.TaskMeta{Type: "cache-cleanup"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "按模式清理缓存", Exec: func(ctx context.Context, log task.StepLog) error {
				r, err := s.cache.CleanupCache(ctx, mode, inUse, StandardCacheAgeDays)
				if err != nil {
					return err
				}
				res = r
				log.Log(string(model.LogOk), fmt.Sprintf("释放 %d 字节（%d 条目）", r.FreedBytes, r.Removed))
				return nil
			}},
		},
		Apply: func() error {
			if res == nil {
				res = &model.CleanupResult{Mode: mode}
			}
			s.auditOp("cache-cleanup", map[string]any{"mode": string(mode), "removed": res.Removed, "freedBytes": res.FreedBytes})
			return nil
		},
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return nil, err
	}
	if res == nil {
		res = &model.CleanupResult{Mode: mode}
	}
	return res, nil
}

// RestoreTrash 从回收站恢复误删条目到原位并注销登记（§5.13.7 可恢复性）
func (s *CleanupService) RestoreTrash(ctx context.Context, id int64) error {
	item, err := s.trashItem(id)
	if err != nil {
		return err
	}
	t := &task.Task{
		ID:    s.newID("trash-restore"),
		Label: "回收站恢复 " + item.OrigPath,
		Meta:  model.TaskMeta{Type: "trash-restore"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "移回原位并注销登记", Exec: func(_ context.Context, log task.StepLog) error {
				if err := s.trash.Restore(item.TrashPath, item.OrigPath); err != nil {
					return err
				}
				if err := s.store.RemoveTrashItem(id); err != nil {
					return err
				}
				log.Log(string(model.LogOk), "已恢复 "+item.OrigPath)
				return nil
			}},
		},
		Apply: func() error {
			s.auditOp("trash-restore", map[string]any{"id": id, "origPath": item.OrigPath})
			return nil
		},
	}
	_, err = s.tasks.Run(ctx, t)
	return err
}

// EmptyExpired 立即清空全部到期回收站条目（未满期不动；与 Clean 顺带清空同一实现）
func (s *CleanupService) EmptyExpired(ctx context.Context) (int, error) {
	n := 0
	t := &task.Task{
		ID:    s.newID("trash-empty"),
		Label: "清空到期回收站",
		Meta:  model.TaskMeta{Type: "trash-empty"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "永久删除到期条目", Exec: func(_ context.Context, log task.StepLog) error {
				c, err := s.purgeExpired(log)
				n = c
				return err
			}},
		},
		Apply: func() error { s.auditOp("trash-empty", map[string]any{"purged": n}); return nil },
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return n, err
	}
	return n, nil
}

// ListOperations 最近审计（UI 历史查询，走 operations 表；文件权威见 operations.log）
func (s *CleanupService) ListOperations(limit int) ([]model.Operation, error) {
	return s.store.ListOperations(limit)
}

// ---- 内部助手 ----

// removeOne 按类型删除单个资源（容器停删合一，RemoveContainer 已 force）
func (s *CleanupService) removeOne(ctx context.Context, r model.DockerResource) error {
	switch r.Type {
	case model.ResContainer:
		return s.dock.RemoveContainer(ctx, r.Name)
	case model.ResVolume:
		return s.dock.RemoveVolume(ctx, r.Name)
	case model.ResNetwork:
		return s.dock.RemoveNetwork(ctx, r.Name)
	case model.ResImage:
		return s.dock.ImageRemove(ctx, r.Name)
	default:
		return fmt.Errorf("未知资源类型: %s", r.Type)
	}
}

// purgeExpired 清空到期回收站，返回删除条数
func (s *CleanupService) purgeExpired(log task.StepLog) (int, error) {
	items, err := s.store.ExpiredTrash(s.now().UTC())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		if err := s.trash.Purge(it.TrashPath); err != nil {
			return n, err
		}
		if err := s.store.RemoveTrashItem(it.ID); err != nil {
			return n, err
		}
		n++
		log.Log(string(model.LogDim), "清空到期回收站 "+it.OrigPath)
	}
	return n, nil
}

func (s *CleanupService) trashItem(id int64) (store.TrashItem, error) {
	items, err := s.store.ListTrash()
	if err != nil {
		return store.TrashItem{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return store.TrashItem{}, fmt.Errorf("回收站条目不存在: %d", id)
}

// installedNames 由权威快照推导期望存在的 phpo 容器名集合（孤儿判定基准）
func (s *CleanupService) installedNames() map[string]bool {
	set := map[string]bool{}
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return set
	}
	for kind, vers := range snap.Installed {
		for _, v := range vers {
			set[engine.ContainerRef{Kind: kind, Version: v}.Name()] = true
		}
	}
	return set
}

// cacheInUse 已安装服务 → 缓存 {kind}/{version} 在用集（缓存清理保护在用条目，§5.14.12）
func (s *CleanupService) cacheInUse() map[string]bool {
	set := map[string]bool{}
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return set
	}
	for kind, vers := range snap.Installed {
		for _, v := range vers {
			set[kind+"/"+v] = true
		}
	}
	return set
}

// auditOp 一次写操作双写：JSON Lines 文件（权威）+ operations 表（UI 历史）
func (s *CleanupService) auditOp(op string, args any) {
	rec := model.Operation{TS: s.now().UTC(), Actor: "ui", Op: op, Args: args, Status: "success"}
	_ = s.audit.Log(rec) // 审计尽力而为：落盘失败不阻断主流程，但仍尝试双写
	_ = s.store.AppendOperation(rec)
}

func (s *CleanupService) emitState() {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		snap = model.NewSnapshot()
	}
	s.em.Emit("state:changed", map[string]any{"snapshot": snap})
}

// planDeletions 依三模式从孤儿报告裁剪删除集（§5.13.6）：
//   - 保守：仅已停止的孤儿容器
//   - 标准：+ 全部孤儿容器 + 孤儿网络 + 孤儿镜像（保留卷，§5.13.5）
//   - 激进：+ 孤儿卷（数据卷删除，前端二次确认后执行）
func planDeletions(mode model.CleanupMode, rep model.OrphanReport) []model.DockerResource {
	var out []model.DockerResource
	for _, c := range rep.Containers {
		if mode == model.CleanupConservative && c.InUse {
			continue
		}
		out = append(out, c)
	}
	if mode != model.CleanupConservative {
		out = append(out, rep.Networks...)
		out = append(out, rep.Images...)
	}
	if mode == model.CleanupAggressive {
		out = append(out, rep.Volumes...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *CleanupService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}
