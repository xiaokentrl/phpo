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

// Logf 发射 task:log。除原文外附带消息码（§5.6 多语言 Phase 3）：
// 这一行的中文原文照旧发出——任务账本存的就是它，前端在没有消息码时也显示它；
// code/params 只是给界面多一个「按用户语言说同一句话」的记号，认不出记号的行照原文显示。
func Logf(e Emitter, id string, level model.LogLevel, text string) {
	code, params := LookupLine(text)
	e.Emit("task:log", model.TaskLogEvent{ID: id, Level: level, Text: text, Code: code, Params: params})
}

// LogCode 发射一行已确定消息码的日志（框架行这类原文与参数都在调用点现拼的走这条）。
func LogCode(e Emitter, id string, level model.LogLevel, code string, params map[string]string, text string) {
	e.Emit("task:log", model.TaskLogEvent{ID: id, Level: level, Text: text, Code: code, Params: params})
}

// Progressf 发射 task:progress
func Progressf(e Emitter, id string, step, total int) {
	e.Emit("task:progress", model.TaskProgressEvent{ID: id, Step: step, Total: total})
}

// Donef 发射 task:done
func Donef(e Emitter, id string, status model.TaskStatus, d time.Duration) {
	e.Emit("task:done", model.TaskDoneEvent{ID: id, Status: status, Duration: d})
}
