// 操作审计模型：~/.phpo/logs/operations.log（JSON Lines，一行一条，§5.13.10）
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
}
