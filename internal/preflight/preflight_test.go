// preflight 单测：17 action 正/负例 + NEEDS_HOME(15) + 全局守卫 + 端口顺延 + 警告不阻止
package preflight

import (
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/pkg/errs"
)

// newWorld 构造一个 home 就绪、含 php/nginx/mysql 与一个站点的常用快照
func readyWorld() *World {
	snap := model.NewSnapshot()
	snap.DirReady["PHPO_HOME"] = true
	snap.DirReady["WWW_ROOT"] = true
	snap.Env["WWW_ROOT"] = "/www"
	snap.Installed["php"] = []string{"8.4", "8.1"}
	snap.Installed["nginx"] = []string{"alpine"}
	snap.Installed["mysql"] = []string{"8.4"}
	snap.Running["php"] = []string{"8.4"}
	snap.Running["nginx"] = []string{"alpine"}
	snap.Sites = []model.Site{{Domain: "demo.test", Port: 80, PHP: "8.4", Root: "/www/demo.test"}}
	return &World{Snap: snap, Backups: []string{"backup-a.tar.gz"},
		Offline: []model.CacheEntry{{Kind: "php", Version: "8.4"}}}
}

// —— rootless 特权端口：只警告不阻止（§5.25 P2b 真机取证：rootlessport bind: permission denied） ——

// rootlessWorld readyWorld 的 rootless 变体（引擎结论 Rootless=true）
func rootlessWorld() *World {
	w := readyWorld()
	if w.Snap.Engine == nil {
		w.Snap.Engine = &model.EngineInfo{}
	}
	w.Snap.Engine.Rootless = true
	return w
}

func TestSiteAddRootlessPrivilegedPortWarns(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "80", PHP: "8.4", Root: "/www/new.test"}, rootlessWorld())
	if !res.Ok {
		t.Fatalf("rootless 特权端口只警告不阻止: %v", res.Errors)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "rootless") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有 rootless 特权端口警告: %v", res.Warnings)
	}
}

func TestSiteAddRootlessUnprivilegedPortNoWarn(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "8080", PHP: "8.4", Root: "/www/new.test"}, rootlessWorld())
	for _, w := range res.Warnings {
		if strings.Contains(w, "rootless") {
			t.Fatalf("非特权端口不应有 rootless 警告: %v", res.Warnings)
		}
	}
}

func firstErr(res *model.PreflightResult) string {
	if len(res.Errors) == 0 {
		return ""
	}
	return res.Errors[0]
}

// —— 切换 PHP：目标容器未运行只警告不阻止（切换链路会自动启动它，追加需求） ——

func TestPhpSwitchStoppedTargetWarnsNotBlocks(t *testing.T) {
	// 8.1 已安装但未运行：警告「将自动启动」，不拦截
	res := Run(ActPhpSwitch, Ctx{Domain: "demo.test", NewPhp: "8.1"}, readyWorld())
	if !res.Ok {
		t.Fatalf("目标容器停着只警告不阻止: %v", res.Errors)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "自动启动") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有「将自动启动」警告: %v", res.Warnings)
	}
}

func TestPhpSwitchRunningTargetNoWarn(t *testing.T) {
	// 8.4 已在运行：不应出现该警告
	res := Run(ActPhpSwitch, Ctx{Domain: "demo.test", NewPhp: "8.4"}, readyWorld())
	if !res.Ok {
		t.Fatalf("切换到运行中的版本应通过: %v", res.Errors)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "自动启动") {
			t.Fatalf("运行中的目标不应有自动启动警告: %v", res.Warnings)
		}
	}
}

// —— 对账：action 数与 NEEDS_HOME 数 ——

func TestActionAndNeedsHomeCounts(t *testing.T) {
	if len(AllActions) != ActionCount || ActionCount != 20 {
		t.Fatalf("action 数应为 20，得 %d/%d", len(AllActions), ActionCount)
	}
	n := 0
	for _, a := range AllActions {
		if needsHome[a] {
			n++
		}
	}
	if n != NeedsHomeCount {
		t.Fatalf("NEEDS_HOME 应为 18，得 %d", n)
	}
	// php-switch 与 backup-delete 不在 NEEDS_HOME（原型）
	if needsHome[ActPhpSwitch] {
		t.Error("php-switch 不应在 NEEDS_HOME")
	}
}

// —— 全局守卫 ——

