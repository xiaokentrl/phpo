// Nginx 服务装配：nginx:{version}，容器内端口发布到宿主（§5.8）
// 发布端口 = 当前站点端口并集（1:1 host==container）；无站点时回落注册表默认（80）。
// 站点增删/改端口后需重建 nginx 容器以重绑宿主端口——见 LifecycleService.RepublishNginx。
package service

import (
	"fmt"
	"strconv"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// NginxService Nginx 种类的安装装配策略（单例）
type NginxService struct{}

func (NginxService) Kind() model.ServiceKind { return model.KindNginx }

func (NginxService) ContainerSpec(version string, env config.Env) (engine.ContainerSpec, error) {
	return nginxSpec(env, version, nil)
}

// nginxSpec 组装 nginx 容器 spec。ports 为空 → 发布注册表默认端口；否则发布站点端口并集（去重、1:1）。
func nginxSpec(env config.Env, version string, ports []int) (engine.ContainerSpec, error) {
	ref, err := engine.ImageRefFor(string(model.KindNginx), version)
	if err != nil {
		return engine.ContainerSpec{}, err
	}
	sp, _ := Get(model.KindNginx)
	spec := engine.ContainerSpec{
		Kind:    string(model.KindNginx),
		Version: version,
		Image:   ref,
	}
	if len(ports) == 0 {
		if sp.HostPort > 0 {
			spec.Exposed = []string{portProto(sp.HostPort)}
			spec.PortMap = map[string]string{portProto(sp.HostPort): strconv.Itoa(sp.HostPort)}
		}
		return spec, nil
	}
	seen := make(map[int]bool, len(ports))
	exposed := make([]string, 0, len(ports))
	portMap := make(map[string]string, len(ports))
	for _, p := range ports {
		if p <= 0 || seen[p] {
			continue
		}
		seen[p] = true
		proto := portProto(p)
		exposed = append(exposed, proto)
		portMap[proto] = strconv.Itoa(p) // 宿主端口 == 容器 listen 端口（vhost `listen <port>`）
	}
	spec.Exposed = exposed
	spec.PortMap = portMap
	return spec, nil
}

// portProto 把端口号格式化为 Docker 端口键，如 8090 → "8090/tcp"
func portProto(p int) string { return fmt.Sprintf("%d/tcp", p) }
