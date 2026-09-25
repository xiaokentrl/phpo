// G6 · 管理扩展弹窗「三档显示」的真机取证（§0.2 规则 37 · §5.16.2，真实 Docker，默认跳过）
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestG6_ExtStatus_Live -v
//
// 这条需求说的是：用户打开「管理扩展」时，每一颗开关只回答「这个扩展在本版本上此刻开着没有」，
// 判据必须是容器里现查的 php -m，不是上次让谁勾选的那一份。此前该口径只有单测假件背书，从未对真容器验过。
// 本用例走的是**应用自己的代码路径**（ExtensionService.Status），因此必须落在独占命名空间里：
// 版本取上游不存在的 9.9（撞名会被 Pre-Clean 摧毁真机环境，硬红线 8/19），库与目录都在 t.TempDir()，
// 基座镜像由本机已有标签提供，全程零网络，用户那份 ~/.config/phpo 一个字节都不碰。
//
// 断言分四段：
//  1. 实测到东西（Live=true）且名字已归一（PDO→pdo、Zend OPcache→opcache，方括号头行不进集）；
//  2. 三档显示的两半都在：可停用项有 ini、静态内建项没有（opcache/sodium 可停，curl/mbstring 停不掉）；
//  3. 计数复现总纲 §0.3 的 21 / 19 / 2（目录 73 项 ∩ 实测启用集 · 其中内建 · 其中可停用）；
//  4. 幂等静默 + 退回口径：同一事实不重发事件；容器停了就报「非实时」、退回库里那份、不写库不发事件。
package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
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

// 前端扩展目录的正则与第 5 项门禁 scripts/check-ext-catalog.go 同源：
// 启用态的判定对象就是「目录里那 73 项」，在后端另立一份名单等于把对账关系拆掉。
var (
	g6CatalogBlock = regexp.MustCompile(`(?s)EXT_CATALOG:\s*ExtDef\[\]\s*=\s*\[(.*?)\n\]`)
	g6EntryObject  = regexp.MustCompile(`(?s)\{[^{}]*\}`)
	g6FieldName    = regexp.MustCompile(`name:\s*'([^']+)'`)
)

// g6CatalogNames 现取 frontend/src/constants/ext.ts 的扩展名全集
func g6CatalogNames(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("../../frontend/src/constants/ext.ts")
	if err != nil {
		t.Fatalf("读取扩展目录失败: %v", err)
	}
	block := g6CatalogBlock.FindStringSubmatch(string(src))
	if block == nil {
		t.Fatal("frontend/src/constants/ext.ts 里找不到 EXT_CATALOG 数组")
	}
	set := map[string]bool{}
	for _, obj := range g6EntryObject.FindAllString(block[1], -1) {
		if n := g6FieldName.FindStringSubmatch(obj); n != nil {
			set[strings.ToLower(n[1])] = true
		}
	}
	if len(set) != 73 {
		t.Fatalf("扩展目录应为 73 项（§0.3），实得 %d", len(set))
	}
	return set
}

