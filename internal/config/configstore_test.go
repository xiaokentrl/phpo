// last_run_version 运行版本戳：每次落盘自动盖上（save 收口），StampRunVersion 立即落盘
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersionStampedOnPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cs, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	cs.SetRunVersion("0.1.107")
	home := filepath.Join(t.TempDir(), "phpo")
	www := filepath.Join(t.TempDir(), "www")
	if err := cs.SetRoots(home, www); err != nil {
		t.Fatalf("SetRoots 落盘: %v", err)
	}
	// 磁盘上的 yaml 直接带 last_run_version 键
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "last_run_version: 0.1.107") {
		t.Fatalf("落盘内容应含 last_run_version: 0.1.107，实际：\n%s", b)
	}
	// 重读内存态：取值一致
	cs2, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cs2.LastRunVersion(); got != "0.1.107" {
		t.Fatalf("重读 LastRunVersion = %q，应 0.1.107", got)
	}
}

func TestStampRunVersionPersistsImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cs, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	cs.SetRunVersion("0.1.107")
	if err := cs.SetRoots(filepath.Join(t.TempDir(), "phpo"), filepath.Join(t.TempDir(), "www")); err != nil {
		t.Fatal(err)
	}
	// 模拟清账本后立即补戳：改内存记号再 Stamp（不经任何其他写路径）
	cs.mu.Lock()
	cs.fc.LastRunVersion = "0.0.1"
	cs.mu.Unlock()
	if err := cs.StampRunVersion(); err != nil {
		t.Fatalf("StampRunVersion: %v", err)
	}
	cs2, err := LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cs2.LastRunVersion(); got != "0.1.107" {
		t.Fatalf("补戳后磁盘应为 0.1.107，实际 %q", got)
	}
}
