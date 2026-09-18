// 容器内命令执行 + 镜像提交：PHP 扩展经内置编译工具（docker-php-ext-install / pecl）在 php 容器内安装，
// 装好后 docker commit 固化为 phpo 专用镜像（§1.13.2「重建镜像 + 重启容器」）。仅真实 Docker 下有义（PHPO_LIVE 覆盖）。
package engine

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// ExecInContainer 在容器内执行命令并回收 stdout+stderr；cmd 以 argv 传入（非 shell 拼接），退出码非零即报错并附输出。
func (c *Client) ExecInContainer(ctx context.Context, name string, cmd []string) (string, error) {
	id, err := c.containerID(ctx, name)
	if err != nil {
		return "", err
	}
	resp, err := c.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	})
	if err != nil {
		return "", fmt.Errorf("docker exec 创建失败: %w", err)
	}
	attach, err := c.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{Tty: false})
	if err != nil {
		return "", fmt.Errorf("docker exec attach 失败: %w", err)
	}
	defer attach.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(attach.Reader); err != nil {
		return buf.String(), fmt.Errorf("读取 exec 输出失败: %w", err)
	}
	insp, err := c.cli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return buf.String(), fmt.Errorf("docker exec inspect 失败: %w", err)
	}
	out := buf.String()
	if insp.ExitCode != 0 {
		return out, fmt.Errorf("容器内命令失败（退出码 %d）: %s", insp.ExitCode, strings.TrimSpace(out))
	}
	return out, nil
}

// CommitContainer 把容器当前文件系统固化为镜像 ref（docker commit -r ref）。
func (c *Client) CommitContainer(ctx context.Context, name, ref string) error {
	id, err := c.containerID(ctx, name)
	if err != nil {
		return err
	}
	if _, err := c.cli.ContainerCommit(ctx, id, container.CommitOptions{Reference: ref, Pause: true}); err != nil {
		return fmt.Errorf("docker commit 失败: %w", err)
	}
	return nil
}

// ImageExists 判断某镜像引用是否已在本地 store（供重装时判断扩展镜像是否就绪）
func (c *Client) ImageExists(ctx context.Context, ref string) (bool, error) {
	if _, err := c.cli.ImageInspect(ctx, ref); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
