//go:build darwin

// macOS 升级安装（T604）：.app 替换（§8 升级安装方式=.app 替换）
// 交给随包分发的更新器完成「下载→挂载→替换 /Applications/phpo.app→卸载」；此处仅触发其执行
package updater

import (
	"context"
	"os/exec"
)

type darwinInstaller struct{}

func newPlatformInstaller() Installer { return darwinInstaller{} }

// Install 运行更新器二进制并等待其退出；ctx 取消即终止
func (darwinInstaller) Install(ctx context.Context, pkgPath string) error {
	cmd := exec.CommandContext(ctx, pkgPath, "--install")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
