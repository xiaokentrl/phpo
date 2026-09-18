// 离线缓存视图模型：OfflineView 统计与条目
package model

import "time"

// CacheEntry 单个 {kind}/{version} 缓存聚合条目
type CacheEntry struct {
	Kind       string    `json:"kind"`
	Version    string    `json:"version"`
	HasImage   bool      `json:"hasImage"`
	ApkCount   int       `json:"apkCount"`
	PeclCount  int       `json:"peclCount"`
	TotalSize  int64     `json:"totalSize"`
	LastVerify time.Time `json:"lastVerify"`
	VerifyOK   bool      `json:"verifyOk"`
}

// CacheStats 缓存根目录总览（§5.14.7）
type CacheStats struct {
	TotalBytes int64 `json:"totalBytes"`
	EntryCount int   `json:"entryCount"`
	ImageCount int   `json:"imageCount"`
	ExtCount   int   `json:"extCount"`
	Corrupted  int   `json:"corrupted"`
}

// CleanupMode 三模式（§5.14.6）：保守=仅损坏 / 标准=+N天未用 / 激进=全部（除在用）
type CleanupMode string

const (
	CleanupConservative CleanupMode = "conservative"
	CleanupStandard     CleanupMode = "standard"
	CleanupAggressive   CleanupMode = "aggressive"
)

type CleanupResult struct {
	Mode       CleanupMode `json:"mode"`
	FreedBytes int64       `json:"freedBytes"`
	Removed    int         `json:"removed"`
}
