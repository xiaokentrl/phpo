// 版本策略边界测试（§5.4 唯一校验规则）：128/129 上限、路径分隔符、控制字符、trim
package config

import (
	"strings"
	"testing"
)

func TestValidateVersion(t *testing.T) {
	long128 := strings.Repeat("v", 128)
	long129 := strings.Repeat("v", 129)
	cases := []struct {
		in     string
		wantOK bool
		want   string
	}{
		{"8.4", true, "8.4"},
		{" 8.4.3 ", true, "8.4.3"}, // 首尾 trim
		{"latest", true, "latest"}, // 不限字符集
		{"my_ver-1.0+build", true, "my_ver-1.0+build"},
		{long128, true, long128}, // 边界：恰好 128
		{long129, false, ""},     // 边界：129 拒绝
		{"", false, ""},          // 空拒绝
		{"   ", false, ""},       // 全空白 trim 后为空
		{"../etc", false, ""},    // .. 穿越
		{"a..b", false, ""},      // 含 .. 即拒
		{".", false, ""},
		{"..", false, ""},
		{"a/b", false, ""}, // 路径分隔符
		{"a\\b", false, ""},
		{"a\x00b", false, ""},  // NUL
		{"a\tb", false, ""},    // 空白
		{"a b", false, ""},     // 空格
		{"8.4\n", true, "8.4"}, // 尾部换行被 trim 掉（trim 在先）
		{"日本語バージョン", true, "日本語バージョン"}, // 非 ASCII 合法
	}
	for _, c := range cases {
		got := ValidateVersion("php", c.in)
		if got.Ok != c.wantOK {
			t.Errorf("ValidateVersion(%q).Ok = %v, 期望 %v (msg=%q)", c.in, got.Ok, c.wantOK, got.Msg)
			continue
		}
		if c.wantOK && got.Value != c.want {
			t.Errorf("ValidateVersion(%q).Value = %q, 期望 %q", c.in, got.Value, c.want)
		}
		if !c.wantOK && got.Msg == "" {
			t.Errorf("ValidateVersion(%q) 拒绝但无消息", c.in)
		}
	}
}

func TestMaxVersionLenIsFrozen(t *testing.T) {
	if MaxVersionLen != 128 {
		t.Fatalf("总纲 §0.3 权威值为 128，禁止漂移: %d", MaxVersionLen)
	}
}
