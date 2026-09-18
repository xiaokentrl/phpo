// 根 Service：前端绑定的唯一入口，持有并转调应用对象图（业务方法自 M2 起逐步挂接）
package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"

	phpapp "phpo/internal/app"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/template"
	"phpo/internal/ui"
)

// errNotReady 前端在启动钩子完成前抢跑调用时的守卫
var errNotReady = errors.New("服务尚未初始化")

// wailsEmitter 将 internal/app.Emitter 适配到 Wails 事件系统（前端订阅唯一通道）
type wailsEmitter struct{ app *application.App }

func (w wailsEmitter) Emit(event string, payload any) {
	if w.app != nil {
		w.app.Event.Emit(event, payload)
	}
}

type App struct {
	wails     *application.App
	container *phpapp.Container
	assembly  *phpapp.Assembly
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

// ServiceShutdown 实现 Wails v3 服务生命周期：退出前收尾
func (a *App) ServiceShutdown() error {
	a.assembly.Shutdown(a.assemblyLifecycleCtx())
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
		"version": "0.1.0",
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
