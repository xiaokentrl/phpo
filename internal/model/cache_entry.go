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

type CacheMissEvent struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Action  string `json:"action"`
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
