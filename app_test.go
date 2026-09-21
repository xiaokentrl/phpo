// 根 Service 的守卫用例：重启只允许一次，宿主未就绪时拒绝
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	phpapp "phpo/internal/app"
	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/preflight"
	"phpo/pkg/errs"
)

// TestRestartBlockedByMarker 已带重启标记仍被请求重启 → 拒绝且不改状态，防无限重启循环
func TestRestartBlockedByMarker(t *testing.T) {
	t.Setenv(restartMarker, "1")
	a := &App{}
	err := a.Restart()
	if err == nil {
		t.Fatal("带重启标记时应拒绝再次重启")
	}
	if !strings.Contains(err.Error(), "重启") {
		t.Fatalf("错误文案应含「重启」，实际：%v", err)
	}
	if a.restartPending {
		t.Fatal("被拒绝时不应置 restartPending")
	}
}

// TestRestartWithoutWails 宿主未注入时返回启动守卫错误，不误置重启标记
func TestRestartWithoutWails(t *testing.T) {
	a := &App{}
	if err := a.Restart(); !errors.Is(err, errNotReady) {
		t.Fatalf("期望 errNotReady，实际：%v", err)
	}
	if a.restartPending {
		t.Fatal("宿主未就绪时不应置 restartPending")
	}
}

// TestSetTrayPrefsWithoutTray 托盘未安装（Attach 前）时拒绝下发偏好，不把偏好丢掉当作已生效
func TestSetTrayPrefsWithoutTray(t *testing.T) {
	a := &App{}
	if err := a.SetTrayPrefs(true, true); !errors.Is(err, errNotReady) {
		t.Fatalf("期望 errNotReady，实际：%v", err)
	}
}

// TestHomeDefaults_RootsExpanded 向导预填与目录选择起始路径必须已展开 `~`：
// 否则原生目录选择器把 `~/www` 当相对路径，报「无法找到 <当前工作目录>/~/www」。
func TestHomeDefaults_RootsExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无可用主目录，跳过 ~ 展开测试")
	}
	c := phpapp.NewContainer()
	c.Env = config.DerivePaths("~/home-defaults-test", "~/www-defaults-test")

	m := (&App{container: c}).HomeDefaults()
	if got, want := m["PHPO_HOME"], filepath.Join(home, "home-defaults-test"); got != want {
		t.Errorf("PHPO_HOME 应为展开路径，want %q，got %q", want, got)
	}
	if got, want := m["WWW_ROOT"], filepath.Join(home, "www-defaults-test"); got != want {
		t.Errorf("WWW_ROOT 应为展开路径，want %q，got %q", want, got)
	}
}

// ---- 后端最终裁决接线（§0.2 #14：UI 校验仅即时反馈，最终裁决在 internal/preflight）----

// pfSrc 裁决三份只读输入的替身；backupErr / cacheErr 用于验证「无关 action 不牵连失败」
type pfSrc struct {
	snap      *model.Snapshot
	backups   []string
	backupErr error
	caches    []model.CacheEntry
	cacheErr  error
}

func (f pfSrc) snapshot() (*model.Snapshot, error)        { return f.snap, nil }
func (f pfSrc) backupFiles() ([]string, error)            { return f.backups, f.backupErr }
func (f pfSrc) cacheEntries() ([]model.CacheEntry, error) { return f.caches, f.cacheErr }

// pfReady 构造「两根已就绪」的快照：needsHome 的 15 个 action 不被目录门禁抢先拦截
func pfReady(installed map[string][]string, sites ...model.Site) *model.Snapshot {
	s := model.NewSnapshot()
	for k, vs := range installed {
		s.Installed[k] = vs
	}
	s.Sites = sites
	s.DirReady["PHPO_HOME"] = true
	s.DirReady["WWW_ROOT"] = true
	return s
}

func TestGateSiteAddBlockedWhenNginxMissing(t *testing.T) {
	src := pfSrc{snap: pfReady(map[string][]string{"php": {"8.4"}})}
	err := runGuard(src, preflight.ActSiteAdd, preflight.Ctx{Domain: "demo.test", Port: 80, PHP: "8.4"})
	if err == nil {
		t.Fatal("nginx 未装时建站必须被后端拦截（硬红线：nginx 是建站唯一服务门禁）")
	}
	if !strings.Contains(err.Error(), errs.NginxNeeded) {
		t.Fatalf("错误应为 nginxNeeded，实际：%v", err)
	}
}

// TestGateSiteAddPortOccupiedOnlyWarns §5.8：新建站点端口占用只告警降级，不得阻断建站
func TestGateSiteAddPortOccupiedOnlyWarns(t *testing.T) {
	src := pfSrc{snap: pfReady(
		map[string][]string{"nginx": {"alpine"}, "php": {"8.4"}},
		model.Site{Domain: "taken.test", Port: 8080, PHP: "8.4"},
	)}
	c := preflight.Ctx{Domain: "new.test", Port: 8080, PHP: "8.4"}
	if err := runGuard(src, preflight.ActSiteAdd, c); err != nil {
		t.Fatalf("端口占用应为告警而非错误，实际：%v", err)
	}
}

