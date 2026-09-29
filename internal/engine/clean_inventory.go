// Docker 全量资源清理 · 采集层：把这台机器上 Docker 的每一份资源数清楚，供总览页那 60 行清单显示真实数字。
//
// 为什么另起一个采集器而不复用 ScanOrphans：孤儿扫描按隔离性只认 phpo- 命名空间内的资源（§5.13.3），
// 而本面板要看的是全量——包括用户自己用 docker CLI / compose 建的东西。两类对象集不同，合并进来等于把隔离性放开。
//
// 这里只数不删。删哪一些是用户在界面上勾出来的决定，由服务层执行；本文件只回答「有什么、多大、有没有在用」。
//
// 取数失败按类别逐笔记账，绝不当成 0：一次 Docker 调用挂了，不能把其余九项的数字也抹成「没有」——
// 那是把「不知道」说成「没有」，界面上等于撒谎（拿不到就显示「无权读取 / 取不到」，见 model.RowUnavailable）。
package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// 采集类别名：失败台账的键。服务层按类别决定「哪几行取不到」，本层不去枚举界面那 60 行（§0.2 规则 12）。
const (
	CatContainers    = "containers"
	CatImages        = "images"
	CatVolumes       = "volumes"
	CatNetworks      = "networks"
	CatPlugins       = "plugins"
	CatSwarmServices = "swarm-services"
	CatSwarmConfigs  = "swarm-configs"
	CatSwarmSecrets  = "swarm-secrets"
	CatSwarmNodes    = "swarm-nodes"
	CatBuildCache    = "buildcache"
	CatLayers        = "layers" // 层重叠体积只来自 DiskUsage，与镜像清单同一端点但单独记一笔
)

// 进度阶段文案（人话）。由服务层经既有 docker:cleanup 事件逐行广播，不新增事件名。
const (
	StageContainers    = "正在统计：容器"
	StageImages        = "正在统计：镜像"
	StageDiskUsage     = "正在统计：磁盘用量与构建缓存（镜像层重叠要跨镜像算，这一步最慢）"
	StageVolumes       = "正在统计：数据卷"
	StageNetworks      = "正在统计：网络"
	StagePlugins       = "正在统计：插件"
	StageSwarmServices = "正在统计：Swarm 服务"
	StageSwarmConfigs  = "正在统计：Swarm 配置"
	StageSwarmSecrets  = "正在统计：Swarm 密文"
	StageSwarmNodes    = "正在统计：Swarm 节点"
)

// DockerStagesTotal 是上面那批进度文案的段数，界面上的进度条拿它当分母（已报到几段 / 共几段）。
// 构建缓存没有单独一段——它和镜像层重叠体积一起走 StageDiskUsage，那一步最慢也最该占一格。
const DockerStagesTotal = 10

// compose / stack 归属标签。这两个名字不在 Docker SDK 的常量里，是 docker CLI 自己写的；
// 认不出来只会让那一组清单为空，不会编出数字来。
const (
	composeProjectLabel = "com.docker.compose.project"
	swarmStackLabel     = "com.docker.stack.namespace"
)

// ComposeProjectLabel / SwarmStackLabel 给服务层按标签归组用（本层不外泄标签字符串常量）。
func ComposeProjectLabel() string { return composeProjectLabel }
func SwarmStackLabel() string     { return swarmStackLabel }

// StageFn 是采集过程的进度回调。扫描期间界面靠它画进度条，没有回调（比如单测）也必须照常跑完。
type StageFn func(stage string)

func (f StageFn) report(stage string) {
	if f != nil {
		f(stage)
	}
}

// InvContainer 一个容器的采集结果。Names 里 Docker 给的是 "/phpo-php-8.4"，这里已去掉开头的斜杠。
type InvContainer struct {
	ID           string
	Name         string
	Image        string // 镜像引用文本（可能已被重新 tag 或删掉）
	ImageID      string // 真正在用的那份镜像，比对时以它为准
	State        string
	Status       string
	Labels       map[string]string
	Running      bool
	Paused       bool
	SizeRw       int64 // 可写层大小（ListOptions.Size=true 才有）
	SizeRootFs   int64
	NamedVolumes []string // 挂进来的数据卷名
	Binds        []string // bind 挂载的宿主路径
	MountCount   int
}

