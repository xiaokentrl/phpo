// 依赖注入：对象图唯一构造入口；上层组件只从 Container 获取依赖，禁止跨层引用（§0.2 规则 12）
package app

import (
	"context"
	"os"
	"time"

	"phpo/internal/cache"
	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/service"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/internal/updater"
	"phpo/internal/vhost"
	"phpo/internal/vhost/hosts"
	"phpo/pkg/dockerutil"
)

// Container 汇集已构造的底层组件；M2 逐层扩充（Store/Config/Cache/TaskManager/Preflight）
type Container struct {
	Emitter        Emitter
	Lifecycle      *Lifecycle
	Env            config.Env
	CurrentVersion string // 应用当前版本（升级比较基准）
	UpdateURL      string // 发布清单地址；为空则不启用自动检查

	// M3 真实对象图：于启动钩子内构造（避免 Build 期产生文件/连接，保持单测纯净）
	AppService  *service.AppService  // 前端绑定的写/读门面；启动后非 nil
	SiteService *service.SiteService // 站点生命周期门面（T403+）；启动后非 nil
	EnvService  *service.EnvService  // 服务密码/端口 env 读写门面（T504）；启动后非 nil

	// M5 配置门面：服务配置文件读写（T505）；启动后非 nil
	ConfigService *service.ConfigService

	// M6 扩展门面：PHP 扩展启停 + 固化镜像重建（T601）；启动后非 nil
	ExtensionService *service.ExtensionService

	// M6 备份门面：打包/恢复/删除备份（T602）；启动后非 nil
	BackupService *service.BackupService

	// M6 诊断门面：§5.7 环境诊断 15 项 + 一键修复（T603）；启动后非 nil
	DoctorService *service.DoctorService

	// M6 清理门面：孤儿三模式清理 + 回收站 + 审计（T605）；启动后非 nil
	CleanupService *service.CleanupService

	// M6 离线缓存门面：§5.14.10 统计/校验/三模式清理/单条删除/lookup/promote/临时目录（T606）；启动后非 nil
	OfflineService *service.OfflineService

	// M6 升级门面：应用版本检查 + 三段式升级（下载→双校验→备份→安装，失败回滚，T604）；启动后非 nil
	UpdateService *service.UpdateService
}

func NewContainer() *Container {
	return &Container{
		Emitter:        NopEmitter{},
		Lifecycle:      NewLifecycle(),
		Env:            config.DerivePaths(config.DefaultHome, config.DefaultWWW),
		CurrentVersion: "0.1.0", // 与 app.go AppInfo 一致；升级比较基准
	}
}

