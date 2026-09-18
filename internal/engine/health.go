// Docker 引擎可用性探测：未装/未运行则拒绝启动服务（硬红线 7），版本过旧仅警告
package engine

import (
	"context"
	"errors"
	"fmt"

	sv "github.com/Masterminds/semver/v3"
)

// 探测失败的哨兵错误：真实 SDK 实现据此区分「未安装」与「未运行」
var (
	ErrDockerNotInstalled = errors.New("docker 未安装")
	ErrDockerNotRunning   = errors.New("docker 未运行")
)

// Docker 最低可用版本（§5.7：低于此仅警告，不阻止）
var dockerMinVersion = sv.MustParse("20.10.0")

const dockerDownloadURL = "https://www.docker.com/products/docker-desktop/"

// Status Docker 引擎探测结论
type Status string

const (
	StatusOK           Status = "ok"            // 可用
	StatusNotInstalled Status = "not_installed" // 未安装：阻止启动服务
	StatusNotRunning   Status = "not_running"   // 未运行：阻止启动服务
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

// Check 依探测结果给出结构化健康结论；不做任何 IO，纯逻辑可测
func Check(ctx context.Context, p Probe) Health {
	ver, err := p.Detect(ctx)
	switch {
	case errors.Is(err, ErrDockerNotInstalled):
		return Health{
			Status:  StatusNotInstalled,
			Message: "Docker 未安装。",
			Hint:    fmt.Sprintf("请下载 Docker Desktop：%s", dockerDownloadURL),
		}
	case errors.Is(err, ErrDockerNotRunning):
		return Health{
			Status:  StatusNotRunning,
			Message: "Docker 未运行。",
			Hint:    "请启动 Docker Desktop。",
		}
	case err != nil:
		return Health{
			Status:  StatusNotRunning,
			Message: fmt.Sprintf("无法连接 Docker：%v", err),
			Hint:    "请确认 Docker Desktop 已启动后重试。",
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
			Message:  fmt.Sprintf("Docker 版本较旧（%s < 20.10），部分功能可能不可用。", ver),
			Hint:     "是否继续？",
		}
	}
	return Health{Status: StatusOK, Version: ver, CanStart: true}
}