// InvImage 一个镜像。SharedSize / Containers 在 SDK 里取不到时是 -1，故各带一个 Has* 判据。
type InvImage struct {
	ID            string
	Refs          []string // RepoTags；为空即无标签（悬空）
	Digests       []string
	Size          int64
	SharedSize    int64
	HasShared     bool
	Containers    int64
	HasContainers bool
	Labels        map[string]string
	Created       int64
	ParentID      string
	ManifestCount int // 多架构清单条数
}

// InvVolume 一个数据卷。VolumeList 只发 filters 参数，UsageData 本就可能缺席，故 HasSize / HasRef 分开判。
type InvVolume struct {
	Name       string
	Driver     string
	Scope      string
	Mountpoint string
	Labels     map[string]string
	Size       int64
	HasSize    bool
	RefCount   int
	HasRef     bool
}

// InvNetwork 一个网络。Attached 即挂着它的容器数，0 才可能算未使用。
type InvNetwork struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Internal   bool
	Attachable bool
	Ingress    bool
	ConfigOnly bool
	Attached   int
	Options    map[string]string
	Labels     map[string]string
}

// InvBuildCache 一条构建缓存记录。
type InvBuildCache struct {
	ID          string
	Type        string
	Description string
	InUse       bool
	Shared      bool
	Size        int64
}

// InvPlugin 一个 Docker 插件。
type InvPlugin struct {
	ID        string
	Name      string
	Reference string
	Enabled   bool
}

// InvNamed 服务 / 配置 / 密文这三种「有名字有标签」的对象共用一份形状。
type InvNamed struct {
	ID     string
	Name   string
	Labels map[string]string
}

// InvNode 一个 Swarm 节点。
type InvNode struct {
	ID           string
	Name         string
	Role         string
	Availability string
}

// InvGroup 按某个归属标签聚合出的一组资源（compose 项目 / swarm stack 共用）。
type InvGroup struct {
	Name       string
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	Services   []string
	Bytes      int64
}

// DockerInventory 是全量采集的结果。
//
// 两份数据来源是刻意重叠的：DiskUsage 给层重叠体积与构建缓存（没有独立的构建缓存列举接口），
// 专用清单接口给各类别的逐项明细。任一头失败只记自己那一笔，不把另一头的数字一起清空。
type DockerInventory struct {
	Containers []InvContainer
	Images     []InvImage
	Volumes    []InvVolume
	Networks   []InvNetwork
	BuildCache []InvBuildCache
	Plugins    []InvPlugin
	Services   []InvNamed
	Configs    []InvNamed
	Secrets    []InvNamed
	Nodes      []InvNode
	LayersSize int64 // 镜像层重叠后的实际占用（DiskUsage 独有，比逐个镜像相加小）
	LayersOK   bool
	Failures   map[string]string // 类别 → 一句人话原因
	ScannedAt  time.Time
}

func (inv *DockerInventory) fail(cat string, err error) {
	if inv.Failures == nil {
		inv.Failures = map[string]string{}
	}
	inv.Failures[cat] = err.Error()
}

// Failed 说这一类东西到底数没数到。读一份未初始化台账的 nil map 是安全的。
func (inv DockerInventory) Failed(cat string) bool { return inv.Failures[cat] != "" }

// Reason 给出该类别取数失败的原因，供界面那一行「取不到」说清为什么。
func (inv DockerInventory) Reason(cat string) string { return inv.Failures[cat] }

