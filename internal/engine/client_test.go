// Docker 端点选择与连接错误归类：真机缺陷取证——daemon 明明在跑（docker info 正常），
// 却因 socket 权限/路径差异被一律报成「Docker 未运行。请启动 Docker Desktop。」
package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
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
	if got := pickEndpoint("linux", "tcp://build-agent:2376", "/run/user/1000", 1000, "/home/u", func(string) bool { return true }); got != "tcp://build-agent:2376" {
		t.Errorf("DOCKER_HOST 已设时应优先，got=%q", got)
	}
	// rootless：系统 socket 不在、XDG_RUNTIME_DIR 下有 → 用后者（桌面会话拿不到 shell 的 export）
	got := pickEndpoint("linux", "", "/run/user/1000", 1000, "/home/u", func(p string) bool {
		return p == "/run/user/1000/docker.sock"
	})
	if got != "unix:///run/user/1000/docker.sock" {
		t.Errorf("应挑中 rootless socket，got=%q", got)
	}
	// 系统 socket 在 → 优先系统端点（与 docker CLI 默认一致）
	got = pickEndpoint("linux", "", "/run/user/1000", 1000, "/home/u", func(p string) bool {
		return p == "/var/run/docker.sock" || p == "/run/user/1000/docker.sock"
	})
	if got != "unix:///var/run/docker.sock" {
		t.Errorf("系统 socket 存在时应优先，got=%q", got)
	}
	// 全都不存在 → 回落默认路径，让报错点名规范位置而不是一个不存在的猜测路径
	got = pickEndpoint("linux", "", "", 0, "", func(string) bool { return false })
	if got != defaultDockerHost {
		t.Errorf("无候选命中应回落默认，got=%q", got)
	}
}

func TestCandidateSocketsCoverage(t *testing.T) {
	cands := candidateSockets("linux", "/run/user/1000", 1000, "/home/u")
	seen := map[string]bool{}
	for _, p := range cands {
		if seen[p] {
			t.Errorf("候选路径重复：%q", p)
		}
		seen[p] = true
	}
	for _, want := range []string{"/var/run/docker.sock", "/run/docker.sock", "/run/user/1000/docker.sock", "/home/u/.docker/run/docker.sock",
		"/home/u/.colima/default/docker.sock", "/home/u/.podman/podman.sock",
		"/run/user/1000/podman/podman.sock", "/run/podman/podman.sock"} {
		if !seen[want] {
			t.Errorf("候选缺少 %q（got=%v）", want, cands)
		}
	}
	// uid 不可得（Windows）时不得凭空造 /run/user//docker.sock
	for _, p := range candidateSockets("linux", "", -1, "") {
		if strings.Contains(p, "/run/user//") {
			t.Errorf("uid 缺席时不应产生该候选：%q", p)
		}
	}
	// Docker 家族全部候选排在 Podman 家族之前（Docker 优先仲裁，§5.25）
	firstDocker, firstPodman := -1, -1
	for i, p := range cands {
		if firstDocker < 0 && strings.Contains(p, "docker.sock") {
			firstDocker = i
		}
		if firstPodman < 0 && strings.Contains(p, "podman.sock") {
			firstPodman = i
		}
	}
	if firstDocker < 0 || firstPodman < 0 || firstDocker > firstPodman {
		t.Fatalf("Docker 候选应排在 Podman 之前：docker@%d podman@%d", firstDocker, firstPodman)
	}
}

// TestClassifyEngineVersion 引擎识别判据（§5.25 实测纪律）：/version 的 Components[0].Name 含
// "Podman" 即 podman；Platform.Name 是宿主系统（实测值 "linux/amd64/ubuntu-26.04"），不能用作判据。
func TestClassifyEngineVersion(t *testing.T) {
	if got := classifyEngineVersion(types.Version{Components: []types.ComponentVersion{{Name: "Podman Engine", Version: "5.7.0"}}}); got != EnginePodman {
		t.Errorf("Podman Engine 应识别为 podman，got=%q", got)
	}
	if got := classifyEngineVersion(types.Version{Components: []types.ComponentVersion{{Name: "Engine", Version: "27.3.1"}}}); got != EngineDocker {
		t.Errorf("Docker Engine 应识别为 docker，got=%q", got)
	}
	if got := classifyEngineVersion(types.Version{}); got != EngineDocker {
		t.Errorf("无 Components 默认按 docker 处理，got=%q", got)
	}
}

// TestPickEndpointDockerWinsOverPodman Docker 与 Podman 候选同时存在 → Docker 优先（Docker 优先仲裁 §5.25）
func TestPickEndpointDockerWinsOverPodman(t *testing.T) {
	got := pickEndpoint("linux", "", "/run/user/1000", 1000, "/home/u", func(p string) bool {
		return p == "/var/run/docker.sock" || p == "/run/user/1000/podman/podman.sock"
	})
	if got != "unix:///var/run/docker.sock" {
		t.Errorf("两引擎并存应选 Docker，got=%q", got)
	}
	// 只有 Podman 可用 → 选 Podman（需求②：有一个就用一个）
	got = pickEndpoint("linux", "", "/run/user/1000", 1000, "/home/u", func(p string) bool {
		return p == "/run/user/1000/podman/podman.sock"
	})
	if got != "unix:///run/user/1000/podman/podman.sock" {
		t.Errorf("podman-only 应选中 podman socket，got=%q", got)
	}
}

