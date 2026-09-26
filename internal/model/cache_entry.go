// 缓存清单模型：manifest.json（§5.14.2 schema_version=1，snake_case 键为冻结契约）
package model

import "time"

type CacheManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Kind          string            `json:"kind"`
	Version       string            `json:"version"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Image         *ManifestImage    `json:"image,omitempty"`
	ExtImage      *ManifestImage    `json:"extensions_image,omitempty"` // 扩展固化镜像 phpo/php:{version}，与基座各一条（§5.14.2）
	Apk           []ManifestPackage `json:"apk,omitempty"`
	Pecl          []ManifestPackage `json:"pecl,omitempty"`
}

type ManifestImage struct {
	Name     string    `json:"name"`
	Digest   string    `json:"digest"`
	Size     int64     `json:"size"`
	Sha256   string    `json:"sha256"`
	CachedAt time.Time `json:"cached_at"`
}

type ManifestPackage struct {
	Name     string    `json:"name"`
	Sha256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	CachedAt time.Time `json:"cached_at"`
}

// cache:* 事件载荷（§5.14.11）

type CacheHitEvent struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Source  string `json:"source"`
	Size    int64  `json:"size"`
}

// CacheMissEvent 未命中缓存。「这次到底有没有拨网络、拨的是哪台」全靠 action + source 两格：
// action=local 是本机已有镜像重建缓存（零网络）；pull/download 才走网络，此时 source 点名用的哪个镜像源，
// 空即直连官方（docker.io）。事件名未新增，只补载荷字段（docker:state-drift 补 gaps 是同先例）。
type CacheMissEvent struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Action  string `json:"action"`
	Source  string `json:"source,omitempty"` // 实际用来拉取的镜像源主机名（空=未走镜像源）
}

type CachePromoteEvent struct {
	Kind    string            `json:"kind"`
	Version string            `json:"version"`
	Entries []ManifestPackage `json:"entries"`
}

type CacheCorruptedEvent struct {
	Kind    string          `json:"kind"`
	Version string          `json:"version"`
	Entry   ManifestPackage `json:"entry"`
}

// CacheCleanupEvent §5.14.11 第 5 事件：缓存清理完成
type CacheCleanupEvent struct {
	Mode       string `json:"mode"`
	FreedBytes int64  `json:"freed_bytes"`
}

type CacheTempdirClearedEvent struct {
	Path   string `json:"path"`
	Reason string `json:"reason"` // compile_ok / compile_failed / cancelled / startup_scan
}
