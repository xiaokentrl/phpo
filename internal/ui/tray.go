// 系统托盘：驻留入口 + 关闭窗口语义（偏好由前端 prefsStore 下发，见 App.SetTrayPrefs）
package ui

import (
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	phpapp "phpo/internal/app"
)

// Tray 托盘本体 + 与之绑定的主窗口；两项偏好为原子量，供窗口关闭钩子在任意线程读取
type Tray struct {
	tray            *application.SystemTray
	visible         atomic.Bool
	minimizeOnClose atomic.Bool
	lastActivation  atomic.Int64
}

// resideOnClose 关闭按钮的唯一裁决：仅「显示托盘」且「关闭时最小化」才驻留。
// 托盘隐藏后没有任何唤回入口，此时驻留只会留下不可见的进程，必须退出。
func resideOnClose(trayVisible, minimizeOnClose bool) bool {
	return trayVisible && minimizeOnClose
}

func resideOnCloseWithSupport(trayVisible, minimizeOnClose, trayAvailable bool) bool {
	return trayAvailable && resideOnClose(trayVisible, minimizeOnClose)
}

func shouldMinimiseOnClose(goos, sessionType, backend, waylandDisplay, xDisplay string) bool {
	if goos != "linux" {
		return false
	}
	if backend != "" {
		for _, candidate := range strings.Split(backend, ",") {
			switch strings.TrimSpace(candidate) {
			case "wayland":
				if waylandDisplay != "" {
					return true
				}
			case "x11":
				if xDisplay != "" {
					return false
				}
			}
		}
		return false
	}
	if sessionType == "x11" {
		return false
	}
	return waylandDisplay != ""
}

func minimiseOnCloseForCurrentDisplay() bool {
	return shouldMinimiseOnClose(
		runtime.GOOS,
		os.Getenv("XDG_SESSION_TYPE"),
		os.Getenv("GDK_BACKEND"),
		os.Getenv("WAYLAND_DISPLAY"),
		os.Getenv("DISPLAY"),
	)
}

func toggleTrayWindow(isMinimised bool, toggle, show func()) {
	if isMinimised {
		show()
		return
	}
	toggle()
}

const linuxTrayActivationDebounce = 500 * time.Millisecond

func handleLinuxTrayActivation(lastActivation *atomic.Int64, now time.Time, show func()) bool {
	nowNanos := now.UnixNano()
	for {
		last := lastActivation.Load()
		if last != 0 && time.Duration(nowNanos-last) < linuxTrayActivationDebounce {
			return false
		}
		if lastActivation.CompareAndSwap(last, nowNanos) {
			show()
			return true
		}
	}
}

func handleTrayClick(isMinimised func() bool, toggle, show func()) {
	toggleTrayWindow(isMinimised(), toggle, show)
}

// InstallTray 创建带应用图标的托盘、挂载菜单并把主窗口绑给托盘（否则点图标恒为无操作）。
// Linux 是否显示托盘图标由桌面环境的 StatusNotifier 宿主决定；没有宿主时窗口仍可正常使用（§8）。
//
// 托盘的真实启动在 app.Run()（SystemTray.New 会延后到 app.Run 执行），故此处不调用 tray.Run()——
// 在 app.Run() 之前调用它是空操作。
func InstallTray(app *application.App, emitter phpapp.Emitter, window *application.WebviewWindow, icon []byte, showMainWindow func()) *Tray {
	t := &Tray{}
	t.visible.Store(true)
	t.minimizeOnClose.Store(false)

	t.tray = app.SystemTray.New()
	t.tray.SetIcon(icon)
	t.tray.SetMenu(BuildTrayMenu(app, emitter, showMainWindow))
	t.tray.SetTooltip("phpo")
	t.tray.AttachWindow(window)
	t.tray.OnClick(func() {
		if runtime.GOOS == "linux" {
			handleLinuxTrayActivation(&t.lastActivation, time.Now(), showMainWindow)
			return
		}
		handleTrayClick(window.IsMinimised, t.tray.ToggleWindow, showMainWindow)
	})

	// 取消窗口默认关闭：驻留时隐藏，Wayland 例外为最小化；否则整进程退出。
	onWindowClosing := func(e *application.WindowEvent) {
		e.Cancel()
		if !resideOnClose(t.visible.Load(), t.minimizeOnClose.Load()) {
			app.Quit()
			return
		}
		support, err := DetectTraySupport()
		if err != nil {
			if app.Logger != nil {
				app.Logger.Error("system tray status check failed; exiting instead of minimizing", "error", err)
			}
			app.Quit()
			return
		}
		if !resideOnCloseWithSupport(t.visible.Load(), t.minimizeOnClose.Load(), support.Available) {
			app.Quit()
			return
		}
		if minimiseOnCloseForCurrentDisplay() {
			window.Minimise()
		} else {
			window.Hide()
		}
	}
	window.RegisterHook(events.Common.WindowClosing, onWindowClosing)
	window.RegisterHook(events.Linux.WindowDeleteEvent, onWindowClosing)
	return t
}

// SetPrefs 下发已按平台能力归一后的托盘偏好。
// Linux 的图标显隐 API 是空操作，因此 App.SetTrayPrefs 会保持 visible=true，并在支持缺失时关闭驻留。
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
