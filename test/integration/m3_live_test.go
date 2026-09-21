// M3 集成验收 · 真实 Docker 冒烟（默认跳过）
//
// 这是 skip 守护的 live 测：仅当显式设 PHPO_LIVE=1 且本机 Docker 可用时才执行，
// 以免在 CI / 日常 go test ./... 下对共享 Docker 守护进程产生拉镜像、建容器等副作用。
// 由用户在本机有 Docker 时手动运行以完成端到端验收：
//
//	PHPO_LIVE=1 go test ./test/integration/ -run TestM3_Live -v
//
// 覆盖：真实 store + engine + cache + task.Manager + LifecycleService + AppService 全接线，
// 走「缓存优先装镜像 → 建启容器 → 校准 → 停 → 卸载保留数据卷」全链路（§5.13 / §5.14 / 硬红线 5/7）。
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
)

// nopEmitter 同时满足 app/cache/service/task 四套结构等价的最小发射接口
type nopEmitter struct{}

func (nopEmitter) Emit(string, any) {}

// newLiveGraph 用临时目录构造真实对象图，避免污染真机工作目录（PHPO_HOME，默认值 `~/phpo`）与用户数据目录
func newLiveGraph(t *testing.T) (*service.AppService, *service.LifecycleService, *engine.Client) {
	t.Helper()
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

	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	return service.NewAppService(lc, tm, cacheMgr, cli, env), lc, cli
}

func skipUnlessLive(t *testing.T, cli *engine.Client) {
	t.Helper()
	if os.Getenv("PHPO_LIVE") != "1" {
		t.Skip("设置 PHPO_LIVE=1 后运行 M3 真实 Docker 验收")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if h := engine.Check(ctx, cli); !h.CanStart {
		t.Skipf("Docker 不可用，跳过 live 验收：%s %s", h.Message, h.Hint)
	}
}

// TestM3_Live 端到端：以 Nginx（alpine 镜像小、启停快）走完整生命周期
func TestM3_Live(t *testing.T) {
	appSvc, lc, cli := newLiveGraph(t)
	skipUnlessLive(t, cli)

	ctx := context.Background()
	kind, version := model.KindNginx, "alpine"

	// 1) 安装：缓存优先拉镜像 + 建启容器 + 三段式 + 任务后校准
	if err := appSvc.Install(ctx, kind, version); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	defer func() { // 无论如何收尾卸载，保 Docker 清洁（§5.13.1）
		_ = appSvc.Remove(context.Background(), kind, version)
	}()

	snap, err := lc.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(snap.Running["nginx"], "alpine") {
		t.Fatalf("安装后 nginx/alpine 应在运行态，实得 %+v", snap.Running)
	}

	// 2) 校准应与实际一致（不产生漂移）
	if _, err := lc.Calibrate(ctx); err != nil {
		t.Fatalf("校准失败: %v", err)
	}

	// 3) 停止 → 运行态清空，安装态保留（数据保留，§5.13.7）
	if err := appSvc.Stop(ctx, kind, version); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	snap, _ = lc.Snapshot()
	if contains(snap.Running["nginx"], "alpine") {
		t.Fatal("停止后不应在运行态")
	}
	if !contains(snap.Installed["nginx"], "alpine") {
		t.Fatal("停止应保留安装态")
	}

	// 4) 卸载：容器移除 + 未安装态
	if err := appSvc.Remove(ctx, kind, version); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	snap, _ = lc.Snapshot()
	if contains(snap.Installed["nginx"], "alpine") {
		t.Fatal("卸载后不应记为已安装")
	}
}

func contains(list []string, x string) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}
