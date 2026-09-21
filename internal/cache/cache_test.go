// 离线缓存核心单测：三条铁律 + 事件序列 + 临时目录四类必清 + 提升/校验/清理/统计
package cache

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

// 本地捕获发射器（避免测试引入 app→cache 循环）
type capEmitter struct {
	mu     sync.Mutex
	events []capEvent
}
type capEvent struct {
	Name    string
	Payload any
}

func (c *capEmitter) Emit(name string, payload any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capEvent{name, payload})
}
func (c *capEmitter) Capture() []capEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capEvent, len(c.events))
	copy(out, c.events)
	return out
}

// —— fake DockerBackend ——

type fakeBackend struct {
	pulled, loaded, saved, downloaded []string
	saveData, downloadData            []byte
	pullErr, saveErr, loadErr, dlErr  error
	exists                            map[string]bool // 本地 Docker store 已有的镜像引用
	existsErr                         error
	existsCalls                       []string
}

func (f *fakeBackend) ImageExists(_ context.Context, ref string) (bool, error) {
	f.existsCalls = append(f.existsCalls, ref)
	return f.exists[ref], f.existsErr
}

func (f *fakeBackend) PullImage(_ context.Context, ref string) error {
	f.pulled = append(f.pulled, ref)
	return f.pullErr
}
func (f *fakeBackend) SaveImage(_ context.Context, ref, dst string) error {
	f.saved = append(f.saved, dst)
	if f.saveErr != nil {
		return f.saveErr
	}
	return os.WriteFile(dst, f.saveData, 0o644)
}
func (f *fakeBackend) LoadImage(_ context.Context, tar string) error {
	f.loaded = append(f.loaded, tar)
	return f.loadErr
}
func (f *fakeBackend) Download(_ context.Context, url, dst string) error {
	f.downloaded = append(f.downloaded, dst)
	if f.dlErr != nil {
		return f.dlErr
	}
	return os.WriteFile(dst, f.downloadData, 0o644)
}

func newMgr(t *testing.T) (*Manager, config.Env, *capEmitter, *fakeBackend) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &capEmitter{}
	fb := &fakeBackend{saveData: []byte("IMG-TAR"), downloadData: []byte("EXT-PKG")}
	return NewManager(env, em, fb), env, em, fb
}

func eventNames(cap *capEmitter) []string {
	out := make([]string, 0, len(cap.Capture()))
	for _, e := range cap.Capture() {
		out = append(out, e.Name)
	}
	return out
}

