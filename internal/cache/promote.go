// 缓存提升（§5.14.4 提升时机：编译/加载成功后立即）：mv 临时→缓存 + 更新 manifest
// 手工导入（需求 1）走同一登记路径，差别只在源文件处置：提升搬走、导入复制保留。
package cache

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"phpo/internal/model"
)

// PromoteImage 把临时镜像 tar 提升到离线缓存并登记 manifest.image（源即临时目录，提升后搬走）
func (m *Manager) PromoteImage(kind, version, ref, tmpTar string) error {
	return m.commitImage(kind, version, ref, tmpTar, false)
}

// ImportImage 把手工选定的镜像 tar 登记进离线缓存；源文件是用户资产，只复制不搬走
func (m *Manager) ImportImage(kind, version, ref, srcTar string) error {
	if err := m.commitImage(kind, version, ref, srcTar, true); err != nil {
		return err
	}
	m.emitPromote(kind, version, nil)
	return nil
}

func (m *Manager) commitImage(kind, version, ref, src string, keepSrc bool) error {
	dst := m.env.OfflineImageTar(kind, version)
	sha, err := FileSHA256(src)
	if err != nil {
		return err
	}
	if err := placeFile(src, dst, keepSrc); err != nil {
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
	return m.commitExtension(phpVersion, extType, tmpFile, false)
}

// ImportExtension 把手工选定的扩展包（apk/pecl）复制进离线缓存并登记 manifest
func (m *Manager) ImportExtension(phpVersion, extType, srcFile string) error {
	if err := m.commitExtension(phpVersion, extType, srcFile, true); err != nil {
		return err
	}
	m.emitPromote("php", phpVersion, nil)
	return nil
}

func (m *Manager) commitExtension(phpVersion, extType, src string, keepSrc bool) error {
	name := filepath.Base(src)
	dstDir := m.env.OfflineExtDir("php", phpVersion, extType)
	dst := filepath.Join(dstDir, name)
	sha, err := FileSHA256(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if err := placeFile(src, dst, keepSrc); err != nil {
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

// placeFile 把源文件落到缓存目标位：keepSrc=true 时只复制（手工导入保留用户原件），否则提升到搬走（临时目录必清）
func placeFile(src, dst string, keepSrc bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if keepSrc {
		return copyFile(src, dst)
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
