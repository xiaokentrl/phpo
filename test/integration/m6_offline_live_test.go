// M6 · T608 画像 F 断网全链路真实 Docker 验收（默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestM6_Offline_FullChain_Live -v
//
// 端到端串起 M1–M6 全部运维面能力，兑现 §5.14 离线优先铁律（硬红线 8）：
//  1. 装机（T607 向导 HomeEnsure：建工作目录子树 + 两根目录落地 config.yaml，dirReady 由快照派生，纯探测无需 Docker）
//  2. 装服务（缓存优先镜像：在线首装必 cache:miss + cache:promote）
//  3. 装扩展（T601：容器内编译 opcache → docker commit 固化 → 提升缓存 → 必清临时目录）
//  4. 建站（T403/T405：写 vhost → 真 nginx -t → 落库；精确上游 php-{ver}-fpm:9000，硬红线 1）
//  5. 切版本（SwitchPHP：上游精确改指，reload）
//  6. 备份（T602：暂停 → 打包 SQLite+配置 → 重启）
//  7. 断网重装（画像 F 核心）：卸载容器 + 抹掉本地基镜像（模拟内网新机器）→ 仅凭离线缓存 tar
//     重装，断言全程 cache:hit 且绝不出现 cache:miss（零外网证据）
//  8. 恢复（T602：清空命名空间 → 解包 → 逻辑重放 SQLite → 重建容器）
//
// skip 守护：仅 PHPO_LIVE=1 且本机 Docker 可用才跑；CI/日常 go test ./... 下只保证编译通过，
// 绝不在共享 Docker 上产生拉镜像/建容器副作用。收尾尽最大努力清容器/固化镜像/缓存目录保 Docker 清洁（§5.13.1）。
package integration

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/internal/vhost"
	"phpo/pkg/dockerutil"
)

// recorder 并发安全的事件记录器：满足 app/cache/service/task 四套同构 Emit(string,any)，按阶段可重置以观测某段事件流
type recorder struct {
	mu    sync.Mutex
	names []string
}

func (r *recorder) Emit(event string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, event)
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = nil
}

func (r *recorder) has(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.names {
		if n == name {
			return true
		}
	}
	return false
}

