// T601 验收：应用扩展三段式——happy（写清单→缓存优先基座→容器内内置工具编译→commit 固化→save 提升→重建→重载→落库广播）、
// 无变化即空操作、编译失败注入回滚（容器退回原镜像、临时目录清空、缓存无新条目、库不变）。
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
)

// ---- 假件 ----

// fakeExtRuntime 记录 docker 侧调用序列；execFailOn 命中即报错以注入编译失败
type fakeExtRuntime struct {
	running     map[string]bool
	images      map[string]string // 容器名 → 当前运行镜像
	execs       []string          // 依次记录的 exec argv（join）
	commits     []string          // commit ref 序列
	removedImg  []string
	savedTo     string
	promoteOK   bool
	execFailOn  string // 命中该子串的 exec 失败
	createCalls []engine.ContainerSpec
}

func newFakeExtRuntime() *fakeExtRuntime {
	return &fakeExtRuntime{running: map[string]bool{}, images: map[string]string{}}
}

func (f *fakeExtRuntime) ManagedContainers(context.Context) ([]engine.ActualState, error) {
	return nil, nil
}
func (f *fakeExtRuntime) CreateServiceContainer(_ context.Context, _ config.Env, spec engine.ContainerSpec) (string, error) {
	name := dockerName(spec.Kind, spec.Version)
	f.createCalls = append(f.createCalls, spec)
	f.images[name] = spec.Image
	return "id", nil
}
func (f *fakeExtRuntime) StartContainer(_ context.Context, name string) error {
	f.running[name] = true
	return nil
}
func (f *fakeExtRuntime) StopContainer(_ context.Context, name string) error {
	f.running[name] = false
	return nil
}
func (f *fakeExtRuntime) RemoveContainer(_ context.Context, name string) error {
	delete(f.running, name)
	delete(f.images, name)
	return nil
}

