// 缓存提升（§5.14.4 提升时机：编译/加载成功后立即）：mv 临时→缓存 + 更新 manifest
package cache

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"phpo/internal/model"
)

// PromoteImage 把临时镜像 tar 提升到离线缓存并登记 manifest.image
func (m *Manager) PromoteImage(kind, version, ref, tmpTar string) error {
	dst := m.env.OfflineImageTar(kind, version)
	sha, err := FileSHA256(tmpTar)
	if err != nil {
		return err
	}
	if err := moveFile(tmpTar, dst); err != nil {
		return err
	}
	mf, err := m.LoadManifest(kind, version)
	if err != nil {
		return err
	}
	mf.Image = &model.ManifestImage{
		Name:     ref,
		Size:     fileSize(dst),
		Sha256:   sha,
		CachedAt: time.Now().UTC(),
	}
	return m.SaveManifest(mf)
}

// PromoteExtension 把临时扩展包提升到离线缓存并登记 manifest.apk/pecl
func (m *Manager) PromoteExtension(phpVersion, extType, tmpFile string) error {
	name := filepath.Base(tmpFile)
	dstDir := m.env.OfflineExtDir("php", phpVersion, extType)
	dst := filepath.Join(dstDir, name)
	sha, err := FileSHA256(tmpFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if err := moveFile(tmpFile, dst); err != nil {
		return err
	}
	mf, err := m.LoadManifest("php", phpVersion)
	if err != nil {
		return err
	}
	entry := model.ManifestPackage{Name: name, Sha256: sha, Size: fileSize(dst), CachedAt: time.Now().UTC()}
	upsertPackage(&mf.Apk, &mf.Pecl, extType, entry)
	return m.SaveManifest(mf)
}

func upsertPackage(apk, pecl *[]model.ManifestPackage, extType string, e model.ManifestPackage) {
	target := apk
	if extType == "pecl" {
		target = pecl
	}
	for i := range *target {
		if (*target)[i].Name == e.Name {
			(*target)[i] = e
			return
		}
	}
	*target = append(*target, e)
}

// moveFile 优先 rename，跨设备回退 copy+remove
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
