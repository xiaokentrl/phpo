// Docker 镜像源单测：地址归一与形状校验（写入唯一判据）+ config.yaml 落库往返 + 读盘侧剔除坏值。
//
// 冻结口径：设置页文本框、preflight、写库三处共用 ValidateRegistryHosts 这一份判据，不另立标准；
// 空清单合法，语义即「不改写镜像名、直连官方」。可达性**不在**此处判——选了连不上的源，
// 用户在拉取那一步会看到逐源点名的失败并自动回落直连官方。
package config

import (
	"os"
	"strings"
	"testing"
)

func TestNormalizeRegistryHost(t *testing.T) {
	cases := map[string]string{
		"docker.m.daocloud.io":             "docker.m.daocloud.io",
		"  https://docker.m.daocloud.io  ": "docker.m.daocloud.io", // 用户照抄浏览器地址
		"http://127.0.0.1:5000":            "127.0.0.1:5000",       // 本机明文 registry
		"registry.example.com/":            "registry.example.com", // 结尾斜杠
		"registry.example.com/v2":          "registry.example.com", // 照抄 docker login 那行
		"https://mirror.local:443/v2":      "mirror.local:443",     // 前缀 + 端口 + /v2
		"mirror.local/v2/":                 "mirror.local",         // /v2 后再带斜杠
	}
	for in, want := range cases {
		if got := NormalizeRegistryHost(in); got != want {
			t.Errorf("NormalizeRegistryHost(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestValidateRegistryHost(t *testing.T) {
	ok := []string{"docker.io", "registry-1.docker.io", "127.0.0.1:5000", "mirror.local:8443", "a-b.example.cn"}
	for _, s := range ok {
		if got, err := ValidateRegistryHost(s); err != nil || got != s {
			t.Errorf("合法地址 %q 被拒: got=%q err=%v", s, got, err)
		}
	}
	bad := []string{"", "   ", "bad host", "https://x.com/path", "a/../b", "../etc", "x.com:port"}
	for _, s := range bad {
		if _, err := ValidateRegistryHost(s); err == nil {
			t.Errorf("非法地址 %q 未被拒", s)
		} else if !strings.Contains(err.Error(), ErrRegistryHost) {
			t.Errorf("非法地址 %q 的错误缺少统一前缀: %v", s, err)
		}
	}
	// 最小限制：归一后可放行的斜杠、形状对但从没测过的端口，都不该在此处拦下
	if got, err := ValidateRegistryHost("registry.example.com/"); err != nil || got != "registry.example.com" {
		t.Errorf("结尾斜杠应归一后放行，得 %q err=%v", got, err)
	}
	if got, err := ValidateRegistryHost("1.2.3.4:99999"); err != nil || got != "1.2.3.4:99999" {
		t.Errorf("形状合法的端口即放行（可达性交给探测），得 %q err=%v", got, err)
	}
}

// 清单级：忽略空行（文本框末尾换行是常态）、保序去重、任一行非法即报错点名首个坏值、永远返回非 nil
func TestValidateRegistryHosts(t *testing.T) {
	got, err := ValidateRegistryHosts([]string{"", " docker.io ", "https://docker.io", "mirror.local:5000", "", "\t"})
	if err != nil {
		t.Fatalf("空行与重复项不该报错: %v", err)
	}
	if len(got) != 2 || got[0] != "docker.io" || got[1] != "mirror.local:5000" {
		t.Errorf("应忽略空行、去重并保持顺序，得 %q", got)
	}
	// 全空即「清除」：非 nil 空切片，语义为不改写镜像名
	empty, err := ValidateRegistryHosts([]string{"", "  ", "\n"})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("全空清单应回落空切片且无错，得 %#v err=%v", empty, err)
	}
	// 坏值点名
	for _, bad := range [][]string{{"docker.io", "not a host"}, {"a/../b"}} {
		if _, err := ValidateRegistryHosts(bad); err == nil {
			t.Errorf("含坏值 %q 应报错", bad)
		} else if !strings.Contains(err.Error(), ErrRegistryHost) {
			t.Errorf("坏值 %q 的错误应点名地址： %v", bad, err)
		}
	}
}

func TestConfigStoreSetDockerSourcesRoundTrip(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	cs := newStoreAt(path)
	if got := cs.DockerSources(); got == nil || len(got) != 0 {
		t.Fatalf("未配置时应为空清单（非 nil），得 %#v", got)
	}
	// 先放一份别的服务设置：保存镜像源不得把用户已有的密码一起写没（config.yaml 是唯一配置权威）
	if err := cs.SetPassword("mysql", "8.4", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetDockerSources([]string{" https://docker.m.daocloud.io ", "", "dockerproxy.net", "docker.m.daocloud.io"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker.m.daocloud.io", "dockerproxy.net"}
	if got := cs.DockerSources(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("内存回显 = %q，期望 %q", got, want)
	}
	// 重新从磁盘载入：落盘的确实是归一后的值，且带 0777（§5.20）
	reloaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.DockerSources(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("落盘往返 = %q，期望 %q", got, want)
	}
	if pw, ok, err := reloaded.GetPassword("mysql", "8.4"); err != nil || !ok || pw != "s3cr3t" {
		t.Errorf("保存镜像源后 mysql/8.4 的密码应还在，得 %q ok=%v err=%v", pw, ok, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o777 {
		t.Errorf("config.yaml 权限应为 0777，得 %v err=%v", fi.Mode(), err)
	}
	// 清空即回落「不改写」
	if err := cs.SetDockerSources(nil); err != nil {
		t.Fatal(err)
	}
	if got := cs.DockerSources(); len(got) != 0 {
		t.Errorf("清空后应回落空清单，得 %q", got)
	}
	// 非法项必须报错且不改动已存的清单（写坏了不能把旧值一起丢了）
	if err := cs.SetDockerSources([]string{"ok.example.com", "https://x.example.com/a/b"}); err == nil {
		t.Fatal("带路径的坏值应报错")
	}
	if got := cs.DockerSources(); len(got) != 0 {
		t.Errorf("报错后清单不应被半写，得 %q", got)
	}
}

// 读盘侧归一：用户手改 config.yaml 留下的坏值静默剔除，不得让整个应用启动失败
func TestConfigStoreDockerSourcesDropsHandEditedGarbage(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	raw := "phpo_home: /h\nwww_root: /w\ndocker_sources:\n    - good.example.com\n    - \"https://good.example.com\"\n    - \"bad host\"\n    - \"\"\n    - ../etc\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("载入带坏值的 config.yaml 不得报错: %v", err)
	}
	got := cs.DockerSources()
	if len(got) != 1 || got[0] != "good.example.com" {
		t.Errorf("应只留下合法且去重后的一项，得 %q", got)
	}
}
