// 跨层 DTO：preflight 三段式返回结构与任务元数据
package model

// PreflightResult 统一校验裁决（原型 preflight() 返回 {ok,errors,warnings,adjusted}）
type PreflightResult struct {
	Ok       bool           `json:"ok"`
	Errors   []string       `json:"errors"`
	Warnings []string       `json:"warnings"`
	Adjusted map[string]any `json:"adjusted,omitempty"` // 如 {"port":81}（端口顺延）
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
