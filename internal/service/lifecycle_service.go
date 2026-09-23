// 生命周期服务：容器装卸/启停 + 状态校准；校准在启动 / 每任务后 / 手动三处触发（§5.13.9）
// 依赖以最小接口注入（DockerOps/StateStore/Emitter），核心判定复用 T305 的 Op 与 engine.Calibrate，无 Docker 亦可测。
package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/pkg/dockerutil"
	"phpo/pkg/errs"
	"phpo/pkg/port"
)

// DockerOps engine.Client 提供的最小容器操作集（*engine.Client 天然满足）
type DockerOps interface {
	ManagedContainers(ctx context.Context) ([]engine.ActualState, error)
	CreateServiceContainer(ctx context.Context, env config.Env, spec engine.ContainerSpec) (string, error)
	StartContainer(ctx context.Context, name string) error
	StopContainer(ctx context.Context, name string) error
	RemoveContainer(ctx context.Context, name string) error
	PreCleanContainer(ctx context.Context, name string) error
	PublishedPorts(ctx context.Context, name string) ([]int, error)
	ContainerRunning(ctx context.Context, name string) (bool, error)
	ContainerExists(ctx context.Context, name string) bool
	ImageExists(ctx context.Context, ref string) (bool, error)
}

// StateStore SQLite 权威视图读写子集（*store.Store 满足）
type StateStore interface {
	BuildSnapshot() (*model.Snapshot, error)
	SetInstalled(kind, version string, installed bool) error
	SetRunning(kind, version string, running bool) error
	SetGaps(gaps []model.ServiceGap) // 缺失态是派生态：随快照广播，不落库（§5.19）
}

// Emitter §5.6 事件发射最小抽象（app.Emitter 满足）
type Emitter interface {
	Emit(event string, payload any)
}

// Service 一种服务的安装装配策略：产出创建容器所需的 spec
type Service interface {
	Kind() model.ServiceKind
	ContainerSpec(version string, env config.Env) (engine.ContainerSpec, error)
}

// ExtImageLoader 从离线缓存零网络载入扩展固化镜像（*cache.Manager 满足）
type ExtImageLoader interface {
	LoadExtImage(ctx context.Context, version string) (ref string, ok bool, err error)
}

// LifecycleService 组合注入依赖；env 为当前派生路径
type LifecycleService struct {
	docker    DockerOps
	store     StateStore
	emitter   Emitter
	env       config.Env
	services  map[model.ServiceKind]Service
	nginx     *NginxService
	extImages ExtImageLoader
}

func NewLifecycle(docker DockerOps, store StateStore, emitter Emitter, env config.Env, reader EnvReader) *LifecycleService {
	svc := &LifecycleService{docker: docker, store: store, emitter: emitter, env: env, services: map[model.ServiceKind]Service{}}
	// Nginx 需读服务端口（用户配过的基准端口）；MySQL / PgSQL / Redis 需读明文密码与按版本端口，reader 为 ConfigStore（config.yaml）
	ng := NewNginxService(reader)
	svc.nginx = ng
	for _, s := range []Service{
		PHPService{}, ng,
		NewDBService(model.KindMySQL, reader),
		NewDBService(model.KindPgsql, reader),
		NewRedisService(reader),
	} {
		svc.services[s.Kind()] = s
	}
	return svc
}

// SetNginxPortSource 注入站点端口来源（di 装配期，SiteService 就绪后调用）：
// 让装/重建 nginx 与站点写链路发布同一份端口并集，不出现「nginx 起来了、站点端口没绑」的空档。
func (l *LifecycleService) SetNginxPortSource(src NginxPortSource) { l.nginx.SetPortSource(src) }

// SetExtImageLoader 注入离线缓存的固化镜像载入器（di 装配期，cache.Manager 就绪后调用）
func (l *LifecycleService) SetExtImageLoader(ld ExtImageLoader) { l.extImages = ld }

// ---- 校准 ----

// Snapshot 读当前权威视图（后端唯一权威，前端经 state:changed 或直接拉取）
func (l *LifecycleService) Snapshot() (*model.Snapshot, error) {
	return l.store.BuildSnapshot()
}

