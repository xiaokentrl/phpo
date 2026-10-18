// 主窗口构建参数（尺寸/标题/背景色与原型一致）
package ui

import "github.com/wailsapp/wails/v3/pkg/application"

// MainWindowOptions 构建主窗口参数。startHidden=true 时窗口建出来但不显示：
// Linux 侧 Wails 见到 Hidden 就不调 show()，窗口从未映射 ⇒ 任务栏没有条目，托盘点击（ToggleWindow）
// 走 Show().Focus() 把它唤回来。取值只影响这一次启动，不参与任何运行态判定。
func MainWindowOptions(startHidden bool) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "phpo",
		Width:            1280,
		Height:           800,
		BackgroundColour: application.NewRGB(22, 24, 28),
		Hidden:           startHidden,
	}
}