// ScanDockerInventory 不带任何命名空间过滤地问一遍 Docker，把能数到的都数回来。
//
// 取消优先：每次调用前先问一次 ctx，收到取消就停下并把后续类别记为失败——
// 半份结果加一段说明，比假装扫完了诚实。
func (c *Client) ScanDockerInventory(ctx context.Context, onStage StageFn) DockerInventory {
	inv := DockerInventory{ScannedAt: time.Now()}

	onStage.report(StageContainers)
	if err := ctx.Err(); err != nil {
		inv.fail(CatContainers, err)
	} else {
		list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Size: true})
		if err != nil {
			inv.fail(CatContainers, fmt.Errorf("统计容器失败: %w", err))
		} else {
			for _, cs := range list {
				rec := InvContainer{
					ID: cs.ID, Name: firstContainerName(cs.Names), Image: cs.Image, ImageID: cs.ImageID,
					State: string(cs.State), Status: cs.Status, Labels: cs.Labels,
					Running: cs.State == container.StateRunning, Paused: cs.State == container.StatePaused,
					SizeRw: cs.SizeRw, SizeRootFs: cs.SizeRootFs, MountCount: len(cs.Mounts),
				}
				for _, m := range cs.Mounts {
					switch m.Type {
					case mount.TypeVolume:
						if m.Name != "" {
							rec.NamedVolumes = append(rec.NamedVolumes, m.Name)
						}
					case mount.TypeBind:
						if m.Source != "" {
							rec.Binds = append(rec.Binds, m.Source)
						}
					}
				}
				inv.Containers = append(inv.Containers, rec)
			}
			sort.Slice(inv.Containers, func(i, j int) bool { return inv.Containers[i].Name < inv.Containers[j].Name })
		}
	}

	// DiskUsage 排在专用清单之前：它一次给出层重叠体积与构建缓存，是这两项的唯一来源。
	onStage.report(StageDiskUsage)
	if err := ctx.Err(); err != nil {
		inv.fail(CatBuildCache, err)
		inv.fail(CatLayers, err)
	} else {
		du, err := c.cli.DiskUsage(ctx, types.DiskUsageOptions{Types: []types.DiskUsageObject{
			types.ContainerObject, types.ImageObject, types.VolumeObject, types.BuildCacheObject,
		}})
		if err != nil {
			inv.fail(CatLayers, fmt.Errorf("统计镜像层占用失败: %w", err))
			inv.fail(CatBuildCache, fmt.Errorf("统计构建缓存失败: %w", err))
		} else {
			inv.LayersSize, inv.LayersOK = du.LayersSize, true
			for _, bc := range du.BuildCache {
				if bc == nil {
					continue
				}
				inv.BuildCache = append(inv.BuildCache, InvBuildCache{
					ID: bc.ID, Type: string(bc.Type), Description: bc.Description,
					InUse: bc.InUse, Shared: bc.Shared, Size: bc.Size,
				})
			}
			sort.Slice(inv.BuildCache, func(i, j int) bool { return inv.BuildCache[i].ID < inv.BuildCache[j].ID })
		}
	}

	onStage.report(StageImages)
	if err := ctx.Err(); err != nil {
		inv.fail(CatImages, err)
	} else {
		// SharedSize 不开这个开关，SDK 回来的是 -1（「没算」而不是「0」）。
		list, err := c.cli.ImageList(ctx, image.ListOptions{All: true, SharedSize: true})
		if err != nil {
			inv.fail(CatImages, fmt.Errorf("统计镜像失败: %w", err))
		} else {
			for _, im := range list {
				rec := InvImage{
					ID: im.ID, Refs: im.RepoTags, Digests: im.RepoDigests, Labels: im.Labels,
					Size: im.Size, Created: im.Created, ParentID: im.ParentID,
					ManifestCount: len(im.Manifests),
				}
				if im.SharedSize >= 0 {
					rec.SharedSize, rec.HasShared = im.SharedSize, true
				}
				if im.Containers >= 0 {
					rec.Containers, rec.HasContainers = im.Containers, true
				}
				inv.Images = append(inv.Images, rec)
			}
			sort.Slice(inv.Images, func(i, j int) bool { return imageSortKey(inv.Images[i]) < imageSortKey(inv.Images[j]) })
		}
	}

	onStage.report(StageVolumes)
	if err := ctx.Err(); err != nil {
		inv.fail(CatVolumes, err)
	} else {
		resp, err := c.cli.VolumeList(ctx, volume.ListOptions{})
		if err != nil {
			inv.fail(CatVolumes, fmt.Errorf("统计数据卷失败: %w", err))
		} else {
			for _, v := range resp.Volumes {
				if v == nil {
					continue
				}
				rec := InvVolume{Name: v.Name, Driver: v.Driver, Scope: v.Scope, Mountpoint: v.Mountpoint, Labels: v.Labels}
				if v.UsageData != nil {
					if v.UsageData.Size >= 0 {
						rec.Size, rec.HasSize = v.UsageData.Size, true
					}
					if v.UsageData.RefCount >= 0 {
						rec.RefCount, rec.HasRef = int(v.UsageData.RefCount), true
					}
				}
				inv.Volumes = append(inv.Volumes, rec)
			}
			sort.Slice(inv.Volumes, func(i, j int) bool { return inv.Volumes[i].Name < inv.Volumes[j].Name })
		}
	}

	onStage.report(StageNetworks)
	if err := ctx.Err(); err != nil {
		inv.fail(CatNetworks, err)
	} else {
		list, err := c.cli.NetworkList(ctx, network.ListOptions{})
		if err != nil {
			inv.fail(CatNetworks, fmt.Errorf("统计网络失败: %w", err))
		} else {
			for _, n := range list {
				inv.Networks = append(inv.Networks, InvNetwork{
					ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope,
					Internal: n.Internal, Attachable: n.Attachable, Ingress: n.Ingress, ConfigOnly: n.ConfigOnly,
					Attached: len(n.Containers), Options: n.Options, Labels: n.Labels,
				})
			}
			sort.Slice(inv.Networks, func(i, j int) bool { return inv.Networks[i].Name < inv.Networks[j].Name })
		}
	}

	onStage.report(StagePlugins)
	if err := ctx.Err(); err != nil {
		inv.fail(CatPlugins, err)
	} else {
		list, err := c.cli.PluginList(ctx, filters.NewArgs())
		if err != nil {
			inv.fail(CatPlugins, fmt.Errorf("统计插件失败: %w", err))
		} else {
			for _, p := range list {
				if p == nil {
					continue
				}
				inv.Plugins = append(inv.Plugins, InvPlugin{
					ID: p.ID, Name: p.Name, Reference: p.PluginReference, Enabled: p.Enabled,
				})
			}
			sort.Slice(inv.Plugins, func(i, j int) bool { return inv.Plugins[i].Name < inv.Plugins[j].Name })
		}
	}

	// Swarm 四项在没 init 的机器上会直接报错（「This node is not a swarm manager」）。
	// 这不是故障，所以只记账、不判死；界面上那几行照常显示 0 份，因为「这台机器没在用 Swarm」是真话，
	// 而「取不到」是另一回事——区分两者的活留给服务层（它看得见错误原文）。
	inv.Services = collectSwarm(ctx, &inv, onStage, CatSwarmServices, StageSwarmServices, "Swarm 服务",
		func() ([]swarm.Service, error) { return c.cli.ServiceList(ctx, swarm.ServiceListOptions{}) },
		func(s swarm.Service) InvNamed { return named(s.ID, s.Spec.Name, s.Spec.Labels) },
		func(v InvNamed) string { return v.Name })

	inv.Configs = collectSwarm(ctx, &inv, onStage, CatSwarmConfigs, StageSwarmConfigs, "Swarm 配置",
		func() ([]swarm.Config, error) { return c.cli.ConfigList(ctx, swarm.ConfigListOptions{}) },
		func(s swarm.Config) InvNamed { return named(s.ID, s.Spec.Name, s.Spec.Labels) },
		func(v InvNamed) string { return v.Name })

	inv.Secrets = collectSwarm(ctx, &inv, onStage, CatSwarmSecrets, StageSwarmSecrets, "Swarm 密文",
		func() ([]swarm.Secret, error) { return c.cli.SecretList(ctx, swarm.SecretListOptions{}) },
		func(s swarm.Secret) InvNamed { return named(s.ID, s.Spec.Name, s.Spec.Labels) },
		func(v InvNamed) string { return v.Name })

	// 节点比其余三类多了角色与可用状态，故这一路单独用自己的形状，不把它们塞进标签里冒充标签。
	inv.Nodes = collectSwarm(ctx, &inv, onStage, CatSwarmNodes, StageSwarmNodes, "Swarm 节点",
		func() ([]swarm.Node, error) { return c.cli.NodeList(ctx, swarm.NodeListOptions{}) },
		func(n swarm.Node) InvNode {
			return InvNode{ID: n.ID, Name: n.Spec.Name, Role: string(n.Spec.Role), Availability: string(n.Spec.Availability)}
		},
		func(v InvNode) string { return v.Name })
	return inv
}