// Calibrate 读 SQLite 期望态与 Docker 实际态比对：
//   - 有运行态修正 → 写回 SetRunning，并依次发 docker:state-drift（漂移详情）与 state:changed（新快照）；
//   - 无变化则静默返回。
//
// 存在性漂移（缺失/孤儿）仅随事件上报，不自动删建，避免误删用户数据（§5.13.13）。
//
// 本口径是「每任务后 / 启动」的轻量校准：容器存在性用已经取到的实际态判，不额外拨 Docker。
// 镜像层面的存在性核查（基座镜像、php 扩展固化镜像）只在手动「同步状态」这条全量口径里做，见 SyncAll。
func (l *LifecycleService) Calibrate(ctx context.Context) (*engine.CalibrateResult, error) {
	return l.calibrate(ctx, false)
}

// SyncAll 手动「同步状态」的全量口径（§5.19）：在轻量校准之上，逐个已安装版本再核一遍
// 基座镜像与 php 扩展固化镜像是否还在本机——用户用第三方工具把镜像也删了，界面必须说出来。
// 三项都只上报缺失态（进快照 Gaps）+ 逐行点名，不自动改 installed、不自动删任何东西。
func (l *LifecycleService) SyncAll(ctx context.Context) (*engine.CalibrateResult, error) {
	return l.calibrate(ctx, true)
}

func (l *LifecycleService) calibrate(ctx context.Context, auditImages bool) (*engine.CalibrateResult, error) {
	snap, err := l.store.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	actual, err := l.docker.ManagedContainers(ctx)
	if err != nil {
		return nil, err
	}
	res := engine.Calibrate(refsOf(snap.Installed), refsOf(snap.Running), actual)
	gaps, err := l.detectGaps(ctx, snap, actual, auditImages)
	if err != nil {
		return nil, err
	}
	// 发不发事件只看「本次比上次多说了什么」：运行态修正在跑，或缺失项集合变了。
	// 不用 res.Changed()——它把「存在性漂移」也算进去，而容器缺席是常态化的（停了就是缺席），
	// 于是每次校准/每次点同步都会重发同样的 drift + 快照，抽屉被同一行刷屏。
	if len(res.Corrections) == 0 && sameGaps(snap.Gaps, gaps) {
		return &res, nil
	}

	for _, c := range res.Corrections {
		if err := l.store.SetRunning(c.Ref.Kind, c.Ref.Version, c.Running); err != nil {
			return nil, fmt.Errorf("校准回写运行态失败 %s/%s: %w", c.Ref.Kind, c.Ref.Version, err)
		}
	}
	l.store.SetGaps(gaps)
	l.emitter.Emit("docker:state-drift", model.StateDrift{
		Expected: driftView(snap.Installed, snap.Running),
		Actual:   actualView(actual),
		Gaps:     gaps,
	})
	fresh, err := l.store.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	l.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return &res, nil
}

// detectGaps 逐个已安装版本核「库里说装过、宿主上却不在了」的东西。
// 容器存在性复用调用方已取到的实际态（零额外 IO）；镜像存在性按 auditImages 分档：
//   - false（每任务后 / 启动校准）只判容器；
//   - true（手动同步状态）再判该版本应运行的镜像，以及 php 的扩展固化镜像。
//
// 探针报错一律上抛：静默当作「不在本机」等于把一次正常的镜像说成缺失，比不报更糟（§5.14.12）。
func (l *LifecycleService) detectGaps(ctx context.Context, snap *model.Snapshot, actual []engine.ActualState, auditImages bool) ([]model.ServiceGap, error) {
	present := make(map[string]bool, len(actual))
	for _, a := range actual {
		present[a.Ref.Name()] = true
	}
	var gaps []model.ServiceGap
	for _, kind := range sortedKinds(snap.Installed) {
		for _, version := range uniqueSorted(snap.Installed[kind]) {
			name := dockerutil.ContainerName(kind, version)
			if !present[name] {
				gaps = append(gaps, model.ServiceGap{Kind: kind, Version: version, Reason: model.GapContainer, Ref: name})
			}
			if !auditImages {
				continue
			}
			extRef := engine.CommittedPHPRef(version)
			hasExt := false
			if kind == string(model.KindPHP) {
				has, err := l.docker.ImageExists(ctx, extRef)
				if err != nil {
					return nil, err
				}
				hasExt = has
				// 库里记着启用过扩展、固化镜像却不在本机：容器只能退回基座，那几项其实没在跑（§5.16.2）
				if !has && len(snap.PHPExtensions[version]) > 0 {
					gaps = append(gaps, model.ServiceGap{Kind: kind, Version: version, Reason: model.GapExtImage, Ref: extRef})
				}
			}
			// 该版本实际应运行的镜像：php 有固化镜像即以它为准，否则是官方基座
			want := extRef
			if !hasExt {
				ref, err := engine.ImageRefFor(kind, version)
				if err != nil {
					return nil, err
				}
				want = ref
			}
			if has, err := l.docker.ImageExists(ctx, want); err != nil {
				return nil, err
			} else if !has {
				gaps = append(gaps, model.ServiceGap{Kind: kind, Version: version, Reason: model.GapImage, Ref: want})
			}
		}
	}
	return gaps, nil
}

