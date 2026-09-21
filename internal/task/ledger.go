// 任务账本：每个任务的终态在 Manager 出口一处落账（复用 §5.13.10 operations 审计表），
// 使用户在任务结束后仍能回看「成功 / 失败 / 失败原因 / 等效日志原文」。
package task

import (
	"strings"
	"sync"
	"time"

	"phpo/internal/model"
)

// maxLedgerLines 落账日志保留的最大行数（取尾部）：长任务的进度行不得让账本无界增长
const maxLedgerLines = 500

// Recorder 任务终态落账抽象（*store.Store 的 AppendOperation 满足）
type Recorder interface {
	AppendOperation(model.Operation) error
}

// logSink 收集本任务发出的 task:log 行（步骤内并发发日志也安全）
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *logSink) add(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, text)
}

// tail 返回最后 n 行（换行分隔）
func (s *logSink) tail(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) > n {
		return strings.Join(s.lines[len(s.lines)-n:], "\n")
	}
	return strings.Join(s.lines, "\n")
}

// recordingEmitter 透传事件的同时把 task:log 载荷收进 sink
type recordingEmitter struct {
	em   Emitter
	sink *logSink
}

func (r *recordingEmitter) Emit(event string, payload any) {
	if l, ok := payload.(model.TaskLogEvent); ok {
		r.sink.add(l.Text)
	}
	r.em.Emit(event, payload)
}

// ledgerOpOf 任务的操作名：优先 Meta.Type（install / site-add / ...），退化为任务 ID 前缀
func ledgerOpOf(t *Task) string {
	if t.Meta.Type != "" {
		return t.Meta.Type
	}
	if i := strings.LastIndex(t.ID, "-"); i > 0 {
		return t.ID[:i]
	}
	return t.ID
}

func (m *Manager) record(t *Task, status model.TaskStatus, runErr error, d time.Duration, sink *logSink) {
	if m.rec == nil || status == "" {
		return
	}
	op := model.Operation{
		TS:         time.Now().UTC(),
		Actor:      "ui",
		Op:         ledgerOpOf(t),
		Args:       map[string]string{"id": t.ID},
		Status:     string(status),
		DurationMs: d.Milliseconds(),
		TaskID:     t.ID,
		Label:      label(t),
		Logs:       sink.tail(maxLedgerLines),
	}
	if runErr != nil {
		op.Error = runErr.Error()
	}
	_ = m.rec.AppendOperation(op) // 账本写失败只影响历史可见性，绝不改变任务结果
}
