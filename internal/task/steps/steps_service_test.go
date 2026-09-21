// T303：安装步骤接线——硬红线 7 门禁 + 缓存优先命中/未命中，事件与零网络断言（真 Manager + fake backend）
package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"phpo/internal/cache"
	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// —— fake DockerBackend ——
type fakeBE struct {
	mu                        sync.Mutex
	pulled, loaded, saved     []string
	loadErr, pullErr, saveErr error
	pullBlocks                bool // true：PullImage 阻塞至 ctx 取消并返回 ctx.Err()（模拟 pull 中途取消）
}

func (f *fakeBE) PullImage(ctx context.Context, ref string) error {
	f.mu.Lock()
	f.pulled = append(f.pulled, ref)
	blocks, err := f.pullBlocks, f.pullErr
	f.mu.Unlock()
	if blocks {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}
func (f *fakeBE) SaveImage(_ context.Context, ref, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, dst)
	if f.saveErr != nil {
		return f.saveErr
	}
	return os.WriteFile(dst, []byte("IMG-TAR"), 0o644)
}
func (f *fakeBE) LoadImage(_ context.Context, tar string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded = append(f.loaded, tar)
	return f.loadErr
}

// ImageExists 恒报「本地无镜像」：这些用例锁的是「未命中 → 走网络 pull」路径，缓存提升断言与 action 无关
func (f *fakeBE) ImageExists(_ context.Context, ref string) (bool, error) { return false, nil }
func (f *fakeBE) Download(_ context.Context, url, dst string) error       { return nil }

// pulledSnapshot 返回当前已发起 pull 的引用快照（用于测试同步点）
func (f *fakeBE) pulledSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pulled...)
}

// —— capture emitter（cache.Emitter 与断言共用）——
type ev struct {
	name string
	p    any
}
type capEm struct {
	mu sync.Mutex
	es []ev
}

func (c *capEm) Emit(name string, p any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.es = append(c.es, ev{name, p})
}
func (c *capEm) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.es))
	for i, e := range c.es {
		out[i] = e.name
	}
	return out
}

// —— fake Probe ——
type fp struct {
	ver string
	err error
}

func (f fp) Detect(context.Context) (string, error) { return f.ver, f.err }

// —— fake StepLog ——
type recLog struct{ lines []string }

func (r *recLog) Log(level, text string) { r.lines = append(r.lines, level+" "+text) }

func newStep(t *testing.T, probe engine.Probe, fb *fakeBE) (*InstallImageStep, *cache.Manager, *capEm, config.Env) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, filepath.Join(home, "www"))
	em := &capEm{}
	cm := cache.NewManager(env, em, fb)
	s := NewInstallImageStep("install-image", cm, probe, env, "php", "8.4")
	return s, cm, em, env
}

func TestInstallBlockedWhenDockerUnavailable(t *testing.T) {
	fb := &fakeBE{}
	s, _, _, _ := newStep(t, fp{err: engine.ErrDockerNotRunning}, fb)
	err := s.Execute(context.Background(), &recLog{})
	if err == nil {
		t.Fatal("Docker 未运行应拒绝安装（硬红线 7）")
	}
	if len(fb.pulled)+len(fb.loaded) != 0 {
		t.Errorf("门禁失败前不得触碰镜像后端: pulled=%v loaded=%v", fb.pulled, fb.loaded)
	}
}

func TestInstallCacheMissPullsAndPromotes(t *testing.T) {
	fb := &fakeBE{}
	s, _, em, env := newStep(t, fp{ver: "28.3.2"}, fb)
	if err := s.Execute(context.Background(), &recLog{}); err != nil {
		t.Fatalf("未命中安装应成功: %v", err)
	}
	if len(fb.pulled) != 1 || fb.pulled[0] != "php:8.4-fpm" {
		t.Errorf("未命中应 pull php:8.4-fpm, got %v", fb.pulled)
	}
	if len(fb.loaded) != 0 {
		t.Errorf("未命中不应走 load, got %v", fb.loaded)
	}
	// 提升后缓存目录应出现 image.tar
	if _, err := os.Stat(env.OfflineImageTar("php", "8.4")); err != nil {
		t.Errorf("应已提升到离线缓存: %v", err)
	}
	// 事件序列：miss → promote → tempdir-cleared
	got := em.names()
	want := []string{"cache:miss", "cache:promote", "cache:tempdir-cleared"}
	if !eqNames(got, want) {
		t.Errorf("事件序列 = %v，期望 %v", got, want)
	}
}

