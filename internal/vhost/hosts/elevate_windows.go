//go:build windows

// Windows hosts 提权：UAC（powershell Start-Process -Verb RunAs）
package hosts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// platformElevate 把内容写临时文件，再用提权的 powershell Copy-Item 覆盖 hosts；UAC 被拒则报错
func platformElevate(path string) Elevator {
	return func(content string) error {
		tmp := filepath.Join(os.TempDir(), "phpo-hosts.txt")
		if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
			return err
		}
		defer os.Remove(tmp)
		ps := fmt.Sprintf(`Start-Process powershell -Verb RunAs -Wait -ArgumentList 'Copy-Item -Force "%s" "%s"'`, tmp, path)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("UAC 提权写入失败: %v: %s", err, out)
		}
		return nil
	}
}
