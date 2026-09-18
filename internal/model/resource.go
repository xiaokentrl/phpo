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

// docker:state-drift 事件载荷（期望态 ≡ 实际态 被破坏时发射）
type StateDrift struct {
	Expected any `json:"expected"`
	Actual   any `json:"actual"`
}
