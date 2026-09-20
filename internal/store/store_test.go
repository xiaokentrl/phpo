// Store 全套：迁移幂等/升降级、快照物化（env/dirReady 由 EnvProvider 合成）、延迟建库、端口占用、审计、回收站、离线表
package store

import (
	"errors"
	"os"
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
	tables := []string{"installed", "sites", "php_extensions", "trash",
		"offline_entries", "update_state", "operations", "cache_manifest"}
	for _, tb := range tables {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tb).Scan(&n); err != nil || n != 1 {
			t.Errorf("表 %s 不存在 (n=%d err=%v)", tb, n, err)
		}
	}
	// dir_ready 已在 0007 下线：就绪判定唯一派生自 config.yaml 两根 + 目录存在性
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='dir_ready'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("SQLite 不应有 dir_ready 表 (n=%d err=%v)", n, err)
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

// fakeEnv 实现 EnvProvider：固定扁平 env + 「两根已持久化且已存在」，供快照合成 env 与派生 dirReady
type fakeEnv struct {
	m         map[string]string
	persisted bool
	home, www bool
}

func (f fakeEnv) FlatEnv() map[string]string { return f.m }
func (f fakeEnv) RootsPersisted() bool       { return f.persisted }
func (f fakeEnv) RootsReady() (bool, bool)   { return f.home, f.www }

// env 表已迁出 SQLite：迁移不应再建 env 表；BuildSnapshot.env 唯一来自注入的 EnvProvider
func TestEnvMigratedOutAndSnapshotUsesProvider(t *testing.T) {
	s := openStore(t)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='env'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("SQLite 不应有 env 表 (n=%d err=%v)", n, err)
	}
	// 未注入 provider：快照 env 为空
	got, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != 0 {
		t.Errorf("未注入 provider 时 env 应为空，实得 %v", got.Env)
	}
	// 注入 provider：快照 env 逐键来自 FlatEnv，dirReady 由 RootsReady 派生
	s.SetEnvProvider(fakeEnv{m: map[string]string{"WWW_ROOT": "~/www", "MYSQL_84_PORT": "3306"}, persisted: true, home: true})
	got, err = s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["WWW_ROOT"] != "~/www" || got.Env["MYSQL_84_PORT"] != "3306" {
		t.Errorf("快照 env 应来自 provider，实得 %v", got.Env)
	}
	if !got.DirReady["PHPO_HOME"] || got.DirReady["WWW_ROOT"] {
		t.Errorf("dirReady 应逐根派生自 RootsReady，实得 %+v", got.DirReady)
	}
}

