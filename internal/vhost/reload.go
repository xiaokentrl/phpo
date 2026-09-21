// nginx 重载器：写盘后经 `docker exec <container> nginx -s reload` 让配置生效（§5.5 第 7 步）。
// 命令执行器注入，避免 vhost 反向依赖 engine，并让单测免依赖真实容器。
package vhost

import (
	"context"
	"fmt"
	"strings"
)

// ExecReloader 通过外部命令重载 nginx；实现 service.Reloader（结构一致，无需显式导入）
type ExecReloader struct {
	run   Runner
	name  string
	argsF func() ([]string, error)
}

// NewExecReloader 用给定命令与执行器构造重载器
func NewExecReloader(run Runner, name string, args ...string) *ExecReloader {
	if run == nil {
		run = defaultRunner
	}
	fixed := append([]string(nil), args...)
	return &ExecReloader{run: run, name: name, argsF: func() ([]string, error) { return fixed, nil }}
}

// NewNginxReloader 以 `docker exec <container> nginx -s reload` 为重载命令。
// 容器名每次调用现取（见 ContainerFunc）：nginx 版本开放输入，装配期写死即重载不到容器。
func NewNginxReloader(container ContainerFunc) *ExecReloader {
	return &ExecReloader{
		run:  defaultRunner,
		name: "docker",
		argsF: func() ([]string, error) {
			name, err := container()
			if err != nil {
				return nil, err
			}
			return []string{"exec", name, "nginx", "-s", "reload"}, nil
		},
	}
}

// Reload 运行重载命令，失败时回传命令原始输出
func (r *ExecReloader) Reload(ctx context.Context) error {
	args, err := r.argsF()
	if err != nil {
		return err
	}
	out, err := r.run(ctx, r.name, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("nginx reload 失败: %s", msg)
	}
	return nil
}
