// 扩展安装编排（§5.14.3 / §1.13.2）：命中复制编译清 · 未命中下载→编译→提升→清
// compile 回调代表真实 apk add / pecl 编译动作；无论成败临时目录必清
package cache

import (
	"context"
	"path/filepath"

	"phpo/internal/model"
)

// InstallExtension 完整编排一个扩展包的取用与缓存提升
func (m *Manager) InstallExtension(ctx context.Context, phpVersion, extType, name, url string, compile func(tmpPath string) error) error {
	if m.db == nil {
		return errNoBackend
	}
	lookup, err := m.LookupExtension(phpVersion, extType, name)
	if err != nil {
		return err
	}
	if lookup.Corrupted {
		m.emitCorrupted("php", phpVersion, model.ManifestPackage{Name: name})
	}

	reason := ReasonCompileFailed
	defer func() { _ = m.ClearTempDir(ctx, "php", phpVersion, reason) }()

	typeDir, err := m.EnsureTypeDir("php", phpVersion, extType)
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(typeDir, name)

	var srcPath string
	if lookup.Hit {
		m.emitHit("php", phpVersion, "offline", lookup.Size)
		if err := copyFile(lookup.Path, tmpPath); err != nil {
			return err
		}
		srcPath = tmpPath
	} else {
		m.emitMiss("php", phpVersion, "download", "")
		if err := m.db.Download(ctx, url, tmpPath); err != nil {
			return err
		}
		srcPath = tmpPath
	}

	if err := compile(srcPath); err != nil {
		return err
	}
	reason = ReasonCompileOK

	// 命中无需再提升；未命中成功后提升进缓存（cache:promote 由 PromoteExtension 发）
	if !lookup.Hit {
		if err := m.PromoteExtension(phpVersion, extType, srcPath); err != nil {
			return err
		}
	}
	return nil
}
