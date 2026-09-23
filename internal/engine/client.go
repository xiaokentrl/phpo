// Docker SDK 客户端封装：端点选择与可用性探测实现，映射连接错误为可操作的三态哨兵
package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/client"
)

// defaultDockerHost 与 docker CLI 的默认端点一致；所有候选都不在盘上时回落它，
// 让报错点名规范位置而不是一个凭空的猜测路径。
const defaultDockerHost = "unix:///var/run/docker.sock"

// Client 暴露本项目需要的最小 Docker 能力；便于上层持有与替换。
// host 是本次实际拨号的端点：所有后续 SDK 调用都走这一份，探测与操作不会各说一套。
type Client struct {
	cli  *client.Client
	host string
}

// New 构造客户端。DOCKER_HOST 已由用户显式设置时原样尊重（含 tcp/ssh 与 TLS 环境变量）；
// 未设置时按候选路径挑盘上真实存在的 socket——从 .desktop 启动的进程拿不到用户 shell 里
// export 的 DOCKER_HOST，rootless / Docker Desktop on Linux 因此会被误判成「未装/未运行」。
// 惰性：不拨号，Docker 缺席亦不报错。
func New() (*Client, error) {
	return newAtEndpoint(pickEndpoint(os.Getenv("DOCKER_HOST"), xdgRuntimeDir(), os.Getuid(),
		os.Getenv("HOME"), socketExists))
}

func newAtEndpoint(host string) (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if os.Getenv("DOCKER_HOST") == "" {
		opts = append(opts, client.WithHost(host)) // FromEnv 之后应用，显式端点优先于环境缺省
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli, host: cli.DaemonHost()}, nil
}

// DockerHost 返回实际使用的端点（供诊断文案与日志点名）
func (c *Client) DockerHost() string { return c.host }

// Close 释放底层连接
func (c *Client) Close() error {
	if c == nil || c.cli == nil {
		return nil
	}
	return c.cli.Close()
}

// Detect 实现 Probe：Ping 判存活，ServerVersion 取版本；错误映射为未装/无权限/未运行
func (c *Client) Detect(ctx context.Context) (string, error) {
	if _, err := c.cli.Ping(ctx); err != nil {
		return "", classifyDialErr(c.host, err, socketExists)
	}
	v, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return "", classifyDialErr(c.host, err, socketExists)
	}
	return v.Version, nil
}

// candidateSockets 按优先级给出 Docker unix socket 候选路径：系统默认 → systemd 运行目录 →
// rootless（XDG_RUNTIME_DIR 与 /run/user/<uid>）→ Docker Desktop on Linux 的用户目录。
// xdgRuntime/home 为空、uid < 0（Windows）时不产生对应候选。
func candidateSockets(xdgRuntime string, uid int, home string) []string {
	cands := []string{"/var/run/docker.sock", "/run/docker.sock"}
	if xdgRuntime != "" {
		cands = append(cands, filepath.Join(xdgRuntime, "docker.sock"))
	}
	if uid >= 0 {
		cands = append(cands, filepath.Join("/run/user", strconv.Itoa(uid), "docker.sock"))
	}
	if home != "" {
		cands = append(cands, filepath.Join(home, ".docker", "run", "docker.sock"))
	}
	out := make([]string, 0, len(cands))
	seen := map[string]bool{}
	for _, p := range cands {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// pickEndpoint 决定拨哪个端点：显式 DOCKER_HOST 原样胜出；否则取第一个「socket 确实在盘上」的
// 候选（转成 unix:// 形式）；一个都不在即回落默认端点。
func pickEndpoint(dockerHost, xdgRuntime string, uid int, home string, exists func(string) bool) string {
	if dockerHost != "" {
		return dockerHost
	}
	for _, p := range candidateSockets(xdgRuntime, uid, home) {
		if exists(p) {
			return "unix://" + p
		}
	}
	return defaultDockerHost
}

// xdgRuntimeDir 取 XDG_RUNTIME_DIR（rootless daemon socket 的规范位置）
func xdgRuntimeDir() string { return os.Getenv("XDG_RUNTIME_DIR") }

// socketExists 路径存在且是 unix socket（普通文件残留不算，避免把误配的路径当端点）
func socketExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// classifyDialErr 把 SDK 连接错误归成三种可操作结论（各带原始错误作诊断依据）：
//   - 无权限：socket 在、当前用户拨不动（apt 装 docker.io 后未把用户加入 docker 组是首要成因）
//   - 未安装：端点根本不在盘上（SDK 只回一句通用的 "Cannot connect to the Docker daemon"，
//     不看 socket 文件就区分不出「没装」与「装了但没起」）
//   - 未运行：其余一切（socket 残留、daemon 停了、远端拒连）
func classifyDialErr(host string, err error, exists func(string) bool) error {
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, os.ErrPermission),
		strings.Contains(msg, "permission denied"),
		strings.Contains(msg, "operation not permitted"),
		strings.Contains(msg, "access is denied"):
		return errors.Join(ErrDockerNoPermission, err)
	case strings.Contains(msg, "no such file or directory"),
		strings.Contains(msg, "cannot find the file"),
		unixSocketMissing(host, exists):
		return errors.Join(ErrDockerNotInstalled, err)
	default:
		return errors.Join(ErrDockerNotRunning, err)
	}
}

// unixSocketMissing 仅对 unix 端点判定 socket 文件缺席；tcp/ssh/npipe 无从判断对端装没装。
func unixSocketMissing(host string, exists func(string) bool) bool {
	if !strings.HasPrefix(host, "unix://") {
		return false
	}
	return !exists(strings.TrimPrefix(host, "unix://"))
}