func TestM6_Offline_FullChain_Live(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	if err := os.MkdirAll(env.BackupRoot, 0o755); err != nil {
		t.Fatal(err)
	}

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

	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	skipUnlessLive(t, cli)

	rec := &recorder{}
	lc := service.NewLifecycle(cli, st, rec, env, cfg)
	tm := task.NewManager(rec)
	cacheMgr := steps.NewCacheManager(env, rec, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)
	wizardSvc := service.NewWizardService(cfg, st, rec, tm)

	const phpVer = "8.3"
	const nginxVer = "alpine"
	nginxContainer := dockerutil.ContainerName(string(model.KindNginx), nginxVer)
	phpContainer := dockerutil.ContainerName(string(model.KindPHP), phpVer)
	committedRef := engine.CommittedPHPRef(phpVer)
	baseNginxRef, _ := engine.ImageRefFor(string(model.KindNginx), nginxVer)

	siteSvc := service.NewSiteService(
		st, vhost.New(env), &fakeHosts{}, engine.NewTrash(home+"/trash"),
		vhost.NewNginxTValidator(nginxContainer), vhost.NewNginxReloader(nginxContainer),
		tm, rec, env,
	)
	extSvc := service.NewExtensionService(cli, cacheMgr, st, nil, rec, env, tm)
	backupSvc := service.NewBackupService(
		st, lc, cacheMgr, cli,
		func(path string) (service.SnapshotReader, error) { return store.Open(path) },
		rec, env, cfg, tm,
	)

	ctx := context.Background()
	bg := context.Background()
	defer func() { // 收尾：删站 + 卸容器 + 删固化镜像 + 清离线缓存，保 Docker 清洁（§5.13.1）
		_ = siteSvc.Remove(bg, "demo.test")
		_ = appSvc.Remove(bg, model.KindPHP, phpVer)
		_ = appSvc.Remove(bg, model.KindNginx, nginxVer)
		_ = cli.ImageRemove(bg, committedRef)
		_ = os.RemoveAll(env.PHPOHome + "/offline")
	}()

	// 1) 装机：向导建目录子树 + 落 config.yaml 根目录 + dirReady（T607，硬红线 3/4）
	if err := wizardSvc.HomeEnsure(ctx, env.PHPOHome, env.WWWRoot); err != nil {
		t.Fatalf("装机向导 HomeEnsure 失败: %v", err)
	}
	if h, _ := cfg.Roots(); h == "" {
		t.Fatalf("装机应落库 PHPO_HOME，实得 %q", h)
	}
	if snap, _ := lc.Snapshot(); !snap.DirReady["PHPO_HOME"] {
		t.Fatalf("装机后 dirReady[PHPO_HOME] 应为 true，实得 %+v", snap.DirReady)
	}

	// 2) 装服务（在线首装）：未命中缓存 → 拉取 → 提升，必发 cache:miss + cache:promote
	rec.reset()
	if err := appSvc.Install(ctx, model.KindNginx, nginxVer); err != nil {
		t.Fatalf("安装 nginx 失败: %v", err)
	}
	if !rec.has("cache:miss") || !rec.has("cache:promote") {
		t.Fatalf("首装 nginx 应 cache:miss+cache:promote，实得事件流 %v", rec.names)
	}
	if err := appSvc.Install(ctx, model.KindPHP, phpVer); err != nil {
		t.Fatalf("安装 php 失败: %v", err)
	}
	waitNginxReady(t, nginxContainer)
	waitPHPReady(t, phpContainer)

	// 3) 装扩展（T601）：容器内编译 opcache → commit 固化 → 提升缓存 → 必清临时目录
	const ext = "opcache"
	if err := extSvc.Apply(ctx, phpVer, []string{ext}); err != nil {
		t.Fatalf("应用扩展失败: %v", err)
	}
	waitPHPReady(t, phpContainer)
	if out, err := dockerExec(phpContainer, "php", "-m"); err != nil || !strings.Contains(strings.ToLower(out), ext) {
		t.Fatalf("应用后 php -m 应含 %s，实得 %q err=%v", ext, out, err)
	}
	if _, err := os.Stat(env.PHPOHome + "/php/" + phpVer + "/ext"); !os.IsNotExist(err) {
		t.Fatalf("扩展任务结束后临时目录必清空，实得 stat err=%v", err) // §5.14.4 必清
	}
	if ok, _ := cli.ImageExists(ctx, committedRef); !ok {
		t.Fatalf("固化镜像 %s 应存在", committedRef)
	}

	// 4) 建站（硬红线 1 精确上游）
	if err := siteSvc.Add(ctx, service.AddInput{Domain: "demo.test", Port: 8090, PHP: phpVer}); err != nil {
		t.Fatalf("建站失败: %v", err)
	}
	confPath := env.NginxSitesRoot + "/demo.test.conf"
	if got := readFile(t, confPath); !strings.Contains(got, "set $php_upstream php-8.3-fpm:9000;") {
		t.Fatalf("vhost 上游应精确 php-8.3-fpm:9000，实得:\n%s", got)
	}

	// 5) 切版本：上游精确改指 8.1，旧 8.3 不残留
	if err := siteSvc.SwitchPHP(ctx, "demo.test", "8.1"); err != nil {
		t.Fatalf("切 PHP 失败: %v", err)
	}
	if got := readFile(t, confPath); !strings.Contains(got, "php-8.1-fpm:9000") || strings.Contains(got, "php-8.3-fpm") {
		t.Fatalf("切换后上游非精确 8.1 或残留 8.3，实得:\n%s", got)
	}

	// 6) 备份：归档落盘且含内容组
	bf, err := backupSvc.Create(ctx)
	if err != nil {
		t.Fatalf("创建备份失败: %v", err)
	}
	if _, err := os.Stat(env.BackupRoot + "/" + bf.File); err != nil {
		t.Fatalf("备份归档应落盘: %v", err)
	}
	if bf.Items == 0 {
		t.Fatalf("备份顶层内容组数应 > 0，实得 %d", bf.Items)
	}

	// 7) 断网重装（画像 F 核心）：卸容器 + 抹本地基镜像（模拟内网新机器），仅凭离线缓存 tar 重装
	if err := siteSvc.Remove(ctx, "demo.test"); err != nil {
		t.Fatalf("删站失败: %v", err)
	}
	if err := appSvc.Remove(ctx, model.KindNginx, nginxVer); err != nil {
		t.Fatalf("卸载 nginx 失败: %v", err)
	}
	if err := cli.ImageRemove(ctx, baseNginxRef); err != nil {
		t.Fatalf("抹除本地 nginx 镜像失败（模拟断网前置）: %v", err)
	}
	if ok, _ := cli.ImageExists(ctx, baseNginxRef); ok {
		t.Fatalf("本地 %s 应已被抹除，重装才有零外网意义", baseNginxRef)
	}
	rec.reset()
	if err := appSvc.Install(ctx, model.KindNginx, nginxVer); err != nil {
		t.Fatalf("断网口径重装 nginx 失败（缓存未命中？）: %v", err)
	}
	if !rec.has("cache:hit") {
		t.Fatalf("重装 nginx 应命中离线缓存 cache:hit，实得事件流 %v", rec.names)
	}
	if rec.has("cache:miss") {
		t.Fatalf("重装 nginx 命中缓存后不应再走网络（cache:miss），实得 %v", rec.names)
	}
	waitNginxReady(t, nginxContainer) // 零外网重装成功且服务可用

	// 8) 恢复：异机口径重建，备份列表仍在
	if err := backupSvc.Restore(ctx, bf.File); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if list, err := backupSvc.List(); err != nil || len(list) == 0 {
		t.Fatalf("恢复后备份列表应非空，实得 %+v err=%v", list, err)
	}
}

// readFile 读文本，文件缺失即致命失败
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}