func seedImage(t *testing.T, m *Manager, env config.Env, kind, version string, data []byte) {
	t.Helper()
	tar := env.OfflineImageTar(kind, version)
	if err := os.MkdirAll(dirOf(tar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tar, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := FileSHA256(tar)
	if err != nil {
		t.Fatal(err)
	}
	mf := newManifest(kind, version)
	mf.Image = &model.ManifestImage{Name: kind + ":" + version, Sha256: sha, Size: int64(len(data))}
	if err := m.SaveManifest(mf); err != nil {
		t.Fatal(err)
	}
}

// —— 镜像：命中零网络 ——

func TestEnsureImageHit(t *testing.T) {
	m, env, cap, fb := newMgr(t)
	seedImage(t, m, env, "php", "8.4", []byte("IMG-TAR"))
	if err := m.EnsureImage(context.Background(), "php", "8.4", "php:8.4-fpm"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulled) != 0 {
		t.Fatal("命中缓存不应拉取网络")
	}
	if len(fb.loaded) != 1 {
		t.Fatalf("命中应 docker load 一次，得 %d", len(fb.loaded))
	}
	if !contains(eventNames(cap), "cache:hit") {
		t.Fatalf("应发 cache:hit，得 %v", eventNames(cap))
	}
}

// —— 镜像：未命中 → 下载 → 提升 → 清临时 ——

func TestEnsureImageMissPromote(t *testing.T) {
	m, env, cap, fb := newMgr(t)
	if err := m.EnsureImage(context.Background(), "mysql", "8.4", "mysql:8.4"); err != nil {
		t.Fatal(err)
	}
	// 缓存 tar 生成
	if _, err := os.Stat(env.OfflineImageTar("mysql", "8.4")); err != nil {
		t.Fatalf("提升后缓存镜像应存在: %v", err)
	}
	// manifest 记录 image + sha
	mf, _ := m.LoadManifest("mysql", "8.4")
	if mf.Image == nil || mf.Image.Sha256 == "" {
		t.Fatalf("manifest.image 未登记: %+v", mf.Image)
	}
	if len(fb.pulled) != 1 || len(fb.saved) != 1 {
		t.Fatalf("未命中应 pull+save 各一次，得 pulled=%v saved=%v", fb.pulled, fb.saved)
	}
	// 临时目录已清空
	if _, err := os.Stat(env.TempExtDir("mysql", "8.4")); !os.IsNotExist(err) {
		t.Fatal("提升后临时目录必须清空")
	}
	names := eventNames(cap)
	for _, want := range []string{"cache:miss", "cache:promote", "cache:tempdir-cleared"} {
		if !contains(names, want) {
			t.Fatalf("事件序列应含 %s，得 %v", want, names)
		}
	}
}

// —— 镜像：损坏 → 告警并回退网络 ——

func TestEnsureImageCorruptedFallback(t *testing.T) {
	m, env, cap, fb := newMgr(t)
	tar := env.OfflineImageTar("redis", "8")
	os.MkdirAll(dirOf(tar), 0o755)
	os.WriteFile(tar, []byte("REAL"), 0o644)
	mf := newManifest("redis", "8")
	mf.Image = &model.ManifestImage{Name: "redis:8", Sha256: "deadbeef"} // 故意不匹配
	m.SaveManifest(mf)

	if err := m.EnsureImage(context.Background(), "redis", "8", "redis:8"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulled) != 1 {
		t.Fatal("损坏应回退网络拉取")
	}
	if !contains(eventNames(cap), "cache:corrupted") {
		t.Fatalf("应发 cache:corrupted，得 %v", eventNames(cap))
	}
}

// —— 镜像：缓存未命中但本地 Docker store 已有镜像 → 免网络 save 后提升（断网/内网机器重建缓存的唯一路径）——

func TestEnsureImageMissRebuildsFromLocalImage(t *testing.T) {
	m, env, cap, fb := newMgr(t)
	fb.exists = map[string]bool{"nginx:alpine": true}
	if err := m.EnsureImage(context.Background(), "nginx", "alpine", "nginx:alpine"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulled) != 0 {
		t.Fatalf("本地已有镜像不得走网络拉取，得 pulled=%v", fb.pulled)
	}
	if len(fb.saved) != 1 {
		t.Fatalf("应就地 save 一次，得 saved=%v", fb.saved)
	}
	if _, err := os.Stat(env.OfflineImageTar("nginx", "alpine")); err != nil {
		t.Fatalf("save 后应提升进离线缓存: %v", err)
	}
	if _, err := os.Stat(env.TempExtDir("nginx", "alpine")); !os.IsNotExist(err) {
		t.Fatal("提升后临时目录必须清空")
	}
	if got := missAction(cap); got != "local" {
		t.Fatalf("cache:miss 的 action 应为 local，得 %q", got)
	}
}

// missAction 取首个 cache:miss 事件的 action（区分真的走了网络还是就地复用本地镜像）
func missAction(cap *capEmitter) string {
	for _, e := range cap.Capture() {
		if e.Name == "cache:miss" {
			if ev, ok := e.Payload.(model.CacheMissEvent); ok {
				return ev.Action
			}
		}
	}
	return ""
}

// 本地镜像探测失败必须如实报错，不得静默回退网络（否则镜像引擎异常会被掩盖成拉取失败）
func TestEnsureImagePropagatesLocalImageProbeError(t *testing.T) {
	m, _, _, fb := newMgr(t)
	boom := errors.New("docker 引擎不可用")
	fb.existsErr = boom
	if err := m.EnsureImage(context.Background(), "redis", "8", "redis:8"); !errors.Is(err, boom) {
		t.Fatalf("应上报探测错误，得 %v", err)
	}
	if len(fb.pulled) != 0 {
		t.Fatal("探测失败不得继续拉取")
	}
}

// —— 扩展：命中复制编译清 ——

func TestInstallExtensionHit(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	// 预置 pecl 缓存
	p := env.OfflineExtDir("php", "8.4", "pecl") + "/redis-6.0.2.tgz"
	os.MkdirAll(dirOf(p), 0o755)
	os.WriteFile(p, []byte("EXT-PKG"), 0o644)
	sha, _ := FileSHA256(p)
	mf := newManifest("php", "8.4")
	mf.Pecl = []model.ManifestPackage{{Name: "redis-6.0.2.tgz", Sha256: sha, Size: 7}}
	m.SaveManifest(mf)

	compiled := false
	err := m.InstallExtension(context.Background(), "8.4", "pecl", "redis-6.0.2.tgz", "",
		func(string) error { compiled = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !compiled {
		t.Fatal("应调用编译回调")
	}
	if _, err := os.Stat(env.TempExtDir("php", "8.4")); !os.IsNotExist(err) {
		t.Fatal("命中路径编译后也应清空临时目录")
	}
	names := eventNames(cap)
	if !contains(names, "cache:hit") || !contains(names, "cache:tempdir-cleared") {
		t.Fatalf("应含 hit + tempdir-cleared，得 %v", names)
	}
}

// —— 扩展：未命中下载 → 编译 → 提升 ——

func TestInstallExtensionMissPromote(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	err := m.InstallExtension(context.Background(), "8.4", "pecl", "xdebug-3.tgz", "https://x/xdebug.tgz",
		func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	p := env.OfflineExtDir("php", "8.4", "pecl") + "/xdebug-3.tgz"
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("未命中编译成功后应提升进缓存: %v", err)
	}
	mf, _ := m.LoadManifest("php", "8.4")
	if len(mf.Pecl) != 1 || mf.Pecl[0].Name != "xdebug-3.tgz" {
		t.Fatalf("manifest.pecl 未登记: %+v", mf.Pecl)
	}
	names := eventNames(cap)
	for _, want := range []string{"cache:miss", "cache:promote", "cache:tempdir-cleared"} {
		if !contains(names, want) {
			t.Fatalf("应含 %s，得 %v", want, names)
		}
	}
}

// —— 扩展：编译失败 → 清空临时且不入缓存 ——

func TestInstallExtensionCompileFailedClears(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	boom := errors.New("compile")
	err := m.InstallExtension(context.Background(), "8.4", "apk", "libzip.apk", "https://x/libzip.apk",
		func(string) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("应返回编译错误，得 %v", err)
	}
	if _, err := os.Stat(env.TempExtDir("php", "8.4")); !os.IsNotExist(err) {
		t.Fatal("编译失败必须清空临时目录")
	}
	// 未提升
	if _, err := os.Stat(env.OfflineExtDir("php", "8.4", "apk") + "/libzip.apk"); !os.IsNotExist(err) {
		t.Fatal("失败不应进缓存")
	}
	// 找到 tempdir-cleared 事件且 reason=compile_failed
	found := false
	for _, e := range cap.Capture() {
		if e.Name == "cache:tempdir-cleared" {
			if p := e.Payload.(model.CacheTempdirClearedEvent); p.Reason != "compile_failed" {
				t.Fatalf("失败清理 reason 应为 compile_failed，得 %s", p.Reason)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("应发射 cache:tempdir-cleared")
	}
}

// —— 启动扫描残留 ——

func TestScanAndClearResidue(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	// 造两个残留临时目录
	d1 := env.TempExtDir("php", "8.4")
	d2 := env.TempExtDir("mysql", "8.4")
	os.MkdirAll(d1, 0o755)
	os.MkdirAll(d2, 0o755)
	os.WriteFile(d1+"/junk", []byte("x"), 0o644)

	if err := m.ScanAndClearResidue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d1); !os.IsNotExist(err) {
		t.Fatal("php 临时残留未清空")
	}
	if _, err := os.Stat(d2); !os.IsNotExist(err) {
		t.Fatal("mysql 临时残留未清空")
	}
	if count(eventNames(cap), "cache:tempdir-cleared") != 2 {
		t.Fatalf("两次清空应发两次事件，得 %v", eventNames(cap))
	}
}

// —— 未注入后端禁止走网络 ——

func TestEnsureImageNoBackend(t *testing.T) {
	env := config.DerivePaths(t.TempDir(), "")
	m := NewManager(env, nil, nil)
	if err := m.EnsureImage(context.Background(), "php", "8.5", "php:8.5"); !errors.Is(err, errNoBackend) {
		t.Fatalf("未注入后端应报 errNoBackend，得 %v", err)
	}
}

// —— 清理：保守仅删损坏 ——

func TestCleanupConservative(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	seedImage(t, m, env, "php", "8.4", []byte("GOOD")) // 完好
	// 造一个损坏条目
	bad := env.OfflineImageTar("nginx", "alpine")
	os.MkdirAll(dirOf(bad), 0o755)
	os.WriteFile(bad, []byte("X"), 0o644)
	mf := newManifest("nginx", "alpine")
	mf.Image = &model.ManifestImage{Name: "nginx", Sha256: "wrong"}
	m.SaveManifest(mf)

	res, err := m.CleanupCache(context.Background(), model.CleanupConservative, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Fatalf("保守应只删损坏的 1 项，得 %d", res.Removed)
	}
	if _, err := os.Stat(env.OfflineImageTar("php", "8.4")); err != nil {
		t.Fatal("完好条目不应被删")
	}
	if !contains(eventNames(cap), "cache:cleanup") {
		t.Fatal("应发 cache:cleanup")
	}
}

// —— 统计 ——

func TestStatsCounts(t *testing.T) {
	m, env, _, _ := newMgr(t)
	seedImage(t, m, env, "php", "8.4", []byte("IMG"))
	st, err := m.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.EntryCount != 1 || st.ImageCount != 1 {
		t.Fatalf("统计应含 1 条目 1 镜像，得 %+v", st)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func count(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}
