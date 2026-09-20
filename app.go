// 根 Service：前端绑定的唯一入口，持有并转调应用对象图（业务方法自 M2 起逐步挂接）
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/template"
	"phpo/internal/ui"
)

// errNotReady 前端在启动钩子完成前抢跑调用时的守卫
var errNotReady = errors.New("服务尚未初始化")

// restartMarker 重启子进程的环境标记：防「刷新失败→重启→仍失败→再重启」无限循环
const restartMarker = "PHPO_RESTARTED"

// wailsEmitter 将 internal/app.Emitter 适配到 Wails 事件系统（前端订阅唯一通道）
type wailsEmitter struct{ app *application.App }

func (w wailsEmitter) Emit(event string, payload any) {
	if w.app != nil {
		w.app.Event.Emit(event, payload)
	}
}

type App struct {
	wails          *application.App
	container      *phpapp.Container
	assembly       *phpapp.Assembly
	restartPending bool // 前端已请求重启：ServiceShutdown 收尾后重新拉起自身
}

func NewApp() *App {
	c := phpapp.NewContainer()
	return &App{container: c, assembly: c.Build()}
}

// Attach 在 application.New 之后注入 Wails 实例：替换真实发射器并安装托盘
func (a *App) Attach(app *application.App) {
	a.wails = app
	emitter := wailsEmitter{app: app}
	a.container.Emitter = emitter
	a.assembly.Emitter = emitter
	ui.InstallTray(app, emitter)
}

// ServiceStartup 实现 Wails v3 服务生命周期（启动钩子含残留临时目录清理，自 T211 注册）
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return a.assembly.Startup(ctx)
}

// ServiceShutdown 实现 Wails v3 服务生命周期：退出前收尾；若前端已请求重启，则在资源释放后重新拉起自身
func (a *App) ServiceShutdown() error {
	a.assembly.Shutdown(a.assemblyLifecycleCtx())
	if a.restartPending {
		relaunchSelf()
	}
	return nil
}

func (a *App) assemblyLifecycleCtx() context.Context {
	if a.wails != nil {
		return a.wails.Context()
	}
	return context.Background()
}

// AppInfo 供前端确认绑定链路已通
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"name":    "phpo",
		"version": a.container.CurrentVersion,
	}
}

// ---- M3 生命周期绑定：所有写操作经 AppService → task.Manager 三段式（硬红线 4/5）----

// GetState 拉取当前权威快照（前端订阅外的兜底）
func (a *App) GetState() (*model.Snapshot, error) {
	if a.container.AppService == nil {
		return nil, errNotReady
	}
	return a.container.AppService.GetState()
}

// Running 是否有任务在执行
func (a *App) Running() bool {
	return a.container.AppService != nil && a.container.AppService.Running()
}

// Install 安装并启动服务版本（缓存优先镜像 → 容器）
func (a *App) Install(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Install(ctx, kind, version)
}

// Start 启动已安装容器
func (a *App) Start(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Start(ctx, kind, version)
}

// Stop 停止容器（保留数据）
func (a *App) Stop(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Stop(ctx, kind, version)
}

// Remove 卸载容器（保留数据卷）
func (a *App) Remove(ctx context.Context, kind model.ServiceKind, version string) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Remove(ctx, kind, version)
}

// Cancel 取消当前运行中任务
func (a *App) Cancel() {
	if a.container.AppService != nil {
		a.container.AppService.Cancel()
	}
}

// Calibrate 手动触发状态校准
func (a *App) Calibrate(ctx context.Context) error {
	if a.container.AppService == nil {
		return errNotReady
	}
	return a.container.AppService.Calibrate(ctx)
}

// DockerStatus 只读探测 Docker 可用性（首启/轮询门禁用；硬红线 7 判定源；不改快照不发事件）
func (a *App) DockerStatus(ctx context.Context) (model.DockerStatus, error) {
	if a.container.AppService == nil {
		return model.DockerStatus{}, errNotReady
	}
	return a.container.AppService.DockerStatus(ctx), nil
}

// ---- M4 站点绑定：所有写操作经 SiteService → task.Manager 三段式（硬红线 4/5）----

// SiteAdd 幂等建站（建目录 → nginx -t 校验写 vhost → 加 hosts → 落库并广播 state:changed）
func (a *App) SiteAdd(ctx context.Context, in service.AddInput) error {
	if a.container.SiteService == nil {
		return errNotReady
	}
	return a.container.SiteService.Add(ctx, in)
}