// Build 依据容器构造应用对象图，并注册启动钩子
func (c *Container) Build() *Assembly {
	// §5.14.4 第 4 类必清时机：应用启动扫描并清空残留临时目录
	c.Lifecycle.AddStartupHook("clear-temp-residue", func(ctx context.Context) error {
		// 读取当前发射器（Attach 已替换为 Wails 实现），保证事件可达前端
		mgr := cache.NewManager(c.Env, c.Emitter, nil)
		return mgr.ScanAndClearResidue(ctx)
	})
	// M3 真实对象图：store + engine + cache + task.Manager + LifecycleService + AppService
	// 于启动钩子内构造——Build 期不产生文件/连接，保持无 Docker 单测纯净；此处失败即中断启动。
	c.Lifecycle.AddStartupHook("object-graph", func(ctx context.Context) error {
		env := config.ExpandEnvHomes(c.Env) // `~/phpo` → 绝对路径供真实 IO
		dbPath, err := config.DBPath()
		if err != nil {
			return err
		}
		st, err := store.Open(dbPath)
		if err != nil {
			return err
		}
		cli, err := engine.New() // 惰性：不拨号，Docker 缺席亦不报错
		if err != nil {
			st.Close()
			return err
		}
		tm := task.NewManager(c.Emitter)
		lc := service.NewLifecycle(cli, st, c.Emitter, env)
		cacheMgr := steps.NewCacheManager(env, c.Emitter, cli)
		c.AppService = service.NewAppService(lc, tm, cacheMgr, cli, env)
		c.EnvService = service.NewEnvService(st, c.Emitter)
		c.ConfigService = service.NewConfigService(env, tm)

		// M4 站点对象图：vhost 管理器 + hosts + 回收站 + 真实 nginx -t/ reload（走 phpo-nginx 容器）
		trashRoot, err := config.TrashRoot()
		if err != nil {
			st.Close()
			_ = cli.Close()
			return err
		}
		nginxContainer := dockerutil.ContainerName(string(model.KindNginx), "alpine")
		c.SiteService = service.NewSiteService(
			st,
			vhost.New(env),
			hosts.New(),
			engine.NewTrash(trashRoot),
			vhost.NewNginxTValidator(nginxContainer),
			vhost.NewNginxReloader(nginxContainer),
			tm, c.Emitter, env,
		)
		// 站点端口并集发布到 nginx（增删改站点端口后重建 nginx 容器以重绑宿主端口）
		c.SiteService.SetNginxPublisher(lc)

		// M6 扩展门面（T601）：容器内内置工具编译 → commit 固化 phpo/php:{version} → save 提升离线缓存 → 重建
		c.ExtensionService = service.NewExtensionService(
			cli, cacheMgr, st, vhost.NewNginxReloader(nginxContainer), c.Emitter, env, tm,
		)

		// M6 备份门面（T602）：打包配置/数据/站点/缓存 + SQLite 快照 → tar.gz；恢复走应用内逻辑重放 + 重建容器
		c.BackupService = service.NewBackupService(
			st, lc, cacheMgr, cli,
			func(path string) (service.SnapshotReader, error) { return store.Open(path) },
			c.Emitter, env, tm,
		)

		// M6 诊断门面（T603）：§5.7 十五项纯读诊断 + 状态校准/清临时目录两类一键修复
		c.DoctorService = service.NewDoctor(cli, cli, st, cacheMgr, lc, env)

		// M6 清理门面（T605）：孤儿三模式删除 + 7 天回收站恢复/清空 + JSON Lines 审计双写
		auditPath, _ := config.AuditLogPath() // 解析失败留空 → 审计降级为仅落表，不阻断清理
		c.CleanupService = service.NewCleanupService(
			cli, st, engine.NewTrash(trashRoot), cacheMgr, engine.NewAudit(auditPath), c.Emitter, tm,
		)

		// M6 离线缓存门面（T606）：§5.14.10 统计/校验/三模式清理/单条删除/lookup/promote/临时目录全接真
		c.OfflineService = service.NewOfflineService(cacheMgr, st, engine.NewAudit(auditPath), c.Emitter, tm)

		// M6 升级门面（T604 / 硬红线 5/6）：编排器 + 三段式 UpdateService；无发布源时 Check 返回错误而非 panic
		// §5.9 中断升级下次启动自动回滚：pending 标记存在且运行版本≠目标 → 恢复旧二进制（失败不阻断 GUI）
		if updatesDir, uerr := config.UpdatesDir(); uerr == nil {
			rb := updater.NewRollback(updatesDir)
			downloads, _ := config.UpdatesSub("downloads")
			backups, _ := config.UpdatesSub("backups")
			var src updater.ReleaseSource
			if c.UpdateURL != "" {
				src = updater.HTTPSource{URL: c.UpdateURL}
			}
			up := updater.New(c.CurrentVersion, downloads, backups, src, nil, nil, rb, c.Emitter)
			c.UpdateService = service.NewUpdateService(up, tm)
			if _, rerr := rb.RecoverOnStartup(c.CurrentVersion, up.Restore); rerr != nil {
				c.Emitter.Emit(EventUpdateDone, map[string]any{"status": "failed", "error": rerr.Error()})
			}
		}

		// §5.13.9 启动时校准：Docker 缺席/未运行时容忍失败，不阻断 GUI 启动
		if _, cerr := lc.Calibrate(ctx); cerr != nil {
			c.Emitter.Emit(EventDockerStateDrift, map[string]any{"error": cerr.Error()})
		}
		c.Lifecycle.AddShutdownHook("close-object-graph", func(context.Context) error {
			_ = cli.Close()
			return st.Close()
		})
		return nil
	})
	// §5.9 升级检查：启动时 + 每 24 小时（仅当配置了发布清单地址）
	if c.UpdateURL != "" {
		c.Lifecycle.AddStartupHook("updater-scheduler", func(ctx context.Context) error {
			src := updater.HTTPSource{URL: c.UpdateURL}
			chk := updater.NewChecker(c.CurrentVersion, src, c.Emitter)
			updater.NewScheduler(chk, updater.DefaultInterval).Start(ctx)
			return nil
		})
	}
	// M1 开发期事件联调：显式开启时以 mock 定时器全量发射 §5.6 事件，前端只订阅
	if os.Getenv("PHPO_MOCK_EVENTS") == "1" {
		c.Lifecycle.AddStartupHook("mock-events", func(ctx context.Context) error {
			StartMockTicker(ctx, c.Emitter, 5*time.Second)
			return nil
		})
	}
	return &Assembly{
		Emitter:   c.Emitter,
		Lifecycle: c.Lifecycle,
	}
}
