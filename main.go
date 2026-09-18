// phpo 桌面应用唯一入口：装配 Wails 应用、注册根 Service、创建主窗口（仅 GUI，无 CLI）
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

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
		Description: "面向 PHP 开发者的本地 Docker 化开发环境管理器",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Services: []application.Service{application.NewService(root)},
		Mac: application.MacOptions{
			// 关闭主窗口后驻留托盘，不退出进程
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})
	root.Attach(app)

	app.Window.NewWithOptions(ui.MainWindowOptions())

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
