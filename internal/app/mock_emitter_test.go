// mock 事件发射测试：EmitAllOnce 必须按 §5.6 顺序逐一发射全部 17 个事件名
package app

import (
	"reflect"
	"testing"
)

func TestEmitAllOnceCoversAllEventsInOrder(t *testing.T) {
	e := &CapturingEmitter{}
	EmitAllOnce(e)

	got := e.Capture()
	if len(got) != 17 {
		t.Fatalf("发射数 = %d, 期望 17", len(got))
	}
	names := make([]string, len(got))
	for i, ev := range got {
		names[i] = ev.Name
		if ev.Payload == nil {
			t.Errorf("事件 %s 载荷为空", ev.Name)
		}
	}
	if want := AllEvents(); !reflect.DeepEqual(names, want) {
		t.Errorf("事件名顺序漂移:\n got=%v\nwant=%v", names, want)
	}
}