// sortedKinds 给出稳定的 kind 顺序：sameGaps 按内容+顺序比对，无序即每次同步都刷一遍日志
func sortedKinds(installed map[string][]string) []string {
	out := make([]string, 0, len(installed))
	for kind := range installed {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

// sameGaps 两组缺失项是否等价（内容与顺序一致即等价；detectGaps 按 kind/version 排序产出，故顺序稳定）
func sameGaps(a, b []model.ServiceGap) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- 生命周期操作（三阶段 Op 包装） ----

// Install 缓存管道（T303）已确保镜像就绪后，创建并启动容器：Pre-Clean 同名 → 建 → 启 → 校验在运行。
// 幂等：Pre-Clean 抹平同名差异；已达成态由 Post-Verify 判定。配置渲染于 T307 补全（此处镜像自带默认即可起）。
func (l *LifecycleService) Install(ctx context.Context, kind model.ServiceKind, version string) error {
	spec, err := l.specFor(ctx, kind, version)
	if err != nil {
		return err
	}
	return l.installSpec(ctx, kind, version, spec, "install")
}

// Reinstall 重建容器，让「只有建容器时才落定」的配置真正生效：宿主端口发布 + 密码 env。
// Start 只启停同名容器，改不动这两项，所以改完端口/密码必须重建（§5.13.4 幂等「重装」）。
// Pre-Clean 只删同名容器，数据卷与绑定目录保留（§5.13.7）。端口占用在 Pre-Clean 之前拒绝：
// 否则容器已删、新端口却绑不上，等于白丢一个正在跑的服务。
func (l *LifecycleService) Reinstall(ctx context.Context, kind model.ServiceKind, version string) error {
	spec, err := l.specFor(ctx, kind, version)
	if err != nil {
		return err
	}
	snap, err := l.store.BuildSnapshot()
	if err != nil {
		return err
	}
	if !snap.HasVersion(string(kind), version) {
		return fmt.Errorf("服务未安装，无法重建：%s/%s", kind, version)
	}
	if err := assertHostPortsFree(snap, kind, version, spec); err != nil {
		return err
	}
	return l.installSpec(ctx, kind, version, spec, "reinstall")
}

// specFor 取该种类的装配策略并产出容器 spec。
// php 额外一步：以扩展链路 commit 出的 phpo/php:{version} 为准——先探本机镜像库，
// 本机没有（docker rmi / 换机）再查离线缓存的 image-extensions.tar 零网络载入回来。
// 从基座 php:{version}-fpm 建容器会抹掉用户已启用的扩展，而 php_extensions 表还记着它们（§5.13.1 一致性）。
// 两处探针报错都必须上抛：静默当作「本机没有」等于把上述抹除过程伪装成一次正常重建（§5.14.12）。
func (l *LifecycleService) specFor(ctx context.Context, kind model.ServiceKind, version string) (engine.ContainerSpec, error) {
	svc, ok := l.services[kind]
	if !ok {
		return engine.ContainerSpec{}, fmt.Errorf("未注册的服务种类: %s", kind)
	}
	spec, err := svc.ContainerSpec(version, l.env)
	if err != nil {
		return spec, err
	}
	if kind == model.KindPHP {
		ref := engine.CommittedPHPRef(version)
		has, err := l.docker.ImageExists(ctx, ref)
		if err != nil {
			return spec, err
		}
		if !has && l.extImages != nil {
			loaded, ok, err := l.extImages.LoadExtImage(ctx, version)
			if err != nil {
				return spec, err
			}
			has, ref = ok, loaded
		}
		if has {
			spec.Image = ref
		}
	}
	return spec, nil
}

// installSpec 三阶段建/启容器并落库：Pre-Clean 同名 → create+start → Post-Verify → commit
func (l *LifecycleService) installSpec(ctx context.Context, kind model.ServiceKind, version string, spec engine.ContainerSpec, label string) error {
	name := dockerutil.ContainerName(string(kind), version)
	if kind == model.KindNginx {
		if err := l.assertSitePortsBindable(ctx, name, spec); err != nil {
			return err
		}
	}
	op := engine.Op{
		Name:     label + " " + name,
		PreClean: func(ctx context.Context) error { return l.docker.PreCleanContainer(ctx, name) },
		Execute: func(ctx context.Context) error {
			if _, err := l.docker.CreateServiceContainer(ctx, l.env, spec); err != nil {
				return err
			}
			return l.docker.StartContainer(ctx, name)
		},
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, true) },
		Rollback:   func(ctx context.Context) error { return l.docker.RemoveContainer(ctx, name) },
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	return l.commit(ctx, kind, version, true, true)
}

// RepublishNginx 重建 nginx 单例容器以重绑站点端口并集（1:1 host==container）。
// Docker 端口绑定只能在建容器时确定，改站点端口须重建 nginx；未安装 nginx 则跳过（建站不应强起 nginx）。
// 只在 nginx **正在服务**时动手：用户主动停掉（或手工删掉）的容器不得被站点写链路悄悄拉起，
// 停机期间站点保持降级（§5.8），端口等 nginx 下次启动的就绪补齐再绑。
// 发布集与容器当前绑定一致即跳过：重建等于把整站白闪断一次，而校对会被补齐链路反复触发。
// 本次新增的宿主端口先实探再动手：占用即在 Pre-Clean 之前拒绝，保住正在服务的 nginx。
func (l *LifecycleService) RepublishNginx(ctx context.Context, ports []int) error {
	snap, err := l.store.BuildSnapshot()
	if err != nil {
		return err
	}
	vers := snap.Installed[string(model.KindNginx)]
	if len(vers) == 0 {
		return nil
	}
	ver := vers[0]
	name := dockerutil.ContainerName(string(model.KindNginx), ver)
	running, err := l.docker.ContainerRunning(ctx, name)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	spec, err := l.nginx.specWith(l.env, ver, ports)
	if err != nil {
		return err
	}
	held, err := l.docker.PublishedPorts(ctx, name)
	if err != nil {
		return err
	}
	if sameHostPorts(spec.PortMap, held) {
		return nil
	}
	if err := l.assertSitePortsBindable(ctx, name, spec); err != nil {
		return err
	}
	op := engine.Op{
		Name:     "重发布 nginx 站点端口 " + name,
		PreClean: func(ctx context.Context) error { return l.docker.PreCleanContainer(ctx, name) },
		Execute: func(ctx context.Context) error {
			if _, err := l.docker.CreateServiceContainer(ctx, l.env, spec); err != nil {
				return err
			}
			return l.docker.StartContainer(ctx, name)
		},
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, model.KindNginx, ver, true) },
		Rollback:   func(ctx context.Context) error { return l.docker.RemoveContainer(ctx, name) },
	}
	return op.Run(ctx)
}