// TestGateSetServicePortRequiresInstalled 未安装的版本改端口/写密码都要被拦
func TestGateSetServicePortRequiresInstalled(t *testing.T) {
	src := pfSrc{snap: pfReady(nil)}
	for _, c := range []preflight.Ctx{
		{Kind: "mysql", Version: "8.4", Field: "port", NewValue: 3307},
		{Kind: "mysql", Version: "8.4", Field: "password", NewValue: "secret"},
	} {
		err := runGuard(src, preflight.ActUpdateConfig, c)
		if err == nil || !strings.Contains(err.Error(), errs.NotInstalled) {
			t.Errorf("field=%s 未安装时应报 notInstalled，实际：%v", c.Field, err)
		}
	}
}

// TestGateSitePortAdvanceNotBlocked 改已有站点端口占用时顺延（§5.8），不得报错
func TestGateSitePortAdvanceNotBlocked(t *testing.T) {
	src := pfSrc{snap: pfReady(
		map[string][]string{"nginx": {"alpine"}, "php": {"8.4"}},
		model.Site{Domain: "a.test", Port: 80, PHP: "8.4"},
		model.Site{Domain: "b.test", Port: 81, PHP: "8.4"},
	)}
	if err := runGuard(src, preflight.ActSitePort, preflight.Ctx{Domain: "a.test", NewValue: 81}); err != nil {
		t.Fatalf("改端口占用应顺延不阻断，实际：%v", err)
	}
}

// TestGateSwitchPhpRequiresInstalled php-switch 前端无镜像调用，靠后端补齐裁决
func TestGateSwitchPhpRequiresInstalled(t *testing.T) {
	src := pfSrc{snap: pfReady(
		map[string][]string{"nginx": {"alpine"}, "php": {"8.4"}},
		model.Site{Domain: "a.test", Port: 80, PHP: "8.4"},
	)}
	if err := runGuard(src, preflight.ActPhpSwitch, preflight.Ctx{Domain: "a.test", NewPhp: "7.4"}); err == nil {
		t.Fatal("切到未安装的 PHP 版本必须被拦截（硬红线 1：上游容器必须存在）")
	}
	if err := runGuard(src, preflight.ActPhpSwitch, preflight.Ctx{Domain: "a.test", NewPhp: "8.4"}); err != nil {
		t.Fatalf("已安装版本不应被拦，实际：%v", err)
	}
}

// TestGateAuxiliaryListErrorScoped 备份/缓存清单读取失败只影响消费它们的 action
func TestGateAuxiliaryListErrorScoped(t *testing.T) {
	src := pfSrc{snap: pfReady(nil), backupErr: errors.New("读目录失败")}
	if err := runGuard(src, preflight.ActInstall, preflight.Ctx{Kind: "php", Version: "8.4"}); err != nil {
		t.Fatalf("install 不消费备份清单，列目录失败不得牵连： %v", err)
	}
	if err := runGuard(src, preflight.ActBackupDelete, preflight.Ctx{File: "x.tar.gz"}); err == nil {
		t.Fatal("restore/backup-delete 在清单不可读时必须失败关闭，不得当作归档不存在而放行")
	}
}

func TestGateRestoreAndPruneRequireExistingEntries(t *testing.T) {
	src := pfSrc{snap: pfReady(nil), backups: []string{"backup-1.tar.gz"}, caches: []model.CacheEntry{{Kind: "php", Version: "8.4"}}}
	if err := runGuard(src, preflight.ActRestore, preflight.Ctx{File: "missing.tar.gz"}); err == nil {
		t.Fatal("不存在的归档不得恢复")
	}
	if err := runGuard(src, preflight.ActRestore, preflight.Ctx{File: "backup-1.tar.gz"}); err != nil {
		t.Fatalf("已存在的归档应放行，实际：%v", err)
	}
	if err := runGuard(src, preflight.ActOfflinePrune, preflight.Ctx{Svc: "nginx", Ver: "alpine"}); err == nil {
		t.Fatal("不存在的缓存条目不得删除")
	}
	if err := runGuard(src, preflight.ActOfflinePrune, preflight.Ctx{Svc: "php", Ver: "8.4"}); err != nil {
		t.Fatalf("已存在的缓存条目应放行，实际：%v", err)
	}
}

// ---- 安装期端口/密码（install 必须携带配置：未安装态过不了 update-config 守卫）----

