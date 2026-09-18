//go:build windows

package disk

import "golang.org/x/sys/windows"

// statfsFree 走 Windows API GetDiskFreeSpaceEx，取调用者可用字节
func statfsFree(path string) (uint64, error) {
	lp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(lp, &available, &total, &free); err != nil {
		return 0, err
	}
	return available, nil
}
