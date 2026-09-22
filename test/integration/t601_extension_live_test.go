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
	"bytes"
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

	ctx := context.Background()
	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env, cfg)
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

// TestT601_Extension_BaseFallback_Live 真机取证 v2.9.10 冻结的「基座退回即全量重编译」（§5.16.2）。
//
// 场景：某版本已应用过扩展，但固化镜像 phpo/php:{ver} 不在本机（换机没带过来、被手工删掉）。
// 此时容器上没有任何 prev 扩展，若仍只编译本次新增项，任务结尾却把完整目标集写进权威库并广播快照——
// 界面显示「已启用」而 php -m 里没有，正是 §5.13.1「Docker 实际状态 ≡ 库里状态」被破。
//
// 断言（旧实现在第 5 步即失败：容器里只有新项、没有 prev 项）：
//  1. 基座镜像不含待测扩展（否则 php -m 假绿）；
//  2. 首轮 Apply 后容器里有该扩展、固化镜像存在；
//  3. 撤掉容器 + 固化镜像，库里 prev 不变；
//  4. 第二轮扩充目标集后，容器内 php -m **同时**含 prev 项与新增项，且 List 与之一致。
//
// 版本标签刻意取上游不存在的 9.9：本机 php:8.4-fpm 与用户的 phpo-php-8.4 容器同名冲突，
// 撞名会被 Pre-Clean 摧毁真实环境（硬红线 8/19）；9.9 用别名镜像独占，离线也不联网拉取。
func TestT601_Extension_BaseFallback_Live(t *testing.T) {
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

	ctx := context.Background()
	var em nopEmitter
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)
	extSvc := service.NewExtensionService(cli, cacheMgr, st, nil, em, env, tm)

	// 两根必须先写入 config.yaml：运行态存储按 RootsPersisted 才读写（§4.2 首启零落盘），
	// 未落根时 BuildSnapshot 一律返回空快照 → List() 恒为空 → prev 恒为空 → 本用例要证的
	// 「基座退回」分支根本进不去（假阴性：看着跑完，其实什么都没测）。
	if err := cfg.SetRoots(home, home+"/www"); err != nil {
		t.Fatalf("落两根失败: %v", err)
	}

	const ver = "9.9"
	const extPrev = "pcntl" // 内置扩展：docker-php-ext-install 无需额外系统依赖
	const extNew = "shmop"  // 与 pcntl 同批编译，且都不在基座默认启用集内

	baseRef, err := engine.ImageRefFor(string(model.KindPHP), ver)
	if err != nil {
		t.Fatalf("镜像引用派生失败: %v", err)
	}
	if ok, e := cli.ImageExists(ctx, baseRef); e != nil || !ok {
		t.Skipf("本机无基座镜像 %s（live 用例不联网）；准备：docker tag php:8.4-fpm %s", baseRef, baseRef)
	}

	container := dockerutil.ContainerName(string(model.KindPHP), ver)
	committedRef := engine.CommittedPHPRef(ver)

	if err := appSvc.Install(ctx, model.KindPHP, ver); err != nil {
		t.Fatalf("安装 php 失败: %v", err)
	}
	defer func() {
		_ = appSvc.Remove(context.Background(), model.KindPHP, ver)
		_ = cli.ImageRemove(context.Background(), committedRef)
		_ = os.RemoveAll(env.PHPOHome + "/offline/php/" + ver)
	}()

	waitPHPReady(t, container)
	out, err := dockerExec(container, "php", "-m")
	if err != nil {
		t.Fatalf("读取 php -m 失败: %v", err)
	}
	if phpHasModule(out, extPrev) || phpHasModule(out, extNew) {
		t.Fatalf("基座镜像不应已含 %s/%s，否则本用例断言假绿，实得:\n%s", extPrev, extNew, out)
	}

	// 首轮：只启用 extPrev → 编译 → 固化 → 重建
	if err := extSvc.Apply(ctx, ver, []string{extPrev}); err != nil {
		t.Fatalf("首轮 Apply 失败: %v", err)
	}
	waitPHPReady(t, container)
	if out, err := dockerExec(container, "php", "-m"); err != nil || !phpHasModule(out, extPrev) {
		t.Fatalf("首轮后 php -m 应含 %s，实得 %q err=%v", extPrev, out, err)
	}
	if ok, err := cli.ImageExists(ctx, committedRef); err != nil || !ok {
		t.Fatalf("固化镜像 %s 应存在，ok=%v err=%v", committedRef, ok, err)
	}

	// 模拟「固化镜像不在本机」：先撤容器（镜像被占用删不掉），再删镜像；库里 prev 集合不动
	if err := cli.PreCleanContainer(ctx, container); err != nil {
		t.Fatalf("Pre-Clean 容器失败: %v", err)
	}
	if err := cli.ImageRemove(ctx, committedRef); err != nil {
		t.Fatalf("删除固化镜像失败: %v", err)
	}
	if ok, err := cli.ImageExists(ctx, committedRef); err != nil || ok {
		t.Fatalf("固化镜像 %s 应已不在本机，ok=%v err=%v", committedRef, ok, err)
	}
	if got, err := extSvc.List(ver); err != nil || len(got) != 1 || got[0] != extPrev {
		t.Fatalf("库里 prev 应为 [%s]，实得 %v err=%v", extPrev, got, err)
	}

	// 第二轮：扩充到 {prev, new}。固化镜像缺席 → 容器从基座重建 → 必须全量重编译
	if err := extSvc.Apply(ctx, ver, []string{extPrev, extNew}); err != nil {
		t.Fatalf("第二轮 Apply 失败: %v", err)
	}
	waitPHPReady(t, container)
	out, err = dockerExec(container, "php", "-m")
	if err != nil {
		t.Fatalf("读取 php -m 失败: %v", err)
	}
	for _, e := range []string{extPrev, extNew} {
		if !phpHasModule(out, e) {
			t.Fatalf("基座退回后容器应含完整目标集，缺 %s（只编译了新增项 = §5.16.2 破口），php -m:\n%s", e, out)
		}
	}
	// 权威库与实际容器必须同集合（硬红线 4 + §5.13.1）
	if got, err := extSvc.List(ver); err != nil || strings.Join(got, " ") != extPrev+" "+extNew {
		t.Fatalf("List(%s) 应为 [%s %s]，实得 %v err=%v", ver, extPrev, extNew, got, err)
	}
}

