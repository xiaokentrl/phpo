// Package disk 提供宿主磁盘剩余空间探测（doctor §5.7 磁盘检查项）。
// 纯系统调用，无业务语义；向上找到最近的已存在父目录再 Statfs，兼容目录尚未创建的装机场景。
package disk

import (
	"os"
	"path/filepath"
)

// Free 返回 path 所在文件系统的可用字节数；path 不存在则上溯到最近的已存在祖先。
func Free(path string) (uint64, error) {
	p := filepath.Clean(path)
	for {
		if _, err := os.Stat(p); err == nil {
			return statfsFree(p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0, errNotFound(path)
		}
		p = parent
	}
}

type notFoundError string

func (e notFoundError) Error() string { return "无法确定磁盘路径: " + string(e) }

func errNotFound(path string) error { return notFoundError(path) }
