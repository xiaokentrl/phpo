// 缓存清理三模式（§5.14.6）：保守=仅损坏 / 标准=+N天未用 / 激进=全部（除在用）
// 静默清理被禁止：调用方须先弹确认；本层只执行并上报释放字节
package cache

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"phpo/internal/model"
)

// CleanupCache 按模式清理缓存目录，返回释放量；inUse 为不可删的 kind/version 键集合
func (m *Manager) CleanupCache(ctx context.Context, mode model.CleanupMode, inUse map[string]bool, maxAgeDays int) (*model.CleanupResult, error) {
	entries, err := m.ListEntries()
	if err != nil {
		return nil, err
	}
	res := &model.CleanupResult{Mode: mode}
	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)

	for _, e := range entries {
		if !shouldRemove(e, mode, cutoff, inUse) {
			continue
		}
		size := e.Size
		if err := os.RemoveAll(e.Dir); err != nil {
			return res, err
		}
		res.FreedBytes += size
		res.Removed++
	}
	m.emitCleanup(string(mode), res.FreedBytes)
	return res, nil
}

func shouldRemove(e Entry, mode model.CleanupMode, cutoff time.Time, inUse map[string]bool) bool {
	key := e.Kind + "/" + e.Version
	switch mode {
	case model.CleanupConservative:
		return e.Corrupted
	case model.CleanupStandard:
		return e.Corrupted || (!e.UpdatedAt.IsZero() && e.UpdatedAt.Before(cutoff))
	case model.CleanupAggressive:
		return !inUse[key]
	default:
		return e.Corrupted
	}
}

// RemoveEntry 删除单个 {kind}/{version} 缓存目录
func (m *Manager) RemoveEntry(kind, version string) error {
	return os.RemoveAll(filepath.Join(m.env.OfflineRoot, kind, version))
}