func TestApplyTaskResultFullSnapshot(t *testing.T) {
	s := openStore(t)
	s.SetEnvProvider(fakeEnv{persisted: true, home: true, www: true})
	snap := model.NewSnapshot()
	snap.Installed["php"] = []string{"8.4", "8.3"}
	snap.Running["php"] = []string{"8.4"}
	snap.Sites = []model.Site{{Domain: "demo.test", Port: 81, PHP: "8.4", Root: "~/www/demo.test", Rewrite: "laravel"}}
	snap.Env["PHPO_HOME"] = "~/phpo"
	snap.PHPExtensions["8.4"] = []string{"redis", "zip"}
	snap.DirReady["PHPO_HOME"] = false // 就绪态为派生量：任务结果里的 dirReady/env 不参与落库

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
		t.Error("dirReady 应由 provider 派生，不受任务快照影响")
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

// lazyEnv 可变 EnvProvider：模拟装机向导把两根写入 config.yaml 前/后的三态
type lazyEnv struct {
	m         map[string]string
	persisted bool
	home, www bool
}

func (e *lazyEnv) FlatEnv() map[string]string { return e.m }
func (e *lazyEnv) RootsPersisted() bool       { return e.persisted }
func (e *lazyEnv) RootsReady() (bool, bool)   { return e.home, e.www }

// 方案B 首启门禁：两根未写入 config.yaml 前，运行态存储不得在用户数据目录留下任何文件；
// 向导落地后首次访问即「建目录 → 建库 → 迁移」，无需重启。
func TestDeferredOpen_FirstLaunchWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "phpo") // 刻意不存在的父目录
	dbPath := filepath.Join(dir, "phpo.db")
	env := &lazyEnv{m: map[string]string{"PHPO_HOME": "~/phpo", "WWW_ROOT": "~/www"}}
	s := New(dbPath)
	s.SetEnvProvider(env)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("未打开的库 Close 应无副作用: %v", err)
		}
	})

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("首启快照应返回空态而非报错: %v", err)
	}
	if len(snap.Installed) != 0 || len(snap.Sites) != 0 || len(snap.PHPExtensions) != 0 {
		t.Errorf("首启运行态应为空: %+v", snap)
	}
	if snap.DirReady["PHPO_HOME"] || snap.DirReady["WWW_ROOT"] {
		t.Errorf("首启 dirReady 应双 false，实得 %+v", snap.DirReady)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("首启读取快照不得创建用户数据目录")
	}

	// 读写一律拒绝（ErrHomeNotSet），且拒绝路径同样零落盘
	if err := s.UpsertSite(model.Site{Domain: "a.test", Port: 80}); !errors.Is(err, ErrHomeNotSet) {
		t.Errorf("写操作应报 ErrHomeNotSet，实得 %v", err)
	}
	if _, err := s.ListSites(); !errors.Is(err, ErrHomeNotSet) {
		t.Errorf("读操作应报 ErrHomeNotSet，实得 %v", err)
	}
	if err := s.AppendOperation(model.Operation{Op: "install"}); !errors.Is(err, ErrHomeNotSet) {
		t.Errorf("审计应报 ErrHomeNotSet，实得 %v", err)
	}
	if _, err := s.AddTrashItem(TrashItem{Kind: "site"}); !errors.Is(err, ErrHomeNotSet) {
		t.Errorf("回收站应报 ErrHomeNotSet，实得 %v", err)
	}
	if err := s.Migrate(); !errors.Is(err, ErrHomeNotSet) {
		t.Errorf("迁移应报 ErrHomeNotSet，实得 %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("被拒绝的写操作同样不得留下用户数据目录")
	}

	// 向导落库两根 → 门禁解除，下一次访问透明建库
	env.persisted, env.home, env.www = true, true, true
	if err := s.UpsertSite(model.Site{Domain: "a.test", Port: 80, PHP: "8.4", Root: "~/www/a.test"}); err != nil {
		t.Fatalf("两根落地后应可写库: %v", err)
	}
	if fi, err := os.Stat(dbPath); err != nil || fi.Size() == 0 {
		t.Fatalf("两根落地后应已建库: %v", err)
	}
	got, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sites) != 1 || got.Sites[0].Domain != "a.test" {
		t.Errorf("落地前的写入不得丢: %+v", got.Sites)
	}
	if !got.DirReady["PHPO_HOME"] || !got.DirReady["WWW_ROOT"] {
		t.Errorf("两根就绪后 dirReady 应双 true，实得 %+v", got.DirReady)
	}
}

// 曾就绪但目录被删：dirReady 回落 false 重新拦截写操作，但库内已装状态不回退（下次建库照常可读）
func TestSnapshot_DirReadyFallsBackWithoutLosingState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "phpo.db")
	env := &lazyEnv{persisted: true, home: true, www: true}
	s := New(dbPath)
	s.SetEnvProvider(env)
	t.Cleanup(func() { s.Close() })

	if err := s.SetInstalled("php", "8.4", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSite(model.Site{Domain: "a.test", Port: 80}); err != nil {
		t.Fatal(err)
	}

	env.home, env.www = false, false // 目录被删，但 config.yaml 两根仍在（persisted 保持 true）
	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("目录缺失时仍应可读库（不丢已装状态）: %v", err)
	}
	if snap.DirReady["PHPO_HOME"] || snap.DirReady["WWW_ROOT"] {
		t.Errorf("目录被删后 dirReady 应回落 false，实得 %+v", snap.DirReady)
	}
	if len(snap.Installed["php"]) != 1 || len(snap.Sites) != 1 {
		t.Errorf("已装状态不得因目录缺失而回退: %+v", snap)
	}
}

// 派生 dirReady 与建库门禁相互独立：仅当两根写入 config.yaml 才建库，与目录是否存在无关
func TestDeferredOpen_PersistedButMissingDirStillOpens(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "phpo.db")
	s := New(dbPath)
	s.SetEnvProvider(&lazyEnv{persisted: true}) // RootsReady 双 false
	if _, err := s.ListSites(); err != nil {
		t.Fatalf("已持久化但目录缺失时仍应打开库: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("库文件应已建立: %v", err)
	}
}

