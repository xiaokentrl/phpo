// M6 · T602 备份/恢复真实 Docker 验收（默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestT602_Backup_Live -v
//
// 覆盖：装一个轻量数据服务（redis）→ 创建备份（暂停/打包/重启）→ 归档落盘且含顶层条目 →
// 异机口径恢复（清空命名空间 → 解包落盘 → 逻辑重放 SQLite → 重建容器）→ 服务重运行、列表仍在。
// 仅在有本地 Docker 且显式 PHPO_LIVE=1 时执行；CI 默认跳过，只保证编译通过。
package integration

import (
	"context"
	"os"
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

func TestT602_Backup_Live(t *testing.T) {
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

	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)
	backupSvc := service.NewBackupService(
		st, lc, cacheMgr, cli,
		func(path string) (service.SnapshotReader, error) { return store.Open(path) },
		em, env, cfg, tm,
	)

	ctx := context.Background()
	const ver = "7"
	container := dockerutil.ContainerName(string(model.KindRedis), ver)

	if err := appSvc.Install(ctx, model.KindRedis, ver); err != nil {
		t.Fatalf("安装 redis 失败: %v", err)
	}
	defer func() { _ = appSvc.Remove(context.Background(), model.KindRedis, ver) }()
	waitRedisReady(t, container)

	// 创建备份
	bf, err := backupSvc.Create(ctx)
	if err != nil {
		t.Fatalf("创建备份失败: %v", err)
	}
	if _, err := os.Stat(env.BackupRoot + "/" + bf.File); err != nil {
		t.Fatalf("归档应落盘: %v", err)
	}
	if bf.Items == 0 {
		t.Fatalf("归档顶层内容组数应 > 0，实得 %d", bf.Items)
	}

	// 备份后数据服务应被自动重启（仍在运行，能 PONG）
	waitRedisReady(t, container)

	// 异机口径恢复：清空命名空间 + 重建
	if err := backupSvc.Restore(ctx, bf.File); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	waitRedisReady(t, container)

	list, err := backupSvc.List()
	if err != nil || len(list) != 1 || list[0].File != bf.File {
		t.Fatalf("恢复后列表应仍含该备份，实得 %+v err=%v", list, err)
	}
}
