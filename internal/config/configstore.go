// 统一配置权威（YAML）：单一 config.yaml（XDG 用户配置目录 os.UserConfigDir()/phpo）承载目录根 + 每服务版本的明文密码/宿主端口。
// 取代旧 ~/phpo/.env 与 ~/.phpo/config.json 双份散落存储（用户裁决：配置读写统一、YAML、不污染用户主目录）。
// SQLite 退居纯运行态（installed/running/sites/extensions）；工作根的「已持久化/已就绪」由本门面派生提供，
// 并作为运行态存储能否建库的门禁（首启未设置工作目录时不得在用户数据目录留下任何文件）。
// snapshot.env 由本门面合成，前端键名契约不变。
// 密码策略 §1.5：明文零校验、可空、任意长度、任意字符——YAML 原样存，绝不加密（硬约束不变）。
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// envVer 键规则：version 去点（8.4 → 84），与端口/密码 env 键命名一致（沿用原型）
func envVer(version string) string { return strings.ReplaceAll(version, ".", "") }

// EnvKeyPassword 密码 env 键：{KIND}_{VER}_PASSWORD
func EnvKeyPassword(kind, version string) string {
	return strings.ToUpper(kind) + "_" + envVer(version) + "_PASSWORD"
}

// EnvKeyPort 端口 env 键：{KIND}_{VER}_PORT
func EnvKeyPort(kind, version string) string {
	return strings.ToUpper(kind) + "_" + envVer(version) + "_PORT"
}

// SvcSetting 某服务版本的可配置项：明文密码 + 宿主发布端口（Port=0 表示未设，回落注册表默认）
type SvcSetting struct {
	Password string `yaml:"password"`
	Port     int    `yaml:"port,omitempty"`
}

// FileConfig config.yaml 的完整磁盘形态（唯一配置真相）：仅存两个根目录与每服务版本设置；
// 派生路径（PHP_ROOT/…/OFFLINE_ROOT）不落盘、由 DerivePaths 现算，杜绝不同步。
type FileConfig struct {
	PHPOHome string                           `yaml:"phpo_home,omitempty"`
	WWWRoot  string                           `yaml:"www_root,omitempty"`
	Services map[string]map[string]SvcSetting `yaml:"services,omitempty"` // kind -> version -> 设置
}

// ConfigStore 配置唯一读写门面：内存缓存 FileConfig + 原子落盘（0600）；写操作在任务串行下调用，加锁仅作兜底。
type ConfigStore struct {
	mu   sync.Mutex
	path string
	fc   FileConfig
}

// ConfigPath 返回 config.yaml 的绝对路径（位于 XDG 用户配置目录内的 phpo 子目录）
func ConfigPath() (string, error) {
	d, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.yaml"), nil
}

// LoadConfigStore 读取 config.yaml；文件缺失即首启空配置（回落 DefaultHome/DefaultWWW），不报错。
func LoadConfigStore() (*ConfigStore, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFromPath(path)
}

// LoadFromPath 从指定路径载入配置（缺失即空配置）；供首启与备份恢复复用。
func LoadFromPath(path string) (*ConfigStore, error) {
	cs := &ConfigStore{path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cs, nil // 首启：空 FileConfig
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &cs.fc); err != nil {
		return nil, err
	}
	return cs, nil
}

// Path 返回配置文件绝对路径（供备份打包/恢复定位）
func (c *ConfigStore) Path() string { return c.path }

// Reload 从磁盘重读 config.yaml 覆盖内存态（备份恢复落盘后热重载，使后续读取反映新配置）。
func (c *ConfigStore) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		c.fc = FileConfig{}
		return nil
	}
	if err != nil {
		return err
	}
	var fc FileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return err
	}
	c.fc = fc
	return nil
}

// Roots 返回原始（未展开 `~`）的两个根目录
func (c *ConfigStore) Roots() (home, www string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fc.PHPOHome, c.fc.WWWRoot
}

// RootsPersisted 报告 config.yaml 是否已写入两个工作根——即装机向导是否完成过。
// 纯读、无副作用。这是「首启 vs 已配置」的唯一持久判据，供运行态存储决定是否创建/打开 phpo.db：
// 未持久化即首启，不得在用户数据目录留下任何文件（含 `~` 未展开亦按原样判定）。
func (c *ConfigStore) RootsPersisted() bool {
	h, w := c.Roots()
	return h != "" && w != ""
}

