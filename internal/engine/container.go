// 容器装配：按命名规范幂等创建/启停/删除 phpo 托管服务容器（§5.13.2/§5.13.3）
package engine

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"phpo/internal/config"
	"phpo/pkg/dockerutil"
)

// ContainerSpec 描述一个 phpo 托管服务容器的创建意图
type ContainerSpec struct {
	Kind    string            // php / mysql / pgsql / redis / nginx
	Version string            // 版本号（保留原样）
	Image   string            // repo:tag
	Env     []string          // KEY=VAL
	Command []string          // 覆盖 CMD
	Exposed []string          // 容器内暴露端口，如 "9000/tcp"
	PortMap map[string]string // 发布映射：容器端口 "9000/tcp" → 宿主端口 "9000"
	Labels  map[string]string // 额外标签（合并进归属标签）
}

// CreateServiceContainer 幂等创建服务容器：确保网络 → 清理同名 → 装配命名/挂载/别名 → create
func (c *Client) CreateServiceContainer(ctx context.Context, env config.Env, spec ContainerSpec) (string, error) {
	name := dockerutil.ContainerName(spec.Kind, spec.Version)
	if err := c.EnsureNetwork(ctx); err != nil {
		return "", err
	}
	if c.ContainerExists(ctx, name) {
		if err := c.RemoveContainer(ctx, name); err != nil {
			return "", err
		}
	}
	cfg, host, netc := buildContainerSpecs(env, spec)
	resp, err := c.cli.ContainerCreate(ctx, cfg, host, netc, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// buildContainerSpecs 纯装配：把 ContainerSpec 翻译为 create 所需的三份配置（不含网络 IO，便于单测）
func buildContainerSpecs(env config.Env, spec ContainerSpec) (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	cfg := &container.Config{
		Image:  spec.Image,
		Env:    spec.Env,
		Cmd:    spec.Command,
		Labels: mergeLabels(OwnershipLabels(spec.Kind, spec.Version), spec.Labels),
	}
	if len(spec.Exposed) > 0 {
		cfg.ExposedPorts = make(map[nat.Port]struct{}, len(spec.Exposed))
		for _, p := range spec.Exposed {
			cfg.ExposedPorts[nat.Port(p)] = struct{}{}
		}
	}

	host := &container.HostConfig{
		Binds:         BuildBinds(env, spec.Kind, spec.Version),
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}
	if len(spec.PortMap) > 0 {
		host.PortBindings = make(nat.PortMap, len(spec.PortMap))
		for cport, hport := range spec.PortMap {
			host.PortBindings[nat.Port(cport)] = []nat.PortBinding{{HostPort: hport}}
		}
	}

	alias := dockerutil.NetworkAlias(spec.Kind, spec.Version)
	netc := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			dockerutil.NetworkName: {Aliases: []string{alias}},
		},
	}
	return cfg, host, netc
}

// StartContainer 启动容器（幂等：已运行则忽略）
func (c *Client) StartContainer(ctx context.Context, name string) error {
	running, err := c.ContainerRunning(ctx, name)
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	return c.cli.ContainerStart(ctx, name, container.StartOptions{})
}

// StopContainer 停止容器（幂等：不存在视为已停止）
func (c *Client) StopContainer(ctx context.Context, name string) error {
	running, err := c.ContainerRunning(ctx, name)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	return c.cli.ContainerStop(ctx, name, container.StopOptions{})
}

// RemoveContainer 删除容器（force + remove volume，幂等：不存在视为成功）
func (c *Client) RemoveContainer(ctx context.Context, name string) error {
	if !c.ContainerExists(ctx, name) {
		return nil
	}
	return c.cli.ContainerRemove(ctx, name, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
}

func mergeLabels(base, extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