// 已有任务在跑时再发起写操作 → 排队（task.Manager 串行 FIFO），不是 preflight 错误。
// 重复提交的去重由 Manager 的 ErrQueued 裁决（它才持有队列真实内容），preflight 不再拦并发。
func TestConcurrentWriteQueuesNotBlocked(t *testing.T) {
	w := readyWorld()
	w.Snap.Tasks.Running = &model.TaskBrief{ID: "t-1", Label: "安装 php-8.1", Type: "install"}
	w.Snap.Tasks.Pending = []model.TaskBrief{{ID: "t-2", Label: "安装 mysql-8.4"}}
	res := Run(ActBackup, Ctx{}, w)
	if !res.Ok {
		t.Fatalf("已有任务在跑时写操作应排队而非报错，得 %+v", res.Errors)
	}
	if firstErr(res) == errs.TaskBusy {
		t.Fatalf("taskBusy 不得再作为全局守卫出现，得 %+v", res.Errors)
	}
}

func TestHomeNotReadyGuard(t *testing.T) {
	w := readyWorld()
	w.Snap.DirReady["PHPO_HOME"] = false
	res := Run(ActInstall, Ctx{Kind: "php", Version: "8.5"}, w)
	if res.Ok || firstErr(res) != errs.HomeNotReady {
		t.Fatalf("HOME 未就绪应报 homeNotReady，得 %+v", res.Errors)
	}
}

// WWW_ROOT 未就绪（目录缺失/未初始化）同样永久阻断 NEEDS_HOME 写操作
func TestWwwRootNotReadyGuard(t *testing.T) {
	w := readyWorld()
	w.Snap.DirReady["WWW_ROOT"] = false
	res := Run(ActInstall, Ctx{Kind: "php", Version: "8.5"}, w)
	if res.Ok || firstErr(res) != errs.HomeNotReady {
		t.Fatalf("WWW_ROOT 未就绪应报 homeNotReady，得 %+v", res.Errors)
	}
}

// —— install ——

func TestInstallOK(t *testing.T) {
	res := Run(ActInstall, Ctx{Kind: "php", Version: "8.3"}, readyWorld())
	if !res.Ok {
		t.Fatalf("安装新版本应通过，得 %+v", res.Errors)
	}
}

func TestInstallSvcMissing(t *testing.T) {
	res := Run(ActInstall, Ctx{Kind: "oracle", Version: "23"}, readyWorld())
	if firstErr(res) != errs.SvcMissing {
		t.Fatalf("未知服务应报 svcMissing，得 %q", firstErr(res))
	}
}

func TestInstallVersionDup(t *testing.T) {
	res := Run(ActInstall, Ctx{Kind: "php", Version: "8.4"}, readyWorld())
	if !contains(res.Errors, errs.VersionDup+": php 8.4") {
		t.Fatalf("重复版本应报 versionDup，得 %+v", res.Errors)
	}
}

func TestInstallNginxSingleton(t *testing.T) {
	res := Run(ActInstall, Ctx{Kind: "nginx", Version: "1.25"}, readyWorld())
	if !contains(res.Errors, errs.NginxSingle) {
		t.Fatalf("Nginx 单例应报 nginxSingle，得 %+v", res.Errors)
	}
}

func TestInstallBadVersionPath(t *testing.T) {
	res := Run(ActInstall, Ctx{Kind: "php", Version: "8.4/../x"}, readyWorld())
	if res.Ok {
		t.Fatal("含 .. 的版本号应被拒")
	}
}

func TestInstallBadExt(t *testing.T) {
	res := Run(ActInstall, Ctx{Kind: "php", Version: "8.6", Extensions: "redis,bad ext!"}, readyWorld())
	if !contains(res.Errors, errs.ExtInvalid+": bad ext!") {
		t.Fatalf("非法扩展名应报 extInvalid，得 %+v", res.Errors)
	}
}