// SiteRemove 删站：根目录入回收站（7 天）→ 删 vhost → 落库删除
func (a *App) SiteRemove(ctx context.Context, domain string) error {
	if a.container.SiteService == nil {
		return errNotReady
	}
	return a.container.SiteService.Remove(ctx, domain)
}

// SiteSetPort 改站点端口（写 vhost listen + nginx -t → 落库）
func (a *App) SiteSetPort(ctx context.Context, domain string, port int) error {
	if a.container.SiteService == nil {
		return errNotReady
	}
	return a.container.SiteService.SetPort(ctx, domain, port)
}

// SiteSwitchPHP 切换 PHP 上游（硬红线 1：精确 php-{version}-fpm:9000）
func (a *App) SiteSwitchPHP(ctx context.Context, domain, php string) error {
	if a.container.SiteService == nil {
		return errNotReady
	}
	return a.container.SiteService.SwitchPHP(ctx, domain, php)
}

// SiteSetRewrite 设置伪静态预设（preset）及自定义规则（rule）
func (a *App) SiteSetRewrite(ctx context.Context, domain, preset, rule string) error {
	if a.container.SiteService == nil {
		return errNotReady
	}
	return a.container.SiteService.SetRewrite(ctx, domain, preset, rule)
}

// SiteSetVhostContent 手改并保存 vhost 正文（落库前 nginx -t 必过，硬红线 2）
func (a *App) SiteSetVhostContent(ctx context.Context, domain, content string) error {
	if a.container.SiteService == nil {
		return errNotReady
	}
	return a.container.SiteService.SetVhostContent(ctx, domain, content)
}

// ---- M5 数据服务 env 绑定：密码/端口明文落库（§1.5 零校验），写入后 state:changed 回流（硬红线 4）----

// GetServicePassword 读某服务版本明文密码供 UI 回显；未设置回落默认 123456
func (a *App) GetServicePassword(kind model.ServiceKind, version string) (string, error) {
	if a.container.EnvService == nil {
		return "", errNotReady
	}
	return a.container.EnvService.GetPassword(kind, version)
}

// SetServicePassword 明文写入密码（空串/任意长度合法，零校验零加密）
func (a *App) SetServicePassword(kind model.ServiceKind, version, password string) error {
	if a.container.EnvService == nil {
		return errNotReady
	}
	return a.container.EnvService.SetPassword(kind, version, password)
}

// GetServicePort 读某服务版本落库的宿主端口；未设置返回 0
func (a *App) GetServicePort(kind model.ServiceKind, version string) (int, error) {
	if a.container.EnvService == nil {
		return 0, errNotReady
	}
	return a.container.EnvService.GetPort(kind, version)
}

// SetServicePort 落库某服务版本的宿主发布端口
func (a *App) SetServicePort(kind model.ServiceKind, version string, port int) error {
	if a.container.EnvService == nil {
		return errNotReady
	}
	return a.container.EnvService.SetPort(kind, version, port)
}

// ---- M5 服务配置绑定：读回显 / 三段式原子保存（硬红线 3/5）----

// ConfigGetFiles 返回该服务版本配置：宿主已存在则回显内容，否则回落模板默认
func (a *App) ConfigGetFiles(kind model.ServiceKind, version string) ([]template.File, error) {
	if a.container.ConfigService == nil {
		return nil, errNotReady
	}
	return a.container.ConfigService.GetFiles(kind, version)
}

// ConfigSaveFiles 原子保存勾选的配置：备份原文件 → 写入 → 失败自动回滚
func (a *App) ConfigSaveFiles(ctx context.Context, kind model.ServiceKind, version string, files []service.ConfigFile) error {
	if a.container.ConfigService == nil {
		return errNotReady
	}
	return a.container.ConfigService.Save(ctx, kind, version, files)
}

// ---- M6 扩展绑定：读启用集 / 三段式应用扩展并重建固化镜像（硬红线 4/5/8）----

// ExtList 返回某 PHP 版本当前启用的扩展（后端权威）
func (a *App) ExtList(version string) ([]string, error) {
	if a.container.ExtensionService == nil {
		return nil, errNotReady
	}
	return a.container.ExtensionService.List(version)
}

// ExtApply 应用目标扩展集：容器内编译 → commit 固化 phpo/php:{version} → 提升离线缓存 → 重建容器 → 重载 nginx
func (a *App) ExtApply(ctx context.Context, version string, enabled []string) error {
	if a.container.ExtensionService == nil {
		return errNotReady
	}
	return a.container.ExtensionService.Apply(ctx, version, enabled)
}

