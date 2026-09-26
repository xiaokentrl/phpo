// T601 · PHP 扩展离线缓存链路：把「启用哪些扩展」落到 phpo 专用镜像并重载 Nginx。
// 机制：扩展经容器内编译工具（docker-php-ext-install / pecl）装好，docker commit 固化出 phpo/php:{version}；
// **扩展包文件本身同样必须离线**（§5.14.3 铁律 1/3）——pecl 的 .tgz 与 Alpine 构建依赖的 .apk 先落容器暂存目录，
// 由宿主取回临时目录 ./php/{ver}/ext/ 再提升进缓存根（默认 ./offline/php/{ver}/{apk|pecl}/），下次命中即零网络回填。
// 全程三段式（硬红线 5）+ 后端权威广播（硬红线 4）+ 无论成败清空临时目录（硬红线 8）。
package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"phpo/internal/cache"
	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/util"
	"phpo/pkg/dockerutil"
)

// ExtRuntime 扩展编排所需的最小 Docker 能力（*engine.Client 满足；单测注入假件）
type ExtRuntime interface {
	DockerOps
	ContainerRunning(ctx context.Context, name string) (bool, error)
	ExecStream(ctx context.Context, name string, cmd []string, stdout, stderr io.Writer) error
	CommitContainer(ctx context.Context, name, ref string) error
	ImageSave(ctx context.Context, ref, dstTar string) error
	ImageRemove(ctx context.Context, ref string) error
	// CopyTo / CopyFrom 是宿主临时目录与容器暂存目录之间的唯一字节通道
	// （PHP 挂载表没有 ext/ 这一档，见 internal/engine/copy.go）
	CopyTo(ctx context.Context, name, dstDir string, hostFiles ...string) error
	CopyFrom(ctx context.Context, name, srcDir, dstDir string) ([]string, error)
}

// ExtImageCache 离线缓存子集（*cache.Manager 满足）：基座镜像 · 固化镜像 · 扩展包文件三类条目
type ExtImageCache interface {
	EnsureImage(ctx context.Context, kind, version, ref string) error
	CachedImageRef(kind, version string) (string, bool)
	PromoteExtImage(version, ref, tmpTar string) error
	EnsureTempDir(kind, version string) (string, error)
	ClearTempDir(ctx context.Context, kind, version, reason string) error
	// 扩展包（apk / pecl）：装前必查（前缀匹配带版本号的产物名）· 取回后提升登记 manifest
	LookupExtPackage(phpVersion, extType, name string) (cache.ExtLookup, error)
	ListExtPackages(phpVersion, extType string) ([]string, error)
	PromoteExtension(phpVersion, extType, tmpFile string) error
}

// ExtStore 扩展启停的权威持久化子集（*store.Store 满足）
type ExtStore interface {
	BuildSnapshot() (*model.Snapshot, error)
	SetPHPExtensions(version string, exts []string) error
}

// ExtensionService 编排一次「应用扩展并重建」；phpKind 恒为 php 种类
type ExtensionService struct {
	rt      ExtRuntime
	cache   ExtImageCache
	store   ExtStore
	reload  Reloader // 写容器后重载 nginx（可为 nil）
	emitter Emitter
	env     config.Env
	tasks   *task.Manager
	seq     atomic.Uint64
}

func NewExtensionService(rt ExtRuntime, cache ExtImageCache, store ExtStore, reload Reloader, emitter Emitter, env config.Env, tm *task.Manager) *ExtensionService {
	return &ExtensionService{rt: rt, cache: cache, store: store, reload: reload, emitter: emitter, env: env, tasks: tm}
}

// List 返回某 php 版本当前启用的扩展（后端权威）。
// 这里读的是库——界面上那颗开关要的是「此刻容器里开着没有」，那是 Status 的职责。
func (s *ExtensionService) List(version string) ([]string, error) {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	exts := snap.PHPExtensions[version]
	if exts == nil {
		return []string{}, nil
	}
	return exts, nil
}