// sameHostPorts 容器当前发布到宿主的端口集是否已与目标一致（顺序无关；重复值按集合比）
func sameHostPorts(want map[string]string, held []int) bool {
	got := make(map[int]bool, len(held))
	for _, p := range held {
		got[p] = true
	}
	if len(got) != len(want) {
		return false
	}
	for _, hostPort := range want {
		p, err := strconv.Atoi(hostPort)
		if err != nil || !got[p] {
			return false
		}
	}
	return true
}

// Start 启动已安装容器（幂等：已运行则 Post-Verify 直接通过）。
// 容器已被外部删除（docker rm / Docker Desktop 重置）时不再抛 "no such container"：
// 库里仍记已安装，就按当前落库配置走一次重建——否则点「启动」永久失败，而 php 这类不发布宿主端口的
// 服务卡片上根本没有重建入口，用户无路可走。
func (l *LifecycleService) Start(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	if !l.docker.ContainerExists(ctx, name) {
		return l.Reinstall(ctx, kind, version)
	}
	op := engine.Op{
		Name:       "start " + name,
		Execute:    func(ctx context.Context) error { return l.docker.StartContainer(ctx, name) },
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, true) },
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	return l.commit(ctx, kind, version, true, true)
}

// Stop 停止容器（幂等；保留数据，§5.13.7）
func (l *LifecycleService) Stop(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	op := engine.Op{
		Name:       "stop " + name,
		Execute:    func(ctx context.Context) error { return l.docker.StopContainer(ctx, name) },
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, false) },
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	return l.commit(ctx, kind, version, true, false)
}

