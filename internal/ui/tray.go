// 系统托盘：驻留入口 + 关闭窗口语义（偏好由前端 prefsStore 下发，见 App.SetTrayPrefs）
package ui

import (
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	phpapp "phpo/internal/app"
)

// Tray 托盘本体 + 与之绑定的主窗口；两项偏好为原子量，供窗口关闭钩子在任意线程读取
type Tray struct {
	tray            *application.SystemTray
	visible         atomic.Bool
	minimizeOnClose atomic.Bool
}

// resideOnClose 关闭按钮的唯一裁决：仅「显示托盘」且「关闭时最小化」才隐藏驻留。
// 托盘隐藏后没有任何唤回入口，此时驻留只会留下不可见的进程，必须退出。
func resideOnClose(trayVisible, minimizeOnClose bool) bool {
	return trayVisible && minimizeOnClose
}

// InstallTray 创建托盘、挂载菜单并把主窗口绑给托盘（否则点图标恒为无操作）。
// Linux 无 libappindicator 时降级为仅窗口模式（§8）。
//
// 托盘的真实启动在 app.Run()（SystemTray.New 会延后到 app.Run 执行），故此处不调用 tray.Run()——
// 在 app.Run() 之前调用它是空操作。
func InstallTray(app *application.App, emitter phpapp.Emitter, window *application.WebviewWindow) *Tray {
	t := &Tray{}
	t.visible.Store(true)
	t.minimizeOnClose.Store(true)

	t.tray = app.SystemTray.New()
	t.tray.SetMenu(BuildTrayMenu(app, emitter))
	t.tray.SetTooltip("phpo")
	t.tray.AttachWindow(window)
	t.tray.OnClick(func() { t.tray.ToggleWindow() })

	// 取消窗口的默认销毁流程：驻留则隐藏，否则整进程退出（窗口销毁但进程存活 = 无法唤回）
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		if resideOnClose(t.visible.Load(), t.minimizeOnClose.Load()) {
			window.Hide()
			return
		}
		app.Quit()
	})
	return t
}

// SetPrefs 下发托盘偏好：visible=false 即撤下托盘图标，唤回入口随之消失。
// 平台差异：Windows/macOS 真撤图标，Linux 侧 Wails beta.23 的 Show/Hide 是空操作（图标仍在），
// 但两项偏好在三平台一律参与上面的驻留/退出裁决，勾选不是装饰。
func (t *Tray) SetPrefs(visible, minimize bool) {
	t.visible.Store(visible)
	t.minimizeOnClose.Store(minimize)
	if t.tray == nil {
		return
	}
	if visible {
		t.tray.Show()
		return
	}
	t.tray.Hide()
}

// SetLabel 更新托盘文本（主题切换/任务状态提示用）
func (t *Tray) SetLabel(label string) {
	if t != nil && t.tray != nil {
		t.tray.SetLabel(label)
	}
}
