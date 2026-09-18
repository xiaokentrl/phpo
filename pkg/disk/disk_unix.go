//go:build unix

package disk

import "syscall"

// statfsFree 用 Bavail×Bsize 计算非特权用户可用字节（Linux / macOS / BSD）
func statfsFree(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
