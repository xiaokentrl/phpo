// 事件协议对账测试：常量集合必须与 AGENTS.md §5.6 事件表逐字一致（17 项、顺序按类别）
package app

import (
	"reflect"
	"testing"
)

// 权威清单：AGENTS.md §5.6，任何增删改名必须先走总纲变更流程
func TestEventNamesMatchMasterPlan(t *testing.T) {
	want := []string{
		"state:changed", "service:changed",
		"task:log", "task:progress", "task:done",
		"update:available", "update:progress", "update:done",
		"docker:cleanup", "docker:orphan-found", "docker:state-drift",
		"cache:hit", "cache:miss", "cache:promote",
		"cache:corrupted", "cache:cleanup", "cache:tempdir-cleared",
	}
	got := AllEvents()
	if len(got) != 17 {
		t.Fatalf("事件名数量 = %d, 期望 17", len(got))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("事件名漂移:\n got=%v\nwant=%v", got, want)
	}
}

func TestCapturingEmitterRecordsOrderAndPayload(t *testing.T) {
	e := &CapturingEmitter{}
	e.Emit(EventCacheHit, map[string]any{"kind": "php", "version": "8.4"})
	e.Emit(EventStateChanged, "snapshot-1")

	got := e.Capture()
	if len(got) != 2 {
		t.Fatalf("记录数 = %d, 期望 2", len(got))
	}
	if got[0].Name != "cache:hit" {
		t.Errorf("事件[0] = %q", got[0].Name)
	}
	payload, ok := got[0].Payload.(map[string]any)
	if !ok || payload["version"] != "8.4" {
		t.Errorf("载荷丢失: %v", got[0].Payload)
	}
	if got[1].Name != "state:changed" || got[1].Payload != "snapshot-1" {
		t.Errorf("事件[1] = %+v", got[1])
	}
}

func TestNopEmitterDoesNotPanic(t *testing.T) {
	var e Emitter = NopEmitter{}
	e.Emit(EventTaskLog, nil)
}
