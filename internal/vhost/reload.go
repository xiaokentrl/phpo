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
	run  Runner
	name string
	args []string
}

// NewExecReloader 用给定命令与执行器构造重载器
func NewExecReloader(run Runner, name string, args ...string) *ExecReloader {
	if run == nil {
		run = defaultRunner
	}
	return &ExecReloader{run: run, name: name, args: args}
}

// NewNginxReloader 以 `docker exec <container> nginx -s reload` 为重载命令
func NewNginxReloader(container string) *ExecReloader {
	return NewExecReloader(nil, "docker", "exec", container, "nginx", "-s", "reload")
}

// Reload 运行重载命令，失败时回传命令原始输出
func (r *ExecReloader) Reload(ctx context.Context) error {
	out, err := r.run(ctx, r.name, r.args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("nginx reload 失败: %s", msg)
	}
	return nil
}
