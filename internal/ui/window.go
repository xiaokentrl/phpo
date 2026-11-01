// 主窗口构建参数（尺寸/标题/背景色与原型一致）
package ui

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// WindowPresenter is the Wails window behavior needed to restore and focus the main window.
type WindowPresenter interface {
	IsMinimised() bool
	IsVisible() bool
	Restore()
	Show() application.Window
	UnMinimise()
	Focus()
}

// ShowMainWindow avoids presenting a Wayland window more than once per activation.
func ShowMainWindow(window WindowPresenter) {
	if runtime.GOOS == "linux" {
		showLinuxMainWindow(window)
		return
	}
	showMainWindow(window, false)
}

func showMainWindow(window WindowPresenter, singlePresentation bool) {
	if !singlePresentation {
		window.Restore()
		window.Show()
		window.Focus()
		return
	}
	showLinuxMainWindow(window)
}

func showLinuxMainWindow(window WindowPresenter) {
	if window.IsMinimised() {
		window.UnMinimise()
		return
	}
	if !window.IsVisible() {
		window.Show()
		return
	}
	window.Focus()
}

// MainWindowOptions 构建主窗口参数。startHidden=true 时窗口建出来但不显示：
// Linux 侧 Wails 见到 Hidden 就不调 show()，窗口从未映射 ⇒ 任务栏没有条目，唤回时只调用 Show。
// 取值只影响这一次启动，不参与任何运行态判定。
func MainWindowOptions(startHidden bool) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "phpo",
		Width:            1280,
		Height:           800,
		BackgroundColour: application.NewRGB(22, 24, 28),
		Hidden:           startHidden,
	}
}
