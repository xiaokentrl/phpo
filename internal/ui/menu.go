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
	// 退出不在此处直接 app.Quit()：忙碌判定（有任务在跑不得退出）与窗口内托盘菜单同在
	// 前端，两条入口必须共用同一裁决与同一出口（前端 AppTrayMenu → 门面 Quit），否则原生
	// 托盘会绕过忙锁退出，且两路同时终止会撞在 Wails 的主线程 InvokeSync 上。
	menu.Add("退出 phpo").OnClick(func(*application.Context) {
		emitter.Emit(EventUICommand, CmdQuit)
	})
	return menu
}
