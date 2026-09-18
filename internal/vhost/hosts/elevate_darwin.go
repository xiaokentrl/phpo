//go:build darwin

// macOS hosts 提权：osascript ... with administrator privileges
package hosts

import (
	"fmt"
	"os"
	"os/exec"
)

// platformElevate 先把内容落到临时文件，再用 osascript 以管理员权限 cp 回 hosts；用户拒绝授权则报错
func platformElevate(path string) Elevator {
	return func(content string) error {
		tmp, err := os.CreateTemp("", "phpo-hosts-*.txt")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString(content); err != nil {
			tmp.Close()
			return err
		}
		tmp.Close()
		script := fmt.Sprintf(`do shell script "cp %s %s" with administrator privileges`, tmp.Name(), path)
		cmd := exec.Command("osascript", "-e", script)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("osascript 提权写入失败: %v: %s", err, out)
		}
		return nil
	}
}