// phpHasModule 按行精确匹配 php -m 的模块名（Contains 会把相邻模块的子串算成命中）
func phpHasModule(out, name string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), name) {
			return true
		}
	}
	return false
}

// TestT601_ExecStream_Live 真机取证：容器内 exec 的输出必须去帧且 stdout/stderr 分流。
// 非 tty 流每条输出前带 8 字节帧头，不去帧即把 0x01 0x00… 混进正文——备份的逻辑转储会被写坏、
// 抽屉日志会带上不可见垃圾（本仓库曾因此出过 sha256 假阴性）。
// 全程只读：只对已在跑的 phpo php 容器执行 echo / exit，不建容器、不改任何状态；
// 本机没有运行中的 php 容器即跳过，绝不去动用户环境。
func TestT601_ExecStream_Live(t *testing.T) {
	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	defer cli.Close()
	skipUnlessLive(t, cli)

	ctx := context.Background()
	actual, err := cli.ManagedContainers(ctx)
	if err != nil {
		t.Fatalf("枚举托管容器失败: %v", err)
	}
	name := ""
	for _, a := range actual {
		if a.Running && a.Ref.Kind == string(model.KindPHP) {
			name = a.Ref.Name()
			break
		}
	}
	if name == "" {
		t.Skip("本机无运行中的 phpo php 容器，跳过只读取证")
	}

	var out, errOut bytes.Buffer
	if err := cli.ExecStream(ctx, name, []string{"sh", "-c", "echo phpo-stdout-marker; echo phpo-stderr-marker 1>&2"}, &out, &errOut); err != nil {
		t.Fatalf("ExecStream 应成功: %v", err)
	}
	if got := out.String(); got != "phpo-stdout-marker\n" {
		t.Fatalf("stdout 混入帧头/串扰，实得 %q", got)
	}
	if got := errOut.String(); got != "phpo-stderr-marker\n" {
		t.Fatalf("stderr 未分流，实得 %q", got)
	}

	// 退出码非零必须上抛：编译失败要靠它判定「哪个扩展装砸了」
	errOut.Reset()
	if err := cli.ExecStream(ctx, name, []string{"sh", "-c", "exit 3"}, &bytes.Buffer{}, &errOut); err == nil {
		t.Fatal("退出码非零应报错")
	} else if !strings.Contains(err.Error(), "3") {
		t.Fatalf("错误消息应含退出码，实得 %v", err)
	}
}
