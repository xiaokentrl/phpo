// phpo 桌面应用唯一入口：装配 Wails 应用、注册根 Service、创建主窗口（仅 GUI，无 CLI）
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
	"phpo/internal/ui"
)

// 前端构建产物（frontend/dist 由 Vite 生成；占位 index.html 保证仓库可独立编译）
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	root := NewApp()

	app := application.New(application.Options{
		Name:        "phpo",
		Description: "面向 PHP 开发者的本地 Docker 化开发环境管理器（仅 GUI，不提供 CLI）",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Services: []application.Service{application.NewService(root)},
		// 单实例（Wails 内置，Linux 走 D-Bus+文件锁，win/mac 各有原生实现）：
		// 重复点快捷方式时第二实例在 New 阶段即以 ExitCode 0 退出——不建窗、不碰 Docker、不开双份后端；
		// 首实例经 OnSecondInstanceLaunch 前置已有窗口（含从托盘召回，见 App.Activate）。
		// 第二实例把自己的版本号带给首实例：与运行中进程不一致 = 磁盘二进制已被覆盖安装/升级，
		// 首实例自动换血重启成新版本（覆盖安装后旧实例驻留托盘、点快捷方式只见旧界面的根治）。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:       "io.github.xiaokentrl.phpo",
			ExitCode:       0,
			AdditionalData: map[string]string{"version": phpapp.Version},
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				root.Activate(data)
			},
		},
		Mac: application.MacOptions{
			// 最后一个窗口关闭不代表退出：退出与否由托盘偏好裁决（见 InstallTray 的 WindowClosing 钩子）
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})
	// 窗口必须先于 Attach 创建：托盘要绑定它，点图标才能唤回窗口
	window := app.Window.NewWithOptions(ui.MainWindowOptions())
	root.Attach(app, window)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
