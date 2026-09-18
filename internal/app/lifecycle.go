// 应用生命周期钩子：启动/关闭阶段的有序扩展点（§5.14.4 启动时清理残留临时目录在此注册）
package app

import (
	"context"
	"fmt"
)

type Hook struct {
	Name string
	Fn   func(ctx context.Context) error
}

type Lifecycle struct {
	startupHooks  []Hook
	shutdownHooks []Hook
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{}
}

// OnStartup 按注册顺序执行启动钩子；任一失败即中断并返回带钩子名的错误
func (l *Lifecycle) OnStartup(ctx context.Context) error {
	for _, h := range l.startupHooks {
		if err := h.Fn(ctx); err != nil {
			return fmt.Errorf("启动钩子 %s 失败: %w", h.Name, err)
		}
	}
	return nil
}

// OnShutdown 逆序执行关闭钩子；关闭阶段不中断，尽力收尾
func (l *Lifecycle) OnShutdown(ctx context.Context) {
	for i := len(l.shutdownHooks) - 1; i >= 0; i-- {
		_ = l.shutdownHooks[i].Fn(ctx)
	}
}

func (l *Lifecycle) AddStartupHook(name string, fn func(ctx context.Context) error) {
	l.startupHooks = append(l.startupHooks, Hook{Name: name, Fn: fn})
}

func (l *Lifecycle) AddShutdownHook(name string, fn func(ctx context.Context) error) {
	l.shutdownHooks = append(l.shutdownHooks, Hook{Name: name, Fn: fn})
}
