// M4 集成验收 · 真实 Docker 冒烟（默认跳过）
//
// skip 守护的 live 测：仅当显式设 PHPO_LIVE=1 且本机 Docker 可用时才执行，
// 以免在 CI / 日常 go test ./... 下对共享 Docker 守护进程产生拉镜像、建容器等副作用。
// 由用户在本机有 Docker 时手动运行以完成端到端验收：
//
//	PHPO_LIVE=1 go test ./test/integration/ -run TestM4_Live -v
//
// 覆盖：真实 AppService 装 nginx（prepareService 落盘含 include /etc/nginx/sites 的 nginx.conf 并建 sites 目录）
// → SiteService 走真实 `docker exec phpo-nginx-alpine nginx -t` 校验器 + `nginx -s reload` 重载器：
// 建站（写 vhost→nginx -t）→ 切 PHP（断言精确上游 php-{ver}-fpm:9000，硬红线 1）→ 手改坏配置（断言 nginx -t 失败并回滚，硬红线 2）。
// hosts 注入假实现，绝不触碰系统 /etc/hosts 或提权。
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/internal/vhost"
	"phpo/internal/vhost/hosts"
	"phpo/pkg/dockerutil"
)

// fakeHosts 满足 steps.HostsOps：记录域名而不写系统 hosts（验收只关注 nginx/vhost 真链路）
type fakeHosts struct{ added []string }

func (f *fakeHosts) Add(domain string) (hosts.Result, error) {
	f.added = append(f.added, domain)
	return hosts.Result{Changed: true}, nil
}

// TestM4_Live 端到端：真实 nginx 容器承载 vhost，验证硬红线 1/2 真实生效
func TestM4_Live(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")

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

	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)

	ctx := context.Background()
	nginxContainer := dockerutil.ContainerName(string(model.KindNginx), "alpine")
	siteSvc := service.NewSiteService(
		st,
		vhost.New(env),
		&fakeHosts{},
		engine.NewTrash(home+"/trash"),
		vhost.NewNginxTValidator(nginxContainer), // 真实 docker exec nginx -t
		vhost.NewNginxReloader(nginxContainer),   // 真实 docker exec nginx -s reload
		tm, em, env,
	)

	// 1) 装 nginx：缓存优先拉镜像 + 落盘配置 + 建启容器（三段式 + 任务后校准）
	if err := appSvc.Install(ctx, model.KindNginx, "alpine"); err != nil {
		t.Fatalf("安装 nginx 失败: %v", err)
	}
	defer func() { // 无论如何收尾：删站点 + 卸载容器，保 Docker 清洁（§5.13.1）
		_ = siteSvc.Remove(context.Background(), "demo.test")
		_ = appSvc.Remove(context.Background(), model.KindNginx, "alpine")
	}()

	// 官方镜像 entrypoint 会先跑 /docker-entrypoint.d/*.sh 再 exec nginx，容器 running ≠ nginx master 就绪；
	// 轮询 pidfile 直到 master 写出 PID，避免紧接着的 nginx -s reload 命中「空 PID」竞态。
	waitNginxReady(t, nginxContainer)

	confPath := filepath.Join(env.NginxSitesRoot, "demo.test.conf")
	read := func() string { b, _ := os.ReadFile(confPath); return string(b) }

	// 2) 建站：写 vhost → 真实 nginx -t 必过 → 落库
	if err := siteSvc.Add(ctx, service.AddInput{Domain: "demo.test", Port: 8090, PHP: "8.4"}); err != nil {
		t.Fatalf("建站失败（nginx -t 未过？）: %v", err)
	}
	got := read()
	for _, want := range []string{"listen 8090;", "server_name demo.test;", "set $php_upstream php-8.4-fpm:9000;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("vhost 缺字段 %q，实得:\n%s", want, got)
		}
	}
	if sites, _ := st.ListSites(); len(sites) != 1 || sites[0].Domain != "demo.test" {
		t.Fatalf("落库站点异常: %+v", sites)
	}

	// 3) 切 PHP：精确上游 php-8.1-fpm:9000（硬红线 1）+ 真实 reload
	if err := siteSvc.SwitchPHP(ctx, "demo.test", "8.1"); err != nil {
		t.Fatalf("切 PHP 失败: %v", err)
	}
	got = read()
	if !strings.Contains(got, "set $php_upstream php-8.1-fpm:9000;") {
		t.Fatalf("切换后上游非精确 8.1，实得:\n%s", got)
	}
	if strings.Contains(got, "php-8.4-fpm") {
		t.Fatalf("切换后仍残留旧上游 8.4:\n%s", got)
	}

	// 4) 硬红线 2：手改坏配置 → 真实 nginx -t 失败 → 写盘回滚（文件保持上一版有效内容）
	before := got
	if err := siteSvc.SetVhostContent(ctx, "demo.test", "server {\n    listen 8090;\n    this_is_not_a_real_nginx_directive;\n}\n"); err == nil {
		t.Fatal("坏配置应被 nginx -t 拒绝，却返回成功")
	} else if !strings.Contains(err.Error(), "nginx -t") {
		t.Fatalf("错误应来自 nginx -t，实得: %v", err)
	}
	if after := read(); after != before {
		t.Fatalf("校验失败后未回滚，文件被改写:\n%s", after)
	}

	// 5) 删站：vhost 文件移除 + 落库删除
	if err := siteSvc.Remove(ctx, "demo.test"); err != nil {
		t.Fatalf("删站失败: %v", err)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Fatalf("删站后 vhost 文件应不存在: %v", err)
	}
	if sites, _ := st.ListSites(); len(sites) != 0 {
		t.Fatalf("删站后库中不应有站点: %+v", sites)
	}
}

// waitNginxReady 轮询直到容器内 nginx master 写出 pidfile（最多 ~30s），
// 规避官方镜像 entrypoint 前置脚本阶段导致的「容器已 running 但 master 未就绪」竞态。
func waitNginxReady(t *testing.T, container string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("docker", "exec", container, "cat", "/var/run/nginx.pid").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("等待 nginx master 就绪超时（pidfile 始终为空）")
}
