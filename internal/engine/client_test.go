// Docker 端点选择与连接错误归类：真机缺陷取证——daemon 明明在跑（docker info 正常），
// 却因 socket 权限/路径差异被一律报成「Docker 未运行。请启动 Docker Desktop。」
package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// SDK 在 unix 端点上的真实原文（本机实测三种形态），分类必须逐一对上号
const (
	rawPermission = "permission denied while trying to connect to the Docker daemon socket at " +
		"unix:///var/run/docker.sock: Head \"http://%2Fvar%2Frun%2Fdocker.sock/_ping\": dial unix " +
		"/var/run/docker.sock: connect: permission denied"
	rawCannotConnect = "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. " +
		"Is the docker daemon running?"
)

func TestClassifyDialErr(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		err      error
		exists   func(string) bool
		wantSent error
	}{
		{
			name: "socket 在但无权访问→无权限（不再误报未运行）",
			host: "unix:///var/run/docker.sock", err: errors.New(rawPermission),
			exists: func(string) bool { return true }, wantSent: ErrDockerNoPermission,
		},
		{
			name: "socket 文件不存在→未安装（SDK 只给通用 Cannot connect 原文）",
			host: "unix:///var/run/docker.sock", err: errors.New(rawCannotConnect),
			exists: func(string) bool { return false }, wantSent: ErrDockerNotInstalled,
		},
		{
			name: "socket 残留但 daemon 没跑→未运行",
			host: "unix:///var/run/docker.sock", err: errors.New(rawCannotConnect),
			exists: func(string) bool { return true }, wantSent: ErrDockerNotRunning,
		},
		{
			name: "tcp 端点拒绝连接→未运行（远端装没装无从判定）",
			host: "tcp://10.0.0.8:2376", err: errors.New(rawCannotConnect),
			exists: func(string) bool { return false }, wantSent: ErrDockerNotRunning,
		},
		{
			name: "Windows 命名管道缺失→未安装",
			host: "npipe:////./pipe/docker_engine", err: errors.New(`open \\\\.\\pipe\\docker_engine: The system cannot find the file specified.`),
			exists: func(string) bool { return true }, wantSent: ErrDockerNotInstalled,
		},
		{
			name: "标准库包装的 EACCES 也归无权限",
			host: "unix:///run/user/1000/docker.sock", err: os.ErrPermission,
			exists: func(string) bool { return true }, wantSent: ErrDockerNoPermission,
		},
	}
	for _, c := range cases {
		got := classifyDialErr(c.host, c.err, c.exists)
		if !errors.Is(got, c.wantSent) {
			t.Errorf("%s: 归类为 %v，期望 %v（原文=%v）", c.name, got, c.wantSent, c.err)
		}
	}
}

func TestPickEndpoint(t *testing.T) {
	// DOCKER_HOST 显式设置即原样尊重（含 tcp/ssh），不擅自改图
	if got := pickEndpoint("tcp://build-agent:2376", "/run/user/1000", 1000, "/home/u", func(string) bool { return true }); got != "tcp://build-agent:2376" {
		t.Errorf("DOCKER_HOST 已设时应优先，got=%q", got)
	}
	// rootless：系统 socket 不在、XDG_RUNTIME_DIR 下有 → 用后者（桌面会话拿不到 shell 的 export）
	got := pickEndpoint("", "/run/user/1000", 1000, "/home/u", func(p string) bool {
		return p == "/run/user/1000/docker.sock"
	})
	if got != "unix:///run/user/1000/docker.sock" {
		t.Errorf("应挑中 rootless socket，got=%q", got)
	}
	// 系统 socket 在 → 优先系统端点（与 docker CLI 默认一致）
	got = pickEndpoint("", "/run/user/1000", 1000, "/home/u", func(p string) bool {
		return p == "/var/run/docker.sock" || p == "/run/user/1000/docker.sock"
	})
	if got != "unix:///var/run/docker.sock" {
		t.Errorf("系统 socket 存在时应优先，got=%q", got)
	}
	// 全都不存在 → 回落默认路径，让报错点名规范位置而不是一个不存在的猜测路径
	got = pickEndpoint("", "", 0, "", func(string) bool { return false })
	if got != defaultDockerHost {
		t.Errorf("无候选命中应回落默认，got=%q", got)
	}
}

func TestCandidateSocketsCoverage(t *testing.T) {
	cands := candidateSockets("/run/user/1000", 1000, "/home/u")
	seen := map[string]bool{}
	for _, p := range cands {
		if seen[p] {
			t.Errorf("候选路径重复：%q", p)
		}
		seen[p] = true
	}
	for _, want := range []string{"/var/run/docker.sock", "/run/docker.sock", "/run/user/1000/docker.sock", "/home/u/.docker/run/docker.sock"} {
		if !seen[want] {
			t.Errorf("候选缺少 %q（got=%v）", want, cands)
		}
	}
	// uid 不可得（Windows）时不得凭空造 /run/user//docker.sock
	for _, p := range candidateSockets("", -1, "") {
		if strings.Contains(p, "/run/user//") {
			t.Errorf("uid 缺席时不应产生该候选：%q", p)
		}
	}
}

// TestDetectReportsEndpointOnClient 端点必须随 Client 落定：后续所有 SDK 调用都走这一份，
// 否则「探测挑到 rootless socket、装服务仍去拨默认路径」是第二条同类缺陷。
func TestDetectReportsEndpointOnClient(t *testing.T) {
	c, err := newAtEndpoint("unix:///nonexistent-phpo-test.sock")
	if err != nil {
		t.Fatalf("构造客户端：%v", err)
	}
	defer c.Close()
	if c.DockerHost() != "unix:///nonexistent-phpo-test.sock" {
		t.Errorf("DockerHost()=%q 未反映实际端点", c.DockerHost())
	}
	_, derr := c.Detect(context.Background())
	if !errors.Is(derr, ErrDockerNotInstalled) {
		t.Errorf("端点不存在应归未安装，got=%v", derr)
	}
}