// TestInstallPortConflictBlocks 安装期就要拦下被占的服务端口（§5.8 服务端口报错、不顺延）：
// install 现在携带用户所填端口，裁决必须与「装完再改端口」同口径，否则容器建起来才发现绑不上。
// 占用表里只认「一个真的应用程序」——数据服务（mysql/pgsql/redis）的宿主端口；站点端口不算。
func TestInstallPortConflictBlocks(t *testing.T) {
	w := readyWorld()
	w.Snap.Env[config.EnvKeyPort("mysql", "8.4")] = "3306"
	res := Run(ActInstall, Ctx{Kind: "pgsql", Version: "17", Port: 3306}, w)
	if res.Ok || !contains(res.Errors, errs.PortInUse+": 3306 (mysql 8.4)") {
		t.Fatalf("装到 mysql 已占的 3306 应报 portInUse，得 %+v", res.Errors)
	}
	if res := Run(ActInstall, Ctx{Kind: "pgsql", Version: "17", Port: 5433}, w); !res.Ok {
		t.Fatalf("空闲端口应放行，得 %+v", res.Errors)
	}
	// 站点端口**不算**占用者：nginx 装到 80 而 demo.test 正在 80 上服务 → 放行。
	// 站点的宿主端口本来就是 Nginx 那一颗容器发布出来的，同一颗 Nginx 按 server_name 分流即可复用，
	// 拿它拦新装的 Nginx 等于让自家的东西占自家的口。真被本机别的进程听着的口，
	// 由「创建并启动」那一步被引擎如实拒绝，不在这里凭空编一个占用者。
	wNoNginx := readyWorld()
	wNoNginx.Snap.Installed["nginx"] = nil
	if res := Run(ActInstall, Ctx{Kind: "nginx", Version: "1.27", Port: 80}, wNoNginx); !res.Ok {
		t.Fatalf("80 只被自家站点（= 自家 Nginx）发布时 nginx 应照常放行，得 %+v", res.Errors)
	}
}

// —— uninstall ——

// TestUninstallDependentSitesWarnNotBlocks 站点仍引用该 PHP 时不再阻止卸载（§0.2 规则 16）：
// 与 service-stop 同口径——说清后果，但放行。卸载后这些站点的 vhost 上游即失效，属用户可自修的结果。
func TestUninstallDependentSitesWarnNotBlocks(t *testing.T) {
	res := Run(ActUninstall, Ctx{Kind: "php", Version: "8.4"}, readyWorld())
	if !res.Ok {
		t.Fatalf("被站点依赖应告警放行，得 errors=%+v", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("应产生依赖告警")
	}
}

// TestUninstallLastPhpAllowed 最后一个 PHP 版本同样允许卸载：本产品不替程序员决定留哪个版本（§1.11）。
// 「卸空了怎么建站」由 site-add 的 PhpNeeded 把住，那是另一条规则的岗位，不是卸载的下限。
func TestUninstallLastPhpAllowed(t *testing.T) {
	w := readyWorld()
	w.Snap.Installed["php"] = []string{"8.4"}
	w.Snap.Sites = nil
	res := Run(ActUninstall, Ctx{Kind: "php", Version: "8.4"}, w)
	if !res.Ok {
		t.Fatalf("最后一个 PHP 应可卸载，得 errors=%+v", res.Errors)
	}
}

// TestUninstallNginxSkipsSiteDependencyCheck 卸载 Nginx 不再检查依赖它的站点（§0.2 规则 42）。
// 这里连告警都不产生：vhost 仍在盘上，重装 nginx 即恢复，多问一句等于替程序员做决定（§0.2 规则 15/16）。
func TestUninstallNginxSkipsSiteDependencyCheck(t *testing.T) {
	res := Run(ActUninstall, Ctx{Kind: "nginx", Version: "alpine"}, readyWorld())
	if !res.Ok {
		t.Fatalf("卸载 nginx 应照常放行，得 errors=%+v", res.Errors)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("卸载 nginx 不该再报站点依赖，得 warnings=%+v", res.Warnings)
	}
}

func TestUninstallNotInstalled(t *testing.T) {
	res := Run(ActUninstall, Ctx{Kind: "php", Version: "7.4"}, readyWorld())
	if firstErr(res) != errs.NotInstalled+": php 7.4" {
		t.Fatalf("未安装应报 notInstalled，得 %q", firstErr(res))
	}
}

// —— service-stop / service-start ——

func TestServiceStopRunningWarnNotBlock(t *testing.T) {
	// 停用 php 8.4（有站点使用）应是 warning 而非 error
	res := Run(ActServiceStop, Ctx{Kind: "php", Version: "8.4"}, readyWorld())
	if !res.Ok {
		t.Fatalf("停用应仅告警不阻止，得 errors=%+v", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("应产生依赖告警")
	}
}

func TestServiceStopNotRunning(t *testing.T) {
	res := Run(ActServiceStop, Ctx{Kind: "mysql", Version: "8.4"}, readyWorld())
	if firstErr(res) != errs.NotRunning+": mysql 8.4" {
		t.Fatalf("未运行应报 notRunning，得 %q", firstErr(res))
	}
}

func TestServiceStartAlreadyRunning(t *testing.T) {
	res := Run(ActServiceStart, Ctx{Kind: "php", Version: "8.4"}, readyWorld())
	if firstErr(res) != errs.IsRunning+": php 8.4" {
		t.Fatalf("已运行应报 isRunning，得 %q", firstErr(res))
	}
}

// —— update-config ——

func TestUpdateConfigPortExcludeCurrent(t *testing.T) {
	w := readyWorld()
	w.Snap.Env["MYSQL_84_PORT"] = "3306"
	// 改成当前端口自身：exclude 命中 → ok
	res := Run(ActUpdateConfig, Ctx{Kind: "mysql", Version: "8.4", Field: "port", NewValue: "3306"}, w)
	if !res.Ok {
		t.Fatalf("改回当前端口应通过，得 %+v", res.Errors)
	}
}

// TestUpdateConfigNginxPortReadsVersionedKey nginx 的端口键与其它服务同源（{KIND}_{VER}_PORT，
// 快照 env 由 config.FlatEnv 按版本扁平化产出）。旧代码读的版本无关键 NGINX_PORT 从不存在，
// 于是「当前值」恒空、exclude 恒空：改回自己已在服务的端口也被判成冲突。
func TestUpdateConfigNginxPortReadsVersionedKey(t *testing.T) {
	w := readyWorld()
	// 端口 env 键形态由 config.EnvKeyPort 定义（版本段只去点、不大写），站点/服务读写两侧同源
	w.Snap.Env[config.EnvKeyPort("nginx", "alpine")] = "80" // 站点 demo.test 正在 80 上服务
	res := Run(ActUpdateConfig, Ctx{Kind: "nginx", Version: "alpine", Field: "port", NewValue: "80"}, w)
	if !res.Ok {
		t.Fatalf("改回当前端口应通过，得 %+v", res.Errors)
	}
}

// —— site-add（端口占用只告警降级，不顺延）——

// TestSiteAddOccupiedPortDegrades 新建站点端口被「不是自家站点」的东西占着（这里是 mysql 的服务端口）：
// 保留用户所填端口、站点照常创建、配置照常落盘，只给一句降级告警说明端口暂不发布（总纲 §5.8 两档口径）
func TestSiteAddOccupiedPortDegrades(t *testing.T) {
	w := readyWorld()
	w.Snap.Env[config.EnvKeyPort("mysql", "8.4")] = "3306"
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "3306", PHP: "8.4", Root: "/www/new.test"}, w)
	if !res.Ok {
		t.Fatalf("端口占用应放行建站，实得错误 %+v", res.Errors)
	}
	if _, adjusted := res.Adjusted["port"]; adjusted {
		t.Fatalf("新建站点不得擅改用户所填端口，实得 %v", res.Adjusted)
	}
	if !contains(res.Warnings, portDegradeWarn(errs.PortInUse+": 3306 (mysql 8.4)")) {
		t.Fatalf("应含端口占用降级告警，实得 %+v", res.Warnings)
	}
}