// ImageExists 扩展链路不探镜像库：本次 commit 过的 ref 即视为本机就绪
func (f *fakeExtRuntime) ImageExists(_ context.Context, ref string) (bool, error) {
	for _, c := range f.commits {
		if c == ref {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeExtRuntime) PreCleanContainer(ctx context.Context, name string) error {
	return f.RemoveContainer(ctx, name)
}

// PublishedPorts 扩展链路不重建 nginx，宿主端口实探恒空
func (f *fakeExtRuntime) PublishedPorts(context.Context, string) ([]int, error) {
	return nil, nil
}

// ContainerExists 假世界以镜像登记记容器存在
func (f *fakeExtRuntime) ContainerExists(_ context.Context, name string) bool {
	_, ok := f.images[name]
	return ok
}
func (f *fakeExtRuntime) ContainerRunning(_ context.Context, name string) (bool, error) {
	return f.running[name], nil
}
func (f *fakeExtRuntime) ExecInContainer(_ context.Context, name string, cmd []string) (string, error) {
	line := strings.Join(cmd, " ")
	f.execs = append(f.execs, line)
	if f.execFailOn != "" && strings.Contains(line, f.execFailOn) {
		return "configure: error", errors.New("容器内命令失败: " + line)
	}
	return "ok", nil
}
func (f *fakeExtRuntime) CommitContainer(_ context.Context, name, ref string) error {
	f.commits = append(f.commits, ref)
	f.images[name] = ref
	return nil
}
func (f *fakeExtRuntime) ImageSave(_ context.Context, ref, dst string) error {
	f.savedTo = dst
	// 模拟 docker save 产物，使提升有文件可校验
	return os.WriteFile(dst, []byte("tar:"+ref), 0o644)
}
func (f *fakeExtRuntime) ImageRemove(_ context.Context, ref string) error {
	f.removedImg = append(f.removedImg, ref)
	return nil
}

// fakeImageCache 记录缓存调用；EnsureTempDir 返回真实临时子目录
type fakeImageCache struct {
	home        string
	ensured     []string
	promoted    []string
	cleared     []string
	cachedRef   string
	cachedRefOK bool
}

func (c *fakeImageCache) EnsureImage(_ context.Context, kind, version, ref string) error {
	c.ensured = append(c.ensured, kind+"/"+version+"="+ref)
	return nil
}
func (c *fakeImageCache) CachedImageRef(string, string) (string, bool) {
	return c.cachedRef, c.cachedRefOK
}
func (c *fakeImageCache) PromoteImage(kind, version, ref, tmpTar string) error {
	if _, err := os.Stat(tmpTar); err != nil {
		return err
	}
	c.promoted = append(c.promoted, kind+"/"+version+"="+ref)
	return nil
}
func (c *fakeImageCache) EnsureTempDir(kind, version string) (string, error) {
	d := filepath.Join(c.home, kind, version, "ext")
	return d, os.MkdirAll(d, 0o755)
}
func (c *fakeImageCache) ClearTempDir(_ context.Context, kind, version, reason string) error {
	d := filepath.Join(c.home, kind, version, "ext")
	if err := os.RemoveAll(d); err != nil {
		return err
	}
	c.cleared = append(c.cleared, reason)
	return nil
}

// extStore 实现 ExtStore
type extStore struct {
	snap    *model.Snapshot
	saved   map[string][]string
	failSet bool
}

func newExtStore() *extStore {
	return &extStore{snap: model.NewSnapshot(), saved: map[string][]string{}}
}
func (s *extStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, nil }
func (s *extStore) SetPHPExtensions(version string, exts []string) error {
	if s.failSet {
		return errors.New("db down")
	}
	s.snap.PHPExtensions[version] = append([]string(nil), exts...)
	s.saved[version] = append([]string(nil), exts...)
	return nil
}

func dockerName(kind, version string) string {
	return "phpo-" + kind + "-" + version
}

func newExtSvc(t *testing.T) (*ExtensionService, *fakeExtRuntime, *fakeImageCache, *extStore, *fakeEmitter, config.Env) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	rt := newFakeExtRuntime()
	c := &fakeImageCache{home: home}
	st := newExtStore()
	em := &fakeEmitter{}
	tm := task.NewManager(em)
	svc := NewExtensionService(rt, c, st, nil, em, env, tm)
	return svc, rt, c, st, em, env
}

// ---- 测试 ----

func TestExtension_Apply_Happy(t *testing.T) {
	svc, rt, c, st, em, env := newExtSvc(t)
	// 基座容器未运行：ensureRunning 先从 base 建并启动
	err := svc.Apply(context.Background(), "8.4", []string{"redis", "gd"})
	if err != nil {
		t.Fatal(err)
	}
	// 编译命令（added 稳定序：gd 内置先，redis 走 pecl 两步）
	want := []string{"docker-php-ext-install gd", "pecl install redis", "docker-php-ext-enable redis"}
	if strings.Join(rt.execs, "|") != strings.Join(want, "|") {
		t.Fatalf("exec 序列不符\n期望 %v\n实得 %v", want, rt.execs)
	}
	// 固化到 phpo 专用 tag
	if got := engine.CommittedPHPRef("8.4"); len(rt.commits) != 1 || rt.commits[0] != got {
		t.Fatalf("应 commit 到 %s，实得 %v", got, rt.commits)
	}
	// 提升离线缓存：kind/version=ref
	if len(c.promoted) != 1 || c.promoted[0] != "php/8.4="+engine.CommittedPHPRef("8.4") {
		t.Fatalf("应提升固化镜像到离线缓存，实得 %v", c.promoted)
	}
	// 最终运行容器用固化镜像
	if rt.images[dockerName("php", "8.4")] != engine.CommittedPHPRef("8.4") {
		t.Fatalf("最终容器应运行固化镜像，实得 %v", rt.images)
	}
	// 临时目录已清空
	if _, err := os.Stat(filepath.Join(env.RootFor("php", "8.4"), "ext")); !os.IsNotExist(err) {
		t.Fatalf("临时目录应已清空")
	}
	// 落库 + 事件
	if strings.Join(st.saved["8.4"], ",") != "redis,gd" {
		t.Fatalf("库未落地扩展集: %v", st.saved)
	}
	if !em.has("service:changed") || !em.has("state:changed") || !em.has("task:done") {
		t.Fatalf("应发 service:changed+state:changed+task:done，实得 %v", em.events)
	}
}

func TestExtension_Apply_NoChange(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	_ = st.SetPHPExtensions("8.4", []string{"redis", "gd"})
	rt.execs = nil
	if err := svc.Apply(context.Background(), "8.4", []string{"gd", "redis"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.execs) != 0 || len(rt.commits) != 0 {
		t.Fatalf("集合未变不应触发任何 docker 操作，execs=%v commits=%v", rt.execs, rt.commits)
	}
}

// TestExtension_Apply_NoCommittedImage_UsesBase 原启用过扩展、但固化镜像已不在本机（手工 docker rmi /
// 换机没带过来）：先恢复容器时必须退回基座镜像。拿一个不存在的 phpo/php:{v} 去建容器，
// 等于让整条扩展链路卡在 "No such image" 死路上——连「重新启用一个扩展」都做不到。
func TestExtension_Apply_NoCommittedImage_UsesBase(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	_ = st.SetPHPExtensions("8.4", []string{"redis", "gd"})
	if err := svc.Apply(context.Background(), "8.4", []string{"redis", "gd", "zip"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.createCalls) == 0 {
		t.Fatal("应有一次建容器")
	}
	if got := rt.createCalls[0].Image; got != "php:8.4-fpm" {
		t.Fatalf("固化镜像不在本机时恢复容器应用基座，实得 %q", got)
	}
}

func TestExtension_Apply_CompileFailure_Rollback(t *testing.T) {
	svc, rt, c, st, em, env := newExtSvc(t)
	rt.execFailOn = "pecl install redis" // 注入编译失败
	svcBefore := len(st.saved)

	err := svc.Apply(context.Background(), "8.4", []string{"redis"})
	if err == nil {
		t.Fatal("编译失败应报错")
	}
	// 未 commit（步骤在编译即失败）
	if len(rt.commits) != 0 {
		t.Fatalf("编译失败不应 commit，实得 %v", rt.commits)
	}
	// 缓存无该条目：未提升
	if len(c.promoted) != 0 {
		t.Fatalf("编译失败不应提升缓存，实得 %v", c.promoted)
	}
	// 临时目录为空（回滚后不存在）
	if _, e := os.Stat(filepath.Join(env.RootFor("php", "8.4"), "ext")); !os.IsNotExist(e) {
		t.Fatalf("编译失败后临时目录应清空")
	}
	// 容器退回原镜像（原为空集 → 基座）：最后一次 create 的镜像应为 base
	base, _ := engine.ImageRefFor("php", "8.4")
	if len(rt.createCalls) == 0 || rt.createCalls[len(rt.createCalls)-1].Image != base {
		t.Fatalf("回滚后应从基座镜像重建，实得 %+v", rt.createCalls)
	}
	// 库不变（未 Apply）
	if len(st.saved) != svcBefore {
		t.Fatalf("失败不应改库，实得 %v", st.saved)
	}
	// 失败任务发 task:done（failed）
	if !em.has("task:done") {
		t.Fatalf("应发 task:done，实得 %v", em.events)
	}
}
