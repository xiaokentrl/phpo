// T606 · OfflineView 真数据化：§5.14.10 全部缓存服务层 API 的门面。
// 组合 *cache.Manager（查找/校验/统计/提升/清理/临时目录）+ store（在用集推导）+ task/审计（缓存写操作）。
// 缓存与 Docker/权威快照完全解耦（§5.14.13），读侧纯查询；写侧（三模式清理/单条删除）经三段式任务并落 JSON Lines 审计（§5.13.10）。
// 提升/清临时目录作为安装管道的组成能力对外暴露，直连 cache（其内部已发 cache:* 事件），不重复包任务。
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"phpo/internal/cache"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
)

// OfflineCache 缓存编排入口（*cache.Manager 满足）
type OfflineCache interface {
	ListEntries() ([]cache.Entry, error)
	Stats() (model.CacheStats, error)
	LookupImage(kind, version string) (cache.ImageLookup, error)
	LookupExtension(phpVersion, extType, name string) (cache.ExtLookup, error)
	VerifyEntry(kind, version string) ([]string, error)
	CleanupCache(ctx context.Context, mode model.CleanupMode, inUse map[string]bool, maxAgeDays int) (*model.CleanupResult, error)
	RemoveEntry(kind, version string) error
	PromoteImage(kind, version, ref, tmpTar string) error
	PromoteExtension(phpVersion, extType, tmpFile string) error
	ClearTempDir(ctx context.Context, kind, version, reason string) error
	LoadManifest(kind, version string) (*model.CacheManifest, error)
}

// OfflineStore 权威快照（推导在用缓存集）+ 审计落表（*store.Store 满足）
type OfflineStore interface {
	BuildSnapshot() (*model.Snapshot, error)
	AppendOperation(model.Operation) error
}

// OfflineAuditor 审计写盘（*engine.Audit 满足）
type OfflineAuditor interface {
	Log(model.Operation) error
}

// OfflineService 离线缓存门面
type OfflineService struct {
	cache OfflineCache
	store OfflineStore
	audit OfflineAuditor
	em    Emitter
	tasks *task.Manager
	now   func() time.Time
	seq   atomic.Uint64
}

func NewOfflineService(cc OfflineCache, st OfflineStore, audit OfflineAuditor, em Emitter, tm *task.Manager) *OfflineService {
	return &OfflineService{cache: cc, store: st, audit: audit, em: em, tasks: tm, now: time.Now}
}

// ---- 读接口（§5.14.7 / §5.14.10）----

// ListCacheEntries 遍历缓存根目录，逐条聚合 manifest 的镜像/apk/pecl 计数与校验态
func (s *OfflineService) ListCacheEntries(ctx context.Context) ([]model.CacheEntry, error) {
	entries, err := s.cache.ListEntries()
	if err != nil {
		return nil, err
	}
	out := make([]model.CacheEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, s.buildEntry(e))
	}
	return out, nil
}

// GetCacheEntry 读取单个 kind/version 缓存条目；目录不存在返回 nil
func (s *OfflineService) GetCacheEntry(ctx context.Context, kind, version string) (*model.CacheEntry, error) {
	entries, err := s.cache.ListEntries()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Kind == kind && e.Version == version {
			ce := s.buildEntry(e)
			return &ce, nil
		}
	}
	return nil, nil
}

// GetCacheStats 缓存根目录总览（总占用 / 条目数 / 镜像数 / 扩展数 / 损坏数）
func (s *OfflineService) GetCacheStats(ctx context.Context) (model.CacheStats, error) {
	return s.cache.Stats()
}

// LookupImage 查镜像离线缓存命中情况（命中前已校验 SHA256）
func (s *OfflineService) LookupImage(ctx context.Context, kind, version string) (model.ImageCacheResult, error) {
	lk, err := s.cache.LookupImage(kind, version)
	if err != nil {
		return model.ImageCacheResult{}, err
	}
	return model.ImageCacheResult{Hit: lk.Hit, Path: lk.Path, Size: lk.Size, Corrupted: lk.Corrupted}, nil
}

// LookupExtension 查扩展包离线缓存命中情况
func (s *OfflineService) LookupExtension(ctx context.Context, phpVersion, extType, name string) (model.ExtCacheResult, error) {
	lk, err := s.cache.LookupExtension(phpVersion, extType, name)
	if err != nil {
		return model.ExtCacheResult{}, err
	}
	return model.ExtCacheResult{Hit: lk.Hit, Path: lk.Path, Size: lk.Size, Corrupted: lk.Corrupted}, nil
}

// ---- 校验接口（§5.14.5）----

// VerifyCacheEntry 逐文件重校验单条缓存，损坏项发射 cache:corrupted
func (s *OfflineService) VerifyCacheEntry(ctx context.Context, kind, version string) (model.VerifyResult, error) {
	failed, err := s.cache.VerifyEntry(kind, version)
	if err != nil {
		return model.VerifyResult{}, err
	}
	return model.VerifyResult{Kind: kind, Version: version, OK: len(failed) == 0, Failed: failed}, nil
}

