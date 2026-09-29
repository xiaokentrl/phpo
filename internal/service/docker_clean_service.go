// Docker 全量资源清理：把「这台机器上 Docker 还占着什么」摊成 60 行清单（容器 / 镜像 / 卷 / 网络 /
// 构建缓存 / 日志 / 插件 / swarm / compose / 宿主目录…），再按「预览 → 一次彻底清空」两段落地。
// 三档口径：Docker 自己能删的给按钮；要管理员授权才读得到、删得掉的按行逐项确认；
// phpo 没有安全删法的只给一句说明、不给按钮。
// 有数据的东西先挪进回收站（7 天可恢复）再删；扫描与列表是纯读，写操作一律走三段式（硬红线 5）。
// 删除能力全部复用 engine 的既有实现（§5.13 清洁机制），本服务不另立第二套删除、不另立第二个回收站。
package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/pkg/dockerutil"
)

// DockerCleanEngine 清点与删除的唯一出口（*engine.Client 满足）。
// PlanHostScan / ComposeProjectLabel / SwarmStackLabel 是包级函数，不进这个接口。
type DockerCleanEngine interface {
	ScanDockerInventory(ctx context.Context, onStage engine.StageFn) engine.DockerInventory
	DockerInfo(ctx context.Context) engine.DaemonInfo
	ScanHost(ctx context.Context, info engine.DaemonInfo, onStage engine.StageFn) (engine.HostScanResult, error)
	RescanHostRow(ctx context.Context, key string, info engine.DaemonInfo) engine.HostRow
	HostPathFacts(paths []string) []engine.HostPathFact
	DeleteDockerObjects(ctx context.Context, ops []engine.DeleteOp, onItem engine.ItemFn) []engine.DeleteResult
	DeleteHostObjects(ctx context.Context, ops []engine.DeleteOp, info engine.DaemonInfo, tr *engine.Trash, onItem engine.ItemFn) []engine.DeleteResult
}

// DockerCleanStore 快照（判断哪些容器属于 phpo 已装版本）/ 回收站登记 / 审计落表（*store.Store 满足）
type DockerCleanStore interface {
	BuildSnapshot() (*model.Snapshot, error)
	AddTrashItem(item store.TrashItem) (int64, error)
	AppendOperation(model.Operation) error
}

// DockerCleanAuditor 审计写盘（*engine.Audit 满足）
type DockerCleanAuditor interface {
	Log(model.Operation) error
}

// DockerCleanUninstaller 卸载 phpo 某个已装版本（*AppService 满足）。
// 清理与卸载是两单：删除任务在跑的时候不能再嵌套提交一单（§0.2 规则 11）。
type DockerCleanUninstaller interface {
	Remove(ctx context.Context, kind model.ServiceKind, version string) error
}

// DockerCleanService 全量清理的门面
type DockerCleanService struct {
	eng    DockerCleanEngine
	store  DockerCleanStore
	audit  DockerCleanAuditor
	uninst DockerCleanUninstaller
	trash  *engine.Trash
	em     Emitter
	tasks  *task.Manager
	now    func() time.Time
	seq    atomic.Uint64

	mu      sync.Mutex
	token   string
	preview *model.CleanPreview
	last    *cleanCache // 上一次扫描的原始数据，供单行重扫与预览复用
}

// cleanCache 缓存本轮扫到的原始清点结果（深扫时含宿主行）
type cleanCache struct {
	info    engine.DaemonInfo
	inv     engine.DockerInventory
	host    engine.HostScanResult
	deep    bool
	scanned time.Time
}

func NewDockerCleanService(eng DockerCleanEngine, st DockerCleanStore, audit DockerCleanAuditor,
	uninst DockerCleanUninstaller, tr *engine.Trash, em Emitter, tm *task.Manager) *DockerCleanService {
	return &DockerCleanService{eng: eng, store: st, audit: audit, uninst: uninst, trash: tr,
		em: em, tasks: tm, now: time.Now}
}

// ---- 行归属与文案常量 ----

