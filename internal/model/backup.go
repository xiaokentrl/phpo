// 备份领域模型：~/phpo/backups/backup-*.tar.gz 的归档条目描述（前端 BackupView 直读该形状）
package model

// BackupFile 一个备份归档的展示条目；Size/At 已格式化为人类可读串，Items 为归档顶层组数
type BackupFile struct {
	File  string `json:"file"`  // 归档文件名
	Size  string `json:"size"`  // 人类可读大小
	At    string `json:"at"`    // 格式化时间 YYYY-MM-DD HH:MM
	Items int    `json:"items"` // 归档顶层内容组数
}
