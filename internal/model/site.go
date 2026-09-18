// 站点领域模型：域名零限制、端口 1–65535、根目录允许 WWW_ROOT 外（警告）
package model

type Site struct {
	Domain  string `json:"domain"`
	Port    int    `json:"port"`
	PHP     string `json:"php"`
	Root    string `json:"root"`
	Rewrite string `json:"rewrite"`

	// 伪静态自定义规则原文（preset==custom 时生效）；vhostCustomized 标记 vhost 被手改过
	// 二者为 vhost 计算/运行态字段，落库在 T403 补 rewrite_rule 列（当前 store 用显式列扫描，不受影响）
	RewriteRule     string `json:"rewriteRule,omitempty"`
	VhostCustomized bool   `json:"vhostCustomized,omitempty"`
}

// URL 展示用访问地址
func (s Site) URL() string {
	scheme := "http"
	if s.Port == 443 {
		scheme = "https"
	}
	return scheme + "://" + s.Domain
}