// cleanHostRowKeys 是「必须由宿主深扫才有数」的那 28 行。
// 引擎侧的同名常量是小写的、包外取不到，这里按字符串登记一份，两侧由对账用例锁死。
var cleanHostRowKeys = map[string]bool{
	"container.checkpoint": true, "container.meta": true, "container.cgroup": true,
	"volume.data": true, "volume.driver": true,
	"network.bridge": true, "network.veth": true, "network.netns": true,
	"network.cni": true, "network.iptables": true,
	"log.container": true, "log.daemon": true, "log.journald": true, "log.rotate": true, "log.build": true,
	"plugin.config": true, "plugin.data": true,
	"system.containerd": true, "system.tmp": true, "system.builder": true, "system.trust": true,
	"system.config": true, "system.user": true, "system.group": true, "system.root": true,
	"other.events": true, "other.cache": true,
	"image.sign": true,
}

// cleanAggregateRows 标记「这份占用已经算进别的行」的那一行，总计不重复相加。
// 引擎侧同表的常量不导出，此处只登记 system.root 一项（口径与其一致）。
var cleanAggregateRows = map[string]bool{"system.root": true}

// msgHostNotScanned 浅扫时宿主那 28 行的说明：不是「没有」，是「还没去读」。
const msgHostNotScanned = "还没读过宿主上的文件——点「详细扫描」再数一次"

// cleanSilentRows 是几个数不出独立占用、也不会单独删的东西：不给 0，只说清数在哪一行。
var cleanSilentRows = map[string]string{
	"build.buildkit": "buildkit 的占用就是宿主上那个构建器目录，数字在「构建器与运行时目录」那一行，这里不重复计",
	"build.buildx":   "buildx 只是下命令的入口，没有一份能单独数出来的东西；真正的占用在「构建缓存」那一行",
	"other.scout":    "Docker Scout 的分析结果不在本机 Docker 的数据目录里，这里没有可数的东西",
	"other.desktop":  "Docker Desktop 自己的程序与虚拟机磁盘不在宿主数据目录里，要清得从 Desktop 自己的入口，这里够不着",
}

// ---- 通用助手 ----

// newID 给任务与一次性凭据起一个本轮内唯一的号
func (s *DockerCleanService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}

// installedNames 返回 phpo 已装版本对应的容器名集合；快照取不到时返回空集（宁可把同名容器当成外来，也不误删）
func (s *DockerCleanService) installedNames() map[string]bool {
	set := map[string]bool{}
	snap, err := s.store.BuildSnapshot()
	if err != nil || snap == nil {
		return set
	}
	for kind, vers := range snap.Installed {
		for _, v := range vers {
			set[engine.ContainerRef{Kind: kind, Version: v}.Name()] = true
		}
	}
	return set
}

// auditOp 落一行审计（JSON Lines）并同步 operations 表；两处写失败都不改变清理结果
func (s *DockerCleanService) auditOp(op string, args any, status string) {
	rec := model.Operation{TS: s.now().UTC(), Actor: "ui", Op: op, Args: args, Status: status}
	_ = s.audit.Log(rec)
	_ = s.store.AppendOperation(rec)
}

// emitState 把后端权威快照推回界面（硬红线 4：前端只订阅，不自改）
func (s *DockerCleanService) emitState() {
	snap, err := s.store.BuildSnapshot()
	if err != nil || snap == nil {
		return
	}
	s.em.Emit("state:changed", map[string]any{"snapshot": snap})
}

// emitStage 把「正在数哪一行」实时推给界面（⑳㉔：复用既有 docker:cleanup，step/total 为可选载荷，事件名一个不增）
func (s *DockerCleanService) emitStage(stage string, step, total int) {
	s.em.Emit("docker:cleanup", map[string]any{"stage": stage, "step": step, "total": total})
}

// cleanProgress 是一个顺序计数器：每次报一段就 +1，用于上面的 step
func (s *DockerCleanService) cleanProgress(total int) engine.StageFn {
	n := 0
	return func(stage string) {
		n++
		if n > total {
			n = total
		}
		s.emitStage(stage, n, total)
	}
}

// rowIsPhpo 判断一个资源名是否属于 phpo 命名空间（§5.13.2）。
// 判据只认 dockerutil 那一处定义：前缀写第二遍迟早和创建资源的那边漂开。
func rowIsPhpo(name string) bool { return dockerutil.IsPhpoResource(name) }

// sortedKeys 稳定输出，避免同一份数据两次扫描给出不同行序
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
