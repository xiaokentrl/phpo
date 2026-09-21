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

	// M6 装机向导门面：创建工作目录子树 + 把两根目录落地到 config.yaml（T607）；启动后非 nil
	WizardService *service.WizardService

	// M6 升级门面：应用版本检查 + 三段式升级（下载→双校验→备份→安装，失败回滚，T604）；启动后非 nil
	UpdateService *service.UpdateService
}

// Version 应用版本单一真实来源；默认值供 `go run`/单测使用，打包时由 Taskfile 经
// `-ldflags "-X phpo/internal/app.Version=$(version)"` 从 wails.json 注入，使运行时/升级比较基准与安装包版本一致。
var Version = "0.1.0"

func NewContainer() *Container {
	return &Container{
		Emitter:        NopEmitter{},
		Lifecycle:      NewLifecycle(),
		Env:            config.DerivePaths(config.DefaultHome, config.DefaultWWW),
		CurrentVersion: Version, // 与安装包版本同源（见 scripts/bump-version.sh）
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
		// 配置唯一权威：载入 XDG config.yaml（缺失即首启空配置，回落默认根目录）；SQLite 退居纯运行态。
		// 后端 Container.Env 与前端 snapshot.env 同源于此，杜绝分散/不同步。
		cfg, err := config.LoadConfigStore()
		if err != nil {
			return err
		}
		c.Env = cfg.Env()        // 原始根派生（含 `~`，供展示/快照）
		env := cfg.ExpandedEnv() // 展开 `~` 供真实 IO / 容器挂载
		// 运行态存储延迟建库：两根目录未写入 config.yaml 前不创建/打开 phpo.db（首启在用户数据目录零落盘）。
		dbPath, err := config.DBPath()
		if err != nil {
			return err
		}
		st := store.New(dbPath)
		st.SetEnvProvider(cfg)
		cli, err := engine.New() // 惰性：不拨号，Docker 缺席亦不报错
		if err != nil {
			_ = st.Close()
			return err
		}
		tm := task.NewManager(c.Emitter)
		// 任务实时反馈三接线（硬红线 4：状态唯一权威在后端）：
		// 队列详情进快照（store 不反向依赖 task，注入 provider）；终态落账本；队列变化重发 state:changed。
		st.SetTaskBoard(tm.Board)
		tm.SetRecorder(st)
		lc := service.NewLifecycle(cli, st, c.Emitter, env, cfg)
		cacheMgr := steps.NewCacheManager(env, c.Emitter, cli)
		c.AppService = service.NewAppService(lc, tm, cacheMgr, cli, env)
		c.EnvService = service.NewEnvService(cfg, st, c.Emitter)
		c.ConfigService = service.NewConfigService(env, tm)

		// M4 站点对象图：vhost 管理器 + hosts + 回收站 + 真实 nginx -t/ reload（走 phpo-nginx 容器）
		trashRoot, err := config.TrashRoot()
		if err != nil {
			st.Close()
			_ = cli.Close()
			return err
		}
		nginxContainer := dockerutil.ContainerName(string(model.KindNginx), "alpine")
		vh := vhost.New(env)
		hm := hosts.New()
		c.SiteService = service.NewSiteService(
			st,
			vh,
			hm,
			engine.NewTrash(trashRoot),
			vhost.NewNginxTValidator(nginxContainer),
			vhost.NewNginxReloader(nginxContainer),
			tm, c.Emitter, env,
		)
		// 站点端口并集发布到 nginx（增删改站点端口后重建 nginx 容器以重绑宿主端口）
		c.SiteService.SetNginxPublisher(lc)
		// nginx 由停到起后补齐降级站点的 vhost 与端口发布（建站门禁在 preflight：nginx 未装即阻断）
		c.AppService.SetSiteHealer(c.SiteService)
		// 快照站点 hosts 真值探针（列表 Hosts 列）：store 不反向依赖 hosts 包，由装配层注入
		st.SetHostsProbe(func(domain string) bool {
			ok, err := hm.Has(domain)
			return err == nil && ok
		})
		// 队列变化（入队/移交/终态）即重发权威快照：排队详情与进度经 state:changed 实时可见，不新增事件名
		tm.SetQueueWatcher(func() {
			snap, err := st.BuildSnapshot()
			if err != nil {
				return // 首启未建库等场景：无快照可发，任务终态仍由账本与 task:* 事件覆盖
			}
			c.Emitter.Emit(EventStateChanged, map[string]any{"snapshot": snap})
		})

		// M6 扩展门面（T601）：容器内内置工具编译 → commit 固化 phpo/php:{version} → save 提升离线缓存 → 重建
		c.ExtensionService = service.NewExtensionService(
			cli, cacheMgr, st, vhost.NewNginxReloader(nginxContainer), c.Emitter, env, tm,
		)

		// M6 备份门面（T602）：打包配置/数据/站点/缓存 + SQLite 快照 → tar.gz；恢复走应用内逻辑重放 + 重建容器
		c.BackupService = service.NewBackupService(
			st, lc, cacheMgr, cli,
			func(path string) (service.SnapshotReader, error) { return store.Open(path) },
			c.Emitter, env, cfg, tm,
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

		// M6 装机向导门面（T607）：创建工作目录子树 + 落地两根目录到 config.yaml，广播 state:changed
		c.WizardService = service.NewWizardService(cfg, st, c.Emitter, tm)

		// dirReady 不再落库：快照按「config.yaml 已持久化 + 目录实际存在」实时派生（首启两根为空 → 双 false → 前端弹装机向导并阻断写操作）

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

		// §5.13.9 启动时校准：仅在工作目录已落地后执行（校准会读写运行态存储并访问容器；首启未配置则零落盘、零拨号）。
		// Docker 缺席/未运行时容忍失败，不阻断 GUI 启动
		if cfg.RootsPersisted() {
			if _, cerr := lc.Calibrate(ctx); cerr != nil {
				c.Emitter.Emit(EventDockerStateDrift, map[string]any{"error": cerr.Error()})
			}
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