// cfgRec 记录安装期配置写入口的调用（*service.EnvService 的 SetPort/SetPassword 满足）
type cfgRec struct {
	ports map[string]int
	pws   map[string]string
}

func (r *cfgRec) SetPort(kind model.ServiceKind, version string, port int) error {
	r.ports[string(kind)+" "+version] = port
	return nil
}

func (r *cfgRec) SetPassword(kind model.ServiceKind, version, password string) error {
	r.pws[string(kind)+" "+version] = password
	return nil
}

// TestApplyInstallOptionsPersistsProvidedConfig 安装弹窗填的端口/密码要先落 config.yaml：
// 二者只在建容器那一刻被读走（{KIND}_{VER}_PORT / _PASSWORD），装配后再改端口只能靠重建。
func TestApplyInstallOptionsPersistsProvidedConfig(t *testing.T) {
	rec := &cfgRec{ports: map[string]int{}, pws: map[string]string{}}
	if err := applyInstallOptions(rec, "mysql", "8.4", model.InstallOptions{Port: 3307, Password: "", HasPassword: true}); err != nil {
		t.Fatal(err)
	}
	if got := rec.ports["mysql 8.4"]; got != 3307 {
		t.Fatalf("用户所填端口应落库供建容器读取，实得 %d", got)
	}
	if _, ok := rec.pws["mysql 8.4"]; !ok {
		t.Fatal("HasPassword 时必须落密码：空密码是合法值（§1.5），不能当作「未提供」丢掉")
	}
	if rec.pws["mysql 8.4"] != "" {
		t.Fatalf("密码应原样落库（可为空），实得 %q", rec.pws["mysql 8.4"])
	}
}

// TestApplyInstallOptionsSkipsUnprovided 未提供端口/密码时一笔都不写：
// php 无宿主端口，写 0 会把「未设置」变成「设成 0」，快照与建容器都会读到脏值。
func TestApplyInstallOptionsSkipsUnprovided(t *testing.T) {
	rec := &cfgRec{ports: map[string]int{}, pws: map[string]string{}}
	if err := applyInstallOptions(rec, "php", "8.4", model.InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(rec.ports) != 0 || len(rec.pws) != 0 {
		t.Fatalf("未提供的配置不得写库，实得 ports=%v pws=%v", rec.ports, rec.pws)
	}
}

// TestGateDirReadyBlocksNeedsHome 两根未就绪时 15 个 needsHome action 一律拦截
func TestGateDirReadyBlocksNeedsHome(t *testing.T) {
	src := pfSrc{snap: model.NewSnapshot(), backups: []string{"backup-1.tar.gz"}}
	err := runGuard(src, preflight.ActSiteAdd, preflight.Ctx{Domain: "demo.test"})
	if err == nil || !strings.Contains(err.Error(), errs.HomeNotReady) {
		t.Fatalf("目录未就绪应报 homeNotReady，实际：%v", err)
	}
	// backup-delete 不在这 15 项内（§0.3 权威值）：不得被目录门禁牵连
	if err := runGuard(src, preflight.ActBackupDelete, preflight.Ctx{File: "backup-1.tar.gz"}); err != nil {
		t.Fatalf("backup-delete 不受 needsHome 拦截，实际：%v", err)
	}
}

// actionConsts 17 个 action → app.go 门面里的裁决常量名（对账：漏接一个即失败）
var actionConsts = map[string]string{
	"install":        "ActInstall",
	"uninstall":      "ActUninstall",
	"service-stop":   "ActServiceStop",
	"service-start":  "ActServiceStart",
	"update-config":  "ActUpdateConfig",
	"service-config": "ActServiceCfg",
	"site-add":       "ActSiteAdd",
	"site-remove":    "ActSiteRemove",
	"site-port":      "ActSitePort",
	"site-vhost":     "ActSiteVhost",
	"php-switch":     "ActPhpSwitch",
	"rewrite":        "ActRewrite",
	"extensions":     "ActExtensions",
	"backup":         "ActBackup",
	"restore":        "ActRestore",
	"backup-delete":  "ActBackupDelete",
	"offline-prune":  "ActOfflinePrune",
}

// TestGateEveryActionWiredInFacade 每个 action 都必须在 App 门面里真跑一次后端裁决；
// 只在前端镜像 usePreflight.ts 校验等于没有裁决（§0.2 #14：最终裁决在 internal/preflight）。
func TestGateEveryActionWiredInFacade(t *testing.T) {
	if len(actionConsts) != preflight.ActionCount {
		t.Fatalf("对账表 %d 项，preflight action %d 项", len(actionConsts), preflight.ActionCount)
	}
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(src)
	for action, constName := range actionConsts {
		if !strings.Contains(code, "guard(preflight."+constName) {
			t.Errorf("action %q 未接后端裁决：app.go 缺少 a.guard(preflight.%s, ...)", action, constName)
		}
	}
}
