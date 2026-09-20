// preflight 单测：17 action 正/负例 + NEEDS_HOME(15) + 全局守卫 + 端口顺延 + 警告不阻止
package preflight

import (
	"testing"

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

func firstErr(res *model.PreflightResult) string {
	if len(res.Errors) == 0 {
		return ""
	}
	return res.Errors[0]
}

// —— 对账：action 数与 NEEDS_HOME 数 ——

func TestActionAndNeedsHomeCounts(t *testing.T) {
	if len(AllActions) != ActionCount || ActionCount != 17 {
		t.Fatalf("action 数应为 17，得 %d/%d", len(AllActions), ActionCount)
	}
	n := 0
	for _, a := range AllActions {
		if needsHome[a] {
			n++
		}
	}
	if n != NeedsHomeCount {
		t.Fatalf("NEEDS_HOME 应为 15，得 %d", n)
	}
	// php-switch 与 backup-delete 不在 NEEDS_HOME（原型）
	if needsHome[ActPhpSwitch] {
		t.Error("php-switch 不应在 NEEDS_HOME")
	}
}

// —— 全局守卫 ——

func TestTaskBusyGuard(t *testing.T) {
	w := readyWorld()
	w.TaskRunning = true
	res := Run(ActBackup, Ctx{}, w)
	if res.Ok || firstErr(res) != errs.TaskBusy {
		t.Fatalf("任务运行中应报 taskBusy，得 %+v", res.Errors)
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

// —— uninstall ——

func TestUninstallHasDependents(t *testing.T) {
	res := Run(ActUninstall, Ctx{Kind: "php", Version: "8.4"}, readyWorld())
	if !contains(res.Errors, errs.HasDependents+": PHP 8.4 ← demo.test") {
		t.Fatalf("被站点依赖应报 hasDependents，得 %+v", res.Errors)
	}
}

func TestUninstallLastPhp(t *testing.T) {
	w := readyWorld()
	w.Snap.Installed["php"] = []string{"8.4"}
	w.Snap.Sites = nil
	res := Run(ActUninstall, Ctx{Kind: "php", Version: "8.4"}, w)
	if !contains(res.Errors, errs.LastPhp) {
		t.Fatalf("最后一个 PHP 应报 lastPhp，得 %+v", res.Errors)
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

// —— site-add（端口占用只告警降级，不顺延）——

// TestSiteAddOccupiedPortDegrades 新建站点端口被占用：保留用户所填端口、不阻断，只给降级告警（总纲 §5.8 / v2.9.2）
func TestSiteAddOccupiedPortDegrades(t *testing.T) {
	res := Run(ActSiteAdd, Ctx{Domain: "new.test", Port: "80", PHP: "8.4", Root: "/www/new.test"}, readyWorld())
	if !res.Ok {
		t.Fatalf("端口占用应放行建站，实得错误 %+v", res.Errors)
	}
	if _, adjusted := res.Adjusted["port"]; adjusted {
		t.Fatalf("新建站点不得擅改用户所填端口，实得 %v", res.Adjusted)
	}
	if !contains(res.Warnings, portDegradeWarn(errs.PortInUse+": 80 (site demo.test)")) {
		t.Fatalf("应含端口占用降级告警，实得 %+v", res.Warnings)
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
	return msg + "（站点仍会创建，但端口暂不发布、vhost 暂不落盘；腾出该端口或改用空闲端口后生效）"
}

// wantNginxNotRunningWarn 测试侧锁死 nginx 已装未运行的降级告警文案（与 rules_site.go#nginxNotRunningWarn、前端 usePreflight.ts 逐字对齐）
func wantNginxNotRunningWarn() string {
	return errs.NotRunning + ": Nginx（站点仍会创建，vhost 暂不落盘、端口暂不发布；启动 Nginx 后自动补齐）"
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
