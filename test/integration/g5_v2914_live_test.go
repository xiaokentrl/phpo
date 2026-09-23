// G5 · v2.9.14 真机取证（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestG5_V2914_Live -v
//
// 一条链路覆盖本轮四项需求的落点：
//  1. 需求③/§5.14.2：扩展固化镜像与基座镜像**各占一个缓存槽位**（image.tar / image-extensions.tar，
//     清单 image / extensions_image 各一条），提升固化镜像不得覆盖基座；
//  2. 需求②/§5.20：phpo 自己的产出物在 umask 0022 的机器上**实盘 0777**（显式 chmod 归一，不是只写入参）；
//  3. 需求①/④/§5.19：用第三方工具（docker CLI）把容器与固化镜像删掉后，轻量档只报 container，
//     手动「同步状态」报出 container + extensions_image，且**绝不自动改 installed / 扩展权威库**；
//     再点一次不得重发同一份漂移（sameGaps 幂等静默）；
//  4. §5.14.3：固化镜像不在本机时，「启用」经 LoadExtImage 从 extTar **零网络 docker load** 恢复，
//     恢复后 php -m 仍含该扩展、缺失态归零。
//
// 版本标签刻意取上游不存在的 9.9：撞名会被 Pre-Clean 摧毁真机环境（硬红线 8/19）；
// 本机离线，故前置条件是本机已有可当基座用的 php:9.9-fpm 标签（见下方 Skip 提示），全程不拨网络。
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/pkg/dockerutil"
)

// recEmitter 记录发射过的事件：幂等静默与零网络恢复都要按事件计数取证
type recEmitter struct {
	mu     sync.Mutex
	events []recEvent
}

type recEvent struct {
	name    string
	payload any
}

func (r *recEmitter) Emit(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recEvent{name, payload})
}

func (r *recEmitter) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.name == name {
			n++
		}
	}
	return n
}

// lastGaps 最近一次 docker:state-drift 携带的缺失态；从未发过则 ok=false
func (r *recEmitter) lastGaps() ([]model.ServiceGap, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].name != "docker:state-drift" {
			continue
		}
		d, ok := r.events[i].payload.(model.StateDrift)
		if !ok {
			return nil, false
		}
		return d.Gaps, true
	}
	return nil, false
}

