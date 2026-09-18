// M1 mock 事件发射器：后端任务/缓存/升级引擎真发射前的定时器替身（§5.6 全量 17 事件）
// 仅在显式开启时挂载（PHPO_MOCK_EVENTS=1），默认不干扰真实装配。
package app

import (
	"context"
	"time"

	"phpo/internal/model"
)

// mockPayloads 为每个事件构造一份代表性载荷，形状对齐 §5.6 事件表与前端 api/events.ts 类型。
func mockPayloads() map[string]any {
	snap := model.NewSnapshot()
	snap.Installed["php"] = []string{"8.5", "8.4", "8.3"}
	snap.Running["php"] = []string{"8.5", "8.4"}
	return map[string]any{
		EventStateChanged:        map[string]any{"snapshot": snap},
		EventServiceChanged:      map[string]any{"kind": "pgsql", "version": "17", "running": true},
		EventTaskLog:             map[string]any{"id": "t-1", "level": "ok", "text": "  ✓ mock log line"},
		EventTaskProgress:        map[string]any{"id": "t-1", "step": 3, "total": 6},
		EventTaskDone:            map[string]any{"id": "t-1", "status": "success", "duration": 2.4},
		EventUpdateAvailable:     map[string]any{"version": "9.9.9", "changelog": "mock", "size": int64(42000000)},
		EventUpdateProgress:      map[string]any{"stage": "downloading", "percent": 55, "speed": int64(1200000)},
		EventUpdateDone:          map[string]any{"status": "success", "version": "9.9.9"},
		EventDockerCleanup:       map[string]any{"stage": "prune", "resource": "phpo-php-8.3", "action": "removed"},
		EventDockerOrphanFound:   map[string]any{"resources": []any{map[string]any{"name": "phpo-redis-7"}}},
		EventDockerStateDrift:    map[string]any{"expected": "running", "actual": "stopped"},
		EventCacheHit:            map[string]any{"kind": "php", "version": "8.4", "source": "offline", "size": int64(450000000)},
		EventCacheMiss:           map[string]any{"kind": "php", "version": "8.5", "action": "pull"},
		EventCachePromote:        map[string]any{"kind": "php", "version": "8.5", "entries": []any{map[string]any{"name": "image.tar"}}},
		EventCacheCorrupted:      map[string]any{"kind": "mysql", "version": "8.4", "entry": map[string]any{"name": "image.tar"}},
		EventCacheCleanup:        map[string]any{"mode": "standard", "freed_bytes": int64(128000000)},
		EventCacheTempdirCleared: map[string]any{"path": "~/phpo/php/8.5/ext", "reason": "compile_done"},
	}
}

// EmitAllOnce 按 §5.6 协议顺序把 17 个事件各发射一次。
func EmitAllOnce(e Emitter) {
	payloads := mockPayloads()
	for _, name := range AllEvents() {
		e.Emit(name, payloads[name])
	}
}

// StartMockTicker 以固定间隔反复发射全量事件，直至 ctx 取消；用于开发期前端联调。
func StartMockTicker(ctx context.Context, e Emitter, interval time.Duration) {
	EmitAllOnce(e)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				EmitAllOnce(e)
			}
		}
	}()
}