// collectSwarm 拉一类 Swarm 对象。四类同一条路：取消先判、失败只记这一类、成功就归一成 U 并按名字排序。
//
// 名字为空的项目**不丢**：swarm 的 config / secret 允许不命名，丢掉等于把清单数少。
// 排序用 Stable——同名（含都叫空）的几项每次扫完顺序一致，界面不会因重扫就跳动。
func collectSwarm[T any, U any](ctx context.Context, inv *DockerInventory, onStage StageFn,
	cat, stage, label string,
	list func() ([]T, error),
	mapping func(T) U,
	nameOf func(U) string,
) []U {
	onStage.report(stage)
	if err := ctx.Err(); err != nil {
		inv.fail(cat, err)
		return nil
	}
	items, err := list()
	if err != nil {
		inv.fail(cat, fmt.Errorf("统计 %s 失败: %w", label, err))
		return nil
	}
	out := make([]U, 0, len(items))
	for _, it := range items {
		out = append(out, mapping(it))
	}
	sort.SliceStable(out, func(i, j int) bool { return nameOf(out[i]) < nameOf(out[j]) })
	return out
}

func named(id, name string, labels map[string]string) InvNamed {
	return InvNamed{ID: id, Name: name, Labels: labels}
}

// firstContainerName 取容器的显示名。SDK 给的是 ["/phpo-php-8.4"]，去掉开头的斜杠才是用户认识的名字。
func firstContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