// RootsReady 逐根报告「已持久化且目录实际存在」，纯读、无副作用。
// 快照 dirReady 由此派生（不再落 SQLite）：两根曾就绪但被删除时自动回落 false，重新拦截写操作。
func (c *ConfigStore) RootsReady() (home, www bool) {
	h, w := c.Roots()
	return h != "" && isDir(ExpandHome(h)), w != "" && isDir(ExpandHome(w))
}

// isDir 路径存在且为目录（跟随符号链接）
func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Env 返回由原始根派生的路径全集（含 `~`，供展示与快照；IO 用 ExpandedEnv）
func (c *ConfigStore) Env() Env {
	c.mu.Lock()
	defer c.mu.Unlock()
	return DerivePaths(c.fc.PHPOHome, c.fc.WWWRoot)
}

// ExpandedEnv 把根目录的 `~` 展开为绝对路径后重新派生（供真实文件 IO / 容器挂载）
func (c *ConfigStore) ExpandedEnv() Env { return ExpandEnvHomes(c.Env()) }

// SetRoots 落库两个根目录（规范化值，原样存含 `~`），原子写盘。装机向导确认调用。
func (c *ConfigStore) SetRoots(home, www string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fc.PHPOHome = home
	c.fc.WWWRoot = www
	return c.save()
}

// setting 取某服务版本设置（内部，调用方持锁）
func (c *ConfigStore) setting(kind, version string) (SvcSetting, bool) {
	if c.fc.Services == nil {
		return SvcSetting{}, false
	}
	ver, ok := c.fc.Services[kind]
	if !ok {
		return SvcSetting{}, false
	}
	s, ok := ver[version]
	return s, ok
}

// putSetting 写入/合并某服务版本设置（内部，调用方持锁）
func (c *ConfigStore) putSetting(kind, version string, mutate func(*SvcSetting)) {
	if c.fc.Services == nil {
		c.fc.Services = map[string]map[string]SvcSetting{}
	}
	if c.fc.Services[kind] == nil {
		c.fc.Services[kind] = map[string]SvcSetting{}
	}
	s := c.fc.Services[kind][version]
	mutate(&s)
	c.fc.Services[kind][version] = s
}

// GetPassword 读明文密码；exists=false 表示未设置（调用方回落默认值）。签名兼容旧 store.EnvReader。
func (c *ConfigStore) GetPassword(kind, version string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.setting(kind, version)
	return s.Password, ok, nil
}

// SetPassword 明文直存（空串/任意长度/任意字符合法，零校验零加密），原子写盘。
func (c *ConfigStore) SetPassword(kind, version, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putSetting(kind, version, func(s *SvcSetting) { s.Password = password })
	return c.save()
}

// GetServicePort 读某版本宿主端口；exists=false 表示未设或为 0。
func (c *ConfigStore) GetServicePort(kind, version string) (int, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.setting(kind, version)
	if !ok || s.Port <= 0 {
		return 0, false, nil
	}
	return s.Port, true, nil
}

// SetServicePort 落库某版本宿主端口并原子写盘。
func (c *ConfigStore) SetServicePort(kind, version string, port int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putSetting(kind, version, func(s *SvcSetting) { s.Port = port })
	return c.save()
}

// FlatEnv 合成前端 snapshot.env 契约：全部派生路径键 + 已设置的密码/端口扁平键。
// 保持与旧 SQLite env 表完全一致的键名，前端 app.env.* 零改动。
// 出口一律展开 `~`：前端把这些值直接用于原生目录选择器与站点根拼接，未展开会被解析成
// 「当前工作目录/~/www」而报错（config.yaml 内仍原样存 `~`，保持跨机可迁移，展开只发生在出口）。
func (c *ConfigStore) FlatEnv() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]string{}
	e := ExpandEnvHomes(DerivePaths(c.fc.PHPOHome, c.fc.WWWRoot))
	for _, k := range []string{"PHPO_HOME", "WWW_ROOT", "PHP_ROOT", "NGINX_ROOT", "NGINX_SITES_ROOT",
		"MYSQL_ROOT", "PGSQL_ROOT", "REDIS_ROOT", "BACKUP_ROOT", "OFFLINE_ROOT"} {
		out[k] = e.Get(k)
	}
	for kind, vers := range c.fc.Services {
		for ver, s := range vers {
			out[EnvKeyPassword(kind, ver)] = s.Password
			if s.Port > 0 {
				out[EnvKeyPort(kind, ver)] = strconv.Itoa(s.Port)
			}
		}
	}
	return out
}

// save 原子写盘（0600 明文密码，Windows 由 NTFS ACL 保证，§8）；调用方须已持锁。
func (c *ConfigStore) save() error {
	b, err := yaml.Marshal(&c.fc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