func TestG6_ExtStatus_Live(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")

	// php-fpm 往宿主 bind 的 logs 目录写出的文件属容器 uid，宿主删不动；交回本用户 uid 后 TempDir 才收得干净
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
	// 两根必须先落盘：未落根时 BuildSnapshot 一律返回空快照 → List() 恒为空 →
	// 「回写权威库」这一步根本看不到变化（假阴性）
	if err := cfg.SetRoots(home, home+"/www"); err != nil {
		t.Fatalf("落两根失败: %v", err)
	}

	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	skipUnlessLive(t, cli)

	ctx := context.Background()
	em := &recEmitter{}
	lc := service.NewLifecycle(cli, st, em, env, cfg)
	tm := task.NewManager(em)
	cacheMgr := steps.NewCacheManager(env, em, cli)
	appSvc := service.NewAppService(lc, tm, cacheMgr, cli, env)
	// reload 传 nil：本用例不装 nginx，扩展的只读探针与它无关
	extSvc := service.NewExtensionService(cli, cacheMgr, st, nil, em, env, tm)

	const ver = "9.9"
	container := dockerutil.ContainerName(string(model.KindPHP), ver)
	baseRef, err := engine.ImageRefFor(string(model.KindPHP), ver)
	if err != nil {
		t.Fatalf("镜像引用派生失败: %v", err)
	}
	if ok, e := cli.ImageExists(ctx, baseRef); e != nil || !ok {
		t.Skipf("本机无基座镜像 %s（live 用例不联网）；准备：docker tag <本机 php 8.4 镜像 ID> %s", baseRef, baseRef)
	}

	if err := appSvc.Install(ctx, model.KindPHP, ver); err != nil {
		t.Fatalf("安装 php 失败: %v", err)
	}
	defer func() {
		_ = appSvc.Remove(context.Background(), model.KindPHP, ver)
		_ = os.RemoveAll(env.PHPOHome + "/offline/php/" + ver)
	}()

	waitPHPReady(t, container)

	// ---- 1. 实测启用态 + 名字归一 ----
	before := em.count("state:changed")
	if got, err := extSvc.List(ver); err != nil || len(got) != 0 {
		t.Fatalf("装完基座后库里应为空集（尚未应用过扩展），实得 %v err=%v", got, err)
	}

	status, err := extSvc.Status(ctx, ver)
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if !status.Live {
		t.Fatalf("容器在跑，必须报实测态（Live=true），实得 Live=false enabled=%v", status.Enabled)
	}
	enabled := status.Enabled
	for i := 1; i < len(enabled); i++ {
		if enabled[i-1] >= enabled[i] {
			t.Fatalf("启用集必须排序且唯一，实得 %v", enabled)
		}
	}
	for _, n := range enabled {
		if strings.HasPrefix(n, "[") || n != strings.ToLower(n) || strings.Contains(n, " ") {
			t.Fatalf("名字未归一（应小写、无显示名括号头行），实得 %q 全集 %v", n, enabled)
		}
	}
	// 归一的具体两例：php -m 打的是 PDO / Zend OPcache，目录名是 pdo / opcache
	for _, n := range []string{"pdo", "opcache"} {
		if !hasName(enabled, n) {
			t.Fatalf("启用集应含归一后的 %s，实得 %v", n, enabled)
		}
	}
	for _, bad := range []string{"zend opcache", "[php]"} {
		if hasName(enabled, bad) {
			t.Fatalf("启用集混入了未归一的显示名 %q", bad)
		}
	}
	// 界面文案举过的例子必须真的亮着——基座自带的扩展从来没进过「上次目标集」，
	// 旧实现正是在这里把它们画成 off
	for _, n := range []string{"curl", "mbstring", "openssl", "dom", "xml", "sodium"} {
		if !hasName(enabled, n) {
			t.Fatalf("基座自带扩展 %s 应显示为已启用，实得 %v", n, enabled)
		}
	}

	// ---- 2. 三档显示：可停用 vs 内建不可停用 ----
	for _, n := range []string{"opcache", "sodium"} {
		if hasName(status.BuiltIn, n) {
			t.Fatalf("%s 有 conf.d ini，属可停用项，不得进内建档（点了没反应却给一颗开关 = 界面撒谎）", n)
		}
	}
	for _, n := range []string{"curl", "mbstring", "openssl", "dom", "xml", "pdo"} {
		if !hasName(status.BuiltIn, n) {
			t.Fatalf("%s 静态编进 PHP 本体，应进内建档（显示为 on · 内建不可停用），实得 builtIn=%v", n, status.BuiltIn)
		}
	}
	for _, b := range status.BuiltIn {
		if !hasName(enabled, b) {
			t.Fatalf("内建档必须是启用集的子集，%s 却不在其中", b)
		}
	}
	// 真机旁证：可停用的那两项确实各有一份 ini，其余没有（探针只看文件名，不改容器）
	if out, err := dockerExec(container, "ls", "-1", config.ExtConfDir); err != nil {
		t.Fatalf("列 conf.d 失败: %v", err)
	} else {
		for _, want := range []string{"docker-php-ext-opcache.ini", "docker-php-ext-sodium.ini"} {
			if !strings.Contains(out, want) {
				t.Fatalf("conf.d 应有 %s，实得:\n%s", want, out)
			}
		}
		// zz-phpo.ini 是挂载进去的 php.ini，不是扩展 ini，不得被当成可停用项的依据
		if strings.Contains(out, "docker-php-ext-curl.ini") {
			t.Fatalf("curl 是静态内建，不该有自己的 ini，实得:\n%s", out)
		}
	}

	// ---- 3. 复现总纲 §0.3 的 21 / 19 / 2 ----
	catalog := g6CatalogNames(t)
	var inCatalog, inCatalogBuiltIn int
	var disableable []string
	for _, n := range enabled {
		if !catalog[n] {
			continue
		}
		inCatalog++
		if hasName(status.BuiltIn, n) {
			inCatalogBuiltIn++
			continue
		}
		disableable = append(disableable, n)
	}
	t.Logf("目录内已启用 %d 项，其中内建不可停用 %d 项，可停用 %v", inCatalog, inCatalogBuiltIn, disableable)
	if inCatalog != 21 || inCatalogBuiltIn != 19 || len(disableable) != 2 {
		t.Fatalf("三档计数应为 21 已启用 / 19 内建 / 2 可停用（§0.3 真机口径），实得 %d / %d / %d（可停用项 %v）",
			inCatalog, inCatalogBuiltIn, len(disableable), disableable)
	}

	// ---- 4a. 实测集回写权威库 + 幂等静默 ----
	if got, err := extSvc.List(ver); err != nil || !sameStrings(got, enabled) {
		t.Fatalf("实测集应回写进权威库，库 %v ≢ 实测 %v err=%v", got, enabled, err)
	}
	if n := em.count("state:changed"); n != before+1 {
		t.Fatalf("首次实测到不同事实应广播一次快照，实得 %d（此前 %d）", n, before)
	}
	before = em.count("state:changed")
	if again, err := extSvc.Status(ctx, ver); err != nil || !again.Live || !sameStrings(again.Enabled, enabled) {
		t.Fatalf("第二次 Status 应给出同一份实测集，实得 %+v err=%v", again, err)
	}
	if n := em.count("state:changed"); n != before {
		t.Fatalf("同一事实不得重发事件（每次开弹窗刷一遍 = §5.19.4 的反面），实得 %d（此前 %d）", n, before)
	}

	// ---- 4b. 容器没跑 → 退回库里那份 + 明示非实时，且不写库不发事件 ----
	if err := appSvc.Stop(ctx, model.KindPHP, ver); err != nil {
		t.Fatalf("停止 php 失败: %v", err)
	}
	before = em.count("state:changed")
	off, err := extSvc.Status(ctx, ver)
	if err != nil {
		t.Fatalf("容器停止后 Status 也应正常返回（退回口径），实得 err=%v", err)
	}
	if off.Live {
		t.Fatal("容器未运行却报 Live=true，界面会把「没查到」说成「查到了」")
	}
	if len(off.BuiltIn) != 0 {
		t.Fatalf("非实时时不得给出内建判定（那是实测才有的信息），实得 %v", off.BuiltIn)
	}
	if !sameStrings(off.Enabled, enabled) {
		t.Fatalf("非实时应退回库里那一份（绝不能把整片铺成 off），实得 %v ≢ %v", off.Enabled, enabled)
	}
	if n := em.count("state:changed"); n != before {
		t.Fatalf("退回分支不写库也不发事件，实得 %d（此前 %d）", n, before)
	}
	if got, err := extSvc.List(ver); err != nil || !sameStrings(got, enabled) {
		t.Fatalf("退回分支不得改写权威库，实得 %v err=%v", got, err)
	}
}

func hasName(set []string, name string) bool {
	for _, s := range set {
		if s == name {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