// imageSortKey 有标签的按第一个标签排，无标签的排在同 ID 段——保证同一份清单每次都稳定输出。
func imageSortKey(im InvImage) string {
	if len(im.Refs) > 0 {
		return im.Refs[0]
	}
	return "~" + im.ID
}

// DanglingImages 返回没有任何标签的镜像，语义与 `docker image ls -f dangling=true` 一致。
func (inv DockerInventory) DanglingImages() []InvImage {
	if inv.Failed(CatImages) {
		return nil
	}
	var out []InvImage
	for _, im := range inv.Images {
		if len(im.Refs) == 0 {
			out = append(out, im)
		}
	}
	return out
}

// usedImageIDs 算出「有容器在用的镜像」集合，并沿父镜像链往上把中间层一起保住。
//
// 为什么要走父链：`docker image ls -a` 里那些中间层没有标签，只看「有没有容器引用」会漏判——
// 但删掉一个还在用的镜像的父层会让那份镜像失去完整性，故一律视为在用。
func (inv DockerInventory) usedImageIDs() map[string]bool {
	used := map[string]bool{}
	byRef := map[string][]string{}
	for _, im := range inv.Images {
		for _, r := range im.Refs {
			byRef[r] = append(byRef[r], im.ID)
		}
		for _, d := range im.Digests {
			byRef[d] = append(byRef[d], im.ID)
		}
	}
	for _, c := range inv.Containers {
		if c.ImageID != "" {
			used[c.ImageID] = true
		}
		for _, id := range byRef[c.Image] {
			used[id] = true
		}
	}
	// 父链可能成环（异常数据），故用「反复一轮没有新增就停」而不是递归。
	for grew := true; grew; {
		grew = false
		for _, im := range inv.Images {
			if !used[im.ID] || im.ParentID == "" || im.ParentID == "<unknown>" {
				continue
			}
			if !used[im.ParentID] {
				used[im.ParentID] = true
				grew = true
			}
		}
	}
	return used
}

