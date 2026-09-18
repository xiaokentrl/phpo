//go:build windows

// Windows 升级安装（T604）：NSIS 安装包静默执行 /S（§8 升级安装方式=NSIS 静默安装）
package updater

import (
	"context"
	"os/exec"
)

type windowsInstaller struct{}

func newPlatformInstaller() Installer { return windowsInstaller{} }

// Install 以静默模式运行 NSIS 安装器；ctx 取消即终止子进程
func (windowsInstaller) Install(ctx context.Context, pkgPath string) error {
	cmd := exec.CommandContext(ctx, pkgPath, "/S")
	cmd.Stderr = nil
	cmd.Stdout = nil
	return cmd.Run()
}
