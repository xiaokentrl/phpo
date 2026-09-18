// 卷装配：确保命名数据卷存在（DB 数据持久；卸载默认保留，§5.13.2）
package engine

import (
	"context"

	"github.com/docker/docker/api/types/volume"
)

// EnsureVolume 确保命名卷存在；已存在则跳过
func (c *Client) EnsureVolume(ctx context.Context, name string) error {
	if _, err := c.cli.VolumeInspect(ctx, name); err == nil {
		return nil
	}
	_, err := c.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: OwnershipLabels("", ""),
	})
	return err
}

// RemoveVolume 删除命名卷（幂等：不存在视为成功）
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	if err := c.cli.VolumeRemove(ctx, name, true); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
