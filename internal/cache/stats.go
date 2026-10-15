// 缓存条目扫描 / 校验 / 统计（§5.14.6 / §5.14.7）：遍历 offline/{kind}/{version}/
package cache

import (
	"os"
	"path/filepath"
	"time"

	"phpo/internal/model"
)

// Entry 单个 {kind}/{version} 缓存目录的概览
type Entry struct {
	Kind      string
	Version   string
	Dir       string
	Size      int64
	Corrupted bool
	UpdatedAt time.Time
}

// ListEntries 遍历缓存根目录，返回每个版本条目（含损坏标记）
func (m *Manager) ListEntries() ([]Entry, error) {
	root := m.env.OfflineRoot
	kinds, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, k := range kinds {
		if !k.IsDir() {
			continue
		}
		vers, err := os.ReadDir(filepath.Join(root, k.Name()))
		if err != nil {
			continue
		}
		for _, v := range vers {
			if !v.IsDir() {
				continue
			}
			e := Entry{Kind: k.Name(), Version: v.Name()}
			e.Dir = filepath.Join(root, k.Name(), v.Name())
			e.Size, e.UpdatedAt = dirStat(e.Dir)
			e.Corrupted = m.corrupted(e.Kind, e.Version)
			out = append(out, e)
		}
	}
	return out, nil
}

// corrupted 判定（轻量，列表/概览用）：镜像/固化镜像/扩展包任一「存在但无 manifest 记录或 size 漂移」。
// 不做全文件 SHA256——每份镜像 tar 数百 MB 起，列表每次进页对每份重哈希 = 风扇狂转（用户真机取证，
// 与备份页 TopLevel 同病）；字节级校验由显式「校验」按钮（VerifyEntry/VerifyAll）负责并发射 cache:corrupted。
func (m *Manager) corrupted(kind, version string) bool {
	if m.corruptTarLight(kind, version, m.env.OfflineImageTar(kind, version), func(mf *model.CacheManifest) *model.ManifestImage {
		return mf.Image
	}) {
		return true
	}
	if kind == "php" {
		if m.corruptTarLight("php", version, m.env.OfflineExtImageTar("php", version), func(mf *model.CacheManifest) *model.ManifestImage {
			return mf.ExtImage
		}) {
			return true
		}
		for _, et := range []string{"apk", "pecl"} {
			for _, name := range listFiles(filepath.Join(m.env.OfflineExtDir(kind, version, et))) {
				if m.corruptExtPkgLight(version, et, name) {
					return true
				}
			}
		}
	}
	return false
}

// VerifyEntry 逐文件重校验 kind/version 缓存（§5.14.5）：基座与固化镜像 tar + php 的 apk/pecl 包各按 manifest SHA256 校验。
// 与 corrupted() 的区别在于本方法会把每个损坏项发射 cache:corrupted，并返回失败文件相对名清单（emitCorrupted 由离线视图「校验」触发，需可见告警）。
func (m *Manager) VerifyEntry(kind, version string) ([]string, error) {
	var failed []string
	il, err := m.LookupImage(kind, version)
	if err != nil {
		return nil, err
	}
	if il.Corrupted {
		name := filepath.Base(il.Path)
		failed = append(failed, name)
		m.emitCorrupted(kind, version, model.ManifestPackage{Name: name})
	}
	if kind == "php" {
		el, err := m.LookupExtImage(version)
		if err != nil {
			return nil, err
		}
		if el.Corrupted {
			name := filepath.Base(el.Path)
			failed = append(failed, name)
			m.emitCorrupted(kind, version, model.ManifestPackage{Name: name})
		}
		for _, et := range []string{"apk", "pecl"} {
			for _, fn := range listFiles(filepath.Join(m.env.OfflineExtDir(kind, version, et))) {
				el, err := m.LookupExtension(version, et, fn)
				if err != nil {
					return nil, err
				}
				if el.Corrupted {
					failed = append(failed, et+"/"+fn)
					m.emitCorrupted("php", version, model.ManifestPackage{Name: fn})
				}
			}
		}
	}
	return failed, nil
}

// Stats 汇总缓存占用（OfflineView 用）
// 这里一次都不做全文件 SHA256：概览每次进页都调它，而每份镜像 tar 动辄数百 MB，逐字节重哈希就是
// 「一进页风扇狂转」（与 corrupted() 同判据；字节级校验仍由显式「校验」VerifyEntry/VerifyAll 负责）。
func (m *Manager) Stats() (model.CacheStats, error) {
	entries, err := m.ListEntries()
	if err != nil {
		return model.CacheStats{}, err
	}
	var st model.CacheStats
	st.EntryCount = len(entries)
	for _, e := range entries {
		st.TotalBytes += e.Size
		if e.Corrupted {
			st.Corrupted++
		}
		if ok, _ := m.tarLightStat(e.Kind, e.Version, m.env.OfflineImageTar(e.Kind, e.Version), func(mf *model.CacheManifest) *model.ManifestImage {
			return mf.Image
		}); ok {
			st.ImageCount++
		} else if e.Kind == "php" {
			// 只有固化镜像的条目（基座缓存被清掉/手工只导入过扩展镜像）同样占一份镜像缓存
			if ok, _ := m.tarLightStat("php", e.Version, m.env.OfflineExtImageTar("php", e.Version), func(mf *model.CacheManifest) *model.ManifestImage {
				return mf.ExtImage
			}); ok {
				st.ImageCount++
			}
		}
	}
	st.ExtCount = m.countExtPackages(entries)
	return st, nil
}

// countExtPackages 数 php 条目下的 apk/pecl 包文件。entries 由调用方给：
// 它本来就是 ListEntries 走一遍目录的结果，再走一遍等于同一批目录扫两次。
func (m *Manager) countExtPackages(entries []Entry) int {
	n := 0
	for _, e := range entries {
		if e.Kind != "php" {
			continue
		}
		for _, et := range []string{"apk", "pecl"} {
			n += len(listFiles(filepath.Join(m.env.OfflineExtDir("php", e.Version, et))))
		}
	}
	return n
}

// —— 文件系统小工具 ——

// dirStat 一次遍历同时得到「目录内文件总大小」与「最新修改时间」。
// 原来分 dirSize / dirMTime 各走一趟 WalkDir，同一批文件白扫一遍；条目多时进页就慢在磁盘遍历上。
func dirStat(path string) (size int64, latest time.Time) {
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		if !d.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, latest
}

func listFiles(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
