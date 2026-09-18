package vhost

import "testing"

func TestPresets_NineKeys(t *testing.T) {
	ps := Presets()
	if len(ps) != 9 {
		t.Fatalf("应有 9 个预设，实得 %d", len(ps))
	}
	want := []string{"none", "laravel", "thinkphp", "yii2", "thinkcmf", "ci", "symfony", "wordpress", "custom"}
	for i, k := range want {
		if ps[i].Key != k {
			t.Fatalf("第 %d 个键应为 %s，实得 %s", i, k, ps[i].Key)
		}
	}
}

func TestRuleFor(t *testing.T) {
	// 已知键返回其规则
	if got := RuleFor("laravel", ""); got == "" || got != presetByKey["laravel"].Rule {
		t.Errorf("laravel 规则不匹配")
	}
	// custom 且非空用自定义
	if got := RuleFor("custom", "my rule"); got != "my rule" {
		t.Errorf("custom 应使用自定义规则，实得 %q", got)
	}
	// custom 空回落 custom 预设规则
	if got := RuleFor("custom", "   "); got != presetByKey["custom"].Rule {
		t.Errorf("custom 空应回落预设，实得 %q", got)
	}
	// 未知键回落 none
	if got := RuleFor("nope", ""); got != presetByKey["none"].Rule {
		t.Errorf("未知键应回落 none，实得 %q", got)
	}
}
