// 缓存查找（铁律 1/2）：装前必查目录 + SHA256 校验；命中零网络，损坏回退网络
package cache

import (
	"os"
	"path/filepath"

	"phpo/internal/model"
)

type ImageLookup struct {
	Hit       bool
	Path      string
	Size      int64
	Corrupted bool // 存在但 SHA256 不匹配 / 无记录
}

// LookupImage 检查离线镜像缓存；命中前必须校验 SHA256（硬约束）
func (m *Manager) LookupImage(kind, version string) (ImageLookup, error) {
	tar := m.env.OfflineImageTar(kind, version)
	if _, err := os.Stat(tar); err != nil {
		if os.IsNotExist(err) {
			return ImageLookup{}, nil
		}
		return ImageLookup{}, err
	}
	want := ""
	if mf, err := m.LoadManifest(kind, version); err == nil && mf.Image != nil {
		want = mf.Image.Sha256
	}
	got, err := FileSHA256(tar)
	if err != nil {
		return ImageLookup{}, err
	}
	if want == "" || !matches(got, want) {
		return ImageLookup{Path: tar, Corrupted: true}, nil
	}
	return ImageLookup{Hit: true, Path: tar, Size: fileSize(tar)}, nil
}

type ExtLookup struct {
	Hit       bool
	Path      string
	Size      int64
	Corrupted bool
}

// LookupExtension 检查扩展包缓存（apk/pecl）；命中前校验 SHA256
func (m *Manager) LookupExtension(phpVersion, extType, name string) (ExtLookup, error) {
	path := filepath.Join(m.env.OfflineExtDir("php", phpVersion, extType), name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ExtLookup{}, nil
		}
		return ExtLookup{}, err
	}
	var want string
	if mf, err := m.LoadManifest("php", phpVersion); err == nil {
		want = manifestExtSha(mf, extType, name)
	}
	got, err := FileSHA256(path)
	if err != nil {
		return ExtLookup{}, err
	}
	if want == "" || !matches(got, want) {
		return ExtLookup{Path: path, Corrupted: true}, nil
	}
	return ExtLookup{Hit: true, Path: path, Size: fileSize(path)}, nil
}

func manifestExtSha(mf *model.CacheManifest, extType, name string) string {
	var list []model.ManifestPackage
	switch extType {
	case "apk":
		list = mf.Apk
	case "pecl":
		list = mf.Pecl
	}
	for _, p := range list {
		if p.Name == name {
			return p.Sha256
		}
	}
	return ""
}