// TestSiteAddSamePortReusesNginx 别的站点已经把 80 交给自家 Nginx 发布：这一档属「能确认是自己站点」，
// 按 server_name 分流复用——不改端口、不报占用、连告警都不给。
func TestSiteAddSamePortReusesNginx(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "80", PHP: "8.4", Root: "/www/new.test"}, readyWorld())
	if !res.Ok {
		t.Fatalf("复用自家 Nginx 的端口应放行建站，实得错误 %+v", res.Errors)
	}
	if _, adjusted := res.Adjusted["port"]; adjusted {
		t.Fatalf("不得擅改用户所填端口，实得 %v", res.Adjusted)
	}
	for _, warn := range res.Warnings {
		if strings.Contains(warn, errs.PortInUse) {
			t.Fatalf("自家 Nginx 复用不该报端口占用，实得 %+v", res.Warnings)
		}
	}
}

// TestSiteAddFreePortNoWarn 空闲端口：无告警、不改端口
func TestSiteAddFreePortNoWarn(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "8090", PHP: "8.4", Root: "/www/new.test"}, readyWorld())
	if !res.Ok {
		t.Fatalf("空闲端口应通过，实得 %+v", res.Errors)
	}
	if len(res.Warnings) != 0 || len(res.Adjusted) != 0 {
		t.Fatalf("空闲端口不应有告警或调整，实得 warn=%+v adj=%+v", res.Warnings, res.Adjusted)
	}
}

// TestSiteAddBadPortStillBlocked 端口格式非法仍阻断：占用降级不等于放开非法值
func TestSiteAddBadPortStillBlocked(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "99999", PHP: "8.4", Root: "/www/new.test"}, readyWorld())
	if firstErr(res) != errs.PortInvalid {
		t.Fatalf("非法端口应报 portInvalid，实得 %q", firstErr(res))
	}
}

