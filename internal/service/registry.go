// 服务元数据注册表：SVC_META 五服务（§0.3 计数=5）的端口/角色静态描述，供生命周期与安装装配复用
package service

import "phpo/internal/model"

// Spec 单个服务种类的角色描述。
// HostPort 为「服务端口」发布到宿主的默认值（§5.8：占用即报 portInUse，不做站点式顺延）；
// HostPort=0 表示不发布到宿主、仅 phpo-network 内可达（如 php-fpm，经网络别名 php-{version}-fpm:9000 命中，硬红线 1）。
type Spec struct {
	Kind          model.ServiceKind
	ContainerPort string // 容器内监听端口，如 "9000/tcp"
	HostPort      int    // 宿主发布端口；0=不发布
	Singleton     bool   // 是否单例（nginx 仅一份）
}

// specs SVC_META 五服务；顺序与 model.AllKinds 一致
var specs = []Spec{
	{Kind: model.KindPHP, ContainerPort: "9000/tcp"}, // 仅内部：不发布宿主端口
	{Kind: model.KindMySQL, ContainerPort: "3306/tcp", HostPort: 3306},
	{Kind: model.KindPgsql, ContainerPort: "5432/tcp", HostPort: 5432},
	{Kind: model.KindRedis, ContainerPort: "6379/tcp", HostPort: 6379},
	{Kind: model.KindNginx, ContainerPort: "80/tcp", HostPort: 80, Singleton: true},
}

// All 返回五服务元数据副本（顺序稳定）
func All() []Spec {
	out := make([]Spec, len(specs))
	copy(out, specs)
	return out
}

// Get 按种类查元数据
func Get(kind model.ServiceKind) (Spec, bool) {
	for _, s := range specs {
		if s.Kind == kind {
			return s, true
		}
	}
	return Spec{}, false
}
