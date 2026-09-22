// 容器内命令执行 + 镜像提交：PHP 扩展经内置编译工具（docker-php-ext-install / pecl）在 php 容器内安装，
// 装好后 docker commit 固化为 phpo 专用镜像（§1.13.2「重建镜像 + 重启容器」）。仅真实 Docker 下有义（PHPO_LIVE 覆盖）。
package engine

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// execAttach 建好 exec 并挂上输出流；调用方负责 attach.Close()。
// Tty=false：Docker 把 stdout/stderr 复用同一条流并加 8 字节帧头，读侧必须 stdcopy 去帧，
// 否则帧头会混进内容——备份转储会被写坏，日志会带上一串不可见垃圾。
func (c *Client) execAttach(ctx context.Context, name string, cmd []string) (string, types.HijackedResponse, error) {
	id, err := c.containerID(ctx, name)
	if err != nil {
		return "", types.HijackedResponse{}, err
	}
	resp, err := c.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	})
	if err != nil {
		return "", types.HijackedResponse{}, fmt.Errorf("docker exec 创建失败: %w", err)
	}
	attach, err := c.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{Tty: false})
	if err != nil {
		return "", types.HijackedResponse{}, fmt.Errorf("docker exec attach 失败: %w", err)
	}
	return resp.ID, attach, nil
}

// execExit 取 exec 退出码；非零即报错。错误消息只带退出码——输出去向由调用方决定并已在流里，
// 把整段 stdout 塞进 error 会让 toast 变成日志堆（错误信息要是人话）。
func (c *Client) execExit(ctx context.Context, execID string) error {
	insp, err := c.cli.ContainerExecInspect(ctx, execID)
	if err != nil {
		return fmt.Errorf("docker exec inspect 失败: %w", err)
	}
	if insp.ExitCode != 0 {
		return fmt.Errorf("容器内命令失败（退出码 %d）", insp.ExitCode)
	}
	return nil
}

// ExecStream 在容器内执行命令，把去帧后的 stdout / stderr 分别写进两个 io.Writer（同源传同一个 writer 即交错保序）。
// 两条用途：备份的逻辑导出把 mysqldump/pg_dumpall 的字节直接写进文件（不经内存串）；
// 扩展编译把 stdout+stderr 逐行实时转进任务抽屉日志（§5.6.2 每一步都要回流）。退出码非零即报错。
// cmd 以 argv 传入容器，不经 shell 拼接（防注入）。
func (c *Client) ExecStream(ctx context.Context, name string, cmd []string, stdout, stderr io.Writer) error {
	execID, attach, err := c.execAttach(ctx, name, cmd)
	if err != nil {
		return err
	}
	defer attach.Close()

	if _, err := stdcopy.StdCopy(stdout, stderr, attach.Reader); err != nil {
		return fmt.Errorf("读取 exec 输出失败: %w", err)
	}
	return c.execExit(ctx, execID)
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
