// Docker 引擎可用性探测：未装/未运行则拒绝启动服务（硬红线 7），版本过旧仅警告
package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	sv "github.com/Masterminds/semver/v3"
)

// 探测失败的哨兵错误：真实 SDK 实现据此区分「未安装」「未运行」与「无权限」
var (
	ErrDockerNotInstalled = errors.New("docker 未安装")
	ErrDockerNotRunning   = errors.New("docker 未运行")
	ErrDockerNoPermission = errors.New("docker socket 权限不足")
)

// Docker 最低可用版本（§5.7：低于此仅警告，不阻止）
var dockerMinVersion = sv.MustParse("20.10.0")

const dockerDownloadURL = "https://www.docker.com/products/docker-desktop/"

// errBriefMax 底层错误压成单行后保留的最大字符数。SDK 或异常端点可能回吐整段 HTML / 多行堆栈，
// 原样铺进首启横幅会撑满界面（§3.2 原则 3：错误信息是人话）。
const errBriefMax = 120

// errBrief 把任意底层错误收成单行、限长的可读片段，超长以省略号收尾。
func errBrief(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > errBriefMax {
		return string(r[:errBriefMax]) + "…"
	}
	return s
}

// sentinels 探测结论的三分法依据；组合错误里除它们之外的段落才是诊断原因。
var sentinels = []error{ErrDockerNotInstalled, ErrDockerNotRunning, ErrDockerNoPermission}

func isSentinel(err error) bool {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// reasonOf 取出 classifyDialErr 用 errors.Join 附在哨兵之后的原始连接错误。
func reasonOf(err error) string {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return ""
	}
	for _, e := range joined.Unwrap() {
		if !isSentinel(e) {
			return errBrief(e)
		}
	}
	return ""
}

// withReason 把人话结论与原始连接错误拼成一行。哨兵吞掉原文会让「Docker 明明在跑却 detect 不到」
// 这类真机问题无从自查（§3.2 原则 3）。
func withReason(main string, err error) string {
	if r := reasonOf(err); r != "" {
		return main + "（" + r + "）"
	}
	return main
}

// Status Docker 引擎探测结论
type Status string

const (
	StatusOK           Status = "ok"            // 可用
	StatusNotInstalled Status = "not_installed" // 未安装：阻止启动服务
	StatusNotRunning   Status = "not_running"   // 未运行：阻止启动服务
	StatusNoPermission Status = "no_permission" // socket 无权限：阻止启动服务
	StatusOldVersion   Status = "old_version"   // 过旧：可用，但警告
)

// Health 探测结果；CanStart=false 时不得启动任何容器（硬红线 7）
type Health struct {
	Status   Status
	Version  string
	CanStart bool
	Warning  bool
	Message  string // 人话错误/提示（§3.2 原则 3）
	Hint     string // 建议动作（可能含下载链接）
}

// Probe 抽象一次 Docker 探测，便于 mock 三态；真实实现见 sdkProbe
// 返回引擎版本字符串，或 ErrDockerNotInstalled / ErrDockerNotRunning
type Probe interface {
	Detect(ctx context.Context) (version string, err error)
}

// isLinuxRuntime Docker 在 Linux 上是系统服务（apt 装的 docker.io / docker-ce），
// 没有 Docker Desktop 这个程序；把用户指向本机不存在的产品等于没有建议（§8 跨平台矩阵）。
var isLinuxRuntime = runtime.GOOS == "linux"

// 各分支的人话建议，按平台与引擎分档（v2.9.16：未安装态给 Docker/Podman 双列引导——需求②
// 「两个都没有就提示用户需要安装 podman 或者 docker」；未运行/无权限态给两引擎各自的修复命令）。
func hintNotInstalled() string {
	if isLinuxRuntime {
		return "未检测到容器引擎，请任选其一安装：Docker（sudo apt install docker.io，或按 docs.docker.com 安装 docker-ce）" +
			"或 Podman（sudo apt install podman，装后执行 systemctl --user enable --now podman.socket）。"
	}
	return "未检测到容器引擎，请安装 Docker Desktop（" + dockerDownloadURL + "）或 Podman（podman.io）。"
}

func hintNotRunning() string {
	if isLinuxRuntime {
		return "请启动容器引擎：Docker（sudo systemctl start docker）或 Podman（systemctl --user start podman.socket）。"
	}
	return "请启动 Docker Desktop 或 Podman machine（podman machine start）。"
}

func hintNoPermission() string {
	if isLinuxRuntime {
		return "当前用户无权访问容器引擎 socket。Docker：请把用户加入 docker 组（sudo usermod -aG docker $USER 后重新登录）；" +
			"Podman：请启用用户级 socket（systemctl --user enable --now podman.socket）。"
	}
	return "当前用户无权访问容器引擎。请重新启动 Docker Desktop / Podman machine；仍不行时以管理员身份运行本应用。"
}

func hintConnectFailed() string {
	if isLinuxRuntime {
		return "请确认容器引擎已就绪（Docker：sudo systemctl start docker；Podman：systemctl --user start podman.socket）且端点可达后重试。"
	}
	return "请确认 Docker Desktop 或 Podman machine 已启动后重试。"
}

// Check 依探测结果给出结构化健康结论；不做任何 IO，纯逻辑可测。
// 文案对 Docker / Podman 双引擎通用（v2.9.16：未安装态即需求②的「请安装 Podman 或 Docker」引导）。
func Check(ctx context.Context, p Probe) Health {
	ver, err := p.Detect(ctx)
	switch {
	case errors.Is(err, ErrDockerNoPermission):
		return Health{
			Status:  StatusNoPermission,
			Message: withReason("当前用户无权访问容器引擎。", err),
			Hint:    hintNoPermission(),
		}
	case errors.Is(err, ErrDockerNotInstalled):
		return Health{
			Status:  StatusNotInstalled,
			Message: withReason("未检测到容器引擎（Docker 或 Podman）。", err),
			Hint:    hintNotInstalled(),
		}
	case errors.Is(err, ErrDockerNotRunning):
		return Health{
			Status:  StatusNotRunning,
			Message: withReason("容器引擎未运行。", err),
			Hint:    hintNotRunning(),
		}
	case err != nil:
		return Health{
			Status:  StatusNotRunning,
			Message: fmt.Sprintf("无法连接容器引擎：%s", errBrief(err)),
			Hint:    hintConnectFailed(),
		}
	}

	// Ping 成功：判断版本是否过旧（§5.7 仅警告）。用 LessThan 直接比较，
	// 避免 semver 约束对预发布版本（-beta 等）的特殊排除导致误判。
	if v, verr := sv.NewVersion(ver); verr == nil && v.LessThan(dockerMinVersion) {
		return Health{
			Status:   StatusOldVersion,
			Version:  ver,
			CanStart: true,
			Warning:  true,
			Message:  fmt.Sprintf("容器引擎版本较旧（%s < 20.10），部分功能可能不可用。", ver),
			Hint:     "是否继续？",
		}
	}
	return Health{Status: StatusOK, Version: ver, CanStart: true}
}
