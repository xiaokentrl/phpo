// 服务领域模型：SVC_META 五种服务（php/mysql/pgsql/redis/nginx），版本开放输入
package model

type ServiceKind string

const (
	KindPHP   ServiceKind = "php"
	KindMySQL ServiceKind = "mysql"
	KindPgsql ServiceKind = "pgsql"
	KindRedis ServiceKind = "redis"
	KindNginx ServiceKind = "nginx"
)

// AllKinds 服务数=5（§0.3 权威值）
var AllKinds = []ServiceKind{KindPHP, KindMySQL, KindPgsql, KindRedis, KindNginx}

// NginxSingleton Nginx 为单例服务（PF.nginxSingle）
func (k ServiceKind) NginxSingleton() bool { return k == KindNginx }

// InstalledVersion 某服务某版本的安装态（Docker 实际状态 ≡ SQLite 状态，硬红线校准）
type InstalledVersion struct {
	Kind    ServiceKind `json:"kind"`
	Version string      `json:"version"`
	Running bool        `json:"running"`
}
