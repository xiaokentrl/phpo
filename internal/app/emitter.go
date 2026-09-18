// 事件总线：后端唯一权威向前端推送状态的唯一通道（AGENTS.md §5.6 事件协议）
package app

import "sync"

// §5.6 事件表全量 17 个事件名，与前端 api/events.ts 常量一一对应，禁止漂移
const (
	EventStateChanged        = "state:changed"
	EventServiceChanged      = "service:changed"
	EventTaskLog             = "task:log"
	EventTaskProgress        = "task:progress"
	EventTaskDone            = "task:done"
	EventUpdateAvailable     = "update:available"
	EventUpdateProgress      = "update:progress"
	EventUpdateDone          = "update:done"
	EventDockerCleanup       = "docker:cleanup"
	EventDockerOrphanFound   = "docker:orphan-found"
	EventDockerStateDrift    = "docker:state-drift"
	EventCacheHit            = "cache:hit"
	EventCacheMiss           = "cache:miss"
	EventCachePromote        = "cache:promote"
	EventCacheCorrupted      = "cache:corrupted"
	EventCacheCleanup        = "cache:cleanup"
	EventCacheTempdirCleared = "cache:tempdir-cleared"
)

// AllEvents 返回协议定义的全部事件名（用于启动自检与测试对账）
func AllEvents() []string {
	return []string{
		EventStateChanged, EventServiceChanged,
		EventTaskLog, EventTaskProgress, EventTaskDone,
		EventUpdateAvailable, EventUpdateProgress, EventUpdateDone,
		EventDockerCleanup, EventDockerOrphanFound, EventDockerStateDrift,
		EventCacheHit, EventCacheMiss, EventCachePromote,
		EventCacheCorrupted, EventCacheCleanup, EventCacheTempdirCleared,
	}
}

// Emitter 是发射事件的最小抽象；实现由装配层注入（Wails 实现见 main 包）
type Emitter interface {
	Emit(event string, payload any)
}

// NopEmitter 丢弃一切事件（单测与启动早期占位）
type NopEmitter struct{}

func (NopEmitter) Emit(string, any) {}

// CapturingEmitter 顺序记录事件，供测试断言事件名与载荷
type CapturingEmitter struct {
	mu     sync.Mutex
	events []CapturedEvent
}

type CapturedEvent struct {
	Name    string
	Payload any
}

func (c *CapturingEmitter) Emit(event string, payload any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, CapturedEvent{Name: event, Payload: payload})
}

func (c *CapturingEmitter) Capture() []CapturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CapturedEvent, len(c.events))
	copy(out, c.events)
	return out
}
