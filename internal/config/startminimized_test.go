// 「启动后最小化」的落盘与回显：设置页那一颗开关的唯一真相在 config.yaml
package config

import (
	"os"
	"strings"
	"testing"
)

// 未勾选即 false（首启没有这一行）；勾选后重新载入仍为 true——建窗那一刻只认落盘值。
func TestConfigStoreStartMinimizedRoundTrip(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	cs := newStoreAt(path)
	if cs.StartMinimized() {
		t.Fatal("首启未配置应为 false（正常显示主窗口）")
	}
	if err := cs.SetStartMinimized(true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "start_minimized: true") {
		t.Errorf("勾选应落出 start_minimized: true，实际配置：\n%s", b)
	}
	reloaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.StartMinimized() {
		t.Error("重新载入后应仍为 true")
	}

	// 取消勾选：值回落 false 且带 omitempty 不留残行（下次升级读旧配置不会被一个 false 绊住）
	if err := reloaded.SetStartMinimized(false); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "start_minimized") {
		t.Errorf("取消后不得留下 start_minimized 残行，实际配置：\n%s", b)
	}
	again, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.StartMinimized() {
		t.Error("取消后重新载入应为 false")
	}
}

// 这颗开关不得牵动别的配置面：两根、镜像源、服务勾选原样还在。
func TestConfigStoreStartMinimizedKeepsOtherSettings(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	cs := newStoreAt(path)
	if err := cs.SetRoots("/h/min", "/w/min"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetDockerSources([]string{"docker.m.daocloud.io"}); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetAutostartRefs([]string{"redis/8"}); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetStartMinimized(true); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Env().PHPOHome; got != "/h/min" {
		t.Errorf("PHPO_HOME 被改写了: %q", got)
	}
	if got := strings.Join(reloaded.DockerSources(), ","); got != "docker.m.daocloud.io" {
		t.Errorf("镜像源被改写了: %q", got)
	}
	if got := strings.Join(reloaded.AutostartRefs(), ","); got != "redis/8" {
		t.Errorf("开机自启清单被改写了: %q", got)
	}
	if !reloaded.StartMinimized() {
		t.Error("勾选自身应落住 true")
	}
}
