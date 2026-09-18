// 装配：将配置、存储、引擎、缓存、任务、服务各层组装为可运行的应用对象图
package app

import "context"

// Assembly 是应用对象图的根；各层构造与依赖注入见 di.go
type Assembly struct {
	Emitter   Emitter
	Lifecycle *Lifecycle
}

// Startup 由根 Service 的生命周期回调转调
func (a *Assembly) Startup(ctx context.Context) error {
	return a.Lifecycle.OnStartup(ctx)
}

// Shutdown 由根 Service 的关闭回调转调
func (a *Assembly) Shutdown(ctx context.Context) {
	a.Lifecycle.OnShutdown(ctx)
}
