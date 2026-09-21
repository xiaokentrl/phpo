// M5 · 集成验收（T506）· 三服务与 PHP 站点联通冒烟（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestM5_WordPress_Live -v
//
// 覆盖 §10.5 / 实施顺序「阶段 5」验收：真实 AppService 装 nginx + php + mysql + redis 四容器（缓存优先），
// 以 WordPress 式最小实例（建库建表写选项 + Redis PING/SET）跑在 php-fpm 容器内，经 phpo-network 用网络别名
// 访问 mysql / redis——一次执行同时验证「PHP ↔ MySQL ↔ Redis ↔ 站点目录」四者联通（站点根 bind /var/www 双方可见）。
// 额外兑现 §1.5 空密码：MySQL 以空 root 密码安装（MYSQL_ALLOW_EMPTY_PASSWORD），PHP 端以空密码连上并读写。
// 数据服务自身的装/启/停/卸 + 卷复用已由 m5_mysql/pgsql/redis_live 覆盖；nginx→php-fpm 精确上游与 nginx -t/reload
// 已由 m4_live 覆盖；本测补齐「跨服务应用级联通」这一 M5 收口项。hosts 注入假实现，绝不触碰系统 /etc/hosts。
package integration

import (
	"context"
	"os"
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
	"phpo/pkg/dockerutil"
)

// smokePHP 是 WordPress 式最小实例：连 MySQL（空密码）建库建表读写选项，连 Redis 裸协议 PING/SET。
// 占位符 __DB_HOST__ / __REDIS_HOST__ / __REDIS_PW__ 由 Go 侧按运行期版本/密码替换。
const smokePHP = `<?php
$db = @new mysqli("__DB_HOST__", "root", "");
if ($db->connect_errno) { fwrite(STDERR, "db_connect:" . $db->connect_error . "\n"); exit(2); }
$db->query("CREATE DATABASE IF NOT EXISTS wp;");
$db->select_db("wp");
$db->query("CREATE TABLE IF NOT EXISTS wp_options (id INT PRIMARY KEY, name VARCHAR(64), value VARCHAR(255));");
$db->query("REPLACE INTO wp_options (id,name,value) VALUES (1,'siteurl','http://demo.test');");
$r = $db->query("SELECT value FROM wp_options WHERE id=1;");
$row = $r->fetch_assoc();
if (($row['value'] ?? '') !== 'http://demo.test') { fwrite(STDERR, "db_value_mismatch\n"); exit(3); }
$db->close();
echo "OK_DB\n";

$fp = @fsockopen("__REDIS_HOST__", 6379, $eno, $estr, 5);
if (!$fp) { fwrite(STDERR, "redis_connect:$estr\n"); exit(4); }
$cmd = function ($fp, ...$a) {
    $o = "*" . count($a) . "\r\n";
    foreach ($a as $x) { $s = (string)$x; $o .= "$" . strlen($s) . "\r\n$s\r\n"; }
    fwrite($fp, $o);
    return fgets($fp);
};
if ("__REDIS_PW__" !== "") { $auth = $cmd($fp, "AUTH", "__REDIS_PW__"); if (strpos($auth, "OK") === false) { fwrite(STDERR, "redis_auth:$auth\n"); exit(5); } }
$pong = $cmd($fp, "PING");
if (strpos($pong, "PONG") === false) { fwrite(STDERR, "redis_ping:$pong\n"); exit(6); }
$set = $cmd($fp, "SET", "wp:hello", "world");
if (strpos($set, "OK") === false) { fwrite(STDERR, "redis_set:$set\n"); exit(7); }
fclose($fp);
echo "OK_REDIS\n";
echo "OK_PHP " . PHP_MAJOR_VERSION . "\n";
`

// waitPHPReady 轮询 php CLI 可执行（容器 running ≠ entrypoint 完成），最长 ~60s。
func waitPHPReady(t *testing.T, container string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		out, err := dockerExec(container, "php", "-r", "echo 1;")
		if err == nil && strings.TrimSpace(out) == "1" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("等待 PHP 运行时就绪超时: %s", container)
}