// ---- M6 备份绑定：列表/下载路径 + 三段式创建/恢复/删除（硬红线 3/4/5）----

// BackupList 返回 BACKUP_ROOT 下备份归档条目（时间倒序，后端权威）
func (a *App) BackupList() ([]model.BackupFile, error) {
	if a.container.BackupService == nil {
		return nil, errNotReady
	}
	return a.container.BackupService.List()
}

// BackupPath 返回某归档的绝对路径（供前端触发本地下载）；仅接受合法文件名
func (a *App) BackupPath(file string) (string, error) {
	if a.container.BackupService == nil {
		return "", errNotReady
	}
	return a.container.BackupService.Path(file)
}

// BackupSaveAs 把归档导出到用户经原生保存框选定的目标路径（下载）
func (a *App) BackupSaveAs(file, dst string) error {
	if a.container.BackupService == nil {
		return errNotReady
	}
	return a.container.BackupService.SaveAs(file, dst)
}

// BackupCreate 创建备份：暂停数据服务 → 导出 SQLite 快照 → 打包 → 自动重启
func (a *App) BackupCreate(ctx context.Context) (model.BackupFile, error) {
	if a.container.BackupService == nil {
		return model.BackupFile{}, errNotReady
	}
	return a.container.BackupService.Create(ctx)
}

// BackupRestore 恢复备份：清空命名空间 → 解包落盘 → 逻辑重放 SQLite → 重建容器
func (a *App) BackupRestore(ctx context.Context, file string) error {
	if a.container.BackupService == nil {
		return errNotReady
	}
	return a.container.BackupService.Restore(ctx, file)
}

// BackupDelete 删除备份归档（二次确认在前端）
func (a *App) BackupDelete(ctx context.Context, file string) error {
	if a.container.BackupService == nil {
		return errNotReady
	}
	return a.container.BackupService.Delete(ctx, file)
}

// DoctorRun 执行 §5.7 环境诊断，返回 15 项检查结果与汇总计数
func (a *App) DoctorRun(ctx context.Context) (model.DoctorReport, error) {
	if a.container.DoctorService == nil {
		return model.DoctorReport{}, errNotReady
	}
	return a.container.DoctorService.Run(ctx), nil
}

// DoctorFix 执行一项一键修复（calibrate 状态校准 / clear_temp 清空临时目录残留），均幂等可重复
func (a *App) DoctorFix(ctx context.Context, id string) error {
	if a.container.DoctorService == nil {
		return errNotReady
	}
	return a.container.DoctorService.Fix(ctx, id)
}

// ---- M6 升级绑定：读当前版本 / 检查更新 / 三段式升级（下载→双校验→备份→安装，失败回滚，硬红线 5/6）----

// UpdateCurrentVersion 返回应用当前版本（前端展示比较基准）
func (a *App) UpdateCurrentVersion() string {
	return a.container.CurrentVersion
}

// UpdateCheck 拉取发布清单，返回是否有新版本及可用信息；未配置发布源则返回错误
func (a *App) UpdateCheck(ctx context.Context) (model.UpdateAvailable, bool, error) {
	if a.container.UpdateService == nil {
		return model.UpdateAvailable{}, false, errNotReady
	}
	return a.container.UpdateService.Check(ctx)
}

// UpdateApply 经任务引擎执行一次升级（单飞；忙则 TASK_BUSY）；进度/结果走 update:progress/done 事件
func (a *App) UpdateApply(ctx context.Context) error {
	if a.container.UpdateService == nil {
		return errNotReady
	}
	return a.container.UpdateService.Apply(ctx)
}

// ---- M6 清理绑定：孤儿三模式 + 回收站 + 审计（T605，§5.13.6/7/10）----

// CleanupScan 全量孤儿扫描并广播 docker:orphan-found，返回分类报告
func (a *App) CleanupScan(ctx context.Context) (model.OrphanReport, error) {
	if a.container.CleanupService == nil {
		return model.OrphanReport{}, errNotReady
	}
	return a.container.CleanupService.Scan(ctx)
}

// CleanupRun 按三模式（conservative/standard/aggressive）删除孤儿资源；激进涉卷删除由前端二次确认
func (a *App) CleanupRun(ctx context.Context, mode string) (model.CleanupReport, error) {
	if a.container.CleanupService == nil {
		return model.CleanupReport{}, errNotReady
	}
	rep, err := a.container.CleanupService.Clean(ctx, model.CleanupMode(mode))
	if err != nil {
		return model.CleanupReport{}, err
	}
	return *rep, nil
}

