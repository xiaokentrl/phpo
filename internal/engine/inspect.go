// 容器状态查询：存在性 / 运行态 / 解析容器 ID（供生命周期与校准复用）
package engine

import (
	"context"

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

// containerID 解析名称到 ID；不存在返回 notFound
func (c *Client) containerID(ctx context.Context, name string) (string, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", err
	}
	return insp.ID, nil
}
