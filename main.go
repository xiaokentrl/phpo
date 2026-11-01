// phpo 桌面应用唯一入口：装配 Wails 应用、注册根 Service、创建主窗口（仅 GUI，无 CLI）
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	phpapp "phpo/internal/app"
	"phpo/internal/config"
	"phpo/internal/identity"
	"phpo/internal/ui"
)

// 前端构建产物（frontend/dist 由 Vite 生成；占位 index.html 保证仓库可独立编译）
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	webViewMigration, err := config.PrepareWebViewDataMigration(identity.DesktopID, identity.Slug)
	if err != nil {
		log.Fatal(err)
	}
	root := NewApp()
	windowFocusRequests := make(chan struct{}, 1)
	windowReady := make(chan struct{})
	requestWindowFocus := func() {
		select {
		case windowFocusRequests <- struct{}{}:
		default:
		}
	}

	app := application.New(application.Options{
		Name:        identity.DisplayName,
		Description: "面向 PHP 开发者的本地 Docker 化开发环境管理器（仅 GUI，不提供 CLI）",
		Icon:        appIcon,
		Linux: application.LinuxOptions{
			// Avoid Wails' derived org.wails.phpo GTK application identity.
			ApplicationID: identity.AppID,
			// GNOME uses the same ID for the GTK app, Wayland window, and desktop entry.
			ProgramName: identity.DesktopID,
		},
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
			UniqueID:       identity.AppID,
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
	// Run after Wails has handled the single-instance check, but before WebKit creates its profile.
	if err := webViewMigration.Migrate(); err != nil {
		log.Fatal(err)
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		close(windowReady)
	})
	// 窗口必须先于 Attach 创建：托盘要绑定它，点图标才能唤回窗口
	window := app.Window.NewWithOptions(ui.MainWindowOptions(startMinimizedAtBoot()))
	root.Attach(app, window, requestWindowFocus)
	go func() {
		<-windowReady
		for range windowFocusRequests {
			ui.ShowMainWindow(window)
		}
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
