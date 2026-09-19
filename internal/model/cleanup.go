// 清理与回收站领域模型（§5.13.6 三模式 / §5.13.7 回收站 / §5.13.10 审计）
package model

import "time"

// CleanedItem 单个资源的一次删除结果
type CleanedItem struct {
	Type  ResourceType `json:"type"`
	Name  string       `json:"name"`
	OK    bool         `json:"ok"`
	Error string       `json:"error,omitempty"`
}

// CleanupReport 一次 Docker 资源清理的汇总（三模式共用形状，§5.13.6）
type CleanupReport struct {
	Mode        CleanupMode   `json:"mode"`
	Items       []CleanedItem `json:"items"`
	Removed     int           `json:"removed"`
	Failed      int           `json:"failed"`
	FreedBytes  int64         `json:"freedBytes"`
	TrashPurged int           `json:"trashPurged"` // 本次顺带清空的到期回收站条目数
}

// TrashEntry 回收站条目的展示形状（7 天保留，§5.13.7）
type TrashEntry struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	OrigPath  string    `json:"origPath"`
	TrashPath string    `json:"trashPath"`
	MovedAt   time.Time `json:"movedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Expired   bool      `json:"expired"` // 已过 7 天保留期
}
