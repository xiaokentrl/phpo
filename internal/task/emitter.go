// 任务事件发射器（本地最小接口，避免 task 反向依赖装配层 app）
// 结构等价于 app.Emitter；装配层实现注入
package task

import (
	"time"

	"phpo/internal/model"
)

// Emitter 推送 task:* 事件的抽象
type Emitter interface {
	Emit(event string, payload any)
}

// NopEmitter 单测/启动早期占位
type NopEmitter struct{}

func (NopEmitter) Emit(string, any) {}

// Logf 发射 task:log
func Logf(e Emitter, id string, level model.LogLevel, text string) {
	e.Emit("task:log", model.TaskLogEvent{ID: id, Level: level, Text: text})
}

// Progressf 发射 task:progress
func Progressf(e Emitter, id string, step, total int) {
	e.Emit("task:progress", model.TaskProgressEvent{ID: id, Step: step, Total: total})
}

// Donef 发射 task:done
func Donef(e Emitter, id string, status model.TaskStatus, d time.Duration) {
	e.Emit("task:done", model.TaskDoneEvent{ID: id, Status: status, Duration: d})
}
