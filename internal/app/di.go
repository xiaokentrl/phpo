// 依赖注入：对象图唯一构造入口；上层组件只从 Container 获取依赖，禁止跨层引用（§0.2 规则 12）
package app

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
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

	// 对象图重绑：装配期下发给各门面的 config.Env 是值拷贝，装机把两根落库后要按新根整棵重建（见 Rebind）
	graphMu      sync.Mutex                  // 串行化对象图重建与回收
	graphKey     string                      // 当前对象图已绑的根指纹（两根 + 自定义缓存/备份根 + 数据目录）；未变即 no-op
	graphCleanup func(context.Context) error // 当前对象图的连接回收（docker client + 运行态存储）
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
	// §5.14.4 第 4 类必清时机：应用启动扫描并清空残留临时目录。
	// 扫描根必须按已持久化的两根展开——未展开的 "~/phpo" 不是合法路径，只会静默扫不到任何东西。
	c.Lifecycle.AddStartupHook("clear-temp-residue", func(ctx context.Context) error {
		// 读取当前发射器（Attach 已替换为 Wails 实现），保证事件可达前端
		return cache.NewManager(residueEnv(), c.Emitter, nil).ScanAndClearResidue(ctx)
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
		if err := c.buildObjectGraph(ctx, cfg, true); err != nil {
			return err
		}
		c.graphKey = rootsKey(cfg)
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
	// 关闭钩子只注册一次：始终回收「当前」对象图，重绑后不会留下旧闭包重复关闭
	c.Lifecycle.AddShutdownHook("close-object-graph", func(ctx context.Context) error {
		c.graphMu.Lock()
		defer c.graphMu.Unlock()
		if c.graphCleanup == nil {
			return nil // 对象图未装配（首启单测 / 启动早退）
		}
		return c.graphCleanup(ctx)
	})
	// 对象图本体见 buildObjectGraph：首启与重绑共用同一构造路径，杜绝两套装配漂移
	return &Assembly{
		Emitter:   c.Emitter,
		Lifecycle: c.Lifecycle,
	}
}

// Rebind 配置里的可自定义根写入 config.yaml 后重绑运行期对象图。
// 装配期下发给各门面的 config.Env 是值拷贝：离线缓存根、临时目录、vhost 目录、容器挂载宿主路径全部派生自此。
// 首启之后落库的两根若不重绑，所有写操作仍落在装配时的默认根上（工作目录污染事故的成因）；
// 自定义缓存根/备份根/数据目录同理——选了路径却不换图，等于没改（需求 2/7/8）。
// 先建新图、成功后才回收旧图：新图构造失败时旧图原样可用，不留半死状态；根指纹未变即 no-op（幂等）。
// 调用前提：任务队列已空闲——app.go 门面在 HomeEnsure 的三段式任务返回后调用，否则旧 task.Manager 会被中途替换。
func (c *Container) Rebind(ctx context.Context) error {
	c.graphMu.Lock()
	defer c.graphMu.Unlock()
	cfg, err := config.LoadConfigStore()
	if err != nil {
		return err
	}
	key := rootsKey(cfg)
	if key == c.graphKey {
		return nil // 两根未变：不换对象图，门面指针与在途状态一律保持不动
	}
	old := c.graphCleanup
	if err := c.buildObjectGraph(ctx, cfg, false); err != nil {
		return err
	}
	c.graphKey = key
	if old != nil {
		_ = old(ctx) // 旧图连接尽力回收；新图已可用，关闭失败不推翻重绑
	}
	return nil
}

// buildObjectGraph 按已载入的配置构造（重绑时重建）整棵运行期对象图。
// env 两份：原始值供展示与快照，展开值供真实文件 IO 与容器挂载。
// calibrate=false 跳过启动校准：重绑只为换根，此刻库里没有任何「已装」记录，校准会把旧根下遗留的
// phpo-* 容器直接标成已安装（其 bind 挂载仍指向旧根），宁可让用户显式「同步状态」后再校准。
func (c *Container) buildObjectGraph(ctx context.Context, cfg *config.ConfigStore, calibrate bool) error {
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
	nginxContainer := nginxContainerOf(st)
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
	// 反向并集：装/重装 nginx 时按当前站点端口集发布宿主端口，单一权威在站点侧（§5.8）
	lc.SetNginxPortSource(c.SiteService)
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
	// §5.13.9「每次任务后校准」接在任务终态出口：不分成败（失败/取消同样可能已改宿主），也不分门面——
	// 站点/配置/备份/清理/缓存各走自己的 tasks.Run，逐个补校准必然漏。Docker 缺席或首启未落两根时
	// 校准必报错，静默降级即可：漂移留给下次成功校准或手动「同步状态」，绝不把校准失败算成任务失败。
	tm.SetDoneWatcher(func() {
		_, _ = lc.Calibrate(context.Background())
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
	if calibrate && cfg.RootsPersisted() {
		if _, cerr := lc.Calibrate(ctx); cerr != nil {
			c.Emitter.Emit(EventDockerStateDrift, map[string]any{"error": cerr.Error()})
		}
	}
	c.graphCleanup = func(ctx context.Context) error {
		_ = cli.Close()
		return st.Close()
	}
	return nil
}

// residueEnv 启动残留扫描用的工作根：按已持久化的 config.yaml 展开；读不到配置即回落默认根（展开后仍是合法绝对路径）
func residueEnv() config.Env {
	cfg, err := config.LoadConfigStore()
	if err != nil {
		return config.ExpandEnvHomes(config.DerivePaths(config.DefaultHome, config.DefaultWWW))
	}
	return cfg.ExpandedEnv()
}

// rootsKey 对象图已绑根指纹（展开后的绝对路径）：两根 + 自定义缓存根/备份根 + 每服务版本数据目录。
// 任一项未变即无需重建，是 Rebind 的幂等判据；漏项会让「改了自定义根却不生效」（需求 2/7/8）。
func rootsKey(cfg *config.ConfigStore) string {
	e := cfg.ExpandedEnv()
	parts := []string{e.PHPOHome, e.WWWRoot, e.OfflineRoot, e.BackupRoot}
	dirs := make([]string, 0, len(e.DataDirs))
	for k, v := range e.DataDirs {
		dirs = append(dirs, k+"\x00"+v)
	}
	sort.Strings(dirs)
	return strings.Join(append(parts, dirs...), "\x00")
}

// nginxSnapshotter 取权威快照以现取 nginx 单例版本（*store.Store 满足）
type nginxSnapshotter interface {
	BuildSnapshot() (*model.Snapshot, error)
}

// nginxContainerOf nginx 单例容器名解析器（供 vhost 的 nginx -t / reload 每次调用现取）。
// 版本不可写死：nginx 版本由用户开放输入（§1.6，UI 建议表就含 alpine 之外的 1.25），
// 装配期定死成 phpo-nginx-alpine 会让装了其他版本的机器把命令 exec 到不存在的容器上（硬红线 2 失效）。
func nginxContainerOf(st nginxSnapshotter) vhost.ContainerFunc {
	return func() (string, error) {
		snap, err := st.BuildSnapshot()
		if err != nil {
			return "", err
		}
		vers := snap.Installed[string(model.KindNginx)]
		if len(vers) == 0 {
			return "", errors.New("nginx 未安装，无法校验或重载 vhost")
		}
		return dockerutil.ContainerName(string(model.KindNginx), vers[0]), nil
	}
}
