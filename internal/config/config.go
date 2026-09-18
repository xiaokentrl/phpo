// 应用配置入口：PHPO_HOME / WWW_ROOT 双根目录与默认值（§4.2 运行时用户数据目录）
package config

// 默认根目录（原型 DEFAULT_HOME/DEFAULT_WWW 直译；`~` 在展示层解析）
const (
	DefaultHome     = "~/phpo"
	DefaultWWW      = "~/www"
	DefaultSitePort = 80 // 站点默认端口；占用则顺延首个可用空位（无窗口上限）
	MinPort         = 1
	MaxPort         = 65535
)

// Config 持久化于 ~/.phpo/config.json（读写在 store 层）
type Config struct {
	PHPOHome string `json:"phpo_home"`
	WWWRoot  string `json:"www_root"`
}

// DeriveEnv 由配置推导全部派生路径
func (c Config) DeriveEnv() Env {
	return DerivePaths(c.PHPOHome, c.WWWRoot)
}
