// T601 验收：应用扩展三段式——happy（写清单→缓存优先基座→容器内内置工具编译→commit 固化→save 提升→重建→重载→落库广播）、
// 无变化即空操作、编译失败注入回滚（容器退回原镜像、临时目录清空、缓存无新条目、库不变）。
package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/cache"
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
	probes      []string          // 包管理器探测单独记账：它不是编译命令，别污染 exec 序列断言
	commits     []string          // commit ref 序列
	removedImg  []string
	savedTo     string
	execFailOn  string // 命中该子串的 exec 失败
	execOut     string // 每次 exec 往 stdout 写的字节，模拟 configure/make 的输出流
	createCalls []engine.ContainerSpec
	hasImages   []string // 本机已存在的镜像（不依赖本次 commit，用于「固化镜像仍在」的正常路径）

	pkgMgr      string   // 基座包管理器探测结果（apk / deb / none）；空即 deb
	copiedTo    []string // CopyTo 记账："dstDir|包文件名"
	copiedFrom  map[string][]string
	copyToErr   error
	copyFromErr error

	// 两条只读探针单独记账：它们读的是容器现状，不是编译命令，
	// 混进 execs 就会让每条「应用扩展」的命令序列断言拖上一行 php -m。
	phpMOut    string   // php -m 的原始输出（显示名，未归一）
	iniFiles   []string // conf.d 目录清单（basename）
	reads      []string // 探针 argv 记账
	phpMErr    error
	iniListErr error
}

