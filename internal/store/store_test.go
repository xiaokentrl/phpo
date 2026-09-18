// Store 全套：迁移幂等/升降级、快照物化、密码/端口键、审计、回收站、离线表
package store

import (
	"path/filepath"
	"testing"
	"time"

	"phpo/internal/model"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "phpo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateIdempotentAndTablesExist(t *testing.T) {
	s := openStore(t)
	if err := s.Migrate(); err != nil { // 重复执行必须安全
		t.Fatal(err)
	}
	tables := []string{"env", "installed", "sites", "php_extensions", "dir_ready", "trash",
		"offline_entries", "update_state", "operations", "cache_manifest"}
	for _, tb := range tables {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tb).Scan(&n); err != nil || n != 1 {
			t.Errorf("表 %s 不存在 (n=%d err=%v)", tb, n, err)
		}
	}
}

func TestMigrateUpDownRoundTrip(t *testing.T) {
	s := openStore(t)
	if err := s.MigrateDownTo(0); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sites'`).Scan(&n)
	if n != 0 {
		t.Fatal("回滚后 sites 应不存在")
	}
	if err := s.Migrate(); err != nil {
		t.Fatal("再次升级失败:", err)
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cache_manifest'`).Scan(&n)
	if n != 1 {
		t.Fatal("升级后 cache_manifest 应存在")
	}
}

func TestEnvSnapshotRoundTrip(t *testing.T) {
	s := openStore(t)
	if err := s.SetEnv("WWW_ROOT", "~/www"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.GetEnv("WWW_ROOT")
	if err != nil || !ok || v != "~/www" {
		t.Fatalf("env 读取错误: %q %v %v", v, ok, err)
	}
	if _, ok, _ := s.GetEnv("NOPE"); ok {
		t.Error("不存在的键必须 exists=false")
	}
	// 空密码写入读回
	if err := s.SetPassword("mysql", "8.4", ""); err != nil {
		t.Fatal(err)
	}
	pw, ok, _ := s.GetPassword("mysql", "8.4")
	if !ok || pw != "" {
		t.Errorf("空密码必须原样存取, got %q ok=%v", pw, ok)
	}
	if key := EnvKeyPassword("mysql", "8.4"); key != "MYSQL_84_PASSWORD" {
		t.Errorf("密码键名 = %q", key)
	}
	if key := EnvKeyPort("pgsql", "17.2"); key != "PGSQL_172_PORT" {
		t.Errorf("端口键名（去点）= %q", key)
	}
}

func TestApplyTaskResultFullSnapshot(t *testing.T) {
	s := openStore(t)
	snap := model.NewSnapshot()
	snap.Installed["php"] = []string{"8.4", "8.3"}
	snap.Running["php"] = []string{"8.4"}
	snap.Sites = []model.Site{{Domain: "demo.test", Port: 81, PHP: "8.4", Root: "~/www/demo.test", Rewrite: "laravel"}}
	snap.Env["PHPO_HOME"] = "~/phpo"
	snap.PHPExtensions["8.4"] = []string{"redis", "zip"}
	snap.DirReady["PHPO_HOME"] = true

	if err := s.ApplyTaskResult(model.TaskMeta{Type: "install"}, snap); err != nil {
		t.Fatal(err)
	}
	got, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Installed["php"]) != 2 || len(got.Running["php"]) != 1 || got.Running["php"][0] != "8.4" {
		t.Errorf("installed/running 物化错误: %+v", got)
	}
	if got.Sites[0].Port != 81 || got.Sites[0].Rewrite != "laravel" {
		t.Errorf("sites 物化错误: %+v", got.Sites)
	}
	if len(got.PHPExtensions["8.4"]) != 2 {
		t.Errorf("扩展物化错误: %+v", got.PHPExtensions)
	}
	if !got.DirReady["PHPO_HOME"] {
		t.Error("dirReady 物化错误")
	}
	// 幂等：重复应用结果一致
	if err := s.ApplyTaskResult(model.TaskMeta{Type: "install"}, snap); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.BuildSnapshot()
	if len(got2.Installed["php"]) != 2 || len(got2.Sites) != 1 {
		t.Error("重复应用破坏幂等")
	}
}

func TestCollectUsedPortsSemantics(t *testing.T) {
	snap := model.NewSnapshot()
	snap.Installed["mysql"] = []string{"8.4"}
	snap.Installed["redis"] = []string{"8"}
	snap.Env["MYSQL_84_PORT"] = "3306"
	snap.Env["REDIS_8_PORT"] = "6379"
	snap.Sites = []model.Site{
		{Domain: "a.test", Port: 80},
		{Domain: "b.test", Port: 80}, // 与 a 冲突：先到先得记 a
		{Domain: "c.test", Port: 8080},
	}
	used := CollectUsedPorts(snap, nil)
	if used[3306] != "mysql 8.4" || used[6379] != "redis 8" {
		t.Errorf("服务端口错误: %v", used)
	}
	if used[80] != "site a.test" {
		t.Errorf("同端口先到先得错误: %v", used[80])
	}
	// 排除域名（编辑 c.test 自身时其端口不算占用）
	used = CollectUsedPorts(snap, []string{"c.test"})
	if _, ok := used[8080]; ok {
		t.Error("排除域名后端口不应计入")
	}
	// 非法端口值跳过
	snap.Env["MYSQL_84_PORT"] = "abc"
	if _, ok := CollectUsedPorts(snap, nil)[3306]; ok {
		t.Error("非数字端口值应跳过")
	}
}

func TestOperationsAndTrash(t *testing.T) {
	s := openStore(t)
	op := model.Operation{Actor: "ui", Op: "install", Args: map[string]any{"kind": "php", "version": "8.4"}, Status: "success", DurationMs: 1200}
	if err := s.AppendOperation(op); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListOperations(10)
	if err != nil || len(list) != 1 || list[0].Op != "install" {
		t.Fatalf("审计查询错误: %v %v", list, err)
	}
	if !list[0].TS.After(time.Time{}) {
		t.Error("TS 为零值")
	}

	id, err := s.AddTrashItem(TrashItem{Kind: "site", OrigPath: "~/www/a.test", TrashPath: "~/.phpo/trash/a.test"})
	if err != nil || id == 0 {
		t.Fatalf("回收站插入失败: %v", err)
	}
	items, _ := s.ListTrash()
	if len(items) != 1 || items[0].ExpiresAt.Sub(items[0].MovedAt) != TrashRetention {
		t.Errorf("保留期应为 7 天: %+v", items[0])
	}
	exp, _ := s.ExpiredTrash(items[0].MovedAt.Add(TrashRetention))
	if len(exp) != 1 {
		t.Error("到期查询错误")
	}
	if err := s.RemoveTrashItem(id); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineEntriesAndManifest(t *testing.T) {
	s := openStore(t)
	e := model.CacheEntry{Kind: "php", Version: "8.4", HasImage: true, TotalSize: 450000000, LastVerify: time.Now().UTC(), VerifyOK: true}
	if err := s.UpsertOfflineEntry(e); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertOfflineEntry(model.CacheEntry{Kind: "php", Version: "8.4", TotalSize: 1}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListOfflineEntries()
	if len(list) != 1 || list[0].TotalSize != 1 {
		t.Errorf("upsert 应覆盖: %+v", list)
	}
	if err := s.SaveManifest("php", "8.4", `{"schema_version":1}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := s.LoadManifest("php", "8.4")
	if err != nil || !ok || raw == "" {
		t.Errorf("清单读取失败: %q %v %v", raw, ok, err)
	}
	if _, ok, _ := s.LoadManifest("php", "9.9"); ok {
		t.Error("不存在版本 exists 应为 false")
	}
}
