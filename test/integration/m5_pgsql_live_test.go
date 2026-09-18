// M5 · PostgreSQL 全生命周期验收（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestM5_PgSQL_Live -v
//
// 覆盖：跨版本 16↔17 并存不串数据——各版本经 store 落库独立宿主端口（避免 §5.8 端口 bind 冲突），
// 数据经宿主 bind 目录 {PGSQL_ROOT}/{ver}/data 天然隔离；DBService 用 config_file 命令加载挂载的 postgresql.conf。
// 查询走 docker exec（容器内 socket，trust 认证，无需宿主端口/密码）。
package integration

import (
	"context"
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

func waitPgReady(t *testing.T, container string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		out, err := dockerExec(container, "pg_isready", "-U", "postgres")
		if err == nil && strings.Contains(out, "accepting connections") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("等待 PostgreSQL 就绪超时: %s", container)
}

func mustPgQuery(t *testing.T, container, sql string) string {
	t.Helper()
	out, err := dockerExec(container, "psql", "-U", "postgres", "-tA", "-c", sql)
	if err != nil {
		t.Fatalf("psql 失败: %v\n输出: %s", err, out)
	}
	return strings.TrimSpace(out)
}

func TestM5_PgSQL_Live(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")

	st, err := store.Open(home + "/phpo.db")
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	skipUnlessLive(t, cli)

	// 为两个版本落库不同宿主端口，使并存不冲突（真实安装由 UI 传端口，这里直写 env 验证读取链路）
	_ = st.SetEnv(store.EnvKeyPort("pgsql", "17"), "5432")
	_ = st.SetEnv(store.EnvKeyPort("pgsql", "16"), "5433")

	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env)
	tm := task.NewManager(em)
	appSvc := service.NewAppService(lc, tm, steps.NewCacheManager(env, em, cli), cli, env)

	ctx := context.Background()
	c17 := dockerutil.ContainerName(string(model.KindPgsql), "17")
	c16 := dockerutil.ContainerName(string(model.KindPgsql), "16")

	if err := appSvc.Install(ctx, model.KindPgsql, "17"); err != nil {
		t.Fatalf("安装 pgsql 17 失败: %v", err)
	}
	if err := appSvc.Install(ctx, model.KindPgsql, "16"); err != nil {
		t.Fatalf("安装 pgsql 16 失败: %v", err)
	}
	defer func() {
		_ = appSvc.Remove(context.Background(), model.KindPgsql, "17")
		_ = appSvc.Remove(context.Background(), model.KindPgsql, "16")
	}()

	waitPgReady(t, c17)
	waitPgReady(t, c16)

	// 各版本写各自标记
	mustPgQuery(t, c17, "CREATE TABLE marker(v text); INSERT INTO marker VALUES ('v17');")
	mustPgQuery(t, c16, "CREATE TABLE marker(v text); INSERT INTO marker VALUES ('v16');")

	// 并存不串数据：17 只见 v17，16 只见 v16
	if got := mustPgQuery(t, c17, "SELECT v FROM marker;"); got != "v17" {
		t.Fatalf("pgsql 17 数据被串扰，实得 %q", got)
	}
	if got := mustPgQuery(t, c16, "SELECT v FROM marker;"); got != "v16" {
		t.Fatalf("pgsql 16 数据被串扰，实得 %q", got)
	}

	// 数据目录按版本独立
	for _, d := range []string{env.PgsqlRoot + "/17/data", env.PgsqlRoot + "/16/data"} {
		if !dirNonEmpty(d) {
			t.Fatalf("版本数据目录应存在且非空: %s", d)
		}
	}
}
