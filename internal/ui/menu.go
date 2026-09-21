// 托盘菜单：动作以 ui:command 事件广播给前端，由前端走统一守卫链路执行
package ui

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
)

// 前端 UI 指令（非 §5.6 状态协议事件；方向为 UI→UI 的本地命令）
const (
	EventUICommand = "ui:command"

	CmdOpenLogs  = "openLogs"
	CmdBackupNow = "backupNow"
	CmdQuit      = "quit"
)

func BuildTrayMenu(app *application.App, emitter phpapp.Emitter) *application.Menu {
	menu := app.NewMenu()
	menu.Add("打开日志").OnClick(func(*application.Context) {
		emitter.Emit(EventUICommand, CmdOpenLogs)
	})
	menu.Add("立即备份").OnClick(func(*application.Context) {
		// 实际执行在前端：走 useModals.runBackup → 后端三段式任务（有任务在跑则排队，队列/日志/成败由事件回流）
		emitter.Emit(EventUICommand, CmdBackupNow)
	})
	menu.AddSeparator()
	menu.Add("退出 phpo").OnClick(func(*application.Context) {
		emitter.Emit(EventUICommand, CmdQuit)
		app.Quit()
	})
	return menu
}