func TestSiteAddOutsideWwwWarning(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "x.test", Port: "8080", PHP: "8.4", Root: "/elsewhere"}, readyWorld())
	if !res.Ok {
		t.Fatalf("WWW_ROOT 外应仅告警，得 %+v", res.Errors)
	}
	if !contains(res.Warnings, errs.RootOutsideWww+": /elsewhere") {
		t.Fatalf("应含 rootOutsideWww 告警，得 %+v", res.Warnings)
	}
}

func TestSiteAddDomainExists(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "demo.test", Port: "8081"}, readyWorld())
	if !contains(res.Errors, errs.DomainExists+": demo.test") {
		t.Fatalf("重复域名应报 domainExists，得 %+v", res.Errors)
	}
}

// TestSiteAddNeedsNginx nginx 未安装是建站的唯一服务门禁：阻断，站点不得创建
func TestSiteAddNeedsNginx(t *testing.T) {
	w := readyWorld()
	w.Snap.Installed["nginx"] = nil
	w.Snap.Running["nginx"] = nil
	res := Run(ActSiteAdd, Ctx{Domain: "z.test", Port: "8082", PHP: "8.4"}, w)
	if firstErr(res) != errs.NginxNeeded {
		t.Fatalf("未装 Nginx 应报 nginxNeeded，实得 %q / %+v", firstErr(res), res.Errors)
	}
}

// TestSiteAddNginxNotRunningWarn nginx 已装但未运行：只告警降级（写 vhost 会因 docker exec 失败），站点照常创建
func TestSiteAddNginxNotRunningWarn(t *testing.T) {
	w := readyWorld()
	w.Snap.Running["nginx"] = nil
	res := Run(ActSiteAdd, Ctx{Domain: "z.test", Port: "8082", PHP: "8.4"}, w)
	if !res.Ok {
		t.Fatalf("Nginx 未运行应放行建站，实得错误 %+v", res.Errors)
	}
	if !contains(res.Warnings, wantNginxNotRunningWarn()) {
		t.Fatalf("应含 nginx 未运行的降级告警，实得 %+v", res.Warnings)
	}
}

// TestSiteAddNeedsOnlyNginx nginx 是唯一服务门禁：未装 PHP/MySQL 等其余服务只告警，不阻断（最小限制原则）
func TestSiteAddNeedsOnlyNginx(t *testing.T) {
	w := readyWorld()
	w.Snap.Installed["php"] = nil
	w.Snap.Installed["mysql"] = nil
	w.Snap.Running["php"] = nil
	res := Run(ActSiteAdd, Ctx{Domain: "z.test", Port: "8082"}, w)
	if !res.Ok {
		t.Fatalf("未装 PHP 应放行建站，实得错误 %+v", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("未装 PHP 建站应降级告警（暂不写 vhost）")
	}
}

// TestSiteAddPhpNotInstalledWarn 选定未安装的 PHP 版本 → 告警降级，不当作错误阻断
func TestSiteAddPhpNotInstalledWarn(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "z.test", Port: "8082", PHP: "9.9"}, readyWorld())
	if !res.Ok {
		t.Fatalf("选未装 PHP 应放行建站，实得错误 %+v", res.Errors)
	}
	if !contains(res.Warnings, phpPendingWarn("9.9")) {
		t.Fatalf("应含 PHP 降级告警，实得 %+v", res.Warnings)
	}
}

func phpPendingWarn(php string) string {
	if php == "" {
		php = "未指定"
	}
	return errs.NotInstalled + ": PHP " + php + "（站点仍会创建，安装或切换到可用 PHP 版本后生效）"
}

// portDegradeWarn 端口占用降级告警文案（与 rules_site.go#siteAdd 逐字对齐，前后端同口径）
func portDegradeWarn(msg string) string {
	return msg + "（站点仍会创建，站点配置照常落盘，只是这个端口暂不发布；腾出该端口或改用空闲端口后自动补齐）"
}

// wantNginxNotRunningWarn 测试侧锁死 nginx 已装未运行的降级告警文案（与 rules_site.go#nginxNotRunningWarn、前端 usePreflight.ts 逐字对齐）
func wantNginxNotRunningWarn() string {
	return errs.NotRunning + ": Nginx（站点仍会创建，站点配置先落盘但未经 Nginx 校验；启动 Nginx 后重新校验并补齐端口发布）"
}

// —— site-port（排除自身域名与当前端口）——