// TestSnapshotSitesEnriched 快照在出口逐站点派生 health/hosts（不落库、前端不推断，硬红线 4）：
// vhost 是否已落盘 / 站点目录是否存在 / 所选 PHP 是否运行 / hosts 探针命中与否，共同决定列表两列。
func TestSnapshotSitesEnriched(t *testing.T) {
	dir := t.TempDir()
	sitesRoot := filepath.Join(dir, "nginx", "sites")
	wwwRoot := filepath.Join(dir, "www")
	if err := os.MkdirAll(sitesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wwwRoot, "ok.test"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 只有 ok.test 的 vhost 已落盘（其余站点为降级态：暂不落盘）
	if err := os.WriteFile(filepath.Join(sitesRoot, "ok.test.conf"), []byte("server{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(filepath.Join(dir, "phpo.db"))
	s.SetEnvProvider(fakeEnv{m: map[string]string{"NGINX_SITES_ROOT": sitesRoot, "WWW_ROOT": wwwRoot}, persisted: true, home: true, www: true})
	s.SetHostsProbe(func(domain string) bool { return domain == "ok.test" })
	t.Cleanup(func() { s.Close() })

	for _, kind := range []struct {
		kind, version string
		running       bool
	}{
		{"nginx", "alpine", true}, {"php", "8.4", true}, {"php", "8.0", false},
	} {
		if err := s.SetInstalled(kind.kind, kind.version, true); err != nil {
			t.Fatal(err)
		}
		if err := s.SetRunning(kind.kind, kind.version, kind.running); err != nil {
			t.Fatal(err)
		}
	}
	sites := []model.Site{
		{Domain: "ok.test", Port: 80, PHP: "8.4", Root: filepath.Join(wwwRoot, "ok.test")},
		{Domain: "degraded.test", Port: 81, PHP: "", Root: wwwRoot},  // 未选 PHP：vhost 未落盘 → 降级
		{Domain: "nostop.test", Port: 82, PHP: "8.0", Root: wwwRoot}, // PHP 已装未运行，且无 conf → 降级
		{Domain: "gone.test", Port: 83, PHP: "8.4", Root: filepath.Join(wwwRoot, "gone.test")},
	}
	for _, st := range sites {
		if err := s.UpsertSite(st); err != nil {
			t.Fatal(err)
		}
	}
	// gone.test：conf 在位但站点目录不存在 → 未响应
	if err := os.WriteFile(filepath.Join(sitesRoot, "gone.test.conf"), []byte("server{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		health string
		hosts  bool
	}{
		"ok.test":       {model.HealthUp, true},
		"degraded.test": {model.HealthWarn, false},
		"nostop.test":   {model.HealthWarn, false},
		"gone.test":     {model.HealthDown, false},
	}
	if len(got.Sites) != len(want) {
		t.Fatalf("站点数不符: %+v", got.Sites)
	}
	for _, st := range got.Sites {
		w, ok := want[st.Domain]
		if !ok {
			t.Fatalf("意外站点: %s", st.Domain)
		}
		if st.Health != w.health || st.Hosts != w.hosts {
			t.Errorf("%s: health=%s hosts=%v，期望 health=%s hosts=%v", st.Domain, st.Health, st.Hosts, w.health, w.hosts)
		}
	}

	// 落库不受派生字段影响：重读库仍为空运行态（health/hosts 不落库）
	raw, err := s.ListSites()
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range raw {
		if st.Health != "" || st.Hosts {
			t.Errorf("%s: health/hosts 不应落库，实得 health=%q hosts=%v", st.Domain, st.Health, st.Hosts)
		}
	}
}

// TestSnapshotSitesWithoutHostsProbe 未注入探针（归档快照、单测）时 hosts 一律按未解析，不得虚报已解析
func TestSnapshotSitesWithoutHostsProbe(t *testing.T) {
	s := openStore(t)
	s.SetEnvProvider(fakeEnv{persisted: true, home: true, www: true})
	if err := s.UpsertSite(model.Site{Domain: "a.test", Port: 80, PHP: "8.4", Root: "/nope"}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Sites[0].Hosts {
		t.Error("未注入 hosts 探针时应为未解析")
	}
	if snap.Sites[0].Health != model.HealthWarn {
		t.Errorf("无 nginx、无 conf 应为降级，实得 %s", snap.Sites[0].Health)
	}
}
