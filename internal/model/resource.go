// Docker 资源模型：孤儿扫描与清理的对象（全部带 phpo- 前缀命名空间）
package model

type ResourceType string

const (
	ResContainer ResourceType = "container"
	ResVolume    ResourceType = "volume"
	ResNetwork   ResourceType = "network"
	ResImage     ResourceType = "image"
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

// docker:state-drift 事件载荷（期望态 ≡ 实际态 被破坏时发射）
type StateDrift struct {
	Expected any `json:"expected"`
	Actual   any `json:"actual"`
}