// Remove 卸载：停 + 删容器，**保留数据卷/绑定目录**（§0.2 规则 20、§5.13.7），并置未安装
func (l *LifecycleService) Remove(ctx context.Context, kind model.ServiceKind, version string) error {
	name := dockerutil.ContainerName(string(kind), version)
	op := engine.Op{
		Name:       "remove " + name,
		Execute:    func(ctx context.Context) error { return l.docker.RemoveContainer(ctx, name) },
		PostVerify: func(ctx context.Context) error { return l.verifyRunning(ctx, kind, version, false) },
		Rollback:   func(context.Context) error { return nil }, // 卸载为终结操作，无自动回滚
	}
	if err := op.Run(ctx); err != nil {
		return err
	}
	if err := l.store.SetInstalled(string(kind), version, false); err != nil {
		return err
	}
	fresh, err := l.store.BuildSnapshot()
	if err != nil {
		return err
	}
	l.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return nil
}

// ---- 内部助手 ----

// hostPortKinds 只有数据服务发布单个「服务端口」（§5.8：占用即报 portInUse，不顺延）。
// php 不发布宿主端口；nginx 发布的是基准端口 + 站点端口并集，那些端口以站点名义登记，不适用本判据
// （改走 assertSitePortsBindable 的本机 TCP 实探）。
var hostPortKinds = map[model.ServiceKind]bool{model.KindMySQL: true, model.KindPgsql: true, model.KindRedis: true}

// assertHostPortsFree 逐个校验 spec 要发布的宿主端口：占用判定复用权威快照的同一判据
// （store.CollectUsedPorts）并补上其它服务未落库的默认端口。取表前先剔除本服务版本，
// 否则它自己落库的新端口会被记成「自用」而把真实冲突掩盖掉。
func assertHostPortsFree(snap *model.Snapshot, kind model.ServiceKind, version string, spec engine.ContainerSpec) error {
	if !hostPortKinds[kind] {
		return nil
	}
	used := store.CollectUsedPorts(snapWithout(snap, kind, version), nil)
	addDefaultServicePorts(snap, kind, version, used)
	for _, hostPort := range spec.PortMap {
		p, err := strconv.Atoi(hostPort)
		if err != nil {
			continue
		}
		if owner, ok := used[p]; ok {
			return fmt.Errorf("%s：端口 %d 已被 %s 占用，%s/%s 未重建（在跑的容器与数据均未改动）", errs.PortInUse, p, owner, kind, version)
		}
	}
	return nil
}

// assertSitePortsBindable nginx 专属预检：本次要发布、且不在当前 nginx 容器上的宿主端口逐个本机实探。
// 站点端口的占用者以「站点」名义登记在权威表里（表内看不出真凶），而真凶常是宿主上的 Apache / IIS /
// 另一个 Docker 容器——逻辑表判不出来，只有 bind 一次才知道。占用了却照常 Pre-Clean，就会出现
// 「在跑的 nginx 已删、新容器绑不上端口、回滚只能删壳」，把整站在用的 nginx 拖下线（§5.13.13）。
// 已被本容器发布的端口必须先剔除：那些端口正被 Docker 占着，再探必然误判为占用。
func (l *LifecycleService) assertSitePortsBindable(ctx context.Context, name string, spec engine.ContainerSpec) error {
	held, err := l.docker.PublishedPorts(ctx, name)
	if err != nil {
		return err
	}
	own := make(map[int]bool, len(held))
	for _, p := range held {
		own[p] = true
	}
	for _, hostPort := range spec.PortMap {
		p, err := strconv.Atoi(hostPort)
		if err != nil || own[p] {
			continue
		}
		if err := port.Probe(p); port.InUse(err) {
			return fmt.Errorf("%s：端口 %d 已被本机其它进程占用，Nginx 未重建（在跑的容器与站点均未改动）", errs.PortInUse, p)
		}
	}
	return nil
}

