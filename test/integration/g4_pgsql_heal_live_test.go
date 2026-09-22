// G4 · pgsql 崩溃循环自愈的 live 验收（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestG4_PgsqlHeal_Live -v
//
// 单测锁的是文件内容与日志文案；这一单补真机上那一段因果：旧版落盘配置让容器内 postgres 往宿主
// bind 进来的 ./pgsql/{ver}/logs（宿主用户 0755、容器内另一 uid）建日志文件 → Permission denied
// → FATAL → unless-stopped 无限重启，服务永远启不来；而「启用」路径的 healPgLogging 必须把它救回
// ready（§5.18）。两步各半：先用裸 engine 启动证明旧配置确实起不来（且报错自带取证），
// 再经 AppService.Start 证明修复后可用且宿主 logs 目录不再落盘。
package integration

import (
	"context"
	"fmt"
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
	"phpo/pkg/dockerutil"
)

// 与 internal/service/workdir.go 的两个常量逐字一致（那边未导出，此处按现场原样复刻）
const (
	g4OldLogBlock = "logging_collector = on\nlog_directory = '/var/log/postgresql'\nlog_filename = 'postgresql-%Y-%m-%d.log'\n"
	g4NewLogBlock = "log_destination = 'stderr'\nlogging_collector = off\n"
)

func TestG4_PgsqlHeal_Live(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")

	// data 目录由容器内 uid 创建，宿主用户删不动；交回本用户 uid 后 t.TempDir 才收得干净，不留 sudo 欠账
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

	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	skipUnlessLive(t, cli)

	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	appSvc := service.NewAppService(lc, task.NewManager(em), steps.NewCacheManager(env, em, cli), cli, env)

	ctx := context.Background()
	// 不用 "17"：真机上 `phpo-pgsql-17` 可能是用户已装服务，同名 Pre-Clean 会把它删掉（§5.13.3）。
	// "17-alpine" 既避开命名冲突，又对应本机已有的 postgres:17-alpine（离线可装）；宿主端口另指一个，
	// 不与已发布端口相碰。
	const version = "17-alpine"
	name := dockerutil.ContainerName(string(model.KindPgsql), version)
	root := env.RootFor(string(model.KindPgsql), version)
	confPath := filepath.Join(root, "conf", "postgresql.conf")
	logsDir := filepath.Join(root, "logs")

	if err := cfg.SetServicePort("pgsql", version, 5418); err != nil {
		t.Fatalf("写入宿主端口失败: %v", err)
	}

	if err := appSvc.Install(ctx, model.KindPgsql, version); err != nil {
		t.Fatalf("安装 pgsql %s 失败: %v", version, err)
	}
	defer func() { _ = appSvc.Remove(context.Background(), model.KindPgsql, version) }()

	// 复刻崩溃现场：新装模板是 stderr 写法，原样换回旧三行（只动日志段，其余内容保持用户既有改动的形态）
	fresh, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("读取 postgresql.conf 失败: %v", err)
	}
	legacy := strings.Replace(string(fresh), g4NewLogBlock, g4OldLogBlock, 1)
	if legacy == string(fresh) {
		t.Fatalf("模板里找不到 %q，无法构造旧版现场", g4NewLogBlock)
	}
	if err := os.WriteFile(confPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("写回旧配置失败: %v", err)
	}
	if err := appSvc.Stop(ctx, model.KindPgsql, version); err != nil {
		t.Fatalf("停止 pgsql 失败: %v", err)
	}

	// ① 旧配置 + 裸 engine 启动 = 起不来，且报错自带状态/退出码/日志尾部（§5.18.3）
	started := time.Now()
	if err := cli.StartContainer(ctx, name); err == nil {
		t.Fatalf("旧配置下容器不应启动成功（等待 %v 后却稳定 running）", time.Since(started))
	} else if !strings.Contains(err.Error(), "容器日志尾部") {
		t.Fatalf("启动失败报错应带容器日志取证，实得: %v", err)
	}
	if tail := cli.LogTail(ctx, name, 50); !strings.Contains(tail, "could not open log file") {
		t.Fatalf("旧配置的失败应是日志文件建不出来，实得容器日志尾部:\n%s", tail)
	}

	// ② 「启用」路径自愈：改完配置即启动成功（崩溃循环中的容器要先打断再等就绪）
	if err := appSvc.Start(ctx, model.KindPgsql, version); err != nil {
		t.Fatalf("旧配置下启用应自愈成功，实得: %v", err)
	}
	healed, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("重新读取 postgresql.conf 失败: %v", err)
	}
	if s := string(healed); strings.Contains(s, "logging_collector = on") || !strings.Contains(s, g4NewLogBlock) {
		t.Fatalf("postgresql.conf 未自愈为 stderr 写法: %q", s)
	}

	waitPgReady(t, name)
	if got := mustPgQuery(t, name, "SELECT 1;"); got != "1" {
		t.Fatalf("自愈后查询失败，实得 %q", got)
	}
	// 日志不再往宿主 bind 目录落盘（§5.18.1）；此前那次崩溃循环也只可能留下 FATAL，不应有日志文件
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("读取宿主 logs 目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("容器又往宿主 logs 目录写了 %d 个文件: %v", len(entries), entries)
	}
}
