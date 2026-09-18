// 用户数据目录与 `~` 展开的单测
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无可用主目录，跳过 ~ 展开测试")
	}
	cases := map[string]string{
		"":              "",
		"~/phpo":        filepath.Join(home, "phpo"),
		"~":             home,
		"/abs/phpo":     "/abs/phpo",
		"~/a/b":         filepath.Join(home, "a", "b"),
		"~backup/x":     "~backup/x", // 非 ~ 前缀（~xxx）不动
		"relative/path": "relative/path",
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestExpandEnvHomes(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无可用主目录")
	}
	e := ExpandEnvHomes(DerivePaths(DefaultHome, DefaultWWW))
	if e.PHPOHome != filepath.Join(home, "phpo") {
		t.Errorf("PHPO_HOME 未展开: %q", e.PHPOHome)
	}
	if e.WWWRoot != filepath.Join(home, "www") {
		t.Errorf("WWW_ROOT 未展开: %q", e.WWWRoot)
	}
	// 派生子目录随之落地（尾部为已展开绝对路径）
	if e.OfflineRoot != filepath.Join(home, "phpo", "offline") {
		t.Errorf("OFFLINE_ROOT 未随展开重派生: %q", e.OfflineRoot)
	}
}

func TestUserDataDirAndDBPath(t *testing.T) {
	d, err := UserDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(d) != "phpo" {
		t.Errorf("用户数据目录应以 phpo 结尾: %q", d)
	}
	db, err := DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(db) != "phpo.db" || filepath.Dir(db) != d {
		t.Errorf("DBPath 应落在用户数据目录内: %q vs %q", db, d)
	}
}
