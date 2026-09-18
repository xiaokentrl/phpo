// 依赖注入：对象图唯一构造入口；上层组件只从 Container 获取依赖，禁止跨层引用（§0.2 规则 12）
package app

import (
	"context"

	"phpo/internal/cache"
	"phpo/internal/config"
	"phpo/internal/updater"
)

// Container 汇集已构造的底层组件；M2 逐层扩充（Store/Config/Cache/TaskManager/Preflight）
type Container struct {
	Emitter        Emitter
	Lifecycle      *Lifecycle
	Env            config.Env
	CurrentVersion string // 应用当前版本（升级比较基准）
	UpdateURL      string // 发布清单地址；为空则不启用自动检查
}

func NewContainer() *Container {
	return &Container{
		Emitter:   NopEmitter{},
		Lifecycle: NewLifecycle(),
		Env:       config.DerivePaths(config.DefaultHome, config.DefaultWWW),
	}
}

// Build 依据容器构造应用对象图，并注册启动钩子
func (c *Container) Build() *Assembly {
	// §5.14.4 第 4 类必清时机：应用启动扫描并清空残留临时目录
	c.Lifecycle.AddStartupHook("clear-temp-residue", func(ctx context.Context) error {
		// 读取当前发射器（Attach 已替换为 Wails 实现），保证事件可达前端
		mgr := cache.NewManager(c.Env, c.Emitter, nil)
		return mgr.ScanAndClearResidue(ctx)
	})
	// §5.9 升级检查：启动时 + 每 24 小时（仅当配置了发布清单地址）
	if c.UpdateURL != "" {
		c.Lifecycle.AddStartupHook("updater-scheduler", func(ctx context.Context) error {
			src := updater.HTTPSource{URL: c.UpdateURL}
			chk := updater.NewChecker(c.CurrentVersion, src, c.Emitter)
			updater.NewScheduler(chk, updater.DefaultInterval).Start(ctx)
			return nil
		})
	}
	return &Assembly{
		Emitter:   c.Emitter,
		Lifecycle: c.Lifecycle,
	}
}