// CleanupCache 按三模式清理离线缓存（§5.14.6）；inUse 由已安装服务推导
func (a *App) CleanupCache(ctx context.Context, mode string) (model.CleanupResult, error) {
	if a.container.CleanupService == nil {
		return model.CleanupResult{}, errNotReady
	}
	res, err := a.container.CleanupService.CleanCache(ctx, model.CleanupMode(mode))
	if err != nil {
		return model.CleanupResult{}, err
	}
	return *res, nil
}

// TrashList 回收站条目（7 天保留，含是否过期）
func (a *App) TrashList() ([]model.TrashEntry, error) {
	if a.container.CleanupService == nil {
		return nil, errNotReady
	}
	return a.container.CleanupService.ListTrash()
}

// TrashRestore 从回收站恢复误删条目到原位并注销登记
func (a *App) TrashRestore(ctx context.Context, id int64) error {
	if a.container.CleanupService == nil {
		return errNotReady
	}
	return a.container.CleanupService.RestoreTrash(ctx, id)
}

// TrashEmptyExpired 立即永久删除全部到期回收站条目（未满期不动）
func (a *App) TrashEmptyExpired(ctx context.Context) (int, error) {
	if a.container.CleanupService == nil {
		return 0, errNotReady
	}
	return a.container.CleanupService.EmptyExpired(ctx)
}

// OperationList 最近审计历史（operations 表，UI 展示；文件权威见 operations.log）
func (a *App) OperationList(limit int) ([]model.Operation, error) {
	if a.container.CleanupService == nil {
		return nil, errNotReady
	}
	return a.container.CleanupService.ListOperations(limit)
}

// ---- M6 离线缓存绑定：§5.14.10 全量缓存服务层 API（T606）----

// OfflineListEntries 遍历离线缓存条目并聚合 manifest 计数/校验态
func (a *App) OfflineListEntries(ctx context.Context) ([]model.CacheEntry, error) {
	if a.container.OfflineService == nil {
		return nil, errNotReady
	}
	return a.container.OfflineService.ListCacheEntries(ctx)
}

// OfflineGetEntry 读取单个 kind/version 缓存条目（不存在返回 null）
func (a *App) OfflineGetEntry(ctx context.Context, kind, version string) (*model.CacheEntry, error) {
	if a.container.OfflineService == nil {
		return nil, errNotReady
	}
	return a.container.OfflineService.GetCacheEntry(ctx, kind, version)
}

// OfflineStats 缓存根目录总览（占用/条目/镜像/扩展/损坏）
func (a *App) OfflineStats(ctx context.Context) (model.CacheStats, error) {
	if a.container.OfflineService == nil {
		return model.CacheStats{}, errNotReady
	}
	return a.container.OfflineService.GetCacheStats(ctx)
}

// OfflineVerifyEntry 逐文件重校验单条缓存，损坏发射 cache:corrupted
func (a *App) OfflineVerifyEntry(ctx context.Context, kind, version string) (model.VerifyResult, error) {
	if a.container.OfflineService == nil {
		return model.VerifyResult{}, errNotReady
	}
	return a.container.OfflineService.VerifyCacheEntry(ctx, kind, version)
}

// OfflineVerifyAll 全量校验所有缓存条目，返回通过/失败汇总与明细
func (a *App) OfflineVerifyAll(ctx context.Context) (model.VerifyAllResult, error) {
	if a.container.OfflineService == nil {
		return model.VerifyAllResult{}, errNotReady
	}
	return a.container.OfflineService.VerifyAllCache(ctx)
}

// OfflineCleanupCache 按三模式清理缓存（§5.14.6）；在用条目受保护，涉删除由前端二次确认
func (a *App) OfflineCleanupCache(ctx context.Context, mode string) (model.CleanupResult, error) {
	if a.container.OfflineService == nil {
		return model.CleanupResult{}, errNotReady
	}
	return a.container.OfflineService.CleanupCache(ctx, model.CleanupMode(mode))
}

// OfflineRemoveEntry 删除单条缓存目录（不可恢复，前端确认后调用）
func (a *App) OfflineRemoveEntry(ctx context.Context, kind, version string) error {
	if a.container.OfflineService == nil {
		return errNotReady
	}
	return a.container.OfflineService.RemoveCacheEntry(ctx, kind, version)
}

