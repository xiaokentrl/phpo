// 派生路径纯函数：derivePaths / verRoot / hostToContainer / MOUNTS / resolveMounts 的 Go 直译
// 金样本 = 原型 前端唯一界面来源.txt 475–590 行，行为逐条对齐
package config

import "strings"

// Env 为派生路径全集，键名与原型 state.env 一致
type Env struct {
	PHPOHome       string `json:"PHPO_HOME"`
	WWWRoot        string `json:"WWW_ROOT"`
	PHPRoot        string `json:"PHP_ROOT"`
	NginxRoot      string `json:"NGINX_ROOT"`
	NginxSitesRoot string `json:"NGINX_SITES_ROOT"`
	MysqlRoot      string `json:"MYSQL_ROOT"`
	PgsqlRoot      string `json:"PGSQL_ROOT"`
	RedisRoot      string `json:"REDIS_ROOT"`
	BackupRoot     string `json:"BACKUP_ROOT"`
	OfflineRoot    string `json:"OFFLINE_ROOT"`
}

func trimTrailingSlashes(s string) string { return strings.TrimRight(s, "/") }

// DerivePaths 空值回落默认；仅裁剪尾部斜杠（原型语义）
func DerivePaths(home, wwwRoot string) Env {
	h := trimTrailingSlashes(orDefault(home, DefaultHome))
	w := trimTrailingSlashes(orDefault(wwwRoot, DefaultWWW))
	return Env{
		PHPOHome:       h,
		WWWRoot:        w,
		PHPRoot:        h + "/php",
		NginxRoot:      h + "/nginx",
		NginxSitesRoot: h + "/nginx/sites",
		MysqlRoot:      h + "/mysql",
		PgsqlRoot:      h + "/pgsql",
		RedisRoot:      h + "/redis",
		BackupRoot:     h + "/backups",
		OfflineRoot:    h + "/offline",
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Get 按原型键名取值（如 "WWW_ROOT"）
func (e Env) Get(key string) string {
	switch key {
	case "PHPO_HOME":
		return e.PHPOHome
	case "WWW_ROOT":
		return e.WWWRoot
	case "PHP_ROOT":
		return e.PHPRoot
	case "NGINX_ROOT":
		return e.NginxRoot
	case "NGINX_SITES_ROOT":
		return e.NginxSitesRoot
	case "MYSQL_ROOT":
		return e.MysqlRoot
	case "PGSQL_ROOT":
		return e.PgsqlRoot
	case "REDIS_ROOT":
		return e.RedisRoot
	case "BACKUP_ROOT":
		return e.BackupRoot
	case "OFFLINE_ROOT":
		return e.OfflineRoot
	}
	return ""
}

// RootFor 返回某服务的版本根目录（原型 verRoot：{KIND_ROOT}/{version}）
func (e Env) RootFor(kind, version string) string {
	return e.Get(strings.ToUpper(kind)+"_ROOT") + "/" + version
}

// VersionSubdirs 服务版本目录布局（VERSION_SUBDIRS 直译）
var VersionSubdirs = map[string][]string{
	"php":   {"conf", "logs"},
	"nginx": {"conf", "logs"},
	"mysql": {"conf", "data", "logs", "initdb"},
	"pgsql": {"conf", "data", "logs", "initdb"},
	"redis": {"conf", "data", "logs"},
}

// 容器内固定挂载点
const (
	WWWContainer   = "/var/www"
	SitesContainer = "/etc/nginx/sites"
)

// HostToContainer 仅映射 WWW_ROOT 与 NGINX_SITES_ROOT 两个前缀（原型逐行语义）
func (e Env) HostToContainer(p string) string {
	if p == "" {
		return p
	}
	if w := e.WWWRoot; w != "" && (p == w || strings.HasPrefix(p, w+"/")) {
		return WWWContainer + p[len(w):]
	}
	if s := e.NginxSitesRoot; s != "" && (p == s || strings.HasPrefix(p, s+"/")) {
		return SitesContainer + p[len(s):]
	}
	return p
}

// ContainerToHost HostToContainer 的逆映射
func (e Env) ContainerToHost(p string) string {
	if p == "" {
		return p
	}
	if p == WWWContainer || strings.HasPrefix(p, WWWContainer+"/") {
		return e.WWWRoot + p[len(WWWContainer):]
	}
	if p == SitesContainer || strings.HasPrefix(p, SitesContainer+"/") {
		return e.NginxSitesRoot + p[len(SitesContainer):]
	}
	return p
}

// Mount 单条挂载；Global=true 取全局键，否则取 {KIND_ROOT}/{version}/{sub}[/{from}]
type Mount struct {
	Global bool   `json:"global,omitempty"`
	Key    string `json:"key,omitempty"`
	Sub    string `json:"sub,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to"`
	Mode   string `json:"mode"`
	Label  string `json:"label"`
	Host   string `json:"host"`
}

// mountsTable MOUNTS 五服务挂载表（总数 5，条目 4+4+4+5+3）
var mountsTable = map[string][]Mount{
	"php": {
		{Global: true, Key: "WWW_ROOT", To: "/var/www", Mode: "rw", Label: "site sources"},
		{Sub: "conf", From: "php.ini", To: "/usr/local/etc/php/conf.d/zz-phpo.ini", Mode: "ro", Label: "php.ini"},
		{Sub: "conf", From: "php-fpm.conf", To: "/usr/local/etc/php-fpm.d/zz-phpo.conf", Mode: "ro", Label: "php-fpm pool"},
		{Sub: "logs", To: "/var/log/php-fpm", Mode: "rw", Label: "FPM logs"},
	},
	"nginx": {
		{Global: true, Key: "WWW_ROOT", To: "/var/www", Mode: "ro", Label: "static files"},
		{Global: true, Key: "NGINX_SITES_ROOT", To: "/etc/nginx/sites", Mode: "ro", Label: "vhosts"},
		{Sub: "conf", From: "nginx.conf", To: "/etc/nginx/nginx.conf", Mode: "ro", Label: "nginx.conf"},
		{Sub: "logs", To: "/var/log/nginx", Mode: "rw", Label: "logs"},
	},
	"mysql": {
		{Sub: "conf", From: "my.cnf", To: "/etc/mysql/conf.d/zz-phpo.cnf", Mode: "ro", Label: "my.cnf"},
		{Sub: "data", To: "/var/lib/mysql", Mode: "rw", Label: "data"},
		{Sub: "logs", To: "/var/log/mysql", Mode: "rw", Label: "logs"},
		{Sub: "initdb", To: "/docker-entrypoint-initdb.d", Mode: "ro", Label: "initdb"},
	},
	"pgsql": {
		{Sub: "conf", From: "postgresql.conf", To: "/etc/postgresql/postgresql.conf", Mode: "ro", Label: "postgresql.conf"},
		{Sub: "conf", From: "pg_hba.conf", To: "/etc/postgresql/pg_hba.conf", Mode: "ro", Label: "pg_hba.conf"},
		{Sub: "data", To: "/var/lib/postgresql/data", Mode: "rw", Label: "data"},
		{Sub: "logs", To: "/var/log/postgresql", Mode: "rw", Label: "logs"},
		{Sub: "initdb", To: "/docker-entrypoint-initdb.d", Mode: "ro", Label: "initdb"},
	},
	"redis": {
		{Sub: "conf", From: "redis.conf", To: "/usr/local/etc/redis/redis.conf", Mode: "ro", Label: "redis.conf"},
		{Sub: "data", To: "/data", Mode: "rw", Label: "data"},
		{Sub: "logs", To: "/var/log/redis", Mode: "rw", Label: "logs"},
	},
}

// ResolveMounts 依 env 解析某服务版本的全部挂载宿主路径（resolveMounts 直译）
func (e Env) ResolveMounts(kind, version string) []Mount {
	list := mountsTable[kind]
	root := e.Get(strings.ToUpper(kind) + "_ROOT")
	out := make([]Mount, 0, len(list))
	for _, m := range list {
		var host string
		if m.Global {
			host = e.Get(m.Key)
		} else if m.Sub != "" {
			host = root + "/" + version + "/" + m.Sub
			if m.From != "" {
				host += "/" + m.From
			}
		}
		m.Host = host
		out = append(out, m)
	}
	return out
}

// HomeSubdirs PHPO_HOME 目录树说明（HOME_SUBDIRS 直译，装机向导用）
var HomeSubdirs = []struct {
	Path  string
	Label string
	Depth int
}{
	{"php", "PHP runtime root", 0},
	{"nginx", "nginx root", 0},
	{"nginx/sites", "nginx vhosts (global)", 1},
	{"mysql", "MySQL root", 0},
	{"pgsql", "PostgreSQL root", 0},
	{"redis", "Redis root", 0},
	{"backups", "backup archives", 0},
	{"offline", "offline cache", 0},
}
