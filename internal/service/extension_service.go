// T601 · PHP 扩展离线缓存链路：把「启用哪些扩展」落到 phpo 专用镜像并重载 Nginx。
// 机制（用户裁决）：扩展经容器内「内置编译工具」（docker-php-ext-install / pecl）安装，不下载 .tgz/.apk；
// 装好后 docker commit 固化出 phpo/php:{version}，重装同配置时经离线缓存零网络加载。
// 全程三段式（硬红线 5）+ 后端权威广播（硬红线 4）+ 无论成败清空临时目录（硬红线 8）。
package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
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
}

// ExtImageCache 离线镜像缓存子集（*cache.Manager 满足）
type ExtImageCache interface {
	EnsureImage(ctx context.Context, kind, version, ref string) error
	CachedImageRef(kind, version string) (string, bool)
	PromoteImage(kind, version, ref, tmpTar string) error
	EnsureTempDir(kind, version string) (string, error)
	ClearTempDir(ctx context.Context, kind, version, reason string) error
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

// List 返回某 php 版本当前启用的扩展（后端权威）
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

	// 跨步状态：本次是否已固化新镜像（决定回滚是否需删除 committedRef）
	var committedNew bool

	steps := []task.Step{
		&task.FuncStep{StepName: "写扩展清单 extensions.env", Exec: func(_ context.Context, log task.StepLog) error {
			log.Log(string(model.LogCmd), "写扩展清单: "+envPath+" → "+strings.Join(enabled, " "))
			return s.writeEnv(envPath, enabled)
		}, RB: func(context.Context) error {
			if hadEnv {
				return os.WriteFile(envPath, prevEnv, 0o644)
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
			for _, e := range removed {
				if !config.ValidateExt(e) {
					return fmt.Errorf("扩展名不合法: %s", e)
				}
				args := extDisableArgs(e)
				log.Log(string(model.LogCmd), "停用扩展 "+e+": "+strings.Join(args, " "))
				if err := s.runInContainer(ctx, name, log, args); err != nil {
					return extFailed(log, e, "停用", err)
				}
				log.Log(string(model.LogOk), "已停用扩展: "+e)
			}
			for _, e := range added {
				cmds := config.ExtInstallCmds(e)
				if len(cmds) == 0 {
					return fmt.Errorf("扩展名不合法: %s", e)
				}
				for _, cmd := range cmds {
					log.Log(string(model.LogCmd), "安装扩展 "+e+": "+strings.Join(cmd, " "))
					if err := s.runInContainer(ctx, name, log, cmd); err != nil {
						return extFailed(log, e, "安装", err)
					}
				}
				log.Log(string(model.LogOk), "已安装扩展: "+e)
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
			return s.promoteCommitted(ctx, version, committedRef, log)
		}},
		&task.FuncStep{StepName: "从扩展镜像重建容器", Exec: func(ctx context.Context, log task.StepLog) error {
			log.Log(string(model.LogCmd), "以扩展镜像重建容器: "+name+" ← "+committedRef)
			if err := s.recreate(ctx, name, version, committedRef); err != nil {
				return err
			}
			log.Log(string(model.LogOk), "容器已运行于固化镜像: "+name)
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
			log.Log(string(model.LogCmd), "nginx -s reload")
			if err := s.reload.Reload(ctx); err != nil {
				return err
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
			if err := s.store.SetPHPExtensions(version, enabled); err != nil {
				return err
			}
			return s.emit(version, enabled)
		},
	}
	_, err = s.tasks.Run(ctx, t)
	return err
}

// runInContainer 在容器内执行 cmd，并把 stdout/stderr 逐行实时转写进任务日志（§5.6.2 每步都要回流）。
func (s *ExtensionService) runInContainer(ctx context.Context, name string, log task.StepLog, cmd []string) error {
	out := &extLogWriter{log: log, level: string(model.LogMeta)}
	defer out.flush()
	errOut := &extLogWriter{log: log, level: string(model.LogDim)}
	defer errOut.flush()
	return s.rt.ExecStream(ctx, name, cmd, out, errOut)
}

// extFailed 失败必须点名是哪个扩展：错误消息会被前端 toast 原样弹出，
// 而「容器内命令失败（退出码 2）」不告诉用户该改哪一项；输出细节已逐行进日志，这里只补一行 err 级定位。
func extFailed(log task.StepLog, name, verb string, err error) error {
	log.Log(string(model.LogErr), fmt.Sprintf("扩展 %s %s失败：%v", name, verb, err))
	return fmt.Errorf("扩展 %s %s失败，本次扩展集未应用", name, verb)
}

// extLogWriter 按行落地容器内命令输出：configure/make 可达数百行且是流式产出，
// 攒成整串再打印等于让用户盯着一段时长未知的「执行中」。无换行的超长进度条按 extLineMax 强制断行。
type extLogWriter struct {
	log   task.StepLog
	level string
	buf   []byte
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
	if s := strings.TrimRight(line, " \r"); s != "" {
		w.log.Log(w.level, s)
	}
}

// phpExtConfDir 官方 php 镜像的扩展 ini 目录：docker-php-ext-enable 即往此处写 docker-php-ext-<name>.ini
const phpExtConfDir = "/usr/local/etc/php/conf.d"

// extDisableArgs 停用扩展 = 删掉它的 ini。真机取证 php 镜像内没有 docker-php-ext-disable
// （只有 -install / -enable / docker-php-source），沿用不存在的命令会让每次取消勾选必然失败并整单回滚。
func extDisableArgs(name string) []string {
	return []string{"rm", "-f", phpExtConfDir + "/docker-php-ext-" + strings.TrimSpace(name) + ".ini"}
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

// promoteCommitted 把固化镜像 docker save 到临时目录并提升到离线缓存；无论成败清空临时目录
func (s *ExtensionService) promoteCommitted(ctx context.Context, version, committedRef string, log task.StepLog) error {
	tmpDir, err := s.cache.EnsureTempDir(string(model.KindPHP), version)
	if err != nil {
		return err
	}
	reason := configReasonFailed
	defer func() { _ = s.cache.ClearTempDir(ctx, string(model.KindPHP), version, reason) }()
	tmpTar := filepath.Join(tmpDir, "image.tar")
	log.Log(string(model.LogCmd), "导出扩展镜像到离线缓存: "+tmpTar)
	if err := s.rt.ImageSave(ctx, committedRef, tmpTar); err != nil {
		return err
	}
	if err := s.cache.PromoteImage(string(model.KindPHP), version, committedRef, tmpTar); err != nil {
		return err
	}
	log.Log(string(model.LogOk), "已提升到离线缓存: "+committedRef)
	reason = configReasonOK
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

func (s *ExtensionService) writeEnv(path string, enabled []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# phpo 启用的 PHP 扩展（每行一个）\n")
	for _, e := range enabled {
		b.WriteString(e + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
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