// addDefaultServicePorts 补齐其它数据服务「未显式配过端口」时实际发布的注册表默认端口。
// 占用表只收 config.yaml 已落库的端口键（store.CollectServicePorts），而多数服务的端口从未被改过；
// 漏掉这一档，就会在 Pre-Clean 删掉在跑的容器之后才发现新端口绑不上。
func addDefaultServicePorts(snap *model.Snapshot, self model.ServiceKind, selfVersion string, used port.Used) {
	for _, k := range []model.ServiceKind{model.KindMySQL, model.KindPgsql, model.KindRedis} {
		sp, _ := Get(k)
		for _, v := range snap.Installed[string(k)] {
			if k == self && v == selfVersion {
				continue
			}
			if _, ok := used[sp.HostPort]; !ok {
				used[sp.HostPort] = string(k) + " " + v
			}
		}
	}
}

// snapWithout 返回剔除某服务版本后的快照副本（只换 Installed，env / 站点表原样复用）
func snapWithout(snap *model.Snapshot, kind model.ServiceKind, version string) *model.Snapshot {
	cp := *snap
	cp.Installed = make(map[string][]string, len(snap.Installed))
	for k, vs := range snap.Installed {
		if k != string(kind) {
			cp.Installed[k] = vs
			continue
		}
		var kept []string
		for _, v := range vs {
			if v != version {
				kept = append(kept, v)
			}
		}
		cp.Installed[k] = kept
	}
	return &cp
}

// verifyRunning 从 Docker 实际态断言某容器的存在/运行判定，作 Op.PostVerify
func (l *LifecycleService) verifyRunning(ctx context.Context, kind model.ServiceKind, version string, wantRunning bool) error {
	actual, err := l.docker.ManagedContainers(ctx)
	if err != nil {
		return err
	}
	target := engine.ContainerRef{Kind: string(kind), Version: version}.Name()
	for _, a := range actual {
		if a.Ref.Name() == target {
			if a.Running != wantRunning {
				return fmt.Errorf("运行态校验失败：%s 期望 running=%v，实际 %v", target, wantRunning, a.Running)
			}
			return nil
		}
	}
	if wantRunning {
		return fmt.Errorf("校验失败：容器 %s 不存在", target)
	}
	return nil // 停止/删除后不存在即符合预期
}

// commit 落地一次运行态变更：写库 + 发 service:changed 与 state:changed
func (l *LifecycleService) commit(ctx context.Context, kind model.ServiceKind, version string, installed, running bool) error {
	if err := l.store.SetInstalled(string(kind), version, installed); err != nil {
		return err
	}
	if err := l.store.SetRunning(string(kind), version, running); err != nil {
		return err
	}
	l.emitter.Emit("service:changed", map[string]any{"kind": string(kind), "version": version, "running": running})
	fresh, err := l.store.BuildSnapshot()
	if err != nil {
		return err
	}
	l.emitter.Emit("state:changed", map[string]any{"snapshot": fresh})
	return nil
}

func refsOf(byKind map[string][]string) []engine.ContainerRef {
	var out []engine.ContainerRef
	for kind, versions := range byKind {
		for _, v := range versions {
			out = append(out, engine.ContainerRef{Kind: kind, Version: v})
		}
	}
	return out
}

// driftView / actualView 组织 docker:state-drift 的可读载荷
func driftView(installed, running map[string][]string) map[string]any {
	return map[string]any{"installed": installed, "running": running}
}

func actualView(actual []engine.ActualState) map[string]bool {
	out := make(map[string]bool, len(actual))
	for _, a := range actual {
		out[a.Ref.Name()] = a.Running
	}
	return out
}
