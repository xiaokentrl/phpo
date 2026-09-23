// 容器状态查询：存在性 / 运行态 / 已发布宿主端口 / 解析容器 ID（供生命周期与校准复用）
package engine

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ContainerExists 按名称判断容器是否存在
func (c *Client) ContainerExists(ctx context.Context, name string) bool {
	_, err := c.cli.ContainerInspect(ctx, name)
	return err == nil
}

// ContainerRunning 按名称判断容器是否在运行；不存在返回 (false, nil)。
// 只够回答「此刻布尔值」；要判「起来又崩了」必须用 ContainerStatus 看状态字符串
// （restarting/exited 时 Running 为 false，光看布尔会把崩溃循环读成「没在跑」）。
func (c *Client) ContainerRunning(ctx context.Context, name string) (bool, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return insp.State != nil && insp.State.Running, nil
}

// ContainerStatus 读容器状态字符串（created/running/restarting/exited/paused/dead）与最近退出码；
// 容器不存在返回 ("", 0, nil)。
func (c *Client) ContainerStatus(ctx context.Context, name string) (string, int, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	if insp.State == nil {
		return "", 0, nil
	}
	return insp.State.Status, insp.State.ExitCode, nil
}

// LogTail 取容器最近 maxLines 行日志（stdout+stderr 合并成单行），供启动失败时把真因带进任务日志。
// 取证是尽力而为：读不到就返回空串，不因此改写调用方已判定的失败原因。
func (c *Client) LogTail(ctx context.Context, name string, maxLines int) string {
	id, err := c.containerID(ctx, name)
	if err != nil {
		return ""
	}
	rc, err := c.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Tail: strconv.Itoa(maxLines),
	})
	if err != nil {
		return ""
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, rc); err != nil { // 非 tty 流带 8 字节帧头，须去帧
		return ""
	}
	return oneLineTail(buf.String(), maxLines)
}

// oneLineTail 取末尾 maxLines 行非空内容并压成单行——多行会打断抽屉的逐行日志格式
func oneLineTail(s string, maxLines int) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, " / ")
}

// PublishedPorts 读某容器当前已发布到宿主的端口（升序）；容器不存在返回空集。
// 用途：重建前只实探「本次新增」的端口——已在自己容器上的端口由 Docker 占着，再探必判占用（自我误判）。
func (c *Client) PublishedPorts(ctx context.Context, name string) ([]int, error) {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
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
