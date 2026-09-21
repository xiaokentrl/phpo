// 容器状态查询：存在性 / 运行态 / 已发布宿主端口 / 解析容器 ID（供生命周期与校准复用）
package engine

import (
	"context"
	"sort"
	"strconv"

	"github.com/docker/docker/errdefs"
)

// ContainerExists 按名称判断容器是否存在
func (c *Client) ContainerExists(ctx context.Context, name string) bool {
	_, err := c.cli.ContainerInspect(ctx, name)
	return err == nil
}

// ContainerRunning 按名称判断容器是否在运行；不存在返回 (false, nil)
func (c *Client) ContainerRunning(ctx context.Context, name string) (bool, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return insp.State != nil && insp.State.Running, nil
}

// PublishedPorts 读某容器当前已发布到宿主的端口（升序）；容器不存在返回空集。
// 用途：重建前只实探「本次新增」的端口——已在自己容器上的端口由 Docker 占着，再探必判占用（自我误判）。
func (c *Client) PublishedPorts(ctx context.Context, name string) ([]int, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []int
	if insp.NetworkSettings.Ports == nil {
		return nil, nil
	}
	for _, bindings := range insp.NetworkSettings.Ports {
		for _, b := range bindings {
			if p, e := strconv.Atoi(b.HostPort); e == nil {
				out = append(out, p)
			}
		}
	}
	sort.Ints(out)
	return out, nil
}

// containerID 解析名称到 ID；不存在返回 notFound
func (c *Client) containerID(ctx context.Context, name string) (string, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", err
	}
	return insp.ID, nil
}
