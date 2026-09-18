// 升级包安装器抽象（T604）：把已验签的升级包落到系统；各平台实现保持精简，exec 可被 ctx 取消
package updater

import "context"

// Installer 应用一个已通过双校验的升级包
type Installer interface {
	Install(ctx context.Context, pkgPath string) error
}

// NewInstaller 返回当前编译平台的安装器实现
func NewInstaller() Installer { return newPlatformInstaller() }
