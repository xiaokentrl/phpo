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
	return m.lookupTar(kind, version, m.env.OfflineImageTar(kind, version), func(mf *model.CacheManifest) *model.ManifestImage {
		return mf.Image
	})
}

// LookupExtImage 检查扩展固化镜像（phpo/php:{version}）的缓存槽位；与基座镜像分文件、分记录
func (m *Manager) LookupExtImage(version string) (ImageLookup, error) {
	return m.lookupTar("php", version, m.env.OfflineExtImageTar("php", version), func(mf *model.CacheManifest) *model.ManifestImage {
		return mf.ExtImage
	})
}

// lookupTar 校验一份镜像 tar 与其清单记录的 SHA256：无记录即不匹配（防误命中零网络 load 到错镜像）
func (m *Manager) lookupTar(kind, version, tar string, rec func(*model.CacheManifest) *model.ManifestImage) (ImageLookup, error) {
	if _, err := os.Stat(tar); err != nil {
		if os.IsNotExist(err) {
			return ImageLookup{}, nil
		}
		return ImageLookup{}, err
	}
	want := ""
	if mf, err := m.LoadManifest(kind, version); err == nil {
		if r := rec(mf); r != nil {
			want = r.Sha256
		}
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

// CachedImageRef 返回基座镜像缓存 tar 所对应的镜像引用（manifest.image.name）；未命中或无记录返回 ("", false)。
// 固化镜像不在此列——它有独立槽位，读 manifest.extensions_image（见 LookupExtImage）。
func (m *Manager) CachedImageRef(kind, version string) (string, bool) {
	lk, err := m.LookupImage(kind, version)
	if err != nil || !lk.Hit {
		return "", false
	}
	mf, err := m.LoadManifest(kind, version)
	if err != nil || mf == nil || mf.Image == nil {
		return "", false
	}
	return mf.Image.Name, true
}
