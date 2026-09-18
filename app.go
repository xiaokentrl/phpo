// 根 Service：前端绑定的唯一入口，持有并转调应用对象图（业务方法自 M2 起逐步挂接）
package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
	"phpo/internal/model"
	"phpo/internal/ui"
)

// errNotReady 前端在启动钩子完成前抢跑调用时的守卫
var errNotReady = errors.New("服务尚未初始化")

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

// ---- M3 生命周期绑定：所有写操作经 AppService → task.Manager 三段式（硬红线 4/5）----

// GetState 拉取当前权威快照（前端订阅外的兜底）
func (a *App) GetState() (*model.Snapshot, error) {
	if a.container.AppService == nil {
		return nil, errNotReady
	}
	return a.container.AppService.GetState()
}

// Running 是否有任务在执行
func (a *App) Running() bool {
	return a.container.AppService != nil && a.container.AppService.Running()
}

// Install 安装并启动服务版本（缓存优先镜像 → 容器）
func (a *App) Install(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Install(ctx, kind, version)
}

// Start 启动已安装容器
func (a *App) Start(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Start(ctx, kind, version)
}

// Stop 停止容器（保留数据）
func (a *App) Stop(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Stop(ctx, kind, version)
}

// Remove 卸载容器（保留数据卷）
func (a *App) Remove(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Remove(ctx, kind, version)
}

// Cancel 取消当前运行中任务
func (a *App) Cancel() {
	if a.container.AppService != nil {
		a.container.AppService.Cancel()
	}
}

// Calibrate 手动触发状态校准
func (a *App) Calibrate(ctx context.Context) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Calibrate(ctx)
}
