// 任务领域模型：4 状态 · 日志 5 行类型 · §5.6 task:* 事件载荷
package model

import "time"

type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskSuccess   TaskStatus = "success"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

type LogLevel string

const (
	LogCmd  LogLevel = "cmd"
	LogMeta LogLevel = "meta"
	LogOk   LogLevel = "ok"
	LogDim  LogLevel = "dim"
	LogErr  LogLevel = "err"
)

// AllLogLevels 供对账测试（5 类）
var AllLogLevels = []LogLevel{LogCmd, LogMeta, LogOk, LogDim, LogErr}

type LogLine struct {
	Level LogLevel `json:"level"`
	Text  string   `json:"text"`
}

// ---- §5.6 事件载荷 ----

type TaskLogEvent struct {
	ID    string   `json:"id"`
	Level LogLevel `json:"level"`
	Text  string   `json:"text"`
}

type TaskProgressEvent struct {
	ID    string `json:"id"`
	Step  int    `json:"step"`
	Total int    `json:"total"`
}

type TaskDoneEvent struct {
	ID       string        `json:"id"`
	Status   TaskStatus    `json:"status"`
	Duration time.Duration `json:"duration"`
}