func TestInstallCacheHitZeroNetwork(t *testing.T) {
	fb := &fakeBE{}
	s, cm, em, env := newStep(t, fp{ver: "28.3.2"}, fb)

	// 预置 image.tar + manifest（正确 SHA256）构造命中
	tar := env.OfflineImageTar("php", "8.4")
	if err := os.MkdirAll(filepath.Dir(tar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tar, []byte("CACHED-TAR"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := cache.FileSHA256(tar)
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.SaveManifest(&model.CacheManifest{
		SchemaVersion: 1, Kind: "php", Version: "8.4",
		Image: &model.ManifestImage{Name: "php:8.4-fpm", Sha256: sha, Size: 10},
	}); err != nil {
		t.Fatalf("保存 manifest 失败: %v", err)
	}

	if err := s.Execute(context.Background(), &recLog{}); err != nil {
		t.Fatalf("命中安装应成功: %v", err)
	}
	if len(fb.pulled) != 0 {
		t.Errorf("命中必须零网络，pull 应为空, got %v", fb.pulled)
	}
	if len(fb.loaded) != 1 {
		t.Errorf("命中应 load 一次, got %v", fb.loaded)
	}
	if !eqNames(em.names(), []string{"cache:hit"}) {
		t.Errorf("命中事件应仅 cache:hit, got %v", em.names())
	}
}

func eqNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tempdirReasons 收集所有 cache:tempdir-cleared 事件的 reason
func (c *capEm) tempdirReasons() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.es {
		if e.name == "cache:tempdir-cleared" {
			if ev, ok := e.p.(model.CacheTempdirClearedEvent); ok {
				out = append(out, ev.Reason)
			}
		}
	}
	return out
}

// pull 中途取消：返回 ctx.Canceled、不提升任何缓存、临时目录以 reason=cancelled 清空（无残留）
func TestInstallCancelDuringPull(t *testing.T) {
	fb := &fakeBE{pullBlocks: true}
	s, _, em, env := newStep(t, fp{ver: "28.3.2"}, fb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Execute(ctx, &recLog{}) }()

	// 等 pull 已进入阻塞，再发起取消
	for len(fb.pulledSnapshot()) == 0 {
		runtime.Gosched()
	}
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应返回 context.Canceled，得 %v", err)
	}

	// 无半截镜像：未 save、未提升缓存
	if len(fb.saved) != 0 {
		t.Errorf("取消后不应发生 save, got %v", fb.saved)
	}
	if _, err := os.Stat(env.OfflineImageTar("php", "8.4")); !os.IsNotExist(err) {
		t.Errorf("取消不得提升缓存, stat err=%v", err)
	}
	// 无临时目录残留，且 reason = cancelled（必清时机 3）
	if _, err := os.Stat(env.TempExtDir("php", "8.4")); !os.IsNotExist(err) {
		t.Errorf("取消后临时目录应被清空, stat err=%v", err)
	}
	reasons := em.tempdirReasons()
	if len(reasons) != 1 || reasons[0] != cache.ReasonCancelled {
		t.Errorf("应恰有一次 tempdir-cleared 且 reason=cancelled，得 %v", reasons)
	}
}

// 未命中且无网络：pull 失败 → 明确报错、不提升任何缓存、临时目录必清（无残留）
func TestInstallMissPullFailureNoDirtyState(t *testing.T) {
	fb := &fakeBE{pullErr: errors.New("network unreachable")}
	s, _, em, env := newStep(t, fp{ver: "28.3.2"}, fb)
	if err := s.Execute(context.Background(), &recLog{}); err == nil {
		t.Fatal("无缓存且拉取失败应明确报错")
	}
	if _, err := os.Stat(env.OfflineImageTar("php", "8.4")); !os.IsNotExist(err) {
		t.Errorf("失败不得提升缓存, stat err=%v", err)
	}
	// tempdir-cleared 必发（必清时机）
	found := false
	for _, n := range em.names() {
		if n == "cache:tempdir-cleared" {
			found = true
		}
	}
	if !found {
		t.Errorf("失败路径必须清空临时目录并发 cache:tempdir-cleared, got %v", em.names())
	}
}
