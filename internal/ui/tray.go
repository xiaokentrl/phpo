// 系统托盘：驻留入口 + 状态圆点（idle/running/error 三态图标资源 M3 接入）
package ui

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
)

type Tray struct {
	tray *application.SystemTray
}

// InstallTray 创建托盘并挂载菜单；Linux 无 libappindicator 时降级为仅窗口模式（§8）
func InstallTray(app *application.App, emitter phpapp.Emitter) *Tray {
	tray := app.SystemTray.New()
	tray.SetMenu(BuildTrayMenu(app, emitter))
	tray.SetTooltip("phpo")
	tray.OnClick(func() { tray.ToggleWindow() })
	tray.Run()
	return &Tray{tray: tray}
}

// SetLabel 更新托盘文本（主题切换/任务状态提示用）
func (t *Tray) SetLabel(label string) {
	if t != nil && t.tray != nil {
		t.tray.SetLabel(label)
	}
}
