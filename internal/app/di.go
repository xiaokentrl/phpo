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
}

func NewContainer() *Container {
	return &Container{
		Emitter:   NopEmitter{},
		Lifecycle: NewLifecycle(),
		Env:       config.DerivePaths(config.DefaultHome, config.DefaultWWW),
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
