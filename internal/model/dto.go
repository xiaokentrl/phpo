// 跨层 DTO：preflight 三段式返回结构与任务元数据
package model

// PreflightResult 统一校验裁决（原型 preflight() 返回 {ok,errors,warnings,adjusted}）
type PreflightResult struct {
	Ok       bool           `json:"ok"`
	Errors   []string       `json:"errors"`
	Warnings []string       `json:"warnings"`
	Adjusted map[string]any `json:"adjusted,omitempty"` // 如 {"port":81}（端口顺延）
}

// HomeVerifyResult 装机向导只读预检输出（T607）：逐条预检行 + 错误行；不创建任何目录/文件，OK=false 时前端不进入确认步
type HomeVerifyResult struct {
	OK     bool     `json:"ok"`
	Lines  []string `json:"lines"`
	Errors []string `json:"errors"`
}

// DockerStatus 首启/轮询的 Docker 可用性探测结果（只读绑定，非可持久 Snapshot 字段）
// status: ok / not_installed / not_running / old_version；CanStart=false 时不得启动任何容器（硬红线 7）
type DockerStatus struct {
	Status   string `json:"status"`
	Version  string `json:"version,omitempty"`
	CanStart bool   `json:"canStart"`
	Warning  bool   `json:"warning"`
	Message  string `json:"message,omitempty"`
	Hint     string `json:"hint,omitempty"`
}

// InstallOptions 安装期配置：端口与密码只在「建容器那一刻」被读走，而未安装态过不了
// update-config 守卫（§5.8 服务端口占用报错）——故由 install 携带并先落 config.yaml，
// 对应原型 applyStateChange 的 type==='install' 分支（meta.port / meta.password）。
type InstallOptions struct {
	Port        int    `json:"port,omitempty"`
	Password    string `json:"password,omitempty"`
	HasPassword bool   `json:"hasPassword,omitempty"` // 空密码合法（§1.5），须区分「未提供」与「提供空串」
}

// TaskMeta runTask 的业务上下文（applyStateChange 按 Type 落地）
type TaskMeta struct {
	Type    string   `json:"type"`
	Kind    string   `json:"kind,omitempty"`
	Version string   `json:"version,omitempty"`
	Files   []string `json:"files,omitempty"`
	// 站点类
	Domain  string `json:"domain,omitempty"`
	Port    int    `json:"port,omitempty"`
	PHP     string `json:"php,omitempty"`
	Rewrite string `json:"rewrite,omitempty"`
	Root    string `json:"root,omitempty"`
	// 数据服务类（明文密码，可为空）
	Password string `json:"password,omitempty"`
	// 扩展类
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}
