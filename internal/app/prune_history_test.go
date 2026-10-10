// pruneHistoryOnUpgrade：覆盖安装（运行版本变化）后任务队列历史与任务日志清空；
// 版本相同保留；首启（两根未落盘）零落盘原则不碰；账本文件缺失只补戳不建库。
// 两段式模拟真实时序：旧进程落盘（戳=旧版）→ 新进程重载（注入新版）→ prune。
package app

import (
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/store"
)

// oldRun 模拟上一个版本的进程：落盘 config.yaml（两根 + 戳=markerVersion）。
func oldRun(t *testing.T, path, markerVersion string) {
	t.Helper()
	cs, err := config.LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	cs.SetRunVersion(markerVersion)
	if err := cs.SetRoots(filepath.Join(t.TempDir(), "phpo"), filepath.Join(t.TempDir(), "www")); err != nil {
		t.Fatal(err)
	}
}

// newRun 模拟本进程启动：重载磁盘配置并注入新运行版本。
func newRun(t *testing.T, path, version string) *config.ConfigStore {
	t.Helper()
	cs, err := config.LoadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	cs.SetRunVersion(version)
	return cs
}

// storeWithRows 打开临时账本并预置 rows 条历史记录，返回存储与 db 路径。
func storeWithRows(t *testing.T, dbPath string, rows int) *store.Store {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for i := 0; i < rows; i++ {
		if err := s.AppendOperation(model.Operation{Op: "rebuild", Status: "success", Label: "重建 mysql 8.4"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func countOps(t *testing.T, s *store.Store) int {
	t.Helper()
	ops, err := s.ListOperations(100)
	if err != nil {
		t.Fatal(err)
	}
	return len(ops)
}

func TestPruneHistoryOnUpgrade_WipesAndStamps(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	oldRun(t, cfgPath, "0.1.105")
	dbPath := filepath.Join(dir, "phpo.db")
	st := storeWithRows(t, dbPath, 3)
	cfg := newRun(t, cfgPath, "0.1.106")

	c := &Container{CurrentVersion: "0.1.106"}
	c.pruneHistoryOnUpgrade(cfg, st, dbPath)

	if n := countOps(t, st); n != 0 {
		t.Fatalf("版本变化后账本应清空，剩 %d 条", n)
	}
	if got := cfg.LastRunVersion(); got != "0.1.106" {
		t.Fatalf("清空后应盖新戳，实际 %q", got)
	}
}

func TestPruneHistoryOnUpgrade_SameVersionKeeps(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	oldRun(t, cfgPath, "0.1.106")
	dbPath := filepath.Join(dir, "phpo.db")
	st := storeWithRows(t, dbPath, 2)
	cfg := newRun(t, cfgPath, "0.1.106")

	c := &Container{CurrentVersion: "0.1.106"}
	c.pruneHistoryOnUpgrade(cfg, st, dbPath)

	if n := countOps(t, st); n != 2 {
		t.Fatalf("版本相同不应清账本，剩 %d 条", n)
	}
}

func TestPruneHistoryOnUpgrade_FirstRunTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	// 首启：config.yaml 从未落盘（无旧戳），但盘上确有一份旧账本（守卫不得误伤）
	cfgPath := filepath.Join(dir, "config.yaml")
	dbPath := filepath.Join(dir, "phpo.db")
	st := storeWithRows(t, dbPath, 2)
	cfg := newRun(t, cfgPath, "0.1.106")

	c := &Container{CurrentVersion: "0.1.106"}
	c.pruneHistoryOnUpgrade(cfg, st, dbPath)

	if n := countOps(t, st); n != 2 {
		t.Fatalf("首启不应清账本，剩 %d 条", n)
	}
	if got := cfg.LastRunVersion(); got != "" {
		t.Fatalf("首启不应落任何戳（零落盘），实际 %q", got)
	}
}

func TestPruneHistoryOnUpgrade_MissingDbOnlyStamps(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	oldRun(t, cfgPath, "0.1.105")
	dbPath := filepath.Join(dir, "phpo.db") // 刻意不创建：配置过但从未产生任务
	cfg := newRun(t, cfgPath, "0.1.106")

	c := &Container{CurrentVersion: "0.1.106"}
	c.pruneHistoryOnUpgrade(cfg, nil, dbPath) // 缺账本分支不触库，nil 即可

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("账本不存在时不得借机建库 (stat err=%v)", err)
	}
	if got := cfg.LastRunVersion(); got != "0.1.106" {
		t.Fatalf("无账本也应补戳防下次误清，实际 %q", got)
	}
}
