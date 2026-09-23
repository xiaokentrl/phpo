//go:build linux

// Linux 升级安装（T604）：AppImage 替换（§8 升级安装方式=AppImage 替换）
// 赋予可执行权限后触发 AppImage 自更新流程；ctx 取消即终止
package updater

import (
	"context"
	"os"
	"os/exec"

	"phpo/internal/util"
)

type linuxInstaller struct{}

func newPlatformInstaller() Installer { return linuxInstaller{} }

// Install  赋权后以 --update 触发 AppImage 自替换
func (linuxInstaller) Install(ctx context.Context, pkgPath string) error {
	if err := os.Chmod(pkgPath, util.FilePerm); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, pkgPath, "--update")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
