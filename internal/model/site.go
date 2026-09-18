// 站点领域模型：域名零限制、端口 1–65535、根目录允许 WWW_ROOT 外（警告）
package model

type Site struct {
	Domain  string `json:"domain"`
	Port    int    `json:"port"`
	PHP     string `json:"php"`
	Root    string `json:"root"`
	Rewrite string `json:"rewrite"`
}

// URL 展示用访问地址
func (s Site) URL() string {
	scheme := "http"
	if s.Port == 443 {
		scheme = "https"
	}
	return scheme + "://" + s.Domain
}
