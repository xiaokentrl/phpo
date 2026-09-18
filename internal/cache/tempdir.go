// 临时目录生命周期（§5.14.4）：路径 ~/phpo/{kind}/{version}/ext/，四类时机必清
// 命中即清，防止失败产物/下载残留污染下次使用
package cache

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"phpo/internal/model"
)

// 四类必清原因（对齐 §5.14.4）
const (
	ReasonCompileOK     = "compile_ok"     // 编译/加载成功后
	ReasonCompileFailed = "compile_failed" // 编译/拉取失败后
	ReasonCancelled     = "cancelled"      // 任务取消后
	ReasonStartupScan   = "startup_scan"   // 应用启动扫描残留
)

// EnsureTempDir 建好临时目录（下载前调用）
func (m *Manager) EnsureTempDir(kind, version string) (string, error) {
	d := m.env.TempExtDir(kind, version)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// EnsureTypeDir 建好临时目录内类型分区 ext/{apk|pecl}
func (m *Manager) EnsureTypeDir(kind, version, extType string) (string, error) {
	d := m.env.TempExtTypeDir(kind, version, extType)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// ClearTempDir 删除临时目录并发射 cache:tempdir-cleared；不存在视为已清空
func (m *Manager) ClearTempDir(ctx context.Context, kind, version, reason string) error {
	d := m.env.TempExtDir(kind, version)
	return m.clearDir(d, reason)
}

func (m *Manager) clearDir(path, reason string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		m.em.Emit("cache:tempdir-cleared", model.CacheTempdirClearedEvent{Path: path, Reason: reason})
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	m.em.Emit("cache:tempdir-cleared", model.CacheTempdirClearedEvent{Path: path, Reason: reason})
	return nil
}

// ScanAndClearResidue 启动时扫描所有 {kind}/{version}/ext 残留并清空（§5.14.4 第 4 时机）
// 遍历 PHPO_HOME 下 php/nginx/mysql/pgsql/redis 各版本目录
func (m *Manager) ScanAndClearResidue(ctx context.Context) error {
	for _, kind := range []string{"php", "nginx", "mysql", "pgsql", "redis"} {
		root := m.env.Get(strings.ToUpper(kind) + "_ROOT")
		versions, err := os.ReadDir(root)
		if err != nil {
			continue // 目录尚未创建，无残留
		}
		for _, v := range versions {
			if !v.IsDir() {
				continue
			}
			ext := filepath.Join(root, v.Name(), "ext")
			if st, err := os.Stat(ext); err == nil && st.IsDir() {
				if err := m.clearDir(ext, ReasonStartupScan); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ScanResidue 只读扫描残留临时目录（不清空），供 doctor「临时目录残留」项计数
func (m *Manager) ScanResidue() ([]string, error) {
	var out []string
	for _, kind := range []string{"php", "nginx", "mysql", "pgsql", "redis"} {
		root := m.env.Get(strings.ToUpper(kind) + "_ROOT")
		versions, err := os.ReadDir(root)
		if err != nil {
			continue // 目录尚未创建，无残留
		}
		for _, v := range versions {
			if !v.IsDir() {
				continue
			}
			ext := filepath.Join(root, v.Name(), "ext")
			if st, err := os.Stat(ext); err == nil && st.IsDir() {
				out = append(out, ext)
			}
		}
	}
	return out, nil
}
