// 状态快照：后端唯一权威的全量视图，state:changed 事件载荷与 store/snapshot 序列化格式
package model

// Snapshot 字段与原型全局 state 的可持久部分对齐
type Snapshot struct {
	Installed     map[string][]string `json:"installed"` // kind → 已安装版本列表
	Running       map[string][]string `json:"running"`   // kind → 运行中版本列表
	Sites         []Site              `json:"sites"`
	Env           map[string]string   `json:"env"`           // 派生路径 + 服务密码/端口等
	PHPExtensions map[string][]string `json:"phpExtensions"` // php version → 启用的扩展
	DirReady      map[string]bool     `json:"dirReady"`      // PHPO_HOME / WWW_ROOT 初始化标记
	Tasks         TaskBoard           `json:"tasks"`         // 任务队列详情（运行中 + 排队中，实时推送）
}

func NewSnapshot() *Snapshot {
	return &Snapshot{
		Installed:     map[string][]string{},
		Running:       map[string][]string{},
		Sites:         []Site{},
		Env:           map[string]string{},
		PHPExtensions: map[string][]string{},
		DirReady:      map[string]bool{},
		Tasks:         TaskBoard{Pending: []TaskBrief{}},
	}
}

// InstalledVersionKinds 判定 kind+version 是否已安装（preflight notInstalled 依据）
func (s *Snapshot) HasVersion(kind, version string) bool {
	for _, v := range s.Installed[kind] {
		if v == version {
			return true
		}
	}
	return false
}
