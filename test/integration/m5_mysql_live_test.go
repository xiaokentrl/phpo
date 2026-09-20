// M5 · MySQL 全生命周期验收（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestM5_MySQL_Live -v
//
// 覆盖：真实 store（实现 PasswordReader）注册 DBService → AppService.Install 缓存优先装 mysql
// → 轮询 mysqladmin ping 就绪（initdb 慢，running ≠ 可连）→ 建库写数 → 停止 → 卸载（宿主 bind 数据目录保留）
// → 复用同宿主数据目录重装 → 查询数据不丢（§5.13.11 重装复用卷的 bind 版）。
// 数据持久走 config.MOUNTS 的宿主 bind（{MYSQL_ROOT}/{ver}/data → /var/lib/mysql），RemoveContainer 不触碰宿主目录。
package integration

import (
	"context"
	"os/exec"
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
	"phpo/pkg/dockerutil"
)

const mysqlRootPassword = config.DefaultPassword // store 未设密码 → DBService 回落默认 123456

func dockerExec(container string, args ...string) (string, error) {
	full := append([]string{"exec", container}, args...)
	out, err := exec.Command("docker", full...).CombinedOutput()
	return string(out), err
}

// waitMysqlReady 轮询 mysqladmin ping 直到 initdb + 启动完成（最长 ~120s）
func waitMysqlReady(t *testing.T, container string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		out, err := dockerExec(container, "mysqladmin", "ping", "-uroot", "-p"+mysqlRootPassword)
		if err == nil && strings.Contains(out, "alive") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("等待 MySQL 就绪超时")
}

func mustMySQLQuery(t *testing.T, container, sql string) string {
	t.Helper()
	out, err := dockerExec(container, "mysql", "-uroot", "-p"+mysqlRootPassword, "-N", "-B", "-e", sql)
	if err != nil {
		t.Fatalf("执行 SQL 失败: %v\n输出: %s", err, out)
	}
	return out
}

func TestM5_MySQL_Live(t *testing.T) {
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
	lc := service.NewLifecycle(cli, st, em, env, cfg) // ConfigStore 提供密码/端口 → mysql/pgsql 注册
	tm := task.NewManager(em)
	appSvc := service.NewAppService(lc, tm, steps.NewCacheManager(env, em, cli), cli, env)

	ctx := context.Background()
	const ver = "8.0"
	container := dockerutil.ContainerName(string(model.KindMySQL), ver)

	// 1) 安装并启动
	if err := appSvc.Install(ctx, model.KindMySQL, ver); err != nil {
		t.Fatalf("安装 mysql 失败: %v", err)
	}
	defer func() { _ = appSvc.Remove(context.Background(), model.KindMySQL, ver) }()

	waitMysqlReady(t, container)

	// 2) 建库写数
	mustMySQLQuery(t, container, "CREATE DATABASE app; CREATE TABLE app.item(id INT PRIMARY KEY, name VARCHAR(32)); INSERT INTO app.item VALUES (1, 'hello-phpo');")
	if got := mustMySQLQuery(t, container, "SELECT name FROM app.item WHERE id=1;"); strings.TrimSpace(got) != "hello-phpo" {
		t.Fatalf("写入后查询异常，实得 %q", got)
	}

	// 3) 停止 → 卸载：容器消失但宿主数据目录保留
	if err := appSvc.Stop(ctx, model.KindMySQL, ver); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	dataDir := env.MysqlRoot + "/" + ver + "/data"
	if err := appSvc.Remove(ctx, model.KindMySQL, ver); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if entries := dirNonEmpty(dataDir); !entries {
		t.Fatalf("卸载后宿主数据目录应保留（§5.13.11），实为空: %s", dataDir)
	}

	// 4) 重装复用同数据目录 → 数据不丢
	if err := appSvc.Install(ctx, model.KindMySQL, ver); err != nil {
		t.Fatalf("重装 mysql 失败: %v", err)
	}
	waitMysqlReady(t, container)
	if got := mustMySQLQuery(t, container, "SELECT name FROM app.item WHERE id=1;"); strings.TrimSpace(got) != "hello-phpo" {
		t.Fatalf("重装后数据未保留，实得 %q", got)
	}
}

func dirNonEmpty(path string) bool {
	ents, err := exec.Command("ls", "-A", path).Output()
	return err == nil && len(strings.TrimSpace(string(ents))) > 0
}
