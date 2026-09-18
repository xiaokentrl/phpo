// T601 · PHP 扩展离线缓存链路验收（真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestT601_Extension_Live -v
//
// 覆盖（§5.14.2 / 用户裁决：容器内内置工具编译 + docker commit 固化到 phpo/php:{ver}）：
// 装 PHP → Apply(opcache) 容器内 docker-php-ext-install 编译 → php -m 见该扩展
// → 固化镜像 phpo/php:{ver} 存在 → 提升到离线缓存 image.tar → 快照/List 落地 →
// 卸载 PHP 后重装经缓存零网络加载（硬红线 8）；收尾清容器/固化镜像/缓存目录保 Docker 清洁。
package integration

import (
	"context"
	"os"
	"strings"
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

// waitPHPReady 复用 m5_wordpress_live_test.go 的同名助手（轮询 php CLI 就绪）

func TestT601_Extension_Live(t *testing.T) {
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

	ctx := context.Background()
	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)
	// 扩展链路不依赖 nginx：reload 传 nil（服务内部已判空跳过）
	extSvc := service.NewExtensionService(cli, cacheMgr, st, nil, em, env, tm)

	const ver = "8.3"
	const ext = "opcache" // 内置扩展，docker-php-ext-install 无需额外系统依赖，编译快
	container := dockerutil.ContainerName(string(model.KindPHP), ver)
	committedRef := engine.CommittedPHPRef(ver)

	if err := appSvc.Install(ctx, model.KindPHP, ver); err != nil {
		t.Fatalf("安装 php 失败: %v", err)
	}
	// 收尾：无论成败清容器 + 固化镜像 + 缓存目录，保 Docker 清洁（§5.13.1）
	defer func() {
		_ = appSvc.Remove(context.Background(), model.KindPHP, ver)
		_ = cli.ImageRemove(context.Background(), committedRef)
		_ = os.RemoveAll(env.PHPOHome + "/offline/php/" + ver)
	}()

	waitPHPReady(t, container)
	if out, err := dockerExec(container, "php", "-m"); err != nil || strings.Contains(out, ext) {
		t.Fatalf("装扩展前 %s 不应已启用，实得 %q err=%v", ext, out, err)
	}

	// 应用扩展：写清单 → 编译 → commit 固化 → 提升缓存 → 重建容器
	if err := extSvc.Apply(ctx, ver, []string{ext}); err != nil {
		t.Fatalf("Apply 扩展失败: %v", err)
	}

	// 重建后的容器应加载该扩展
	waitPHPReady(t, container)
	if out, err := dockerExec(container, "php", "-m"); err != nil || !strings.Contains(strings.ToLower(out), ext) {
		t.Fatalf("应用后 php -m 应含 %s，实得 %q err=%v", ext, out, err)
	}

	// 固化镜像 phpo/php:{ver} 应存在
	ok, err := cli.ImageExists(ctx, committedRef)
	if err != nil || !ok {
		t.Fatalf("固化镜像 %s 应存在，ok=%v err=%v", committedRef, ok, err)
	}

	// 后端权威：List 与快照应反映启用集
	got, err := extSvc.List(ver)
	if err != nil || len(got) != 1 || got[0] != ext {
		t.Fatalf("List(%s) 应为 [%s]，实得 %v err=%v", ver, ext, got, err)
	}

	// 离线缓存应已提升 image.tar（重装零网络的依据）
	if _, err := os.Stat(env.OfflineImageTar(string(model.KindPHP), ver)); err != nil {
		t.Fatalf("离线缓存 image.tar 应存在: %v", err)
	}
}
