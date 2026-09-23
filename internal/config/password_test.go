// 密码策略测试：零校验 + 明文 YAML 往返（任意字符 / 空值 / 超长均为合法密码，§1.5）
package config

import (
	"os"
	"strings"
	"testing"
)

func TestPasswordPolicy(t *testing.T) {
	if DefaultPassword != "123456" {
		t.Fatalf("默认密码必须为 123456（§0.3），got %q", DefaultPassword)
	}
	p := GenPassword()
	if len(p) != 16 {
		t.Errorf("GenPassword 长度 = %d, 期望 16 位 hex", len(p))
	}
	if p == GenPassword() {
		t.Error("两次随机密码不应相同")
	}
}

// newStoreAt 测试用：把 ConfigStore 指到临时路径（同包内可直接构造）
func newStoreAt(path string) *ConfigStore { return &ConfigStore{path: path} }

func TestConfigStoreHostilePasswordsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	cases := map[[2]string]string{
		{"mysql", "8.4"}:    "123456",
		{"redis", "8"}:      "",                              // 空密码合法
		{"pgsql", "17"}:     "with spaces and = equals sign", // 含空格与 =
		{"php", "8.4"}:      "#notacomment",                  // 值以 # 开头
		{"nginx", "alpine"}: "tab\tand newline",
	}
	cs := newStoreAt(path)
	for kv, pw := range cases {
		if err := cs.SetPassword(kv[0], kv[1], pw); err != nil {
			t.Fatal(err)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o777 {
		t.Errorf("config.yaml 权限应为 777（phpo 产出物一律 0777，见总纲「文件权限策略」）, got %v %v", fi.Mode(), err)
	}
	// 重新从磁盘载入，逐项核对明文原样往返
	reloaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for kv, want := range cases {
		got, ok, _ := reloaded.GetPassword(kv[0], kv[1])
		if !ok {
			t.Errorf("%s %s 密码键丢失", kv[0], kv[1])
			continue
		}
		if got != want {
			t.Errorf("%s %s 往返失败: %q → %q", kv[0], kv[1], want, got)
		}
	}
}

func TestConfigStoreMissingFileIsEmpty(t *testing.T) {
	cs := newStoreAt(t.TempDir() + "/nope.yaml")
	if _, ok, err := cs.GetPassword("mysql", "8.4"); ok || err != nil {
		t.Errorf("缺文件时 GetPassword 应 exists=false 无错，得 ok=%v err=%v", ok, err)
	}
	if h, w := cs.Roots(); h != "" || w != "" {
		t.Errorf("缺文件时根应为空，得 %q %q", h, w)
	}
}

func TestConfigStoreLongPasswordUnbounded(t *testing.T) {
	cs := newStoreAt(t.TempDir() + "/config.yaml")
	long := strings.Repeat("x", 200) // 200 位密码（§1.5：任意长度合法）
	if err := cs.SetPassword("mysql", "8.4", long); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadFromPath(cs.path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := reloaded.GetPassword("mysql", "8.4"); got != long {
		t.Error("超长密码往返丢失")
	}
}
