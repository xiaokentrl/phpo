// Docker SDK 客户端封装：构造探测实现，映射连接错误为哨兵状态
package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/docker/docker/client"
)

// Client 暴露本项目需要的最小 Docker 能力；便于上层持有与替换
type Client struct {
	cli *client.Client
}

// New 用环境（DOCKER_HOST 等）构造 Docker 客户端
func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli}, nil
}

// Close 释放底层连接
func (c *Client) Close() error {
	if c == nil || c.cli == nil {
		return nil
	}
	return c.cli.Close()
}

// Detect 实现 Probe：Ping 判存活，ServerVersion 取版本；错误映射为未装/未运行
func (c *Client) Detect(ctx context.Context) (string, error) {
	if _, err := c.cli.Ping(ctx); err != nil {
		return "", classifyDialErr(err)
	}
	v, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return "", classifyDialErr(err)
	}
	return v.Version, nil
}

// classifyDialErr 把 SDK 连接错误归类：socket/endpoint 不存在→未安装；拒绝连接→未运行
func classifyDialErr(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "command not found"),
		strings.Contains(msg, "no such file or directory"),
		strings.Contains(msg, "cannot find the file"):
		return errors.Join(ErrDockerNotInstalled, err)
	default:
		return errors.Join(ErrDockerNotRunning, err)
	}
}
