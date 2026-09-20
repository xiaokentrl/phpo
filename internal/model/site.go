// 站点领域模型：域名零限制、端口 1–65535、根目录允许 WWW_ROOT 外（警告）
package model

type Site struct {
	Domain  string `json:"domain"`
	Port    int    `json:"port"`
	PHP     string `json:"php"`
	Root    string `json:"root"`
	Rewrite string `json:"rewrite"`

	// 展示用运行态：每次快照按宿主真值派生，不落库（列表的 Hosts 列与健康列唯一来源）
	Hosts  bool   `json:"hosts"`  // 系统 hosts 已把本域名解析到 127.0.0.1
	Health string `json:"health"` // up 正常 / warn 降级 / down 未响应，见 Health*

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

// 站点健康度三态（与前端 types.Health、locales sites.health.* 逐字对齐）
const (
	HealthUp   = "up"   // 正常：已对外服务
	HealthWarn = "warn" // 降级：暂不对外服务（vhost 未落盘 / nginx 未运行），站点照常存在
	HealthDown = "down" // 未响应：配置在位但上游或根目录缺失，访问必失败
)

// SiteRuntime 判定健康度所需的宿主侧真值
type SiteRuntime struct {
	NginxRunning bool // nginx 有运行中的版本
	VHostOnDisk  bool // 站点 conf 已落盘
	PHPRunning   bool // 站点所选 PHP 版本正在运行
	RootExists   bool // 站点根目录存在
}

// Health 派生展示健康度：nginx 未运行或 vhost 未落盘即降级（§5.8 降级态——暂不对外服务，非故障）；
// 二者就绪但上游 PHP 未运行或站点目录已不在，即为未响应（nginx 会回 502/404）。
func (r SiteRuntime) Health() string {
	if !r.NginxRunning || !r.VHostOnDisk {
		return HealthWarn
	}
	if !r.PHPRunning || !r.RootExists {
		return HealthDown
	}
	return HealthUp
}
