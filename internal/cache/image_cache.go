// 镜像安装编排（§5.14.3）：查→命中 load 零网络 / 未命中 pull→save→promote→清临时
package cache

import (
	"context"
	"path/filepath"

	"phpo/internal/model"
)

// EnsureImage 保证 kind/version 对应镜像已在本地 Docker store，全程遵守三条铁律
func (m *Manager) EnsureImage(ctx context.Context, kind, version, ref string) error {
	if m.db == nil {
		return errNoBackend
	}
	lookup, err := m.LookupImage(kind, version)
	if err != nil {
		return err
	}
	if lookup.Corrupted {
		m.emitCorrupted(kind, version, model.ManifestPackage{Name: filepath.Base(lookup.Path)})
	}
	if lookup.Hit {
		if err := m.db.LoadImage(ctx, lookup.Path); err != nil {
			return err
		}
		m.emitHit(kind, version, "offline", lookup.Size)
		return nil
	}

	// 未命中（含损坏回退）：走网络下载 + 提升 + 必清临时目录
	m.emitMiss(kind, version, "pull")
	reason := ReasonCompileFailed
	// 取消优先于失败：ctx 被取消时按必清时机 3（cancelled）上报
	defer func() {
		r := reason
		if ctx.Err() != nil {
			r = ReasonCancelled
		}
		_ = m.ClearTempDir(ctx, kind, version, r)
	}()

	tmpDir, err := m.EnsureTempDir(kind, version)
	if err != nil {
		return err
	}
	tmpTar := filepath.Join(tmpDir, "image.tar")

	if err := m.db.PullImage(ctx, ref); err != nil {
		return err
	}
	if err := m.db.SaveImage(ctx, ref, tmpTar); err != nil {
		return err
	}
	if err := m.PromoteImage(kind, version, ref, tmpTar); err != nil {
		return err
	}
	reason = ReasonCompileOK
	m.emitPromote(kind, version, nil)
	return nil
}
