// 根 Service：前端绑定的唯一入口，持有并转调应用对象图（业务方法自 M2 起逐步挂接）
package main

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
	"phpo/internal/ui"
)

// wailsEmitter 将 internal/app.Emitter 适配到 Wails 事件系统（前端订阅唯一通道）
type wailsEmitter struct{ app *application.App }

func (w wailsEmitter) Emit(event string, payload any) {
	if w.app != nil {
		w.app.Event.Emit(event, payload)
	}
}

type App struct {
	wails     *application.App
	container *phpapp.Container
	assembly  *phpapp.Assembly
}

func NewApp() *App {
	c := phpapp.NewContainer()
	return &App{container: c, assembly: c.Build()}
}

// Attach 在 application.New 之后注入 Wails 实例：替换真实发射器并安装托盘
func (a *App) Attach(app *application.App) {
	a.wails = app
	emitter := wailsEmitter{app: app}
	a.container.Emitter = emitter
	a.assembly.Emitter = emitter
	ui.InstallTray(app, emitter)
}

// ServiceStartup 实现 Wails v3 服务生命周期（启动钩子含残留临时目录清理，自 T211 注册）
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return a.assembly.Startup(ctx)
}

// ServiceShutdown 实现 Wails v3 服务生命周期：退出前收尾
func (a *App) ServiceShutdown() error {
	a.assembly.Shutdown(a.assemblyLifecycleCtx())
	return nil
}

func (a *App) assemblyLifecycleCtx() context.Context {
	if a.wails != nil {
		return a.wails.Context()
	}
	return context.Background()
}

// AppInfo 供前端确认绑定链路已通
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"name":    "phpo",
		"version": "0.1.0",
	}
}
