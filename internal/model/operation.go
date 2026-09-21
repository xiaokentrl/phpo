// 操作审计模型：<用户数据目录>/logs/operations.log（JSON Lines，一行一条，§5.13.10）
package model

import "time"

type Operation struct {
	TS         time.Time `json:"ts"`
	Actor      string    `json:"actor"` // 固定 ui（无 CLI）或 system
	Op         string    `json:"op"`    // 幂等操作名：install/uninstall/...
	Args       any       `json:"args"`
	Status     string    `json:"status"` // success / failed / cancelled
	DurationMs int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
	// 以下三项由任务引擎在每个三段式任务终态写入：任务账本（历史任务）的完整可读记录
	TaskID string `json:"taskId,omitempty"`
	Label  string `json:"label,omitempty"`
	Logs   string `json:"logs,omitempty"` // 任务日志原文（换行分隔，尾部截断）
}
