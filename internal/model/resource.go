// Docker 资源模型：孤儿扫描与清理的对象
package model

// 前四种带 phpo- 前缀命名空间（§5.13.5 孤儿扫描只报托管资源）；
// 后五种是「Docker 全量资源清理」面板会逐项删除的对象——插件与 Swarm 那几样不在 phpo- 命名空间里，
// 但删除结果同样要按类型点名，否则日志里只有一串 ID，用户看不出清掉的是什么（§5.16.3 失败点名的同一口径）。
type ResourceType string

const (
	ResContainer    ResourceType = "container"
	ResVolume       ResourceType = "volume"
	ResNetwork      ResourceType = "network"
	ResImage        ResourceType = "image"
	ResPlugin       ResourceType = "plugin"
	ResSwarmService ResourceType = "swarm_service"
	ResSwarmConfig  ResourceType = "swarm_config"
	ResSwarmSecret  ResourceType = "swarm_secret"
	ResBuildCache   ResourceType = "buildcache"
)

type DockerResource struct {
	Type  ResourceType `json:"type"`
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Size  int64        `json:"size,omitempty"`
	InUse bool         `json:"inUse"`
}

// docker:orphan-found 事件载荷
type OrphanFound struct {
	Resources []DockerResource `json:"resources"`
}

// OrphanReport 一次全量孤儿扫描的分类结果（容器/卷/网络/镜像四矩阵，§5.13.5）
type OrphanReport struct {
	Containers []DockerResource `json:"containers"`
	Volumes    []DockerResource `json:"volumes"`
	Networks   []DockerResource `json:"networks"`
	Images     []DockerResource `json:"images"`
}

// Total 四类孤儿总数
func (r OrphanReport) Total() int {
	return len(r.Containers) + len(r.Volumes) + len(r.Networks) + len(r.Images)
}

// All 平铺为单一列表（docker:orphan-found 事件载荷用）
func (r OrphanReport) All() []DockerResource {
	out := make([]DockerResource, 0, r.Total())
	out = append(out, r.Containers...)
	out = append(out, r.Volumes...)
	out = append(out, r.Networks...)
	out = append(out, r.Images...)
	return out
}

// 缺失态类别：库里记为已安装、但宿主上已经不在了的那一样东西
const (
	GapContainer = "container"        // 同名容器不存在（被 docker rm / 第三方工具删掉）
	GapImage     = "image"            // 该版本应运行的镜像不存在（被 docker rmi 删掉）
	GapExtImage  = "extensions_image" // php：库里记着已启用扩展，固化镜像 phpo/php:{version} 却不在本机
)

// ServiceGap 一条「已被外部删除」点名项。
// 只上报、不自动改 installed：外部删容器不等于用户要卸载，数据卷与重建入口都必须留着（§5.13.1 不破坏用户数据）。
// 界面据此把服务卡片标成缺失态，抽屉日志逐行点名（§5.19）。
type ServiceGap struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
	Ref     string `json:"ref"` // 缺席对象的具体名字：容器名 / 镜像引用
}

// docker:state-drift 事件载荷（期望态 ≡ 实际态 被破坏时发射）
type StateDrift struct {
	Expected any          `json:"expected"`
	Actual   any          `json:"actual"`
	Gaps     []ServiceGap `json:"gaps,omitempty"` // 全量同步点名的缺失项；无缺失即不发（前端不铺空行）
}
