// T606a 验收：OfflineService §5.14.10 全量门面。
//   - 读侧：ListEntries/GetEntry/Stats/Lookup 均走真实 *cache.Manager（临时目录 env），聚合 manifest 计数。
//   - 校验：VerifyEntry 检出损坏并发射 cache:corrupted；VerifyAll 汇总。
//   - 写侧：CleanupCache（三模式，inUse 由快照推导，删非在用）+ RemoveCacheEntry → 三段式任务 + JSON Lines 审计。
//   - 提升/清临时：PromoteImage 登记 manifest.image；ClearTempDir 发射 cache:tempdir-cleared。
package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/cache"
	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
)

func newOfflineSvc(t *testing.T) (*OfflineService, *cache.Manager, config.Env, *fakeEmitter, *clStore, string) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	m := cache.NewManager(env, em, nil) // 读/管理接口不需 DockerBackend
	st := &clStore{snap: model.NewSnapshot()}
	auditPath := filepath.Join(t.TempDir(), "logs", "operations.log")
	svc := NewOfflineService(m, st, engine.NewAudit(auditPath), em, task.NewManager(em))
	return svc, m, env, em, st, auditPath
}

// seedImage 写入缓存 tar + 登记 manifest.image（sha 为真实计算值或故意错值）
func seedImage(t *testing.T, m *cache.Manager, env config.Env, kind, version string, data []byte, wantSHA string) {
	t.Helper()
	tar := env.OfflineImageTar(kind, version)
	if err := os.MkdirAll(filepath.Dir(tar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tar, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sha := wantSHA
	if sha == "" {
		got, err := cache.FileSHA256(tar)
		if err != nil {
			t.Fatal(err)
		}
		sha = got
	}
	mf, err := m.LoadManifest(kind, version)
	if err != nil {
		t.Fatal(err)
	}
	mf.Image = &model.ManifestImage{Name: kind + ":" + version, Sha256: sha, Size: int64(len(data))}
	if err := m.SaveManifest(mf); err != nil {
		t.Fatal(err)
	}
}

// seedPecl 写入 php 的 pecl 扩展包 + 登记 manifest.pecl
func seedPecl(t *testing.T, m *cache.Manager, env config.Env, version, name string, data []byte) {
	t.Helper()
	p := filepath.Join(env.OfflineExtDir("php", version, "pecl"), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := cache.FileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	mf, _ := m.LoadManifest("php", version)
	mf.Pecl = append(mf.Pecl, model.ManifestPackage{Name: name, Sha256: sha, Size: int64(len(data))})
	if err := m.SaveManifest(mf); err != nil {
		t.Fatal(err)
	}
}

func TestOffline_ListEntries_AggregatesManifest(t *testing.T) {
	svc, m, env, _, _, _ := newOfflineSvc(t)
	seedImage(t, m, env, "php", "8.4", []byte("IMG"), "") // 完好
	seedPecl(t, m, env, "8.4", "redis-6.tgz", []byte("EXT"))
	// nginx 损坏：manifest 记 sha 与真实不符
	seedImage(t, m, env, "nginx", "alpine", []byte("REAL"), "deadbeef")

	list, err := svc.ListCacheEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应 2 条目，实得 %d", len(list))
	}
	var php, ngx model.CacheEntry
	for _, e := range list {
		if e.Kind == "php" {
			php = e
		} else {
			ngx = e
		}
	}
	if !php.HasImage || php.PeclCount != 1 || !php.VerifyOK {
		t.Fatalf("php 条目应完好且有 1 pecl，实得 %+v", php)
	}
	if ngx.VerifyOK {
		t.Fatalf("nginx 条目应标损坏，实得 %+v", ngx)
	}
	if php.Path == "" {
		t.Fatal("条目应带缓存绝对路径")
	}
}

func TestOffline_VerifyEntry_EmitsCorrupted(t *testing.T) {
	svc, m, env, em, _, _ := newOfflineSvc(t)
	seedImage(t, m, env, "redis", "8", []byte("REAL"), "bad-sha")
	res, err := svc.VerifyCacheEntry(context.Background(), "redis", "8")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || len(res.Failed) != 1 || res.Failed[0] != "image.tar" {
		t.Fatalf("应检出 1 个 image.tar 失败，实得 %+v", res)
	}
	if !em.has("cache:corrupted") {
		t.Fatalf("校验损坏应发 cache:corrupted，实得 %v", em.events)
	}
}

func TestOffline_VerifyAll(t *testing.T) {
	svc, m, env, _, _, _ := newOfflineSvc(t)
	seedImage(t, m, env, "php", "8.4", []byte("OK"), "")
	seedImage(t, m, env, "mysql", "8.4", []byte("X"), "wrong")
	res, err := svc.VerifyAllCache(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.OK != 1 || res.Failed != 1 {
		t.Fatalf("汇总应为 total2/ok1/failed1，实得 %+v", res)
	}
}

func TestOffline_CleanupCache_AggressiveProtectsInUse(t *testing.T) {
	svc, m, env, _, st, auditPath := newOfflineSvc(t)
	seedImage(t, m, env, "php", "8.4", []byte("IMG"), "")    // 在用
	seedImage(t, m, env, "nginx", "alpine", []byte("X"), "") // 非在用
	st.snap.Installed["php"] = []string{"8.4"}

	res, err := svc.CleanupCache(context.Background(), model.CleanupAggressive)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Fatalf("激进应只删非在用的 nginx，实得 %d", res.Removed)
	}
	if _, err := os.Stat(env.OfflineImageTar("php", "8.4")); err != nil {
		t.Fatal("在用 php 缓存不应被删")
	}
	if _, err := os.Stat(env.OfflineImageTar("nginx", "alpine")); !os.IsNotExist(err) {
		t.Fatal("非在用 nginx 缓存应被删")
	}
	ops := drain(t, auditPath)
	if len(ops) != 1 || ops[0].Op != "cache-cleanup" {
		t.Fatalf("应有 1 条 cache-cleanup 审计，实得 %+v", ops)
	}
}

func TestOffline_RemoveCacheEntry(t *testing.T) {
	svc, m, env, _, _, auditPath := newOfflineSvc(t)
	seedImage(t, m, env, "redis", "8", []byte("IMG"), "")
	if err := svc.RemoveCacheEntry(context.Background(), "redis", "8"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.OfflineRoot, "redis", "8")); !os.IsNotExist(err) {
		t.Fatal("单条删除后目录应消失")
	}
	ops := drain(t, auditPath)
	if len(ops) != 1 || ops[0].Op != "cache-remove" {
		t.Fatalf("应有 1 条 cache-remove 审计，实得 %+v", ops)
	}
}

func TestOffline_LookupImage(t *testing.T) {
	svc, m, env, _, _, _ := newOfflineSvc(t)
	seedImage(t, m, env, "php", "8.4", []byte("IMG"), "")
	hit, err := svc.LookupImage(context.Background(), "php", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if !hit.Hit || hit.Corrupted || hit.Size == 0 {
		t.Fatalf("完好镜像应命中且非损坏，实得 %+v", hit)
	}
	miss, _ := svc.LookupImage(context.Background(), "php", "9.9")
	if miss.Hit || miss.Corrupted {
		t.Fatalf("未缓存应全 false，实得 %+v", miss)
	}
}

func TestOffline_PromoteImage(t *testing.T) {
	svc, m, env, _, _, _ := newOfflineSvc(t)
	tmp := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(tmp, []byte("TAR"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.PromoteImage(context.Background(), "pgsql", "17", tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(env.OfflineImageTar("pgsql", "17")); err != nil {
		t.Fatalf("提升后缓存 tar 应存在: %v", err)
	}
	mf, _ := m.LoadManifest("pgsql", "17")
	if mf.Image == nil || mf.Image.Name != "postgres:17" || mf.Image.Sha256 == "" {
		t.Fatalf("manifest.image 应登记且 ref 推导为 postgres:17，实得 %+v", mf.Image)
	}
}

func TestOffline_ClearTempDir_Emits(t *testing.T) {
	svc, _, env, em, _, _ := newOfflineSvc(t)
	d := env.TempExtDir("php", "8.4")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := svc.ClearTempDir(context.Background(), "php", "8.4", "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Fatal("临时目录应被清空")
	}
	if !em.has("cache:tempdir-cleared") {
		t.Fatalf("应发 cache:tempdir-cleared，实得 %v", em.events)
	}
}
