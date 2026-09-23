// vhost 校验器：写入前 nginx -t 必过（硬红线 2）。校验能力注入，避免 vhost 反向依赖 engine。
package vhost

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Validator 校验 vhost 正文是否可被 nginx 接受；实现方负责 `nginx -t`（或等效）。
// 纯内容校验器走「校验前于写盘」路径，不落磁盘。
type Validator interface {
	Validate(ctx context.Context, domain, content string) error
}

// FileValidator 是 Validator 的可选扩展：需要 conf 已在磁盘上才能校验（真实 nginx -t 核验整套配置）。
// Save 检测到实现方为 FileValidator 时，先写盘再校验，校验失败自动回滚为原内容或删除新文件。
type FileValidator interface {
	ValidateFile(ctx context.Context, path string) error
}

// Runner 执行一条命令并返回合并输出（stdout+stderr）；注入以便单测免依赖真实 docker/nginx
type Runner func(ctx context.Context, name string, arg ...string) ([]byte, error)

// ContainerFunc 惰性解析 nginx 单例容器名：nginx 版本由用户开放输入（§1.6），
// 校验/重载每次调用都要现取，装配期定死的名字会在换版本后 exec 到不存在的容器上。
type ContainerFunc func() (string, error)

// ExecValidator 通过外部命令（如 `docker exec phpo-nginx-{version} nginx -t`）核验 vhost 落盘正文。
// 需 conf 在磁盘上，故实现 FileValidator；命令原始错误一并回传（T406：展示 nginx 错误原文）。
type ExecValidator struct {
	run   Runner
	name  string
	argsF func() ([]string, error)
}

// NewExecValidator 用给定命令（name + args）与执行器构造真实 nginx -t 校验器
func NewExecValidator(run Runner, name string, args ...string) *ExecValidator {
	if run == nil {
		run = defaultRunner
	}
	fixed := append([]string(nil), args...)
	return &ExecValidator{run: run, name: name, argsF: func() ([]string, error) { return fixed, nil }}
}

// NewNginxTValidator 以 `docker exec <container> nginx -t` 为校验命令（nginx 跑在容器内）。
// 容器名每次调用现取：nginx 版本由用户开放输入（§1.6），装配期写死即 exec 到不存在的容器。
func NewNginxTValidator(container ContainerFunc) *ExecValidator {
	return &ExecValidator{
		run:  defaultRunner,
		name: "docker",
		argsF: func() ([]string, error) {
			name, err := container()
			if err != nil {
				return nil, err
			}
			return []string{"exec", name, "nginx", "-t"}, nil
		},
	}
}

// ValidateFile 运行 nginx -t；失败时把命令输出并入错误返回
func (v *ExecValidator) ValidateFile(ctx context.Context, _ string) error {
	args, err := v.argsF()
	if err != nil {
		return err
	}
	out, err := v.run(ctx, v.name, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("nginx -t 失败: %s", msg)
	}
	return nil
}

// Validate 满足 Validator 接口：内容级校验无法核验磁盘态，交由 FileValidator 路径处理，此处空过
func (v *ExecValidator) Validate(context.Context, string, string) error { return nil }

// defaultRunner 使用 os/exec 执行命令并返回合并输出
func defaultRunner(ctx context.Context, name string, arg ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, arg...).CombinedOutput()
}