// UnusedImages 返回有标签但没有任何容器在用的镜像（`docker image ls -f dangling=false` 里未使用的那些）。
//
// 容器那一项没数到时一律返回空：拿不到「谁在用」就断言「没人用」，会把在跑的镜像删掉。
func (inv DockerInventory) UnusedImages() []InvImage {
	if inv.Failed(CatImages) || inv.Failed(CatContainers) {
		return nil
	}
	used := inv.usedImageIDs()
	var out []InvImage
	for _, im := range inv.Images {
		if len(im.Refs) == 0 || used[im.ID] {
			continue
		}
		out = append(out, im)
	}
	return out
}

// UnusedVolumes 返回没人挂载且引用计数为 0 的数据卷，语义对齐 `docker volume ls -f dangling=true`。
//
// 两处刻意保守：① 引用计数取不到（VolumeList 不保证带 UsageData）就不判未使用；
// ② 容器清单一度失败时，只剩「卷自己说没人用」这一条证据，此时仍只认 HasRef 为真的那些。
// 卷有数据，界面走回收站而不是直接删（④）。
func (inv DockerInventory) UnusedVolumes() []InvVolume {
	if inv.Failed(CatVolumes) {
		return nil
	}
	namesUsed := map[string]bool{}
	if !inv.Failed(CatContainers) {
		for _, c := range inv.Containers {
			for _, v := range c.NamedVolumes {
				namesUsed[v] = true
			}
		}
	}
	var out []InvVolume
	for _, v := range inv.Volumes {
		if namesUsed[v.Name] || !v.HasRef || v.RefCount > 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

// UnusedNetworks 返回没有任何容器连接、且不是 Docker 自带三张网（bridge/host/none）与 ingress 的网络。
func (inv DockerInventory) UnusedNetworks() []InvNetwork {
	if inv.Failed(CatNetworks) {
		return nil
	}
	var out []InvNetwork
	for _, n := range inv.Networks {
		if n.Attached > 0 || n.Ingress {
			continue
		}
		switch n.Name {
		case "bridge", "host", "none":
			continue
		}
		out = append(out, n)
	}
	return out
}

// Groups 按某个归属标签（compose 项目 / swarm stack）把资源归组，供界面那一行「按项目整删」列清单。
//
// 认不出标签就返回空列表——界面上写「未发现该项目」，而不是给一个算不出来的数字。
func (inv DockerInventory) Groups(label string) []InvGroup {
	groups := map[string]*InvGroup{}
	take := func(name string) *InvGroup {
		if g := groups[name]; g != nil {
			return g
		}
		g := &InvGroup{Name: name}
		groups[name] = g
		return g
	}
	if !inv.Failed(CatContainers) {
		for _, c := range inv.Containers {
			if name := c.Labels[label]; name != "" {
				g := take(name)
				g.Containers = append(g.Containers, c.Name)
				g.Bytes += c.SizeRw
			}
		}
	}
	if !inv.Failed(CatImages) {
		for _, im := range inv.Images {
			if name := im.Labels[label]; name != "" {
				g := take(name)
				ref := im.ID
				if len(im.Refs) > 0 {
					ref = im.Refs[0]
				}
				g.Images = append(g.Images, ref)
				g.Bytes += im.Size
			}
		}
	}
	if !inv.Failed(CatVolumes) {
		for _, v := range inv.Volumes {
			if name := v.Labels[label]; name != "" {
				g := take(name)
				g.Volumes = append(g.Volumes, v.Name)
				if v.HasSize {
					g.Bytes += v.Size
				}
			}
		}
	}
	if !inv.Failed(CatNetworks) {
		for _, n := range inv.Networks {
			if name := n.Labels[label]; name != "" {
				g := take(name)
				g.Networks = append(g.Networks, n.Name)
			}
		}
	}
	if !inv.Failed(CatSwarmServices) {
		for _, s := range inv.Services {
			if name := s.Labels[label]; name != "" {
				g := take(name)
				g.Services = append(g.Services, s.Name)
			}
		}
	}
	out := make([]InvGroup, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.Containers)
		sort.Strings(g.Images)
		sort.Strings(g.Volumes)
		sort.Strings(g.Networks)
		sort.Strings(g.Services)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