// Status 打开「管理扩展」弹窗时到容器里现查一次启用态（§5.16.2）：
// 唯一判据是容器内实测的 php -m，名字归一后回写权威库并随快照回流；「能不能停用」再看 conf.d 里有没有那份 ini。
// 三种情况一律退回库里那份并标 Live=false——容器没跑、探针失败、实测为空（真机上的 php 再裁也会报 Core/date，
// 一条都没有就等于没读到东西）。退回时**不写库、不发事件**：伪造一份没人实测过的权威值，比显示旧值更糟。
// 取到实测且与库里不同才回写广播；相同则一写都不发（每次开弹窗都重发一遍同样的事实等于刷屏，§5.19.4 同口径）。
func (s *ExtensionService) Status(ctx context.Context, version string) (model.ExtStatus, error) {
	name := dockerutil.ContainerName(string(model.KindPHP), version)
	fallback := func() (model.ExtStatus, error) {
		exts, err := s.List(version)
		if err != nil {
			return model.ExtStatus{Version: version}, err
		}
		return model.ExtStatus{Version: version, Enabled: exts, BuiltIn: []string{}, Live: false}, nil
	}
	running, err := s.rt.ContainerRunning(ctx, name)
	if err != nil || !running {
		return fallback()
	}
	enabled, builtIn, ok := s.probeEnabled(ctx, name)
	if !ok {
		return fallback()
	}
	prev, err := s.List(version)
	if err != nil {
		return model.ExtStatus{Version: version}, err
	}
	if !sameExtSet(prev, enabled) {
		if err := s.store.SetPHPExtensions(version, enabled); err != nil {
			return model.ExtStatus{Version: version}, err
		}
		if err := s.emit(version, enabled); err != nil {
			return model.ExtStatus{Version: version}, err
		}
	}
	return model.ExtStatus{Version: version, Enabled: enabled, BuiltIn: builtIn, Live: true}, nil
}

// probeEnabled 发两条只读探针：php -m 给「开着哪些」，conf.d 清单给「哪些删得掉」。
// 都必须是 argv（不经 shell，防注入），且任一条读失败就整体判为「没实测到」——
// 把 ls 失败说成「全部内建」等于给每一项都画上一颗哑开关。
func (s *ExtensionService) probeEnabled(ctx context.Context, ctr string) (enabled, builtIn []string, ok bool) {
	var out bytes.Buffer
	if err := s.rt.ExecStream(ctx, ctr, config.ExtLoadedProbeCmd, &out, io.Discard); err != nil {
		return nil, nil, false
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		if n, valid := config.NormalizeExtName(line); valid {
			set[n] = true
		}
	}
	if len(set) == 0 {
		return nil, nil, false
	}
	inis, err := s.probeExtInis(ctx, ctr)
	if err != nil {
		return nil, nil, false
	}
	enabled = make([]string, 0, len(set))
	for e := range set {
		enabled = append(enabled, e)
	}
	sort.Strings(enabled)
	for _, e := range enabled {
		if !inis[config.ExtIniFile(e)] {
			builtIn = append(builtIn, e)
		}
	}
	if builtIn == nil {
		builtIn = []string{}
	}
	return enabled, builtIn, true
}

// probeExtInis 列 conf.d，返回该目录里实际存在的文件名集合。
func (s *ExtensionService) probeExtInis(ctx context.Context, ctr string) (map[string]bool, error) {
	var out bytes.Buffer
	if err := s.rt.ExecStream(ctx, ctr, config.ExtIniListCmd, &out, io.Discard); err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		if n := strings.TrimSpace(line); n != "" {
			m[n] = true
		}
	}
	return m, nil
}

func sameExtSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa, sb := toSet(a), toSet(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

// diffExts 计算新增 / 停用集合（均去重、稳定序）
func diffExts(prev, next []string) (added, removed []string) {
	inPrev := toSet(prev)
	inNext := toSet(next)
	for _, e := range next {
		if !inPrev[e] {
			added = append(added, e)
		}
	}
	for _, e := range prev {
		if !inNext[e] {
			removed = append(removed, e)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// uniqueSorted 去重并给出稳定序，与 diffExts 的 added/removed 同口径
func uniqueSorted(xs []string) []string {
	set := toSet(xs)
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// Apply 应用某 php 版本的目标扩展集：写清单 → 保证镜像/容器 → 容器内编译 → commit 固化 → 重建容器 → 重载 nginx → 落库广播。
// 编译失败即回滚（容器恢复原镜像、临时目录清空、缓存不写入该固化镜像）。
func (s *ExtensionService) Apply(ctx context.Context, version string, enabled []string) error {
	prev, err := s.List(version)
	if err != nil {
		return err
	}
	added, removed := diffExts(prev, enabled)
	if len(added) == 0 && len(removed) == 0 {
		return nil // 无变化：不产生任务、不重建
	}

	baseRef, err := engine.ImageRefFor(string(model.KindPHP), version)
	if err != nil {
		return err
	}
	committedRef := engine.CommittedPHPRef(version)
	name := dockerutil.ContainerName(string(model.KindPHP), version)
	// 回滚后应恢复运行的镜像：原启用过扩展且固化镜像真在本机 → 固化镜像；否则基座镜像。
	// 镜像被手工删掉/换机没带过来时退回基座，否则第一步建容器就 "No such image"，
	// 用户连「重新点一次扩展」这条恢复路都没有（§5.13.1 可恢复性）。
	prevRef := baseRef
	// rebuiltFromBase：退回基座即等于「原启用集从未装进容器」——基座不含任何 prev 扩展。
	// 此时若仍只编译 added，prev 那几项没被编译却照样在任务结尾写进权威库并广播快照，
	// 界面显示「已启用」而 php -m 里没有（§5.16.2：Docker 实际状态 ≡ 库里状态，§5.13.1）。
	// 故该支改为全量重编译目标集；基座本就不含 removed 那几项，无需再删 ini。
	var rebuiltFromBase bool
	if len(prev) > 0 {
		has, err := s.rt.ImageExists(ctx, committedRef)
		if err != nil {
			return err
		}
		if has {
			prevRef = committedRef
		} else {
			rebuiltFromBase = true
			added, removed = uniqueSorted(enabled), nil
		}
	}

	// env 文件旧内容（用于回滚；不存在记 nil）
	envPath := s.envPath(version)
	var prevEnv []byte
	var hadEnv bool
	if b, e := os.ReadFile(envPath); e == nil {
		prevEnv, hadEnv = b, true
	}

	// 临时目录 ./php/{ver}/ext/ 覆盖整条链路（扩展包取回 + 固化镜像导出），任务退出必清（§5.14.4）——
	// 取消与失败也一样清：清的理由随结果变，故 reason 在 Run 之后才定，defer 在此之前先挂上。
	tmpDir, err := s.cache.EnsureTempDir(string(model.KindPHP), version)
	if err != nil {
		return err
	}
	reason := configReasonFailed
	defer func() { _ = s.cache.ClearTempDir(ctx, string(model.KindPHP), version, reason) }()

	// 跨步状态：本次是否已固化新镜像（决定回滚是否需删除 committedRef）
	var committedNew bool
	// 落库的必须是容器重建后实测到的那一份，不是本次请求的目标集——基座自带的扩展（curl/mbstring/PDO…）
	// 从来没进过目标集，只写目标集就等于库里说「这个版本只装了 redis」而 php -m 里有十四项（§5.16.2）。
	var measured []string

	steps := []task.Step{
		&task.FuncStep{StepName: "写扩展清单 extensions.env", Exec: func(_ context.Context, log task.StepLog) error {
			log.Log(string(model.LogCmd), "写扩展清单: "+envPath+" → "+strings.Join(enabled, " "))
			return s.writeEnv(envPath, enabled)
		}, RB: func(context.Context) error {
			if hadEnv {
				return util.WriteFile(envPath, prevEnv)
			}
			if e := os.Remove(envPath); e != nil && !os.IsNotExist(e) {
				return e
			}
			return nil
		}},
		&task.FuncStep{StepName: "准备基座镜像（缓存优先）", Exec: func(ctx context.Context, log task.StepLog) error {
			log.Log(string(model.LogCmd), "确保基座镜像就绪: "+baseRef)
			return s.cache.EnsureImage(ctx, string(model.KindPHP), version, baseRef)
		}},
		&task.FuncStep{StepName: "容器内编译扩展", Exec: func(ctx context.Context, log task.StepLog) error {
			log.Log(string(model.LogMeta), fmt.Sprintf("待启用 %d 项 / 待停用 %d 项", len(added), len(removed)))
			if rebuiltFromBase {
				log.Log(string(model.LogDim), "固化镜像 "+committedRef+" 不在本机，容器上无原扩展集，目标扩展集全量重编译")
			}
			if err := s.ensureRunning(ctx, name, version, prevRef, log); err != nil {
				return err
			}
			if len(added) > 0 {
				s.prefetchBuildDeps(ctx, name, version, tmpDir, log)
			}
			// 停用只对「conf.d 里有那份 ini」的扩展有效：基座静态内建的那几项既无 .so 也无 ini，
			// 让它们进循环等于日志报「已停用 gd」而 php -m 里 gd 照旧在——界面撒谎（§5.16.2 三档显示 / §5.16.4 边界）。
			targets := removed
			if len(targets) > 0 {
				inis, err := s.probeExtInis(ctx, name)
				if err != nil {
					// 分不清哪几项删得掉时照原样逐项处理：rm -f 对不存在的 ini 退出码 0（真机取证，幂等），删不动也不报错
					log.Log(string(model.LogDim), "扩展 ini 清单读取失败，本次逐项照原样停用: "+err.Error())
				} else {
					var keep, skipped []string
					for _, e := range targets {
						if inis[config.ExtIniFile(e)] {
							keep = append(keep, e)
						} else {
							skipped = append(skipped, e)
						}
					}
					if len(skipped) > 0 {
						log.Log(string(model.LogDim), "基座内建（无 conf.d ini 可删），停用无效，已跳过: "+strings.Join(skipped, " "))
					}
					targets = keep
				}
			}
			for _, e := range targets {
				if !config.ValidateExt(e) {
					return fmt.Errorf("扩展名不合法: %s", e)
				}
				args := extDisableArgs(e)
				log.Log(string(model.LogCmd), "停用扩展 "+e+": "+strings.Join(args, " "))
				if _, err := s.runInContainer(ctx, name, log, args); err != nil {
					return extFailed(log, e, "停用", err, nil)
				}
				log.Log(string(model.LogOk), "已停用扩展: "+e)
			}
			for _, e := range added {
				if err := s.installExt(ctx, name, version, tmpDir, e, log); err != nil {
					return err
				}
				log.Log(string(model.LogOk), "已安装扩展: "+e)
			}
			if len(added) > 0 {
				// 暂存目录必须在 commit 之前清掉：包文件留在容器层就会被固化进 phpo/php:{version}
				log.Log(string(model.LogCmd), "清空容器暂存目录: "+strings.Join(config.ExtStagingCleanupCmd, " "))
				if _, err := s.runInContainer(ctx, name, log, config.ExtStagingCleanupCmd); err != nil {
					return err
				}
			}
			return nil
		}, RB: func(ctx context.Context) error {
			// 编译失败：容器 fs 已被 exec 污染，用未变动的原镜像重建一个干净容器即回滚
			return s.recreate(ctx, name, version, prevRef)
		}},
		&task.FuncStep{StepName: "固化扩展镜像 phpo/php:" + version, Exec: func(ctx context.Context, log task.StepLog) error {
			log.Log(string(model.LogCmd), "docker commit → "+committedRef)
			if err := s.rt.CommitContainer(ctx, name, committedRef); err != nil {
				return err
			}
			committedNew = true
			log.Log(string(model.LogOk), "已固化镜像: "+committedRef)
			return s.promoteCommitted(ctx, version, committedRef, tmpDir, log)
		}},
		&task.FuncStep{StepName: "从扩展镜像重建容器", Exec: func(ctx context.Context, log task.StepLog) error {
			log.Log(string(model.LogCmd), "以扩展镜像重建容器: "+name+" ← "+committedRef)
			if err := s.recreate(ctx, name, version, committedRef); err != nil {
				return err
			}
			log.Log(string(model.LogOk), "容器已运行于固化镜像: "+name)
			// 容器已经换到固化镜像上运行，此刻到里面现查一次「到底哪些扩展开着」，
			// 任务结尾落库的就是这一份。拿不到只退回落库目标集并留一行说明——
			// 已编译生效的扩展不该因为一次读探针失败而被判死（§0.2 规则 16）。
			en, _, ok := s.probeEnabled(ctx, name)
			if !ok {
				log.Log(string(model.LogDim), "未能实测启用集（容器内 php -m），本次落库的是请求的目标集，下次打开「管理扩展」即按实测纠正（非实时）")
			} else {
				measured = en
			}
			return nil
		}, RB: func(ctx context.Context) error {
			// 重建失败：撤回本次固化镜像，退回原镜像运行态
			if committedNew {
				if e := s.rt.ImageRemove(ctx, committedRef); e != nil {
					return e
				}
			}
			return s.recreate(ctx, name, version, prevRef)
		}},
		&task.FuncStep{StepName: "重载 Nginx", Exec: func(ctx context.Context, log task.StepLog) error {
			if s.reload == nil {
				log.Log(string(model.LogDim), "未接入 nginx，跳过重载")
				return nil
			}
			name, ok := s.nginxContainerName()
			if !ok {
				log.Log(string(model.LogDim), "nginx 未安装，跳过重载")
				return nil
			}
			running, err := s.rt.ContainerRunning(ctx, name)
			if err != nil {
				log.Log(string(model.LogDim), "nginx 容器 "+name+" 运行态未知，跳过重载: "+err.Error())
				return nil
			}
			if !running {
				log.Log(string(model.LogDim), "nginx 容器 "+name+" 未运行，跳过重载")
				return nil
			}
			log.Log(string(model.LogCmd), "nginx -s reload")
			if err := s.reload.Reload(ctx); err != nil {
				// 此刻扩展已固化、容器已在扩展镜像上运行，而本次一步都没改过 vhost：
				// 把 nginx 自身的问题判死整单会撤回做对了的扩展工作（§0.2 规则 16）。点名即可。
				log.Log(string(model.LogErr), "Nginx 重载失败（扩展已生效，站点若 502 请检查 nginx 配置）: "+err.Error())
				return nil
			}
			log.Log(string(model.LogOk), "Nginx 已重载，上游指向 "+name)
			return nil
		}},
	}

	t := &task.Task{
		ID:    s.newID("extensions"),
		Label: fmt.Sprintf("应用 PHP %s 扩展 (%s)", version, joinDiff(added, removed)),
		Meta:  model.TaskMeta{Type: "extensions", Kind: string(model.KindPHP), Version: version},
		Steps: steps,
		Apply: func() error {
			persist := measured
			if len(persist) == 0 {
				persist = enabled
			}
			if err := s.store.SetPHPExtensions(version, persist); err != nil {
				return err
			}
			return s.emit(version, persist)
		},
	}
	_, err = s.tasks.Run(ctx, t)
	if err == nil {
		reason = configReasonOK
	}
	return err
}

// runInContainer 在容器内执行 cmd，并把 stdout/stderr 逐行实时转写进任务日志（§5.6.2 每步都要回流）。
// 第二个返回值是本次输出里被 configure 报「找不到」的系统开发包名（多半为空）——扩展编译失败时
// 要靠它把「该装什么」说给用户（§5.16.6）。
func (s *ExtensionService) runInContainer(ctx context.Context, name string, log task.StepLog, cmd []string) ([]string, error) {
	missing := map[string]bool{}
	out := &extLogWriter{log: log, level: string(model.LogMeta), missing: missing}
	defer out.flush()
	errOut := &extLogWriter{log: log, level: string(model.LogDim), missing: missing}
	defer errOut.flush()
	err := s.rt.ExecStream(ctx, name, cmd, out, errOut)
	// 返回前要先把不足一行的尾巴落地，否则最后一句 configure 报错进不了 missing
	//（defer 那两份保留着兜 panic 路径；flush 在 buf 已空时不产行）
	out.flush()
	errOut.flush()
	return sortedSet(missing), err
}

// sortedSet 把收集袋拍平成字典序切片：日志与 toast 要有稳定形状，不能随 map 迭代顺序漂
func sortedSet(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// extFailed 失败必须点名是哪个扩展：错误消息会被前端 toast 原样弹出，
// 而「容器内命令失败（退出码 2）」不告诉用户该改哪一项；输出细节已逐行进日志，这里只补一行 err 级定位。
// missing 非空时再多说两句：光说「扩展 gd 安装失败」，用户看不见这次真正缺的是系统开发包 zlib，
// 只能反复点「应用并重建」反复失败（真机即如此）；包名按基座两种写法都给出，因为 phpo 不代装系统包。
func extFailed(log task.StepLog, name, verb string, err error, missing []string) error {
	log.Log(string(model.LogErr), fmt.Sprintf("扩展 %s %s失败：%v", name, verb, err))
	if len(missing) == 0 {
		return fmt.Errorf("扩展 %s %s失败，本次扩展集未应用", name, verb)
	}
	hints := make([]string, 0, len(missing))
	for _, p := range missing {
		hints = append(hints, config.ExtSysPkgHint(p))
	}
	log.Log(string(model.LogErr), fmt.Sprintf("扩展 %s 缺编译要用的系统开发包: %s", name, strings.Join(hints, "、")))
	log.Log(string(model.LogDim), "phpo 不代装系统包：先在正在运行的这个 php 容器里装上上面这些包（Debian 基座 apt-get install -y、Alpine 基座 apk add），再点一次「应用并重建」——装进容器的那一份会随扩展镜像一起固化，下次不用再装。")
	return fmt.Errorf("扩展 %s %s失败（缺系统开发包 %s），本次扩展集未应用", name, verb, strings.Join(missing, "、"))
}

// extLogWriter 按行落地容器内命令输出：configure/make 可达数百行且是流式产出，
// 攒成整串再打印等于让用户盯着一段时长未知的「执行中」。无换行的超长进度条按 extLineMax 强制断行。
// missing 非 nil 时顺带认出 configure 报「找不到」的系统开发包名（只收集，不改写日志）。
type extLogWriter struct {
	log     task.StepLog
	level   string
	buf     []byte
	missing map[string]bool
}

const extLineMax = 4096

func (w *extLogWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > extLineMax {
		w.emit(string(w.buf[:extLineMax]))
		w.buf = w.buf[extLineMax:]
	}
	return len(p), nil
}

// flush 收尾不足一行的一段输出（容器命令结束时通常没有末尾换行）
func (w *extLogWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

func (w *extLogWriter) emit(line string) {
	s := strings.TrimRight(line, " \r")
	if s == "" {
		return
	}
	for _, d := range config.ExtMissingDepNames(s) {
		w.missing[d] = true
	}
	w.log.Log(w.level, s)
}

// installExt 装一项扩展：先让**包文件**离线可得（命中缓存即零网络回填容器暂存目录，未命中则下载 → 取回 → 提升），
// 再从暂存文件编译；pecl 包文件取不到时退回在线 `pecl install`（缓存是加速手段，不是新增的失败面，§0.2 规则 16）。
func (s *ExtensionService) installExt(ctx context.Context, ctr, version, tmpDir, ext string, log task.StepLog) error {
	cmds := config.ExtInstallCmds(ext)
	if config.ClassifyExt(ext) == config.ExtToolPECL {
		if staged := s.peclPackage(ctx, ctr, version, tmpDir, ext, log); staged != "" {
			cmds = config.ExtInstallFromFileCmds(ext, staged)
		}
	}
	return s.runCmds(ctx, ctr, ext, cmds, log)
}

// runCmds 逐条执行安装命令；失败点名到扩展（toast 直出，§3.2 原则 3）
func (s *ExtensionService) runCmds(ctx context.Context, ctr, ext string, cmds [][]string, log task.StepLog) error {
	for _, c := range cmds {
		log.Log(string(model.LogCmd), "安装扩展 "+ext+": "+strings.Join(c, " "))
		missing, err := s.runInContainer(ctx, ctr, log, c)
		if err != nil {
			return extFailed(log, ext, "安装", err, missing)
		}
	}
	return nil
}

// peclPackage 让 pecl 的 .tgz 本体进离线缓存，返回容器内可直接 `pecl install` 的包文件路径。
// 命中：CopyTo 回填暂存目录（零网络）；未命中：`pecl download` 只取包不编译 → 宿主取回临时目录 → 提升登记 manifest。
// 任一缓存环节（查询/回填/下载/取回/提升）失败都只记一行 dim 并返回 ""，让在线编译继续——
// 编译才是本单的目的，缓存失败判死整单等于把已做对的工作撤回（§5.16.3）。
func (s *ExtensionService) peclPackage(ctx context.Context, ctr, version, tmpDir, ext string, log task.StepLog) string {
	lk, err := s.cache.LookupExtPackage(version, config.ExtTypePECL, ext)
	switch {
	case err != nil:
		log.Log(string(model.LogDim), "扩展包缓存查询失败，本次走网络: "+err.Error())
	case lk.Corrupted:
		s.emitCacheCorrupted(version, filepath.Base(lk.Path))
		log.Log(string(model.LogDim), "扩展包缓存校验失败，回退网络: "+lk.Path)
	case lk.Hit:
		staged := path.Join(config.ExtStagingPECL, filepath.Base(lk.Path))
		if e := s.rt.CopyTo(ctx, ctr, config.ExtStagingPECL, lk.Path); e != nil {
			log.Log(string(model.LogDim), "扩展包回填容器失败，本次走网络: "+e.Error())
			return ""
		}
		s.emitter.Emit("cache:hit", model.CacheHitEvent{
			Kind: string(model.KindPHP), Version: version, Source: "offline", Size: lk.Size})
		log.Log(string(model.LogOk), "命中扩展包缓存（零网络）: "+lk.Path+" → "+staged)
		return staged
	}

	s.emitter.Emit("cache:miss", model.CacheMissEvent{
		Kind: string(model.KindPHP), Version: version, Action: "download"})
	cmd := config.ExtPeclDownloadCmd(ext)
	log.Log(string(model.LogCmd), "取扩展包到容器暂存目录: "+strings.Join(cmd, " "))
	if _, e := s.runInContainer(ctx, ctr, log, cmd); e != nil {
		log.Log(string(model.LogDim), "pecl download 失败，退回在线编译: "+e.Error())
		return ""
	}
	files, e := s.rt.CopyFrom(ctx, ctr, config.ExtStagingPECL, filepath.Join(tmpDir, config.ExtTypePECL))
	if e != nil {
		log.Log(string(model.LogDim), "扩展包取回宿主失败（本次不提升缓存）: "+e.Error())
		return ""
	}
	dstDir := s.env.OfflineExtDir(string(model.KindPHP), version, config.ExtTypePECL)
	staged := ""
	for _, f := range files {
		if !strings.HasPrefix(f, ext+"-") {
			continue
		}
		if e := s.cache.PromoteExtension(version, config.ExtTypePECL, filepath.Join(tmpDir, config.ExtTypePECL, f)); e != nil {
			log.Log(string(model.LogDim), "扩展包提升失败（不影响本次编译）: "+e.Error())
		} else {
			log.Log(string(model.LogOk), "已缓存扩展包: pecl/"+f+" → "+dstDir)
		}
		staged = path.Join(config.ExtStagingPECL, f) // files 已排序，取最后一份即版本序最大
	}
	if staged == "" {
		log.Log(string(model.LogDim), "暂存目录内无 "+ext+"-*.tgz，退回在线编译")
	}
	return staged
}

// prefetchBuildDeps 把 Alpine 基座的构建依赖（$PHPIZE_DEPS：phpize/autoconf/g++ 一类）的 .apk 也离线化：
// apk add --cache-dir 才有可取回的包文件（官方脚本用的 --no-cache 用完即弃，永远拿不到包）。
// 命中缓存先把 .apk 回填进容器缓存目录，之后 apk add 即零网络；取回后逐份提升登记。
// 基座不是 Alpine 时没有这一类包文件（Debian 的 docker-php-ext-install 不下系统包），一行 dim 说明后跳过——
// 不为此造 deb 槽位（YAGNI，§3.4.2）。本步任何失败都不得判死扩展编译。
func (s *ExtensionService) prefetchBuildDeps(ctx context.Context, ctr, version, tmpDir string, log task.StepLog) {
	var out bytes.Buffer
	if err := s.rt.ExecStream(ctx, ctr, config.ExtPkgProbeCmd, &out, io.Discard); err != nil {
		log.Log(string(model.LogDim), "基座包管理器探测失败，跳过构建依赖预取: "+err.Error())
		return
	}
	if pm := config.PkgManager(strings.TrimSpace(out.String())); pm != config.PkgManagerAPK {
		log.Log(string(model.LogDim), "基座包管理器为 "+string(pm)+"，构建依赖不产生可离线包文件")
		return
	}

	cached, err := s.cache.ListExtPackages(version, config.ExtTypeAPK)
	if err != nil {
		log.Log(string(model.LogDim), "apk 缓存查询失败，本次走网络: "+err.Error())
	} else if len(cached) > 0 {
		if e := s.rt.CopyTo(ctx, ctr, config.ExtStagingAPK, cached...); e != nil {
			log.Log(string(model.LogDim), "apk 缓存回填失败，本次走网络: "+e.Error())
		} else {
			log.Log(string(model.LogOk), fmt.Sprintf("命中 apk 构建依赖缓存（零网络）: %d 份 → %s", len(cached), config.ExtStagingAPK))
		}
	}

	cmd := config.ExtApkPrefetchCmd()
	log.Log(string(model.LogCmd), "预取构建依赖: "+strings.Join(cmd, " "))
	if _, err := s.runInContainer(ctx, ctr, log, cmd); err != nil {
		log.Log(string(model.LogDim), "构建依赖预取失败（不影响扩展编译）: "+err.Error())
		return
	}

	files, err := s.rt.CopyFrom(ctx, ctr, config.ExtStagingAPK, filepath.Join(tmpDir, config.ExtTypeAPK))
	if err != nil {
		log.Log(string(model.LogDim), "apk 取回宿主失败（本次不提升缓存）: "+err.Error())
		return
	}
	dstDir := s.env.OfflineExtDir(string(model.KindPHP), version, config.ExtTypeAPK)
	for _, f := range files {
		if e := s.cache.PromoteExtension(version, config.ExtTypeAPK, filepath.Join(tmpDir, config.ExtTypeAPK, f)); e != nil {
			log.Log(string(model.LogDim), "apk 提升失败（不影响本次编译）: "+e.Error())
			continue
		}
		log.Log(string(model.LogOk), "已缓存构建依赖包: apk/"+f+" → "+dstDir)
	}
}

// emitCacheCorrupted 缓存条目 SHA256 不匹配的取证行（§5.14.5）
func (s *ExtensionService) emitCacheCorrupted(version, entry string) {
	s.emitter.Emit("cache:corrupted", model.CacheCorruptedEvent{
		Kind: string(model.KindPHP), Version: version, Entry: model.ManifestPackage{Name: entry}})
}

// extDisableArgs 停用扩展 = 删掉它的 ini。真机取证 php 镜像内没有 docker-php-ext-disable
// （只有 -install / -enable / docker-php-source），沿用不存在的命令会让每次取消勾选必然失败并整单回滚。
func extDisableArgs(name string) []string {
	return []string{"rm", "-f", config.ExtConfDir + "/" + config.ExtIniFile(name)}
}

// ensureRunning 保证 name 容器以 image 运行：未运行则从 image 重建并启动
func (s *ExtensionService) ensureRunning(ctx context.Context, name, version, image string, log task.StepLog) error {
	running, err := s.rt.ContainerRunning(ctx, name)
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	log.Log(string(model.LogDim), "php 容器未运行，先恢复: "+name)
	return s.recreate(ctx, name, version, image)
}

// recreate 幂等地让 name 以 image 处于运行：Pre-Clean 同名 → 建（复用 PHP 装配，仅覆盖镜像）→ 启
func (s *ExtensionService) recreate(ctx context.Context, name, version, image string) error {
	if err := s.rt.PreCleanContainer(ctx, name); err != nil {
		return err
	}
	spec, err := (PHPService{}).ContainerSpec(version, s.env)
	if err != nil {
		return err
	}
	spec.Image = image
	if _, err := s.rt.CreateServiceContainer(ctx, s.env, spec); err != nil {
		return err
	}
	return s.rt.StartContainer(ctx, name)
}

// promoteCommitted 把固化镜像 docker save 到临时目录并提升到离线缓存的空闲槽位 image-extensions.tar。
// 临时目录由 Apply 统管（整条链路共用一份，任务退出必清），此处只借用不再另建另清。
func (s *ExtensionService) promoteCommitted(ctx context.Context, version, committedRef, tmpDir string, log task.StepLog) error {
	tmpTar := filepath.Join(tmpDir, "image.tar")
	extTar := s.env.OfflineExtImageTar(string(model.KindPHP), version)
	log.Log(string(model.LogCmd), "导出扩展镜像到临时目录: "+tmpTar)
	if err := s.rt.ImageSave(ctx, committedRef, tmpTar); err != nil {
		return err
	}
	if err := s.cache.PromoteExtImage(version, committedRef, tmpTar); err != nil {
		return err
	}
	// 提升落点必须点名到缓存槽位：与基座 image.tar 各占一份，用户据此核对「扩展装进缓存了没有」
	log.Log(string(model.LogOk), "已提升到离线缓存: "+committedRef+" → "+extTar)
	return nil
}

const (
	configReasonOK     = "compile_ok"
	configReasonFailed = "compile_failed"
)

func (s *ExtensionService) emit(version string, exts []string) error {
	s.emitter.Emit("service:changed", map[string]any{"kind": string(model.KindPHP), "version": version, "extensions": exts})
	fresh, err := s.store.BuildSnapshot()
	if err != nil {
		return err
	}
	s.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return nil
}

func (s *ExtensionService) envPath(version string) string {
	return filepath.Join(s.env.RootFor(string(model.KindPHP), version), "conf", "extensions.env")
}

// nginxContainerName 现取 nginx 单例容器名；未安装 nginx 即 ok=false。
// 判据必须在权威快照里现取，不能只看装配期注入的 Reloader —— 生产恒注入非 nil，
// 「未接入 nginx」那一支在真机上永不成立，于是没装 nginx 的机器每次应用扩展都被判死并整单回滚。
func (s *ExtensionService) nginxContainerName() (string, bool) {
	snap, err := s.store.BuildSnapshot()
	if err != nil || snap == nil {
		return "", false
	}
	vers := snap.Installed[string(model.KindNginx)]
	if len(vers) == 0 {
		return "", false
	}
	return dockerutil.ContainerName(string(model.KindNginx), vers[0]), true
}

func (s *ExtensionService) writeEnv(path string, enabled []string) error {
	if err := util.MkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# phpo 启用的 PHP 扩展（每行一个）\n")
	for _, e := range enabled {
		b.WriteString(e + "\n")
	}
	return util.WriteFile(path, []byte(b.String()))
}

func (s *ExtensionService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}

func joinDiff(added, removed []string) string {
	var parts []string
	for _, a := range added {
		parts = append(parts, "+"+a)
	}
	for _, r := range removed {
		parts = append(parts, "-"+r)
	}
	return strings.Join(parts, " ")
}
