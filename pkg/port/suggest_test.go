// pkg/port 顺延链与 validatePort 语义测试（对照原型 1647–1684 行）
package port

import (
	"strings"
	"testing"

	"phpo/pkg/errs"
)

func TestFindNextAvailableUpThenWrap(t *testing.T) {
	used := Used{80: "site a", 81: "mysql 8.4", 82: "site b"}
	if got := FindNextAvailable(80, 179, 80, used); got == nil || *got != 83 {
		t.Errorf("80 起顺延应得 83, got %v", got)
	}
	// desired=179 → 上界无 → 回绕到 min..178
	full := Used{}
	for p := 81; p <= 179; p++ {
		full[p] = "x"
	}
	if got := FindNextAvailable(179, 179, 80, full); got == nil || *got != 80 {
		t.Errorf("179 应回绕至 80, got %v", got)
	}
	// 全段耗尽
	for p := 80; p <= 179; p++ {
		full[p] = "x"
	}
	if got := FindNextAvailable(80, 179, 80, full); got != nil {
		t.Errorf("耗尽应得 nil, got %d", *got)
	}
}

func TestValidatePortBranches(t *testing.T) {
	used := Used{80: "site demo.test", 3306: "mysql 8.4"}
	cases := []struct {
		name       string
		raw        string
		opts       Options
		wantOK     bool
		wantValue  int
		wantAdj    bool
		wantMsgPre string
	}{
		{"空闲直通", "8080", Options{}, true, 8080, false, ""},
		{"格式非法-字母", "8a", Options{}, false, 0, false, errs.PortInvalid},
		{"格式非法-超5位", "123456", Options{}, false, 0, false, errs.PortInvalid},
		{"范围-0", "0", Options{}, false, 0, false, errs.PortInvalid},
		{"范围-65536", "65536", Options{}, false, 0, false, errs.PortInvalid},
		{"占用无顺延", "80", Options{}, false, 0, false, errs.PortInUse},
		{"占用但被排除", "80", Options{Exclude: []int{80}}, true, 80, false, ""},
		{"占用顺延", "80", Options{AutoAdvance: true}, true, 81, true, ""},
		// 站点端口占用：全范围向上顺延至首个可用（3307 空闲即返回，不再受窗口限制）
		{"服务端口号顺延向上", "3306", Options{AutoAdvance: true}, true, 3307, true, ""},
	}
	for _, c := range cases {
		got := Validate(c.raw, used, c.opts)
		if got.Ok != c.wantOK {
			t.Errorf("%s: Ok=%v 期望 %v (msg=%q)", c.name, got.Ok, c.wantOK, got.Msg)
			continue
		}
		if c.wantOK && got.Value != c.wantValue {
			t.Errorf("%s: Value=%d 期望 %d", c.name, got.Value, c.wantValue)
		}
		if c.wantOK && got.Adjusted != c.wantAdj {
			t.Errorf("%s: Adjusted=%v 期望 %v", c.name, got.Adjusted, c.wantAdj)
		}
		if !c.wantOK && !strings.HasPrefix(got.Msg, c.wantMsgPre) {
			t.Errorf("%s: Msg=%q 应以 %q 开头", c.name, got.Msg, c.wantMsgPre)
		}
	}
}

// TestValidateKeepOnConflict 新建站点端口冲突口径（总纲 §5.8）：保留用户所填端口、只标记占用，不顺延、不判失败
func TestValidateKeepOnConflict(t *testing.T) {
	used := Used{80: "site demo.test", 3306: "mysql 8.4"}
	got := Validate("80", used, Options{KeepOnConflict: true})
	if !got.Ok || got.Value != 80 || got.Adjusted || !got.Occupied {
		t.Fatalf("占用应保留原端口并标记 Occupied，实得 %+v", got)
	}
	if !strings.HasPrefix(got.Msg, errs.PortInUse) {
		t.Errorf("Msg=%q 应以 %q 开头（供上层拼降级告警）", got.Msg, errs.PortInUse)
	}
	// 空闲端口不受影响：不标占用、不改值
	if r := Validate("8080", used, Options{KeepOnConflict: true}); !r.Ok || r.Occupied || r.Value != 8080 {
		t.Errorf("空闲端口应直通: %+v", r)
	}
	// 自身端口排除后视为空闲
	if r := Validate("80", used, Options{Exclude: []int{80}, KeepOnConflict: true}); !r.Ok || r.Occupied {
		t.Errorf("被排除的端口不应判占用: %+v", r)
	}
	// 格式/范围非法仍阻断（占用降级不放行非法端口）
	if r := Validate("99999", used, Options{KeepOnConflict: true}); r.Ok || r.Msg != errs.PortInvalid {
		t.Errorf("非法端口仍须阻断: %+v", r)
	}
}

func TestValidateAdjustedCarriesOriginal(t *testing.T) {
	used := Used{80: "site a"}
	got := Validate("80", used, Options{AutoAdvance: true})
	if !got.Adjusted || got.Original != 80 || got.Value != 81 {
		t.Fatalf("adjusted 链错误: %+v", got)
	}
}

func TestValidateOccupiedAlwaysAdvances(t *testing.T) {
	// 80–179 占满：不再报耗尽，全范围向上顺延至首个可用（180）
	used := Used{}
	for p := 80; p <= 179; p++ {
		used[p] = "x"
	}
	got := Validate("100", used, Options{AutoAdvance: true})
	if !got.Ok || got.Value != 180 || !got.Adjusted || got.Original != 100 {
		t.Errorf("窗口占满应顺延至 180, got %+v", got)
	}
	// desired 高于窗口：直接向上找首个可用（8889 空闲）
	got = Validate("8888", Used{8888: "x"}, Options{AutoAdvance: true})
	if !got.Ok || got.Value != 8889 || !got.Adjusted || got.Original != 8888 {
		t.Errorf("8888 占用应顺延至 8889, got %+v", got)
	}
}

func TestValidateEmptyAndSpaces(t *testing.T) {
	if r := Validate("", nil, Options{}); r.Ok || r.Msg != errs.PortInvalid {
		t.Errorf("空端口应报格式错: %+v", r)
	}
	if r := Validate(" 443 ", Used{}, Options{}); !r.Ok || r.Value != 443 {
		t.Errorf("trim 后 443 应合法: %+v", r)
	}
}
