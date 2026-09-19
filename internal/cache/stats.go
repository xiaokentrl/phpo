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
			e.Size = dirSize(e.Dir)
			e.UpdatedAt = dirMTime(e.Dir)
			e.Corrupted = m.corrupted(e.Kind, e.Version)
			out = append(out, e)
		}
	}
	return out, nil
}

// corrupted 判定：镜像/扩展任一存在文件校验失败
func (m *Manager) corrupted(kind, version string) bool {
	if il, _ := m.LookupImage(kind, version); il.Corrupted {
		return true
	}
	if kind == "php" {
		for _, et := range []string{"apk", "pecl"} {
			for _, name := range listFiles(filepath.Join(m.env.OfflineExtDir(kind, version, et))) {
				if el, _ := m.LookupExtension(version, et, name); el.Corrupted {
					return true
				}
			}
		}
	}
	return false
}

// VerifyEntry 逐文件重校验 kind/version 缓存（§5.14.5）：镜像 tar + php 的 apk/pecl 包各按 manifest SHA256 校验。
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
		if il, _ := m.LookupImage(e.Kind, e.Version); il.Hit || il.Corrupted {
			st.ImageCount++
		}
	}
	st.ExtCount = m.countExtPackages()
	return st, nil
}

func (m *Manager) countExtPackages() int {
	n := 0
	entries, _ := m.ListEntries()
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

func dirSize(path string) int64 {
	var total int64
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func dirMTime(path string) time.Time {
	var latest time.Time
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest
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