func TestM5_WordPress_Live(t *testing.T) {
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
	appSvc := service.NewAppService(lc, tm, steps.NewCacheManager(env, em, cli), cli, env)
	envSvc := service.NewEnvService(cfg, st, em)
	nginxContainer := dockerutil.ContainerName(string(model.KindNginx), "alpine")
	siteSvc := service.NewSiteService(
		st, vhost.New(env), &fakeHosts{},
		engine.NewTrash(home+"/trash"),
		vhost.NewNginxTValidator(fixedContainer(nginxContainer)),
		vhost.NewNginxReloader(fixedContainer(nginxContainer)),
		tm, em, env,
	)

	ctx := context.Background()
	const (
		phpVer   = "8.4"
		mysqlVer = "8.0" // 空密码验收版本
		redisVer = "7"   // 默认密码 123456
		domain   = "demo.test"
		sitePort = 8090
	)

	phpContainer := dockerutil.ContainerName(string(model.KindPHP), phpVer)
	mysqlContainer := dockerutil.ContainerName(string(model.KindMySQL), mysqlVer)
	redisContainer := dockerutil.ContainerName(string(model.KindRedis), redisVer)

	// waitMysqlReadyEmpty 轮询空 root 密码可连（MYSQL_ALLOW_EMPTY_PASSWORD：mysqld -uroot 免密）
	waitMysqlReadyEmpty := func(t *testing.T, container string) {
		t.Helper()
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			out, err := dockerExec(container, "mysql", "-uroot", "-e", "SELECT 1;")
			if err == nil && strings.Contains(out, "1") {
				return
			}
			time.Sleep(time.Second)
		}
		t.Fatalf("等待 MySQL(空密码) 就绪超时: %s", container)
	}

	// §1.5 空密码：装 MySQL 前落库空 root 密码（exists=true），DBService 走 MYSQL_ALLOW_EMPTY_PASSWORD
	if err := envSvc.SetPassword(model.KindMySQL, mysqlVer, ""); err != nil {
		t.Fatalf("设置空密码失败: %v", err)
	}

	// 1) 装四服务（缓存优先）
	for _, inst := range []struct {
		kind model.ServiceKind
		ver  string
	}{{model.KindNginx, "alpine"}, {model.KindPHP, phpVer}, {model.KindMySQL, mysqlVer}, {model.KindRedis, redisVer}} {
		if err := appSvc.Install(ctx, inst.kind, inst.ver); err != nil {
			t.Fatalf("安装 %s/%s 失败: %v", inst.kind, inst.ver, err)
		}
	}
	defer func() { // §5.13.1 无论成败收尾：删站 + 逆序卸载，保 Docker 清洁
		_ = siteSvc.Remove(context.Background(), domain)
		for _, u := range []struct {
			kind model.ServiceKind
			ver  string
		}{{model.KindRedis, redisVer}, {model.KindMySQL, mysqlVer}, {model.KindPHP, phpVer}, {model.KindNginx, "alpine"}} {
			_ = appSvc.Remove(context.Background(), u.kind, u.ver)
		}
	}()

	waitNginxReady(t, nginxContainer)
	waitMysqlReadyEmpty(t, mysqlContainer)
	waitRedisReady(t, redisContainer)
	waitPHPReady(t, phpContainer)

	// 2) 建站：写 vhost（真实 nginx -t 必过）+ reload
	if err := siteSvc.Add(ctx, service.AddInput{Domain: domain, Port: sitePort, PHP: phpVer}); err != nil {
		t.Fatalf("建站失败: %v", err)
	}

	// 3) 落 WordPress 式实例到站点根（宿主 bind → php 容器 /var/www 可见）
	script := strings.NewReplacer(
		"__DB_HOST__", dockerutil.NetworkAlias(string(model.KindMySQL), mysqlVer),
		"__REDIS_HOST__", dockerutil.NetworkAlias(string(model.KindRedis), redisVer),
		"__REDIS_PW__", redisPassword,
	).Replace(smokePHP)
	hostFile := filepath.Join(env.WWWRoot, domain, "index.php")
	if err := os.MkdirAll(filepath.Dir(hostFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostFile, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4) 在 php-fpm 容器内执行：一次覆盖 PHP↔MySQL(空密码)↔Redis↔站点目录挂载
	out, err := dockerExec(phpContainer, "php", config.WWWContainer+"/"+domain+"/index.php")
	if err != nil {
		t.Fatalf("站点实例执行失败: %v\n输出:\n%s", err, out)
	}
	for _, want := range []string{"OK_DB", "OK_REDIS", "OK_PHP"} {
		if !strings.Contains(out, want) {
			t.Fatalf("联通冒烟缺 %s，实得:\n%s", want, out)
		}
	}
}
