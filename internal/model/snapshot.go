// 状态快照：后端唯一权威的全量视图，state:changed 事件载荷与 store/snapshot 序列化格式
package model

// EngineInfo 容器引擎检测结果（v2.9.16 新增，§5.25）：快照唯一来源是装配层启动时的一次拨号识别
// （判据 = /version 的 Components[0].Name，实测「Podman Engine」自报；Platform.Name 是宿主系统不可用）。
// Kind 空串 = 尚未识别（拨号失败/未装配）；派生态不落库。
type EngineInfo struct {
	Kind     string `json:"kind"`     // docker / podman；空串 = 尚未识别
	Version  string `json:"version"`  // 引擎自报版本（识别失败时为空）
	Endpoint string `json:"endpoint"` // 实际使用的端点（socket 路径）
	Rootless bool   `json:"rootless"` // rootless 引擎（socket 在 /run/user/<uid> 下）：无法绑定 <1024 特权端口（§5.25 P2b 真机取证）
}

// Snapshot 字段与原型全局 state 的可持久部分对齐
type Snapshot struct {
	Installed     map[string][]string `json:"installed"` // kind → 已安装版本列表
	Running       map[string][]string `json:"running"`   // kind → 运行中版本列表
	Sites         []Site              `json:"sites"`
	Env           map[string]string   `json:"env"`           // 派生路径 + 服务密码/端口等
	PHPExtensions map[string][]string `json:"phpExtensions"` // php version → 启用的扩展
	DirReady      map[string]bool     `json:"dirReady"`      // PHPO_HOME / WWW_ROOT 初始化标记
	Tasks         TaskBoard           `json:"tasks"`         // 任务队列详情（运行中 + 排队中，实时推送）
	Gaps          []ServiceGap        `json:"gaps"`          // 已被外部删除的容器/镜像点名项（§5.19；不落库，由同步状态现取）
	Discovered    []DiscoveredService `json:"discovered"`    // Docker 上此刻实际存在的服务容器（含已停止的；不落库，由同步状态现取）
	Engine        *EngineInfo         `json:"engine"`        // 容器引擎检测结果（v2.9.16；不落库，由装配层启动探测派生）
	Flatpak       bool                `json:"flatpak"`       // 本进程跑在 Flatpak 沙箱里（FLATPAK_ID 或 /.flatpak-info 判定）：引导用户放行宿主 socket
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
		Gaps:          []ServiceGap{},
		Discovered:    []DiscoveredService{},
		Engine:        &EngineInfo{},
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
