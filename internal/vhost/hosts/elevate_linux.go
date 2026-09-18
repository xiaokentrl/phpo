//go:build linux

// Linux hosts 提权：polkit（pkexec）以 root 重写整份 hosts
package hosts

import (
	"fmt"
	"os/exec"
	"strings"
)

// platformElevate 用 pkexec tee {path} 把内容写入；无 pkexec 或用户拒绝则报错
func platformElevate(path string) Elevator {
	return func(content string) error {
		if _, err := exec.LookPath("pkexec"); err != nil {
			return fmt.Errorf("系统无 pkexec（polkit），无法自动提权: %w", err)
		}
		cmd := exec.Command("pkexec", "tee", path)
		cmd.Stdin = strings.NewReader(content)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("polkit 提权写入失败: %v: %s", err, out)
		}
		return nil
	}
}