// OfflineLookupImage 查镜像离线缓存命中情况（命中前已校验 SHA256）
func (a *App) OfflineLookupImage(ctx context.Context, kind, version string) (model.ImageCacheResult, error) {
	if a.container.OfflineService == nil {
		return model.ImageCacheResult{}, errNotReady
	}
	return a.container.OfflineService.LookupImage(ctx, kind, version)
}

// OfflineLookupExtension 查扩展包离线缓存命中情况
func (a *App) OfflineLookupExtension(ctx context.Context, phpVersion, extType, name string) (model.ExtCacheResult, error) {
	if a.container.OfflineService == nil {
		return model.ExtCacheResult{}, errNotReady
	}
	return a.container.OfflineService.LookupExtension(ctx, phpVersion, extType, name)
}

// OfflinePromoteImage 把临时镜像 tar 提升到离线缓存
func (a *App) OfflinePromoteImage(ctx context.Context, kind, version, tarPath string) error {
	if a.container.OfflineService == nil {
		return errNotReady
	}
	return a.container.OfflineService.PromoteImage(ctx, kind, version, tarPath)
}

// OfflinePromoteExtension 把临时扩展包提升到离线缓存
func (a *App) OfflinePromoteExtension(ctx context.Context, phpVersion, extType, filePath string) error {
	if a.container.OfflineService == nil {
		return errNotReady
	}
	return a.container.OfflineService.PromoteExtension(ctx, phpVersion, extType, filePath)
}

// OfflineClearTempDir 清空指定 kind/version 临时目录并发射 cache:tempdir-cleared
func (a *App) OfflineClearTempDir(ctx context.Context, kind, version, reason string) error {
	if a.container.OfflineService == nil {
		return errNotReady
	}
	return a.container.OfflineService.ClearTempDir(ctx, kind, version, reason)
}

// ---- M6 装机向导（T607）----

// HomeVerify 只读预检工作目录（判存在 + 判可写，不创建任何目录/文件，不落库）；返回逐条预检行与错误
func (a *App) HomeVerify(ctx context.Context, home, www string) (model.HomeVerifyResult, error) {
	if a.container.WizardService == nil {
		return model.HomeVerifyResult{}, errNotReady
	}
	return a.container.WizardService.HomeVerify(ctx, home, www)
}

// HomeEnsure 装机确认：三段式创建工作目录子树并落地派生 env + dirReady，随后广播 state:changed
func (a *App) HomeEnsure(ctx context.Context, home, www string) error {
	if a.container.WizardService == nil {
		return errNotReady
	}
	return a.container.WizardService.HomeEnsure(ctx, home, www)
}

// HomeDefaults 返回后端已解析的工作目录默认值（config.yaml 已存根目录 > ~/phpo 默认），供装机向导预填与浏览起始目录。
// 与快照 env 同源：首启快照为空时，前端据此拿到 config.yaml 预置的自定义根目录而非硬编码 ~/phpo。
// CONFIGURED 携带「工作目录是否已设置」（两根已持久化且目录已存在）：向导据此先决检测，已设置即不再进入设置流程。
func (a *App) HomeDefaults() map[string]string {
	m := map[string]string{
		"PHPO_HOME": a.container.Env.PHPOHome,
		"WWW_ROOT":  a.container.Env.WWWRoot,
	}
	if a.container.WizardService != nil {
		m["CONFIGURED"] = strconv.FormatBool(a.container.WizardService.Configured())
	}
	return m
}

// Restart 请求应用重启：装机向导刷新主界面后仍未就绪（对象图按启动时的旧根展开）时的兜底归位。
// 重启过一次仍未就绪即拒绝，避免「刷新失败→重启」无限循环；实际拉起在 ServiceShutdown 释放资源后进行。
func (a *App) Restart() error {
	if os.Getenv(restartMarker) == "1" {
		return fmt.Errorf("已重启过一次，工作目录仍未就绪：请检查 config.yaml 的根目录配置与目录权限")
	}
	if a.wails == nil {
		return errNotReady
	}
	a.restartPending = true
	a.wails.Quit()
	return nil
}

// relaunchSelf 以后台子进程重新拉起当前可执行文件，并带上重启标记；与父进程脱离，失败静默（前端已提示过）
func relaunchSelf() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), restartMarker+"=1")
	if err := cmd.Start(); err != nil {
		return
	}
	cmd.Process.Release()
}
