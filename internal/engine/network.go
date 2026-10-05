// 网络装配：确保共享 bridge 网络 phpo-network 存在（幂等，§5.13.2）
package engine

import (
	"context"
	"errors"

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

// NetworkGateway 返回网络的网关 IP（v2.9.16/S3：Podman 的 aardvark-dns 挂在网关上，
// vhost 的 resolver 地址按引擎动态化时取它）。多子网时取第一个带 Gateway 的配置。
func (c *Client) NetworkGateway(ctx context.Context, name string) (string, error) {
	n, err := c.cli.NetworkInspect(ctx, name, network.InspectOptions{})
	if err != nil {
		return "", err
	}
	for _, cfg := range n.IPAM.Config {
		if cfg.Gateway != "" {
			return cfg.Gateway, nil
		}
	}
	return "", errors.New("网络 " + name + " 未配置网关")
}