func newFakeExtRuntime() *fakeExtRuntime {
	return &fakeExtRuntime{running: map[string]bool{}, images: map[string]string{}, copiedFrom: map[string][]string{}}
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

// ImageExists 假世界以「本次 commit 过的 ref」＋「显式预置的 hasImages」视为本机就绪
func (f *fakeExtRuntime) ImageExists(_ context.Context, ref string) (bool, error) {
	for _, r := range append(append([]string(nil), f.commits...), f.hasImages...) {
		if r == ref {
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

// ExecStream 将去帧后的 stdout/stderr 分别写进两个 writer；假件把 execOut 当 stdout、把命令行当 stderr，
// 以便测试断言编译输出逐行进了任务日志。execFailOn 命中则返回失败错误，模拟编译中断。
// 包管理器探测单独记账（它是判定基座、不是编译命令），否则每条 want 序列都要拖一行 sh -c 探针。
func (f *fakeExtRuntime) ExecStream(_ context.Context, name string, cmd []string, stdout, stderr io.Writer) error {
	line := strings.Join(cmd, " ")
	if strings.Contains(line, "/lib/apk/db/installed") {
		f.probes = append(f.probes, line)
		pm := f.pkgMgr
		if pm == "" {
			pm = string(config.PkgManagerDeb)
		}
		_, _ = io.WriteString(stdout, pm+"\n")
		return nil
	}
	// 启用态探针：php -m 打的是显示名（PDO / Zend OPcache），由被测代码负责归一
	if len(cmd) == 2 && cmd[0] == "php" && cmd[1] == "-m" {
		f.reads = append(f.reads, line)
		if f.phpMErr != nil {
			return f.phpMErr
		}
		_, _ = io.WriteString(stdout, f.phpMOut)
		return nil
	}
	if len(cmd) == 3 && cmd[0] == "ls" && cmd[2] == config.ExtConfDir {
		f.reads = append(f.reads, line)
		if f.iniListErr != nil {
			return f.iniListErr
		}
		for _, ini := range f.iniFiles {
			_, _ = io.WriteString(stdout, ini+"\n")
		}
		return nil
	}
	f.execs = append(f.execs, line)
	if f.execOut != "" {
		_, _ = io.WriteString(stdout, f.execOut)
	}
	_, _ = io.WriteString(stderr, "stderr<"+line+">\n")
	if f.execFailOn != "" && strings.Contains(line, f.execFailOn) {
		return errors.New("容器内命令失败（退出码 2）")
	}
	return nil
}

// CopyTo 记账「回填进容器的包文件」；CopyFrom 把 copiedFrom[srcDir] 落到 dstDir，
// 使「取回 → 提升」这条链在假件里仍有真文件可校验（PromoteExtension 会 stat）。
func (f *fakeExtRuntime) CopyTo(_ context.Context, _, dstDir string, hostFiles ...string) error {
	if f.copyToErr != nil {
		return f.copyToErr
	}
	for _, h := range hostFiles {
		f.copiedTo = append(f.copiedTo, dstDir+"|"+filepath.Base(h))
	}
	return nil
}

func (f *fakeExtRuntime) CopyFrom(_ context.Context, _, srcDir, dstDir string) ([]string, error) {
	if f.copyFromErr != nil {
		return nil, f.copyFromErr
	}
	names := f.copiedFrom[srcDir]
	if len(names) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dstDir, n), []byte("pkg:"+n), 0o644); err != nil {
			return nil, err
		}
	}
	return append([]string(nil), names...), nil
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
	promotedPkg []string // 提升过的扩展包文件："pecl/redis-6.0.2.tgz"
	cleared     []string
	cachedRef   string
	cachedRefOK bool

	extHits   map[string]cache.ExtLookup // "pecl/redis" → 查缓存结果
	pkgList   map[string][]string        // "apk" → ListExtPackages 命中路径
	lookupErr error
	promoteEr error
}

func (c *fakeImageCache) EnsureImage(_ context.Context, kind, version, ref string) error {
	c.ensured = append(c.ensured, kind+"/"+version+"="+ref)
	return nil
}
func (c *fakeImageCache) CachedImageRef(string, string) (string, bool) {
	return c.cachedRef, c.cachedRefOK
}
func (c *fakeImageCache) PromoteExtImage(version, ref, tmpTar string) error {
	if _, err := os.Stat(tmpTar); err != nil {
		return err
	}
	c.promoted = append(c.promoted, "php/"+version+"="+ref)
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

// 扩展包（apk / pecl）缓存：命中结果由用例显式预置，未命中即零值（走网络分支）
func (c *fakeImageCache) LookupExtPackage(_, extType, name string) (cache.ExtLookup, error) {
	if c.lookupErr != nil {
		return cache.ExtLookup{}, c.lookupErr
	}
	return c.extHits[extType+"/"+name], nil
}

func (c *fakeImageCache) ListExtPackages(_, extType string) ([]string, error) {
	return c.pkgList[extType], nil
}

func (c *fakeImageCache) PromoteExtension(_, extType, tmpFile string) error {
	if c.promoteEr != nil {
		return c.promoteEr
	}
	if _, err := os.Stat(tmpFile); err != nil {
		return err
	}
	c.promotedPkg = append(c.promotedPkg, extType+"/"+filepath.Base(tmpFile))
	return nil
}

// extStore 实现 ExtStore
type extStore struct {
	snap     *model.Snapshot
	saved    map[string][]string
	failSet  bool
	setCalls int // 回写次数：实测集与库里一致时必须一写都不发
}

func newExtStore() *extStore {
	return &extStore{snap: model.NewSnapshot(), saved: map[string][]string{}}
}
func (s *extStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, nil }
func (s *extStore) SetPHPExtensions(version string, exts []string) error {
	s.setCalls++
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

// j 把 argv 拼成假件记账的单行形态
func j(cmd []string) string { return strings.Join(cmd, " ") }

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
	// pecl 未命中缓存：先 pecl download 取包 → 宿主取回 → 提升进缓存 → 再从暂存文件编译
	rt.copiedFrom[config.ExtStagingPECL] = []string{"redis-6.0.2.tgz"}
	// 基座容器未运行：ensureRunning 先从 base 建并启动
	err := svc.Apply(context.Background(), "8.4", []string{"redis", "gd"})
	if err != nil {
		t.Fatal(err)
	}
	// 编译命令（added 稳定序：gd 内置先，redis 走 pecl：取包 → 从文件装 → 写 ini），末尾清空容器暂存目录
	want := []string{
		"docker-php-ext-install gd",
		j(config.ExtPeclDownloadCmd("redis")),
		"pecl install " + path.Join(config.ExtStagingPECL, "redis-6.0.2.tgz"),
		"docker-php-ext-enable redis",
		j(config.ExtStagingCleanupCmd),
	}
	if got := strings.Join(rt.execs, "|"); got != strings.Join(want, "|") {
		t.Fatalf("exec 序列不符\n期望 %v\n实得 %v", want, rt.execs)
	}
	// .tgz 本体已提升进离线缓存（下次即零网络命中）
	if strings.Join(c.promotedPkg, ",") != "pecl/redis-6.0.2.tgz" {
		t.Fatalf("pecl 包文件应进缓存，实得 %v", c.promotedPkg)
	}
	if !em.has("cache:miss") {
		t.Fatalf("未命中扩展包缓存应发 cache:miss，实得 %v", em.events)
	}
	// 暂存目录清理必须在 commit 之前：否则包文件被固化进 phpo/php:{version}（镜像层垃圾）
	if len(rt.execs) == 0 || rt.execs[len(rt.execs)-1] != j(config.ExtStagingCleanupCmd) {
		t.Fatalf("最后一条 exec 应是清空容器暂存目录，实得 %v", rt.execs)
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

// TestExtension_Apply_PeclCacheHit_ZeroNetwork 命中 pecl 包缓存时必须零网络：把 .tgz 回填容器暂存目录，
// 再从该文件 `pecl install`，不得出现 `pecl download`（在线取包等于把缓存形同虚设）。
func TestExtension_Apply_PeclCacheHit_ZeroNetwork(t *testing.T) {
	svc, rt, c, _, em, env := newExtSvc(t)
	pkg := filepath.Join(env.OfflineExtDir("php", "8.4", "pecl"), "redis-6.0.2.tgz")
	c.extHits = map[string]cache.ExtLookup{"pecl/redis": {Hit: true, Path: pkg, Size: 4096}}
	if err := svc.Apply(context.Background(), "8.4", []string{"redis"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pecl install " + config.ExtStagingPECL + "/redis-6.0.2.tgz",
		"docker-php-ext-enable redis",
		j(config.ExtStagingCleanupCmd),
	}
	if got := strings.Join(rt.execs, "|"); got != strings.Join(want, "|") {
		t.Fatalf("命中缓存应零网络从暂存文件编译\n期望 %v\n实得 %v", want, rt.execs)
	}
	if strings.Join(rt.copiedTo, ",") != config.ExtStagingPECL+"|redis-6.0.2.tgz" {
		t.Fatalf("应把缓存包回填容器暂存目录，实得 %v", rt.copiedTo)
	}
	if !em.has("cache:hit") {
		t.Fatalf("命中扩展包缓存应发 cache:hit，实得 %v", em.events)
	}
	if len(c.promotedPkg) != 0 {
		t.Fatalf("命中缓存不应重复提升，实得 %v", c.promotedPkg)
	}
}

// TestExtension_Apply_ApkPrefetchOnAlpine Alpine 基座的 PHPIZE_DEPS 是 .apk 形态的可离线文件：
// 先回填既有 apk 缓存，再用 --cache-dir 预取（官方脚本的 --no-cache 用完即弃，永远拿不到包文件），
// 取回宿主并提升进缓存根。
func TestExtension_Apply_ApkPrefetchOnAlpine(t *testing.T) {
	svc, rt, c, _, _, env := newExtSvc(t)
	rt.pkgMgr = string(config.PkgManagerAPK)
	c.pkgList = map[string][]string{"apk": {filepath.Join(env.OfflineExtDir("php", "8.4", "apk"), "libzip-1.0.apk")}}
	rt.copiedFrom[config.ExtStagingAPK] = []string{"oniguruma-7.9.1.apk"}
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		j(config.ExtApkPrefetchCmd()),
		"docker-php-ext-install gd",
		j(config.ExtStagingCleanupCmd),
	}
	if got := strings.Join(rt.execs, "|"); got != strings.Join(want, "|") {
		t.Fatalf("Alpine 基座应先预取构建依赖\n期望 %v\n实得 %v", want, rt.execs)
	}
	if strings.Join(rt.copiedTo, ",") != config.ExtStagingAPK+"|libzip-1.0.apk" {
		t.Fatalf("应把 apk 缓存回填暂存目录（零网络复用），实得 %v", rt.copiedTo)
	}
	if strings.Join(c.promotedPkg, ",") != "apk/oniguruma-7.9.1.apk" {
		t.Fatalf("新取回的 .apk 应提升进缓存，实得 %v", c.promotedPkg)
	}
}

// TestExtension_Apply_DebBaseSkipsApkPrefetch 官方 php:8.x-fpm 是 Debian 基座：那里没有 .apk 形态的
// 构建依赖可离线。不得照抄 apk 命令（容器内没有 apk 即必然失败并判死整单），给一行 dim 说明即可。
func TestExtension_Apply_DebBaseSkipsApkPrefetch(t *testing.T) {
	svc, rt, _, _, em, _ := newExtSvc(t)
	rt.pkgMgr = string(config.PkgManagerDeb)
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker-php-ext-install gd", j(config.ExtStagingCleanupCmd)}
	if got := strings.Join(rt.execs, "|"); got != strings.Join(want, "|") {
		t.Fatalf("deb 基座不应跑 apk 预取\n期望 %v\n实得 %v", want, rt.execs)
	}
	if !strings.Contains(strings.Join(em.logs, "\n"), "不产生可离线包文件") {
		t.Fatalf("跳过 apk 预取要有一行说明，不得静默，实得 %v", em.logs)
	}
}

// TestExtension_Apply_CacheFailuresDegrade 缓存这一层是「让下次零网络」的优化，不是本次编译的前提：
// 包取不回宿主 → 不提升、改走在线编译；提升失败 → 本次照常成功。判死整单等于把优化路径变成新的故障源。
func TestExtension_Apply_CacheFailuresDegrade(t *testing.T) {
	svc, rt, c, _, em, _ := newExtSvc(t)
	rt.copiedFrom[config.ExtStagingPECL] = []string{"redis-6.0.2.tgz"}
	rt.copyFromErr = errors.New("container is not running")
	if err := svc.Apply(context.Background(), "8.4", []string{"redis"}); err != nil {
		t.Fatalf("取回扩展包失败不应判死扩展单: %v", err)
	}
	if !containsStr(rt.execs, "pecl install redis") {
		t.Fatalf("未取得暂存包应退回在线编译，实得 %v", rt.execs)
	}
	if len(c.promotedPkg) != 0 {
		t.Fatalf("取回失败不应提升，实得 %v", c.promotedPkg)
	}
	if !strings.Contains(strings.Join(em.logs, "\n"), "取回宿主失败") {
		t.Fatalf("取回失败要留一行说明，实得 %v", em.logs)
	}

	// 提升失败（缓存根只读）：本次编译仍用暂存文件完成
	svc2, rt2, c2, _, em2, _ := newExtSvc(t)
	rt2.copiedFrom[config.ExtStagingPECL] = []string{"redis-6.0.2.tgz"}
	c2.promoteEr = errors.New("read-only cache root")
	if err := svc2.Apply(context.Background(), "8.4", []string{"redis"}); err != nil {
		t.Fatalf("提升失败不应判死扩展单: %v", err)
	}
	if !containsStr(rt2.execs, "pecl install "+config.ExtStagingPECL+"/redis-6.0.2.tgz") {
		t.Fatalf("包已在容器暂存目录，仍应零网络编译，实得 %v", rt2.execs)
	}
	if len(c2.promotedPkg) != 0 {
		t.Fatalf("提升失败不应记账，实得 %v", c2.promotedPkg)
	}
	if !strings.Contains(strings.Join(em2.logs, "\n"), "提升失败") {
		t.Fatalf("提升失败要留一行说明，实得 %v", em2.logs)
	}
}

// TestExtension_Apply_ExtPkgCorrupted 缓存里有包但 SHA256 不符：先发 cache:corrupted 再回退网络，
// 不得静默用坏包编译（§5.14.5）。
func TestExtension_Apply_ExtPkgCorrupted(t *testing.T) {
	svc, rt, c, _, em, env := newExtSvc(t)
	c.extHits = map[string]cache.ExtLookup{"pecl/redis": {Corrupted: true, Path: filepath.Join(env.OfflineExtDir("php", "8.4", "pecl"), "redis-6.0.2.tgz")}}
	if err := svc.Apply(context.Background(), "8.4", []string{"redis"}); err != nil {
		t.Fatal(err)
	}
	if !em.has("cache:corrupted") {
		t.Fatalf("损坏条目应发 cache:corrupted，实得 %v", em.events)
	}
	if !containsStr(rt.execs, j(config.ExtPeclDownloadCmd("redis"))) {
		t.Fatalf("损坏后应回退网络取包，实得 %v", rt.execs)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
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

// TestExtension_Apply_BaseFallbackRecompilesFullSet 退回基座那一支必须连带把目标集全量重编译。
// 基座镜像里不含任何 prev 扩展：若只编译 added，prev 那几项从未装进容器，
// 而任务结尾仍把「完整目标集」写库并广播快照——界面显示「已启用」，实际 php -m 里没有
// （§5.13.1 一致性：Docker 实际状态 ≡ 库里状态）。
func TestExtension_Apply_BaseFallbackRecompilesFullSet(t *testing.T) {
	svc, rt, _, st, em, _ := newExtSvc(t)
	_ = st.SetPHPExtensions("8.4", []string{"redis", "gd"})
	// 固化镜像不在本机（假件只认本次 commit 过的 ref），目标集在原集上再加一项
	if err := svc.Apply(context.Background(), "8.4", []string{"redis", "gd", "zip"}); err != nil {
		t.Fatal(err)
	}
	// gd 与 zip 内置（各一条命令），redis 走 pecl（取包 + install + enable 三条）；全量重编译即三项都在，按扩展名稳定序
	want := []string{
		"docker-php-ext-install gd",
		j(config.ExtPeclDownloadCmd("redis")),
		"pecl install redis",
		"docker-php-ext-enable redis",
		"docker-php-ext-install zip",
		j(config.ExtStagingCleanupCmd),
	}
	if got := strings.Join(rt.execs, "|"); got != strings.Join(want, "|") {
		t.Fatalf("退回基座时应全量重编译目标扩展集\n期望 %v\n实得 %v", want, rt.execs)
	}
	if strings.Join(st.saved["8.4"], ",") != "redis,gd,zip" {
		t.Fatalf("库应落地完整目标集，实得 %v", st.saved)
	}
	// 不静默：日志要说明为什么从基座重建并全量重编译
	if all := strings.Join(em.logs, "\n"); !strings.Contains(all, "不在本机") {
		t.Fatalf("应有一行说明固化镜像缺席，实得 %v", em.logs)
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

// TestExtension_Apply_DisableRemovesIni 停用扩展走删 ini：官方 php 镜像里根本没有 docker-php-ext-disable
// （真机取证 command -v 缺失），启用态由 conf.d/docker-php-ext-<name>.ini 表达。
// 继续调不存在的命令 = 用户每取消勾选一个扩展就必然编译失败并整单回滚。
func TestExtension_Apply_DisableRemovesIni(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	_ = st.SetPHPExtensions("8.4", []string{"redis", "gd"})
	// 正常路径：原扩展集来自本机已有的固化镜像（否则就是 TestExtension_Apply_BaseFallbackRecompilesFullSet 那一支）
	rt.hasImages = []string{engine.CommittedPHPRef("8.4")}
	// redis 的 ini 在盘上＝它确实可停用；若清单里没有它，停用那一步会被当作内建项跳过（另一条用例锁那种情形）
	rt.iniFiles = []string{"docker-php-ext-redis.ini"}
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatal(err)
	}
	want := "rm -f /usr/local/etc/php/conf.d/docker-php-ext-redis.ini"
	if len(rt.execs) != 1 || rt.execs[0] != want {
		t.Fatalf("停用应删该扩展的 ini\n期望 %s\n实得 %v", want, rt.execs)
	}
}

// TestExtension_Apply_StreamsBuildOutput 编译输出必须逐行实时进抽屉日志（§5.6.2 每一步都要回流）：
// 用户在界面上要看得见 configure/make 正在跑，而不是盯着一段时长未知的「执行中」。stdout 与 stderr 都要有。
func TestExtension_Apply_StreamsBuildOutput(t *testing.T) {
	svc, rt, _, _, em, _ := newExtSvc(t)
	rt.execOut = "Configuring for redis\nBuild complete. Don't forget to enable your extensions\n"
	if err := svc.Apply(context.Background(), "8.4", []string{"redis"}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(em.logs, "\n")
	for _, want := range []string{"Configuring for redis", "Build complete", "stderr<pecl install redis>"} {
		if !strings.Contains(all, want) {
			t.Fatalf("编译输出未进日志 %q，实得 %v", want, em.logs)
		}
	}
}

// TestExtension_Apply_FailureNamesExtension 失败必须点名是哪个扩展，并留一行 err 级日志：
// 错误消息会被 toast 原样弹出，「容器内命令失败（退出码 2）」不告诉用户该改哪一个。
func TestExtension_Apply_FailureNamesExtension(t *testing.T) {
	svc, rt, _, _, em, _ := newExtSvc(t)
	rt.execFailOn = "docker-php-ext-install gd"
	err := svc.Apply(context.Background(), "8.4", []string{"gd", "redis"})
	if err == nil {
		t.Fatal("编译失败应报错")
	}
	if !strings.Contains(err.Error(), "gd") {
		t.Fatalf("错误消息未点名失败的扩展: %v", err)
	}
	if !strings.Contains(err.Error(), "未应用") {
		t.Fatalf("错误消息应说明本次扩展集未生效: %v", err)
	}
	var hasErrLine bool
	for i, l := range em.levels {
		if l == "err" && strings.Contains(em.logs[i], "gd") {
			hasErrLine = true
		}
	}
	if !hasErrLine {
		t.Fatalf("应有一行 err 级日志点名扩展，实得 %+v %v", em.levels, em.logs)
	}
}

// ---- 重载 Nginx 这一步的三种缺席情形（§5.16.3「未接入 nginx 时 dim 跳过重载，不得静默」）----

// fakeReloader 记录 nginx 重载被调了几次、返回什么
type fakeReloader struct {
	calls int
	err   error
}

func (r *fakeReloader) Reload(context.Context) error {
	r.calls++
	return r.err
}

// installNginx 在权威快照里登记 nginx 单例版本（重载器按它现取容器名）
func installNginx(st *extStore, version string) {
	st.snap.Installed[string(model.KindNginx)] = []string{version}
}

// TestExtension_Apply_ReloadsWhenNginxRunning nginx 在跑时扩展重建换了 php 容器 IP，
// nginx 缓存的上游会失效 —— 这一步必须真的执行，并给一行 ok。
func TestExtension_Apply_ReloadsWhenNginxRunning(t *testing.T) {
	svc, rt, _, st, em, _ := newExtSvc(t)
	rl := &fakeReloader{}
	svc.reload = rl
	installNginx(st, "1.25")
	rt.running[dockerName("nginx", "1.25")] = true
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatal(err)
	}
	if rl.calls != 1 {
		t.Fatalf("nginx 在跑时应重载一次，实得 %d", rl.calls)
	}
	if !strings.Contains(strings.Join(em.logs, "\n"), "Nginx 已重载") {
		t.Fatalf("重载成功应有 ok 行，实得 %v", em.logs)
	}
}

// TestExtension_Apply_SkipsReloadWhenNginxNotInstalled nginx 未安装时重载器必然解析不到容器
// （生产里 NewNginxReloader 返回「nginx 未安装」错误）。判死整单等于：没装 nginx 的机器
// 永远装不上任何 PHP 扩展 —— 弹窗不关、扩展不落库、下次打开也没勾选。
func TestExtension_Apply_SkipsReloadWhenNginxNotInstalled(t *testing.T) {
	svc, _, _, _, em, _ := newExtSvc(t)
	rl := &fakeReloader{}
	svc.reload = rl
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatalf("nginx 未安装不应让扩展失败: %v", err)
	}
	if rl.calls != 0 {
		t.Fatalf("nginx 未安装不应调用重载，实得 %d", rl.calls)
	}
	if !strings.Contains(strings.Join(em.logs, "\n"), "跳过重载") {
		t.Fatalf("跳过重载必须给一行说明，不得静默，实得 %v", em.logs)
	}
}

// TestExtension_Apply_SkipsReloadWhenNginxNotRunning 装了但没在跑（含容器被第三方删掉）：
// 没有可重载的对象，同样跳过而非判失败。
func TestExtension_Apply_SkipsReloadWhenNginxNotRunning(t *testing.T) {
	svc, _, _, st, em, _ := newExtSvc(t)
	rl := &fakeReloader{}
	svc.reload = rl
	installNginx(st, "1.25")
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatalf("nginx 未运行不应让扩展失败: %v", err)
	}
	if rl.calls != 0 {
		t.Fatalf("nginx 未运行不应调用重载，实得 %d", rl.calls)
	}
	all := strings.Join(em.logs, "\n")
	if !strings.Contains(all, "未运行") || !strings.Contains(all, "跳过重载") {
		t.Fatalf("应点名「容器未运行 + 跳过重载」，实得 %v", em.logs)
	}
}

// TestExtension_Apply_ReloadFailureKeepsExtensions 重载真失败（nginx 配置坏了）不回滚扩展：
// 固化镜像已 commit、容器已在扩展镜像上运行、vhost 内容本次没改过 —— 撤回整单只会把
// 做对了的扩展工作一起丢掉，而 nginx 的问题在日志里点名即可（§0.2 规则 16）。
func TestExtension_Apply_ReloadFailureKeepsExtensions(t *testing.T) {
	svc, rt, c, st, em, _ := newExtSvc(t)
	rl := &fakeReloader{err: errors.New("nginx: [emerg] bad config")}
	svc.reload = rl
	installNginx(st, "1.25")
	rt.running[dockerName("nginx", "1.25")] = true
	if err := svc.Apply(context.Background(), "8.4", []string{"gd"}); err != nil {
		t.Fatalf("重载失败不应判死扩展单: %v", err)
	}
	if strings.Join(rt.removedImg, ",") != "" {
		t.Fatalf("不应撤回固化镜像，实得 %v", rt.removedImg)
	}
	if rt.images[dockerName("php", "8.4")] != engine.CommittedPHPRef("8.4") {
		t.Fatalf("容器应留在固化镜像上，实得 %v", rt.images)
	}
	if strings.Join(st.saved["8.4"], ",") != "gd" {
		t.Fatalf("扩展应落库，实得 %v", st.saved)
	}
	if len(c.promoted) != 1 {
		t.Fatalf("固化镜像应已提升，实得 %v", c.promoted)
	}
	var hasErrLine bool
	for i, l := range em.levels {
		if l == "err" && strings.Contains(em.logs[i], "bad config") {
			hasErrLine = true
		}
	}
	if !hasErrLine {
		t.Fatalf("重载失败要留一行 err 级日志点名原因，实得 %+v %v", em.levels, em.logs)
	}
}