func TestG5_V2914_Live(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")

	// php-fpm 往宿主 bind 的 logs 目录写出的文件属容器 uid，宿主删不动；交回本用户 uid 后 TempDir 才收得干净
	t.Cleanup(func() {
		_ = exec.Command("docker", "run", "--rm", "-u", "0", "-v", home+":/t", "alpine:latest",
			"chown", "-R", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "/t").Run()
	})

	st, err := store.Open(home + "/phpo.db")
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg, err := config.LoadFromPath(home + "/config.yaml")
	if err != nil {
		t.Fatalf("载入 ConfigStore 失败: %v", err)
	}
	st.SetEnvProvider(cfg)
	// 两根必须先落盘：未落根时 BuildSnapshot 一律返回空快照，installed / php_extensions 全空
	// → 本用例要证的缺失态与「不自动改库」根本进不去（假绿）
	if err := cfg.SetRoots(home, home+"/www"); err != nil {
		t.Fatalf("落两根失败: %v", err)
	}

	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	skipUnlessLive(t, cli)

	ctx := context.Background()
	em := &recEmitter{}
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	lc.SetExtImageLoader(cacheMgr) // 与 internal/app/di.go 同接线：零网络恢复固化镜像的能力来自这里
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)
	extSvc := service.NewExtensionService(cli, cacheMgr, st, nil, em, env, tm)

	const ver = "9.9"
	const ext = "pcntl" // 内置扩展：docker-php-ext-install 无额外系统依赖，且不在基座默认启用集内
	container := dockerutil.ContainerName(string(model.KindPHP), ver)
	committedRef := engine.CommittedPHPRef(ver)
	baseRef, err := engine.ImageRefFor(string(model.KindPHP), ver)
	if err != nil {
		t.Fatalf("镜像引用派生失败: %v", err)
	}
	if ok, e := cli.ImageExists(ctx, baseRef); e != nil || !ok {
		t.Skipf("本机无基座镜像 %s（live 用例不联网）；准备：docker tag <本机 php 8.4 镜像 ID> %s", baseRef, baseRef)
	}

	if err := appSvc.Install(ctx, model.KindPHP, ver); err != nil {
		t.Fatalf("安装 php 失败: %v", err)
	}
	defer func() {
		_ = appSvc.Remove(context.Background(), model.KindPHP, ver)
		_ = cli.ImageRemove(context.Background(), committedRef)
	}()

	waitPHPReady(t, container)
	if out, err := dockerExec(container, "php", "-m"); err != nil || phpHasModule(out, ext) {
		t.Fatalf("装扩展前 %s 不应已启用，实得 %q err=%v", ext, out, err)
	}
	if err := extSvc.Apply(ctx, ver, []string{ext}); err != nil {
		t.Fatalf("Apply 扩展失败: %v", err)
	}
	waitPHPReady(t, container)
	if out, err := dockerExec(container, "php", "-m"); err != nil || !phpHasModule(out, ext) {
		t.Fatalf("应用后 php -m 应含 %s，实得 %q err=%v", ext, out, err)
	}

	// ---- ① 两个镜像槽位各占一份，固化镜像不得覆盖基座 ----
	baseTar := env.OfflineImageTar(string(model.KindPHP), ver)
	extTar := env.OfflineExtImageTar(string(model.KindPHP), ver)
	shaBase, shaExt := fileSha(t, baseTar), fileSha(t, extTar)
	if shaBase == shaExt {
		t.Fatalf("两槽位内容相同 = 固化镜像覆盖了基座缓存（§5.14.2）")
	}
	mf, err := cacheMgr.LoadManifest("php", ver)
	if err != nil {
		t.Fatalf("读取缓存清单失败: %v", err)
	}
	if mf == nil || mf.Image == nil || mf.Image.Name != baseRef {
		t.Fatalf("清单 image 应为基座 %s，实得 %+v", baseRef, mf)
	}
	if mf.ExtImage == nil || mf.ExtImage.Name != committedRef {
		t.Fatalf("清单 extensions_image 应为 %s，实得 %+v", committedRef, mf)
	}
	if mf.Image.Sha256 != shaBase || mf.ExtImage.Sha256 != shaExt {
		t.Fatalf("清单两条 SHA256 应与两个槽位文件逐一对应，实得 image=%s ext=%s（盘上 %s / %s）",
			mf.Image.Sha256, mf.ExtImage.Sha256, shaBase, shaExt)
	}
	// 需求③的核对落点：提升日志必须点名 image-extensions.tar 全路径
	if !dirGone(env.TempExtDir(string(model.KindPHP), ver)) {
		t.Fatalf("临时目录 %s 应已清空（§5.14.4）", env.TempExtDir(string(model.KindPHP), ver))
	}

	// ---- ② phpo 自己的产出物在 umask 0022 机器上实盘 0777 ----
	for _, p := range []string{
		env.OfflineRoot,
		filepath.Dir(baseTar),
		baseTar, extTar,
		env.OfflineManifestFile("php", ver),
		home + "/config.yaml",
		home + "/phpo.db",
	} {
		if got := permOf(t, p); got != 0o777 {
			t.Fatalf("%s 权限应为 0777（§5.20），实得 %04o", p, got)
		}
	}

	// ---- ③ 第三方工具删掉容器与固化镜像：全量口径点名，且不自动改库 ----
	if out, err := exec.Command("docker", "rm", "-f", container).CombinedOutput(); err != nil {
		t.Fatalf("外部删除容器失败: %v\n%s", err, out)
	}
	if out, err := exec.Command("docker", "rmi", "-f", committedRef).CombinedOutput(); err != nil {
		t.Fatalf("外部删除固化镜像失败: %v\n%s", err, out)
	}

	if _, err := lc.Calibrate(ctx); err != nil { // 轻量档：只判容器存在性
		t.Fatalf("轻量校准失败: %v", err)
	}
	gaps, ok := em.lastGaps()
	if !ok {
		t.Fatal("容器缺席应发 docker:state-drift")
	}
	if reasonOf(gaps, container) != model.GapContainer || hasGap(gaps, model.GapExtImage) {
		t.Fatalf("轻量档应只报 container 缺失，实得 %+v", gaps)
	}

	if _, err := lc.SyncAll(ctx); err != nil { // 手动「同步状态」：全量口径
		t.Fatalf("全量同步失败: %v", err)
	}
	gaps, _ = em.lastGaps()
	if reasonOf(gaps, container) != model.GapContainer || !hasGap(gaps, model.GapExtImage) {
		t.Fatalf("全量同步应报出 container + extensions_image，实得 %+v", gaps)
	}
	// 「该版本实际会跑的那一份」：固化镜像缺席 → 基座在机即不算镜像缺失
	if hasGap(gaps, model.GapImage) {
		t.Fatalf("基座镜像 %s 仍在本机，不应报 image 缺失，实得 %+v", baseRef, gaps)
	}
	// 绝不自动改写 installed / 扩展权威库（§5.19.3 硬口径 2）
	snap, err := lc.Snapshot()
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if !contains(snap.Installed["php"], ver) {
		t.Fatalf("外部删容器不等于卸载，installed 应保持 %s，实得 %+v", ver, snap.Installed)
	}
	if got, err := extSvc.List(ver); err != nil || len(got) != 1 || got[0] != ext {
		t.Fatalf("扩展权威库应原样保留 [%s]，实得 %v err=%v", ext, got, err)
	}

	// 幂等静默：同一份缺失不得重发（否则每次点同步都刷屏）
	before := em.count("docker:state-drift")
	if _, err := lc.SyncAll(ctx); err != nil {
		t.Fatalf("第二次全量同步失败: %v", err)
	}
	if got := em.count("docker:state-drift"); got != before {
		t.Fatalf("缺失项未变时不应重发 docker:state-drift，前 %d 后 %d", before, got)
	}

	// ---- ④ 启用：经 LoadExtImage 从 extTar 零网络恢复，扩展仍在容器里 ----
	hits := em.count("cache:hit")
	if err := appSvc.Start(ctx, model.KindPHP, ver); err != nil {
		t.Fatalf("启用应经扩展缓存槽位零网络恢复成功: %v", err)
	}
	if em.count("cache:hit") == hits {
		t.Fatal("零网络恢复固化镜像应发 cache:hit（§5.14.12）")
	}
	if ok, err := cli.ImageExists(ctx, committedRef); err != nil || !ok {
		t.Fatalf("恢复后固化镜像 %s 应回到本机，ok=%v err=%v", committedRef, ok, err)
	}
	waitPHPReady(t, container)
	if out, err := dockerExec(container, "php", "-m"); err != nil || !phpHasModule(out, ext) {
		t.Fatalf("恢复后 php -m 应含 %s，实得 %q err=%v", ext, out, err)
	}
	if _, err := lc.SyncAll(ctx); err != nil {
		t.Fatalf("恢复后再同步失败: %v", err)
	}
	snap, _ = lc.Snapshot()
	if len(snap.Gaps) != 0 {
		t.Fatalf("恢复后缺失态应归零，实得 %+v", snap.Gaps)
	}
}

// reasonOf 指定容器/引用那条缺失项的原因；没有该条即返回空
func reasonOf(gaps []model.ServiceGap, refOrName string) string {
	for _, g := range gaps {
		if g.Ref == refOrName {
			return g.Reason
		}
	}
	return ""
}

func hasGap(gaps []model.ServiceGap, reason string) bool {
	for _, g := range gaps {
		if g.Reason == reason {
			return true
		}
	}
	return false
}

func fileSha(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	if len(b) == 0 {
		t.Fatalf("%s 为空文件", path)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func permOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s 失败: %v", path, err)
	}
	return fi.Mode().Perm()
}

func dirGone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