// TestRootlessDetection rootless 判据（§5.25 P2b）：端点在 /run/user/<uid> 下即 rootless——
// rootless Podman 与 rootless Docker 的用户级 socket 都长在这里；rootful 与 tcp 端点不算。
// DOCKER_HOST 钉空（本机实测导出了 podman socket，FromEnv 会覆盖测试端点）。
func TestRootlessDetection(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	cases := []struct {
		host string
		want bool
	}{
		{"unix:///run/user/1000/podman/podman.sock", true},
		{"unix:///run/user/1000/docker.sock", true},
		{"unix:///var/run/docker.sock", false},
		{"unix:///run/podman/podman.sock", false},
		{"tcp://127.0.0.1:9321", false},
	}
	for _, c := range cases {
		cl, err := newAtEndpoint(c.host)
		if err != nil {
			t.Fatalf("%s: 构造客户端：%v", c.host, err)
		}
		cl.Close()
		if cl.Rootless() != c.want {
			t.Errorf("%s: Rootless()=%v 期望 %v", c.host, cl.Rootless(), c.want)
		}
	}
}

// TestDetectReportsEndpointOnClient 端点必须随 Client 落定：后续所有 SDK 调用都走这一份，
// 否则「探测挑到 rootless socket、装服务仍去拨默认路径」是第二条同类缺陷。
// DOCKER_HOST 必须钉空：FromEnv 会吃掉环境里的显式端点（本机实测导出了 podman socket），
// 让 WithHost 的测试端点失效——单测不得依赖宿主环境。
func TestDetectReportsEndpointOnClient(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
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

func TestCandidateSocketsWindows(t *testing.T) {
	cands := candidateSockets("windows", "", -1, "")
	if len(cands) != 3 {
		t.Fatalf("Windows 应给出 3 根 named pipe 候选，got=%v", cands)
	}
	// Docker 家族在前（§5.25 仲裁序）；unix 路径不得混入
	for i, p := range cands {
		if !strings.HasPrefix(p, "npipe://") {
			t.Fatalf("Windows 候选应为 npipe，got[%d]=%q", i, p)
		}
	}
	if !strings.Contains(cands[0], "docker_engine") {
		t.Fatalf("docker_engine 应排首位，got=%v", cands)
	}
}

func TestPickEndpointWindows(t *testing.T) {
	// 无 DOCKER_HOST：直接用系统默认管道（管道不可 stat，缺席由拨号归类）
	if got := pickEndpoint("windows", "", "", 0, "", func(string) bool { return false }); got != windowsDefaultDockerHost {
		t.Fatalf("Windows 无 DOCKER_HOST 应回落 npipe 默认，got=%q", got)
	}
	// DOCKER_HOST 显式设置仍原样尊重
	if got := pickEndpoint("windows", "npipe:////./pipe/docker_engine", "", 0, "", func(string) bool { return false }); got != "npipe:////./pipe/docker_engine" {
		t.Fatalf("Windows DOCKER_HOST 已设应尊重，got=%q", got)
	}
}

func TestPickFromCandidates_SchemeEndpointsBypassExists(t *testing.T) {
	// tcp:// 等 DOCKER_HOST 端点不做 stat（socketExists 对 tcp 地址必然失败），原样放行
	got := pickFromCandidates([]string{"tcp://build-agent:2376"}, func(string) bool { return false }, defaultDockerHost)
	if got != "tcp://build-agent:2376" {
		t.Fatalf("带 scheme 的端点应原样选中，got=%q", got)
	}
	// 重复候选只看一次；全不命中回落
	got = pickFromCandidates([]string{"/a.sock", "/a.sock", "/b.sock"}, func(p string) bool { return p == "/a.sock" }, defaultDockerHost)
	if got != "unix:///a.sock" {
		t.Fatalf("应命中首个在盘候选，got=%q", got)
	}
}

func TestDiscoverExtraSockets(t *testing.T) {
	home := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 真 socket（测试里以普通文件冒充，exists 打桩放行）
	write(".colima/pineapple/docker.sock", "")
	write(".lima/default/sock/docker.sock", "")
	write(".docker/contexts/meta/aaa/meta.json", `{"Endpoints":{"docker":{"Host":"unix:///mnt/wsl/docker.sock"}}}`)
	write(".docker/contexts/meta/bad/meta.json", `not-json`)
	// context 指向 tcp：不收集（只收 unix://）
	write(".docker/contexts/meta/tcp/meta.json", `{"Endpoints":{"docker":{"Host":"tcp://remote:2375"}}}`)

	exists := func(p string) bool { return true }
	got := discoverExtraSockets("linux", home, exists)
	want := []string{
		filepath.Join(home, ".colima", "pineapple", "docker.sock"),
		filepath.Join(home, ".lima", "default", "sock", "docker.sock"),
		"/mnt/wsl/docker.sock",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("发现结果 = %v，应 %v", got, want)
	}
	// Windows / 空 home 一律不扫
	if got := discoverExtraSockets("windows", home, exists); got != nil {
		t.Fatalf("Windows 不做 glob 发现，got=%v", got)
	}
	if got := discoverExtraSockets("linux", "", exists); got != nil {
		t.Fatalf("home 缺席不做发现，got=%v", got)
	}
}