func TestSitePortKeepCurrent(t *testing.T) {
	res := Run(ActSitePort, Ctx{Domain: "demo.test", NewValue: "80"}, readyWorld())
	if !res.Ok {
		t.Fatalf("保持当前端口应通过，得 %+v", res.Errors)
	}
}

func TestSitePortMissing(t *testing.T) {
	res := Run(ActSitePort, Ctx{Domain: "nope.test", NewValue: "8080"}, readyWorld())
	if firstErr(res) != errs.SiteMissing+": nope.test" {
		t.Fatalf("站点不存在应报 siteMissing，得 %q", firstErr(res))
	}
}

// —— site-vhost / php-switch / rewrite ——

func TestSiteVhostEmptyContent(t *testing.T) {
	empty := "   "
	res := Run(ActSiteVhost, Ctx{Domain: "demo.test", Content: &empty}, readyWorld())
	if firstErr(res) != errs.ConfigEmpty {
		t.Fatalf("空内容应报 configEmpty，得 %q", firstErr(res))
	}
}

func TestPhpSwitchNotInstalledWarn(t *testing.T) {
	// vhost 里指定未装 PHP 是 warning
	res := Run(ActSiteVhost, Ctx{Domain: "demo.test", PHP: "9.9"}, readyWorld())
	if !res.Ok {
		t.Fatalf("vhost 未装 PHP 应仅告警，得 %+v", res.Errors)
	}
	// 但 php-switch 切到未装版本是 error
	res2 := Run(ActPhpSwitch, Ctx{Domain: "demo.test", NewPhp: "9.9"}, readyWorld())
	if !contains(res2.Errors, errs.NotInstalled+": PHP 9.9") {
		t.Fatalf("切换未装 PHP 应报 notInstalled，得 %+v", res2.Errors)
	}
}

func TestRewriteOK(t *testing.T) {
	res := Run(ActRewrite, Ctx{Domain: "demo.test"}, readyWorld())
	if !res.Ok {
		t.Fatalf("rewrite 应通过，得 %+v", res.Errors)
	}
}

// wantNginxNotServingErr 锁死手改正文（site-vhost）在 nginx 未运行时的拦截文案（与 rules_site.go#nginxNotServingErr、前端 usePreflight.ts 逐字对齐）
func wantNginxNotServingErr() string {
	return errs.NotRunning + ": Nginx（vhost 改动须经运行中的 Nginx 校验后才能落盘，请先启动 Nginx 再重试）"
}

// wantNginxNotServingWarn 锁死模板生成型改动（改端口 / 切 PHP / 伪静态）在 nginx 未运行时的告警文案
// （与 rules_site.go#nginxNotServingWarn、前端 usePreflight.ts 逐字对齐）
func wantNginxNotServingWarn() string {
	return errs.NotRunning + ": Nginx（站点配置仍会落盘，但未经 Nginx 校验、端口暂不发布；启动 Nginx 后自动重新校验并补齐）"
}

// TestEditVhostRequiresServingNginx 四条编辑链在 Nginx 缺席/停着时的两档处置：
// 模板生成的那三处（改端口 / 切 PHP / 伪静态）没在跑也照常落盘、只给一句告警，等 Nginx 起来重新校验并补齐
// （硬红线 2 的登记例外）；手改正文那一处没有模板兜底，写坏了 nginx 就起不来，未装报 nginxNeeded、
// 停着则必须在裁决层拦住并给人话下一步。
func TestEditVhostRequiresServingNginx(t *testing.T) {
	content := "server {\n  listen 80;\n}"
	cases := []struct {
		name string
		act  string
		c    Ctx
	}{
		{"site-port", ActSitePort, Ctx{Domain: "demo.test", NewValue: "8080"}},
		{"site-vhost", ActSiteVhost, Ctx{Domain: "demo.test", Content: &content}},
		{"php-switch", ActPhpSwitch, Ctx{Domain: "demo.test", NewPhp: "8.1"}},
		{"rewrite", ActRewrite, Ctx{Domain: "demo.test"}},
	}

	// nginx 就绪（已装且运行）：四条编辑链都不该因 nginx 被拦
	for _, tc := range cases {
		if res := Run(tc.act, tc.c, readyWorld()); !res.Ok {
			t.Errorf("%s 在 nginx 运行时应通过，得 %+v", tc.name, res.Errors)
		}
	}

	// 未装 nginx：一律 nginxNeeded
	noNginx := readyWorld()
	noNginx.Snap.Installed["nginx"] = nil
	noNginx.Snap.Running["nginx"] = nil
	for _, tc := range cases {
		res := Run(tc.act, tc.c, noNginx)
		if res.Ok || !contains(res.Errors, errs.NginxNeeded) {
			t.Errorf("%s 未装 nginx 应报 nginxNeeded，得 %+v", tc.name, res.Errors)
		}
	}

	// 已装但停着：模板生成的那三处只告警、照常放行；手改正文仍拦
	stopped := readyWorld()
	stopped.Snap.Running["nginx"] = nil
	for _, tc := range cases {
		res := Run(tc.act, tc.c, stopped)
		if tc.act == ActSiteVhost {
			if res.Ok || !contains(res.Errors, wantNginxNotServingErr()) {
				t.Errorf("%s nginx 未运行应拦截并给人话提示，得 %+v / %+v", tc.name, res.Errors, res.Warnings)
			}
			continue
		}
		if !res.Ok {
			t.Errorf("%s nginx 停着时模板生成的改动不该被拦，得 %+v", tc.name, res.Errors)
		}
		if !contains(res.Warnings, wantNginxNotServingWarn()) {
			t.Errorf("%s 应含 nginx 未运行的告警，实得 %+v", tc.name, res.Warnings)
		}
	}
}

