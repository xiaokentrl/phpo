// 网络装配：确保共享 bridge 网络 phpo-network 存在（幂等，§5.13.2）
package engine

import (
	"context"

	"phpo/pkg/dockerutil"

	"github.com/docker/docker/api/types/network"
)

// OwnershipLabels phpo 资源归属标签（孤儿扫描/清理识别用）
func OwnershipLabels(kind, version string) map[string]string {
	l := map[string]string{"phpo.managed": "true"}
	if kind != "" {
		l["phpo.kind"] = kind
	}
	if version != "" {
		l["phpo.version"] = version
	}
	return l
}

// EnsureNetwork 创建共享 bridge 网络 phpo-network；已存在则跳过
func (c *Client) EnsureNetwork(ctx context.Context) error {
	if _, err := c.cli.NetworkInspect(ctx, dockerutil.NetworkName, network.InspectOptions{}); err == nil {
		return nil
	}
	_, err := c.cli.NetworkCreate(ctx, dockerutil.NetworkName, network.CreateOptions{
		Driver: "bridge",
		Labels: OwnershipLabels("", ""),
	})
	return err
}
