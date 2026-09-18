// 主窗口构建参数（尺寸/标题/背景色与原型一致）
package ui

import "github.com/wailsapp/wails/v3/pkg/application"

func MainWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "phpo",
		Width:            1280,
		Height:           800,
		BackgroundColour: application.NewRGB(22, 24, 28),
	}
}