// VerifyAllCache 全量校验所有缓存条目，汇总通过/失败并逐条保留明细
func (s *OfflineService) VerifyAllCache(ctx context.Context) (model.VerifyAllResult, error) {
	entries, err := s.cache.ListEntries()
	if err != nil {
		return model.VerifyAllResult{}, err
	}
	res := model.VerifyAllResult{Total: len(entries)}
	for _, e := range entries {
		failed, err := s.cache.VerifyEntry(e.Kind, e.Version)
		if err != nil {
			return res, err
		}
		res.Entries = append(res.Entries, model.VerifyResult{Kind: e.Kind, Version: e.Version, OK: len(failed) == 0, Failed: failed})
		if len(failed) == 0 {
			res.OK++
		} else {
			res.Failed++
		}
	}
	return res, nil
}

// ---- 写接口（三模式清理 / 单条删除：三段式 + 审计）----

// CleanupCache 按三模式清理缓存（§5.14.6）；inUse 由当前已安装服务推导，涉缓存删除的确认由前端 DangerConfirm 承担
func (s *OfflineService) CleanupCache(ctx context.Context, mode model.CleanupMode) (model.CleanupResult, error) {
	inUse := s.cacheInUse()
	res := &model.CleanupResult{Mode: mode}
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
			s.auditOp("cache-cleanup", map[string]any{"mode": string(mode), "removed": res.Removed, "freedBytes": res.FreedBytes})
			return nil
		},
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return *res, err
	}
	return *res, nil
}

// RemoveCacheEntry 删除单条 {kind}/{version} 缓存目录（不可恢复，前端确认后调用）
func (s *OfflineService) RemoveCacheEntry(ctx context.Context, kind, version string) error {
	t := &task.Task{
		ID:    s.newID("cache-remove"),
		Label: "删除缓存 " + kind + "/" + version,
		Meta:  model.TaskMeta{Type: "cache-remove"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "删除缓存目录", Exec: func(_ context.Context, log task.StepLog) error {
				if err := s.cache.RemoveEntry(kind, version); err != nil {
					return err
				}
				log.Log(string(model.LogOk), "已删除缓存 "+kind+"/"+version)
				return nil
			}},
		},
		Apply: func() error {
			s.auditOp("cache-remove", map[string]any{"kind": kind, "version": version})
			return nil
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// ---- 提升 / 临时目录（安装管道组成能力，直连 cache；其内部已发 cache:* 事件）----

// PromoteImage 把临时镜像 tar 提升到离线缓存；镜像引用按 kind/version 推导（§5.14.10 签名无 ref）
func (s *OfflineService) PromoteImage(ctx context.Context, kind, version, tarPath string) error {
	ref, err := engine.ImageRefFor(kind, version)
	if err != nil {
		return err
	}
	return s.cache.PromoteImage(kind, version, ref, tarPath)
}

// PromoteExtension 把临时扩展包提升到离线缓存
func (s *OfflineService) PromoteExtension(ctx context.Context, phpVersion, extType, filePath string) error {
	return s.cache.PromoteExtension(phpVersion, extType, filePath)
}

// ClearTempDir 清空指定 kind/version 的临时目录并发射 cache:tempdir-cleared
func (s *OfflineService) ClearTempDir(ctx context.Context, kind, version, reason string) error {
	return s.cache.ClearTempDir(ctx, kind, version, reason)
}

// ---- 内部助手 ----

// buildEntry 用 manifest 聚合镜像存在性与 apk/pecl 计数；VerifyOK 取自条目损坏标记，LastVerify 取缓存目录最近变更时间
func (s *OfflineService) buildEntry(e cache.Entry) model.CacheEntry {
	ce := model.CacheEntry{
		Kind:       e.Kind,
		Version:    e.Version,
		TotalSize:  e.Size,
		VerifyOK:   !e.Corrupted,
		LastVerify: e.UpdatedAt,
		Path:       filepath.ToSlash(e.Dir),
	}
	if mf, err := s.cache.LoadManifest(e.Kind, e.Version); err == nil && mf != nil {
		ce.HasImage = mf.Image != nil
		ce.ApkCount = len(mf.Apk)
		ce.PeclCount = len(mf.Pecl)
	}
	return ce
}

// cacheInUse 已安装服务 → 缓存 {kind}/{version} 在用集（清理保护在用条目，§5.14.12）
func (s *OfflineService) cacheInUse() map[string]bool {
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

// auditOp 一次缓存写操作双写：JSON Lines 文件（权威）+ operations 表（UI 历史）；尽力而为不阻断主流程
func (s *OfflineService) auditOp(op string, args any) {
	rec := model.Operation{TS: s.now().UTC(), Actor: "ui", Op: op, Args: args, Status: "success"}
	_ = s.audit.Log(rec)
	_ = s.store.AppendOperation(rec)
}

func (s *OfflineService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}
