// 离线缓存核心：装前必查 · 命中零网络 · 未命中临时下载编译 · 成功后提升 · 无论成败清临时
// 三条铁律与四类必清时机的落地载体（§5.14 / §1.13）。真实 Docker 调用经 DockerBackend 注入
package cache

import (
	"context"
	"errors"

	"phpo/internal/config"
	"phpo/internal/model"
)

// errNoBackend 未注入 DockerBackend 时禁止走网络路径
var errNoBackend = errors.New("cache: 未注入 DockerBackend")

// Emitter 本地最小事件接口（避免反向依赖装配层）
type Emitter interface {
	Emit(event string, payload any)
}

// NopEmitter 单测占位
type NopEmitter struct{}

func (NopEmitter) Emit(string, any) {}

// DockerBackend 网络/引擎侧操作，M3 由 engine 实现；单测注入 fake
type DockerBackend interface {
	PullImage(ctx context.Context, ref string) error                       // 未命中：拉取到本地 store
	PullFromSource(ctx context.Context, host, ref string) error            // 未命中且配了镜像源：从该源拉回并归一到 ref（带源前缀那份由引擎侧清掉）
	ProbeSources(ctx context.Context, hosts []string) []model.MirrorSource // 并发测速，返回顺序与入参一致
	SaveImage(ctx context.Context, ref, dstTar string) error               // docker save 到临时 tar
	LoadImage(ctx context.Context, tarPath string) error                   // 命中：零网络 docker load
	ImageExists(ctx context.Context, ref string) (bool, error)             // 未命中先探本地 store，已有则免拉取
	Download(ctx context.Context, url, dstPath string) error               // 扩展包下载（apk/pecl）
}

// Manager 缓存编排入口
type Manager struct {
	env config.Env
	em  Emitter
	db  DockerBackend
	// src 每次拉取现读镜像源清单（装配层注入 ConfigStore.DockerSources）：
	// 用户在设置页改完保存即生效，不需要重启也不需要换对象图（它不是路径，不参与 rootsKey）
	src func() []string
}

func NewManager(env config.Env, em Emitter, db DockerBackend) *Manager {
	if em == nil {
		em = NopEmitter{}
	}
	return &Manager{env: env, em: em, db: db}
}

// Env 暴露派生路径（供上层拼目录）
func (m *Manager) Env() config.Env { return m.env }

// SetSourcesProvider 注入镜像源清单的取值回调（config.yaml 的 docker_sources，已归一为主机[:端口]）。
// 未注入或回调给出空表即「没配源」：拉取直连官方，与配源前行为一致。
func (m *Manager) SetSourcesProvider(fn func() []string) { m.src = fn }

// sourceHosts 现读一次镜像源清单；回调缺席即无源
func (m *Manager) sourceHosts() []string {
	if m.src == nil {
		return nil
	}
	return m.src()
}

// 缓存提升/清理的事件发射小工具

func (m *Manager) emitHit(kind, version, source string, size int64) {
	m.em.Emit("cache:hit", model.CacheHitEvent{Kind: kind, Version: version, Source: source, Size: size})
}

// emitMiss 的 source 只服务 action=pull：说清这次是从哪一个镜像源拉回来的（空 = 直连官方）。
// action=local（用本机已有镜像重建缓存）与 download（扩展包联网取）都不涉及镜像源，留空。
func (m *Manager) emitMiss(kind, version, action, source string) {
	m.em.Emit("cache:miss", model.CacheMissEvent{Kind: kind, Version: version, Action: action, Source: source})
}

func (m *Manager) emitPromote(kind, version string, entries []model.ManifestPackage) {
	m.em.Emit("cache:promote", model.CachePromoteEvent{Kind: kind, Version: version, Entries: entries})
}

func (m *Manager) emitCorrupted(kind, version string, entry model.ManifestPackage) {
	m.em.Emit("cache:corrupted", model.CacheCorruptedEvent{Kind: kind, Version: version, Entry: entry})
}

func (m *Manager) emitCleanup(mode string, freed int64) {
	m.em.Emit("cache:cleanup", model.CacheCleanupEvent{Mode: mode, FreedBytes: freed})
}
