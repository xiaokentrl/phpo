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

// ---- 任务面板（快照字段，随 state:changed 实时推送；不落库）----

// TaskBrief 面板上的一条任务摘要：所在分区（Running / Pending）即其排队态，
// 不新增第 5 个任务状态（§0.3 任务状态冻结为 4 个）。
type TaskBrief struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Type      string    `json:"type"`              // install / start / site-add / ...
	Kind      string    `json:"kind,omitempty"`    // 服务类任务的目标种类：卡片据此亮「运行中…」
	Version   string    `json:"version,omitempty"` // 服务类任务的目标版本
	Domain    string    `json:"domain,omitempty"`  // 站点类任务的目标域名：站点列表行据此亮「运行中…」
	Step      int       `json:"step"`              // 已完成步骤数
	Total     int       `json:"total"`
	StartedAt time.Time `json:"startedAt"`
}

// TaskBoard 任务队列详情：当前运行任务 + 其后 FIFO 排队项
type TaskBoard struct {
	Running *TaskBrief  `json:"running"`
	Pending []TaskBrief `json:"pending"`
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