// wantNginxNotRunningWarnRootless 锁死 rootless 引擎下建站那句降级告警（与 rules_site.go#nginxNotRunningWarn 的 rootless
// 分支、前端 usePreflight.ts 逐字对齐）：端口绑不上的真因是引擎模式，不是「Nginx 没跑」，两句不能混着说
func wantNginxNotRunningWarnRootless() string {
	return errs.NotRunning + ": Nginx（站点仍会创建，站点配置先落盘但未经 Nginx 校验；启动 Nginx 后重新校验并补齐端口发布。另外这台机器用的是 rootless 容器引擎，1024 以下的端口本来就绑不上，补齐只对 1024 以上的端口生效——把站点改成这样的端口即可，本站点端口不替你改）" //nolint:lll
}

// wantNginxNotServingWarnRootless 同上，锁死模板生成型改动（改端口 / 切 PHP / 伪静态）在 rootless 引擎上的那一句告警
func wantNginxNotServingWarnRootless() string {
	return errs.NotRunning + ": Nginx（站点配置仍会落盘，但未经 Nginx 校验、端口暂不发布；启动 Nginx 后自动重新校验并补齐。另外这台机器用的是 rootless 容器引擎，1024 以下的端口本来就绑不上，补齐只对 1024 以上的端口生效——把站点改成这样的端口即可，本站点端口不替你改）" //nolint:lll
}

// TestRootlessEngineNamedInDegradeCopy rootless 那一份告警必须把「1024 以下的端口本来就绑不上」说出来——
// 只测非 rootless 那一支等于让 Podman rootless 用户读到一句把真因说成「Nginx 没跑」的假话。
// 手改正文（site-vhost）不看引擎结论：没有模板兜底，照旧硬拦（nginxNotServingErr 因此不带 rootless 参数）。
func TestRootlessEngineNamedInDegradeCopy(t *testing.T) {
	content := "server {\n  listen 80;\n}"
	w := rootlessWorld()
	w.Snap.Running["nginx"] = nil

	// 建站：照常放行（rootless 不是拦人的理由），但要把引擎这一层原因讲清
	res := Run(ActSiteAdd, Ctx{Domain: "z.test", Port: "8082", PHP: "8.4"}, w)
	if !res.Ok {
		t.Errorf("rootless + nginx 未运行不该拦建站，得 %+v", res.Errors)
	}
	if !contains(res.Warnings, wantNginxNotRunningWarnRootless()) {
		t.Errorf("建站告警应点名 rootless 引擎，实得 %+v", res.Warnings)
	}

	// 三处模板生成的改动：照常落盘、只告警，且告警带上 rootless 那一句
	for _, tc := range []struct {
		name string
		act  string
		c    Ctx
	}{
		{"site-port", ActSitePort, Ctx{Domain: "demo.test", NewValue: "8080"}},
		{"php-switch", ActPhpSwitch, Ctx{Domain: "demo.test", NewPhp: "8.1"}},
		{"rewrite", ActRewrite, Ctx{Domain: "demo.test"}},
	} {
		r := Run(tc.act, tc.c, w)
		if !r.Ok {
			t.Errorf("%s rootless 时模板生成的改动不该被拦，得 %+v", tc.name, r.Errors)
			continue
		}
		if !contains(r.Warnings, wantNginxNotServingWarnRootless()) {
			t.Errorf("%s 应含点名 rootless 的告警，实得 %+v", tc.name, r.Warnings)
		}
	}

	// 手改正文与引擎模式无关，照旧拦
	if r := Run(ActSiteVhost, Ctx{Domain: "demo.test", Content: &content}, w); r.Ok || !contains(r.Errors, wantNginxNotServingErr()) {
		t.Errorf("site-vhost 在 rootless 下仍应硬拦并给人话提示，得 %+v / %+v", r.Errors, r.Warnings)
	}
}

