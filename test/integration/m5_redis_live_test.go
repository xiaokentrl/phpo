// M5 · Redis 全生命周期验收（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestM5_Redis_Live -v
//
// 覆盖：密码走容器 env（--requirepass 覆盖配置占位）→ PING 探活 → 持久化开关（appendonly yes）
// → 写键 → 停止/卸载（宿主 bind {REDIS_ROOT}/{ver}/data 保 AOF）→ 重装读回（数据不丢）。
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

const redisPassword = config.DefaultPassword

func redisCLI(container string, args ...string) (string, error) {
	full := append([]string{"-a", redisPassword}, args...)
	return dockerExec(container, append([]string{"redis-cli"}, full...)...)
}

func waitRedisReady(t *testing.T, container string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		out, err := redisCLI(container, "ping")
		if err == nil && strings.TrimSpace(out) == "PONG" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("等待 Redis 就绪超时: %s", container)
}

func TestM5_Redis_Live(t *testing.T) {
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

	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env)
	tm := task.NewManager(em)
	appSvc := service.NewAppService(lc, tm, steps.NewCacheManager(env, em, cli), cli, env)

	ctx := context.Background()
	const ver = "7"
	container := dockerutil.ContainerName(string(model.KindRedis), ver)

	if err := appSvc.Install(ctx, model.KindRedis, ver); err != nil {
		t.Fatalf("安装 redis 失败: %v", err)
	}
	defer func() { _ = appSvc.Remove(context.Background(), model.KindRedis, ver) }()

	waitRedisReady(t, container)

	// 无密码应被拒（密码确实生效）
	if out, err := dockerExec(container, "redis-cli", "ping"); err == nil && strings.Contains(out, "PONG") {
		t.Fatalf("未带密码不应通过认证，实得 %q", out)
	}

	// 持久化开关：配置 appendonly yes
	if out, err := redisCLI(container, "config", "get", "appendonly"); err == nil {
		if !strings.Contains(out, "yes") {
			t.Fatalf("appendonly 应为 yes，实得 %q", out)
		}
	}

	// 写键
	if out, err := redisCLI(container, "set", "greeting", "hello-redis"); err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("写键失败: %v %s", err, out)
	}

	// 停止 → 卸载：容器消失，宿主数据目录（AOF）保留
	if out, err := redisCLI(container, "save"); err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("同步落盘失败: %v %s", err, out)
	}
	_ = appSvc.Stop(ctx, model.KindRedis, ver)
	if err := appSvc.Remove(ctx, model.KindRedis, ver); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	dataDir := env.RedisRoot + "/" + ver + "/data"
	if !dirNonEmpty(dataDir) {
		t.Fatalf("卸载后宿主数据目录应保留 AOF，实为空: %s", dataDir)
	}

	// 重装复用同数据目录 → 读回
	if err := appSvc.Install(ctx, model.KindRedis, ver); err != nil {
		t.Fatalf("重装 redis 失败: %v", err)
	}
	waitRedisReady(t, container)
	if out, err := redisCLI(container, "get", "greeting"); err != nil || strings.TrimSpace(out) != "hello-redis" {
		t.Fatalf("重装后数据未保留，实得 %q err=%v", out, err)
	}
}
