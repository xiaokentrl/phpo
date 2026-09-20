// preflight 框架（硬红线 #5 三段式首段：唯一权威裁决层）
// 直译原型 preflight()（前端唯一界面来源.txt:1707–1940）：17 action + NEEDS_HOME(15)
// 返回 {ok,errors,warnings,adjusted}；能警告的绝不阻止（§3.3 最小限制）
package preflight

import (
	"fmt"
	"strconv"

	"phpo/internal/model"
	"phpo/pkg/errs"
)

// 17 个 action 名（§0.3 权威值：service 6 + site 6 + ops/cache 5）
const (
	ActInstall      = "install"
	ActUninstall    = "uninstall"
	ActServiceStop  = "service-stop"
	ActServiceStart = "service-start"
	ActUpdateConfig = "update-config"
	ActServiceCfg   = "service-config"
	ActSiteAdd      = "site-add"
	ActSiteRemove   = "site-remove"
	ActSitePort     = "site-port"
	ActSiteVhost    = "site-vhost"
	ActPhpSwitch    = "php-switch"
	ActRewrite      = "rewrite"
	ActExtensions   = "extensions"
	ActBackup       = "backup"
	ActRestore      = "restore"
	ActBackupDelete = "backup-delete"
	ActOfflinePrune = "offline-prune"
)

// ActionCount 供对账测试：preflight action 总数
const ActionCount = 17

// AllActions 17 个 action 名（顺序对应分组：service→site→ops/cache）
var AllActions = []string{
	ActInstall, ActUninstall, ActServiceStop, ActServiceStart, ActUpdateConfig, ActServiceCfg,
	ActSiteAdd, ActSiteRemove, ActSitePort, ActSiteVhost, ActPhpSwitch, ActRewrite,
	ActExtensions, ActBackup, ActRestore, ActBackupDelete, ActOfflinePrune,
}

// NeedsHome：需要 PHPO_HOME 就绪的 15 个 action（§0.3 权威值，php-switch/backup 不在内）
var needsHome = map[string]bool{
	ActInstall: true, ActUninstall: true, ActServiceStop: true, ActServiceStart: true,
	ActUpdateConfig: true,
	ActSiteAdd:      true, ActSiteRemove: true, ActSitePort: true, ActSiteVhost: true,
	ActRewrite: true, ActExtensions: true, ActServiceCfg: true,
	ActRestore: true, ActOfflinePrune: true,
	// 注：backup 原型 NEEDS_HOME 含之，见下
}

func init() {
	// 原型 NEEDS_HOME 明确含 'backup'，共 15 项
	needsHome[ActBackup] = true
}

// NeedsHomeCount 供对账测试
const NeedsHomeCount = 15

// World 只读快照 + 运行时上下文（后端唯一权威）
type World struct {
	Snap        *model.Snapshot
	TaskRunning bool
	Backups     []string // 备份归档文件名
	Offline     []model.CacheEntry
}

func (w *World) isRunning(kind, version string) bool {
	for _, v := range w.Snap.Running[kind] {
		if v == version {
			return true
		}
	}
	return false
}

// Ctx 各 action 的入参联合（字段按原型解构命名）
type Ctx struct {
	Kind       string
	Version    string
	Port       any // nil=未提供；string/int 均可
	Extensions string
	Field      string
	NewValue   any
	Domain     string
	PHP        string
	Root       string
	Content    *string // nil=未提供；*""=提供空内容（configEmpty）
	NewPhp     string
	FinalExts  []string
	Files      []string
	File       string
	Svc        string
	Ver        string
}

// run 收集器（对应原型 errors/warnings/adjusted 三数组）
type run struct {
	c        Ctx
	w        *World
	errors   []string
	warnings []string
	adjusted map[string]any
}

func (r *run) errf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *run) warnf(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

func (r *run) setAdjustedPort(v int) {
	if r.adjusted == nil {
		r.adjusted = map[string]any{}
	}
	r.adjusted["port"] = v
}

// Run 执行 preflight 裁决：先全局守卫（taskBusy/homeNotReady），再分派 17 action
func Run(action string, c Ctx, w *World) *model.PreflightResult {
	r := &run{c: c, w: w}
	if w.Snap == nil {
		w.Snap = model.NewSnapshot()
	}

	if w.TaskRunning {
		r.errors = append(r.errors, errs.TaskBusy)
	}
	if needsHome[action] && !(w.Snap.DirReady["PHPO_HOME"] && w.Snap.DirReady["WWW_ROOT"]) {
		r.errors = append(r.errors, errs.HomeNotReady)
	}

	switch action {
	case ActInstall:
		r.install()
	case ActUninstall:
		r.uninstall()
	case ActServiceStop:
		r.serviceStop()
	case ActServiceStart:
		r.serviceStart()
	case ActUpdateConfig:
		r.updateConfig()
	case ActServiceCfg:
		r.serviceConfig()
	case ActSiteAdd:
		r.siteAdd()
	case ActSiteRemove:
		r.siteRemove()
	case ActSitePort:
		r.sitePort()
	case ActSiteVhost:
		r.siteVhost()
	case ActPhpSwitch:
		r.phpSwitch()
	case ActRewrite:
		r.rewrite()
	case ActExtensions:
		r.extensions()
	case ActBackup:
		r.backup()
	case ActRestore:
		r.restore()
	case ActBackupDelete:
		r.backupDelete()
	case ActOfflinePrune:
		r.offlinePrune()
	}

	return &model.PreflightResult{
		Ok:       len(r.errors) == 0,
		Errors:   r.errors,
		Warnings: r.warnings,
		Adjusted: r.adjusted,
	}
}

// asString 把 any 端口/密码值转字符串（nil→""）
func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case *int:
		if x == nil {
			return ""
		}
		return strconv.Itoa(*x)
	}
	return ""
}
