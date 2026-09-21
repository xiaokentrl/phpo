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
	PullImage(ctx context.Context, ref string) error           // 未命中：拉取到本地 store
	SaveImage(ctx context.Context, ref, dstTar string) error   // docker save 到临时 tar
	LoadImage(ctx context.Context, tarPath string) error       // 命中：零网络 docker load
	ImageExists(ctx context.Context, ref string) (bool, error) // 未命中先探本地 store，已有则免拉取
	Download(ctx context.Context, url, dstPath string) error   // 扩展包下载（apk/pecl）
}

// Manager 缓存编排入口
type Manager struct {
	env config.Env
	em  Emitter
	db  DockerBackend
}

func NewManager(env config.Env, em Emitter, db DockerBackend) *Manager {
	if em == nil {
		em = NopEmitter{}
	}
	return &Manager{env: env, em: em, db: db}
}

// Env 暴露派生路径（供上层拼目录）
func (m *Manager) Env() config.Env { return m.env }

// 缓存提升/清理的事件发射小工具

func (m *Manager) emitHit(kind, version, source string, size int64) {
	m.em.Emit("cache:hit", model.CacheHitEvent{Kind: kind, Version: version, Source: source, Size: size})
}

func (m *Manager) emitMiss(kind, version, action string) {
	m.em.Emit("cache:miss", model.CacheMissEvent{Kind: kind, Version: version, Action: action})
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