// —— extensions / backup / restore / backup-delete / offline-prune ——

func TestExtensionsOK(t *testing.T) {
	res := Run(ActExtensions, Ctx{Version: "8.4", FinalExts: []string{"redis", "openssl"}}, readyWorld())
	if !res.Ok {
		t.Fatalf("合法扩展应通过，得 %+v", res.Errors)
	}
}

func TestBackupEmptyEnvWarn(t *testing.T) {
	w := readyWorld()
	w.Snap.Installed = map[string][]string{}
	w.Snap.Sites = nil
	res := Run(ActBackup, Ctx{}, w)
	if !res.Ok || len(res.Warnings) == 0 {
		t.Fatalf("空环境备份应告警不阻止，得 res=%+v", res)
	}
}

func TestRestoreMissingBackup(t *testing.T) {
	res := Run(ActRestore, Ctx{File: "nope.tar.gz"}, readyWorld())
	if firstErr(res) != errs.BackupMissing+": nope.tar.gz" {
		t.Fatalf("缺备份应报 backupMissing，得 %q", firstErr(res))
	}
}

func TestOfflinePruneOK(t *testing.T) {
	res := Run(ActOfflinePrune, Ctx{Svc: "php", Ver: "8.4"}, readyWorld())
	if !res.Ok {
		t.Fatalf("缓存条目存在应通过，得 %+v", res.Errors)
	}
}

func TestOfflinePruneMissing(t *testing.T) {
	res := Run(ActOfflinePrune, Ctx{Svc: "redis", Ver: "8"}, readyWorld())
	if firstErr(res) != errs.OfflineMissing+": redis/8" {
		t.Fatalf("缺缓存应报 offlineMissing，得 %q", firstErr(res))
	}
}

// —— docker-source-set（镜像源只裁决地址合形，可达性不在 preflight 拦）——

func TestDockerSourceSetOK(t *testing.T) {
	res := Run(ActDockerSourceSet, Ctx{Sources: []string{"docker.m.daocloud.io", "127.0.0.1:5000"}}, readyWorld())
	if !res.Ok {
		t.Fatalf("合形主机名应通过，得 %+v", res.Errors)
	}
}

// 界面上的文本框每行一个地址：行间与末尾的空行不是错误（与写路径 ValidateRegistryHosts 同判据）；
// 全是空行等于「清空清单」，照常通过并给出「回落直连官方」的警告。
func TestDockerSourceSetIgnoresBlankLines(t *testing.T) {
	res := Run(ActDockerSourceSet, Ctx{Sources: []string{"  ", "docker.m.daocloud.io", ""}}, readyWorld())
	if !res.Ok || len(res.Warnings) != 0 {
		t.Fatalf("空行应被忽略且不误报「未填写」，得 errors=%+v warnings=%+v", res.Errors, res.Warnings)
	}
	empty := Run(ActDockerSourceSet, Ctx{Sources: []string{"", "   "}}, readyWorld())
	if !empty.Ok || len(empty.Warnings) == 0 {
		t.Fatalf("全是空行应等同清空清单：通过 + 一条直连官方警告，得 errors=%+v warnings=%+v", empty.Errors, empty.Warnings)
	}
}

// 空清单合法，但必须告知「等于直连官方」，不得静默让用户以为配好了源
func TestDockerSourceSetEmptyWarns(t *testing.T) {
	res := Run(ActDockerSourceSet, Ctx{Sources: nil}, readyWorld())
	if !res.Ok {
		t.Fatalf("清空镜像源应通过，得 %+v", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("空镜像源应给一条警告说明回落直连官方")
	}
}

func TestDockerSourceSetRejectsBadHost(t *testing.T) {
	for _, bad := range []string{"https://x.example.com/path", "bad host", "../etc"} {
		res := Run(ActDockerSourceSet, Ctx{Sources: []string{bad}}, readyWorld())
		if res.Ok {
			t.Fatalf("非法镜像源 %q 应被拒绝", bad)
		}
		if firstErr(res) == "" {
			t.Fatalf("非法镜像源 %q 应点名原值", bad)
		}
	}
}
