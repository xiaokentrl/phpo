// T305 验收：Pre-Clean 归属判定纯逻辑——空闲 / phpo 托管 / 外部冲突三态
package engine

import "testing"

func TestClassifyOwner(t *testing.T) {
	cases := []struct {
		name    string
		exists  bool
		managed bool
		want    ownerState
	}{
		{"空闲", false, false, ownerFree},
		{"空闲(托管标志无意义)", false, true, ownerFree},
		{"phpo 托管", true, true, ownerPhpo},
		{"外部资源占用", true, false, ownerForeign},
	}
	for _, c := range cases {
		if got := classifyOwner(c.exists, c.managed); got != c.want {
			t.Errorf("%s: classifyOwner(%v,%v)=%v，期望 %v", c.name, c.exists, c.managed, got, c.want)
		}
	}
}

func TestIsPhpoManaged(t *testing.T) {
	if !isPhpoManaged(map[string]string{"phpo.managed": "true"}) {
		t.Error("应识别托管标签")
	}
	if isPhpoManaged(map[string]string{"phpo.managed": "false"}) {
		t.Error("managed=false 不算托管")
	}
	if isPhpoManaged(nil) {
		t.Error("空标签不算托管")
	}
	if isPhpoManaged(map[string]string{"other": "true"}) {
		t.Error("无关标签不算托管")
	}
}
