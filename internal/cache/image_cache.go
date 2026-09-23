// 镜像安装编排（§5.14.3）：查→命中 load 零网络 / 未命中先探本地镜像（已有即免拉取）→save→promote→清临时
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

	// 未命中（含损坏回退）：先探本地 Docker store——已有镜像即就地 save 提升，全程不碰网络
	// （断网/内网机器缓存丢失后重建的唯一路径）；确实没有才拉取。走网络时仍遵守必清临时目录。
	has, err := m.db.ImageExists(ctx, ref)
	if err != nil {
		return err
	}
	action := "local"
	if !has {
		action = "pull"
	}
	m.emitMiss(kind, version, action)
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

	if !has {
		if err := m.db.PullImage(ctx, ref); err != nil {
			return err
		}
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

// LoadExtImage 把缓存里的扩展固化镜像零网络载入本机 Docker，返回它的 ref（§5.14.3 第一优先级）。
// 缓存缺席返回 ("", false, nil)——由调用方回落基座；损坏先告警再按缺席处理（校验失败不得当作命中）。
func (m *Manager) LoadExtImage(ctx context.Context, version string) (string, bool, error) {
	if m.db == nil {
		return "", false, errNoBackend
	}
	lk, err := m.LookupExtImage(version)
	if err != nil {
		return "", false, err
	}
	if lk.Corrupted {
		m.emitCorrupted("php", version, model.ManifestPackage{Name: filepath.Base(lk.Path)})
	}
	if !lk.Hit {
		return "", false, nil
	}
	mf, err := m.LoadManifest("php", version)
	if err != nil {
		return "", false, err
	}
	if mf == nil || mf.ExtImage == nil || mf.ExtImage.Name == "" {
		return "", false, nil
	}
	if err := m.db.LoadImage(ctx, lk.Path); err != nil {
		return "", false, err
	}
	m.emitHit("php", version, "offline", lk.Size)
	return mf.ExtImage.Name, true, nil
}
