// 工作根与装配期值拷贝的一致性回归：启动残留清理与对象图都必须按 config.yaml 已持久化的两根寻址
package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/config"
)

// TestStartupResidueUsesPersistedRoot 锁死 §5.14.4 必清时机 4：启动扫描残留临时目录必须落在已持久化的工作根，
// 而不是装配默认值 ~/phpo（否则真机上的残留永不被清，且会在错误目录下做文件系统操作）
func TestStartupResidueUsesPersistedRoot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "work")
	www := filepath.Join(t.TempDir(), "www")
	cfg, err := config.LoadConfigStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoots(home, www); err != nil {
		t.Fatal(err)
	}

	ext := filepath.Join(home, "php", "8.4", "ext")
	if err := os.MkdirAll(filepath.Join(ext, "pecl"), 0o755); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(ext, "pecl", "redis-6.0.2.tgz")
	if err := os.WriteFile(residue, []byte("PKG"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewContainer()
	c.Build()
	if err := runHook(t, c.Lifecycle, "clear-temp-residue"); err != nil {
		t.Fatalf("残留清理钩子应成功: %v", err)
	}
	if _, err := os.Stat(ext); !os.IsNotExist(err) {
		t.Fatalf("启动扫描必须清空 %s（残留文件 %s 仍在）", ext, residue)
	}
}

// TestRebindAfterHomeEnsure 装机把两根写入 config.yaml 后必须重绑对象图：
// 各门面拿到的 config.Env 是装配期值拷贝，不重绑则离线缓存根、临时目录、vhost、容器挂载仍指向装机前的默认根。
// 两根未变时 Rebind 必须幂等（不换图，门面指针不动）。
func TestRebindAfterHomeEnsure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "work")
	www := filepath.Join(t.TempDir(), "www")

	c := NewContainer()
	c.Build()
	ctx := context.Background()
	if err := runHook(t, c.Lifecycle, "object-graph"); err != nil {
		t.Fatalf("首启 object-graph 应成功: %v", err)
	}
	t.Cleanup(func() { c.Lifecycle.OnShutdown(ctx) })

	// 首启两根未落库 → 对象图绑默认根（含 `~` 的未展开值，正是重绑要消灭的陈旧态）
	if c.Env.PHPOHome != config.DefaultHome {
		t.Fatalf("首启对象图应绑默认根，得 %q", c.Env.PHPOHome)
	}

	if err := c.WizardService.HomeEnsure(ctx, home, www); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}

	// 新根下播种一个缓存条目：重绑成功才能被离线缓存门面扫到
	if err := os.MkdirAll(filepath.Join(home, "offline", "php", "9.9"), 0o755); err != nil {
		t.Fatal(err)
	}

	before := c.OfflineService
	if err := c.Rebind(ctx); err != nil {
		t.Fatalf("重绑对象图应成功: %v", err)
	}
	if c.Env.PHPOHome != home {
		t.Fatalf("重绑后 PHPO_HOME 应为新根 %q，得 %q", home, c.Env.PHPOHome)
	}
	if c.OfflineService == before {
		t.Fatal("重绑后应换成新根构建的门面对象")
	}
	stats, err := c.OfflineService.GetCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.EntryCount != 1 {
		t.Fatalf("重绑后缓存统计应按新根扫到 1 条，得 %+v", stats)
	}

	// 幂等：两根未变即不换图
	old := c.OfflineService
	if err := c.Rebind(ctx); err != nil {
		t.Fatalf("二次重绑应成功: %v", err)
	}
	if c.OfflineService != old {
		t.Fatal("两根未变时 Rebind 必须 no-op，不得重建对象图")
	}
}
