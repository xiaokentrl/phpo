// T305 验收：VerifyState 纯比对——缺项 / 多余 / 停止 / 意外运行 / 完全一致，输出有序确定
package engine

import (
	"reflect"
	"testing"
)

func ref(kind, ver string) ContainerRef { return ContainerRef{Kind: kind, Version: ver} }
func act(kind, ver string, running bool) ActualState {
	return ActualState{Ref: ref(kind, ver), Running: running}
}

func TestVerifyState_Match(t *testing.T) {
	installed := []ContainerRef{ref("php", "8.4"), ref("mysql", "8.4")}
	running := []ContainerRef{ref("php", "8.4")}
	actual := []ActualState{act("php", "8.4", true), act("mysql", "8.4", false)}
	if d := VerifyState(installed, running, actual); !d.Empty() {
		t.Fatalf("期望一致，实得漂移 %+v", d)
	}
}

func TestVerifyState_DriftBuckets(t *testing.T) {
	installed := []ContainerRef{ref("php", "8.4"), ref("php", "8.3"), ref("redis", "8")}
	running := []ContainerRef{ref("php", "8.4"), ref("redis", "8")} // php8.4 期望在跑；实际停了
	// 实际：php8.3 在跑（意外运行）、php8.4 停止、nginx 多余（孤儿）、redis 缺失
	actual := []ActualState{
		act("php", "8.3", true),
		act("php", "8.4", false),
		act("nginx", "alpine", true),
	}
	d := VerifyState(installed, running, actual)

	wantMissing := []string{"phpo-redis-8"}
	wantExtra := []string{"phpo-nginx-alpine"}
	wantStopped := []string{"phpo-php-8.4"}
	wantUnexpected := []string{"phpo-php-8.3"}
	if !reflect.DeepEqual(d.Missing, wantMissing) {
		t.Errorf("Missing = %v，期望 %v", d.Missing, wantMissing)
	}
	if !reflect.DeepEqual(d.Extra, wantExtra) {
		t.Errorf("Extra = %v，期望 %v", d.Extra, wantExtra)
	}
	if !reflect.DeepEqual(d.Stopped, wantStopped) {
		t.Errorf("Stopped = %v，期望 %v", d.Stopped, wantStopped)
	}
	if !reflect.DeepEqual(d.Unexpected, wantUnexpected) {
		t.Errorf("Unexpected = %v，期望 %v", d.Unexpected, wantUnexpected)
	}
	if d.Empty() {
		t.Error("有漂移时 Empty 应为 false")
	}
}

func TestVerifyState_EmptyInputs(t *testing.T) {
	if d := VerifyState(nil, nil, nil); !d.Empty() {
		t.Fatalf("全空应无漂移，得 %+v", d)
	}
	// 仅有实际、无期望 → 全部计为多余（残留）
	d := VerifyState(nil, nil, []ActualState{act("php", "8.4", true)})
	if !reflect.DeepEqual(d.Extra, []string{"phpo-php-8.4"}) {
		t.Errorf("孤儿容器应进 Extra，得 %+v", d)
	}
}

func TestContainerRefName(t *testing.T) {
	if got := ref("php", "8.4").Name(); got != "phpo-php-8.4" {
		t.Errorf("Name = %q，期望 phpo-php-8.4", got)
	}
}
