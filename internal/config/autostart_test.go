package config

import (
	"os"
	"strings"
	"testing"
)

// 开机自启清单的落盘与回显：勾选集是「全量覆盖」，取消只翻标记，不带走端口/密码/数据目录。
func TestConfigStoreAutostartRefsRoundTrip(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	cs := newStoreAt(path)
	if got := cs.AutostartRefs(); got != nil {
		t.Fatalf("未配置时应返回 nil（前端据此显示未勾选），得 %#v", got)
	}
	// 已有端口/数据目录的设置：勾选自启不得改写它们
	if err := cs.SetServicePort("mysql", "8.4", 3307); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetDataDir("redis", "8", "/srv/redis8"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetAutostartRefs([]string{"mysql/8.4", "redis/8", "nginx/alpine"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"mysql/8.4", "nginx/alpine", "redis/8"}
	if got := cs.AutostartRefs(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("回显 = %q，期望 %q", got, want)
	}
	// 此前没有任何配置记录的 nginx/alpine：勾选时按当前生效的默认密码落定，
	// 免得留一个空密码被下次重建容器当成「用户刻意设了空密码」
	if pw, ok, err := cs.GetPassword("nginx", "alpine"); err != nil || !ok || pw != DefaultPassword {
		t.Errorf("新勾选的记录应落定当前生效的默认密码，得 %q ok=%v err=%v", pw, ok, err)
	}
	// 已有记录（mysql/8.4 只有端口）：勾选不替它编一个密码，读回来仍是「未设置」
	if pw, ok, err := cs.GetPassword("mysql", "8.4"); err != nil || pw != "" {
		t.Errorf("已有记录不得被补写密码，得 %q ok=%v err=%v", pw, ok, err)
	}
	// 重新载入：落盘的确实是勾选集
	reloaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.AutostartRefs(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("落盘往返 = %q，期望 %q", got, want)
	}
	if p, ok, err := reloaded.GetServicePort("mysql", "8.4"); err != nil || !ok || p != 3307 {
		t.Errorf("勾选自启不得改端口，得 %d ok=%v err=%v", p, ok, err)
	}
	if d := reloaded.DataDir("redis", "8"); d != "/srv/redis8" {
		t.Errorf("勾选自启不得改数据目录，得 %q", d)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o777 {
		t.Errorf("config.yaml 权限应为 0777，得 %v err=%v", fi.Mode(), err)
	}
}

// 全量覆盖：第二次保存没带上的那一档必须取消，且取消不带走它的端口/密码。
func TestConfigStoreAutostartUncheckKeepsOtherFields(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	cs := newStoreAt(path)
	if err := cs.SetPassword("pgsql", "17", "pw-for-pg"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetAutostartRefs([]string{"pgsql/17", "redis/8"}); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetAutostartRefs([]string{"redis/8"}); err != nil {
		t.Fatal(err)
	}
	if got := cs.AutostartRefs(); strings.Join(got, ",") != "redis/8" {
		t.Errorf("取消后只剩 redis/8，得 %q", got)
	}
	if pw, ok, err := cs.GetPassword("pgsql", "17"); err != nil || !ok || pw != "pw-for-pg" {
		t.Errorf("取消自启不得清掉密码，得 %q ok=%v err=%v", pw, ok, err)
	}
	// 清空全部勾选
	if err := cs.SetAutostartRefs(nil); err != nil {
		t.Fatal(err)
	}
	if got := cs.AutostartRefs(); len(got) != 0 {
		t.Errorf("清空后应为空，得 %q", got)
	}
	if pw, ok, _ := cs.GetPassword("pgsql", "17"); !ok || pw != "pw-for-pg" {
		t.Errorf("清空勾选后 pgsql/17 的配置应原样还在，得 %q ok=%v", pw, ok)
	}
}

// 形状不合法的引用点名报错，且不半写：已有的勾选集不能因为一个坏值被改掉。
func TestConfigStoreAutostartRejectsMalformedRefs(t *testing.T) {
	cs := newStoreAt(t.TempDir() + "/config.yaml")
	if err := cs.SetAutostartRefs([]string{"redis/8"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"redis", "redis/", "/8", "redis/8/data", "", "   "} {
		if err := cs.SetAutostartRefs([]string{bad}); err == nil {
			t.Errorf("坏引用 %q 应报错", bad)
		}
	}
	if got := cs.AutostartRefs(); strings.Join(got, ",") != "redis/8" {
		t.Errorf("报错后勾选集不应被半写，得 %q", got)
	}
}

// 同 kind/version 重复出现只算一次；引用两侧空白在 SplitSvcRef 里已去掉。
func TestConfigStoreAutostartDedupesTrimmedRefs(t *testing.T) {
	cs := newStoreAt(t.TempDir() + "/config.yaml")
	if err := cs.SetAutostartRefs([]string{" mysql/8.4", "mysql/8.4", "nginx/alpine"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"mysql/8.4", "nginx/alpine"}
	if got := cs.AutostartRefs(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("去重后 = %q，期望 %q", got, want)
	}
}

func TestSplitSvcRefShapes(t *testing.T) {
	cases := []struct {
		in        string
		kind, ver string
		ok        bool
	}{
		{"php/8.4", "php", "8.4", true},
		{" pgsql/17 ", "pgsql", "17", true},
		{"redis", "", "", false},
		{"redis/", "", "", false},
		{"/8", "", "", false},
		{"redis/8/data", "", "", false}, // 版本不带斜杠：多一段就是写坏了，不认
		{"redis//8", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := SplitSvcRef(c.in)
		if ok != c.ok || k != c.kind || v != c.ver {
			t.Errorf("SplitSvcRef(%q) = (%q,%q,%v)，期望 (%q,%q,%v)", c.in, k, v, ok, c.kind, c.ver, c.ok)
		}
	}
	if got := JoinSvcRef("mysql", "8.4"); got != "mysql/8.4" {
		t.Errorf("JoinSvcRef = %q", got)
	}
}
