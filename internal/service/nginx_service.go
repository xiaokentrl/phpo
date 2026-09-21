// Nginx 服务装配：nginx:{version}，容器内端口发布到宿主（§5.8）
// 发布端口 = 站点端口并集（1:1 host==container）；用户配过的服务端口（NGINX_{VER}_PORT）始终在内，
// 未配置时只在无站点回落注册表默认（80）——避免把没人监听的 80 硬绑上去，反而让整机起不来。
// 站点增删/改端口后需重建 nginx 容器以重绑宿主端口——见 LifecycleService.RepublishNginx。
package service

import (
	"fmt"
	"strconv"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// NginxPortSource 给出当前应发布给 nginx 的站点端口集合（真实实现：*SiteService.PublishPorts）。
// 装/重建 nginx 与站点写链路共用同一判据，不在两侧各立一套发布口径。
type NginxPortSource interface {
	PublishPorts() []int
}

// NginxService Nginx 种类的安装装配策略（单例）
type NginxService struct {
	reader EnvReader
	ports  NginxPortSource
}

func NewNginxService(reader EnvReader) *NginxService { return &NginxService{reader: reader} }

func (*NginxService) Kind() model.ServiceKind { return model.KindNginx }

// SetPortSource 注入站点端口来源（di 装配期调用）；未注入则安装时只发布基准端口
func (s *NginxService) SetPortSource(src NginxPortSource) { s.ports = src }

func (s *NginxService) ContainerSpec(version string, env config.Env) (engine.ContainerSpec, error) {
	var ports []int
	if s.ports != nil {
		ports = s.ports.PublishPorts()
	}
	return nginxSpec(env, version, ports, s.reader)
}

// specWith 用调用方即时算出的端口集产出 spec（站点写链路的重发布走这一路，避免读到自己尚未落库的中间态）
func (s *NginxService) specWith(env config.Env, version string, ports []int) (engine.ContainerSpec, error) {
	return nginxSpec(env, version, ports, s.reader)
}

// nginxSpec 组装 nginx 容器 spec。发布集 = 基准端口 ∪ ports（去重、1:1）；
// basePort：config.yaml 配过则用配置值（用户的显式要求，照单发布），
// 否则只在没有任何站点端口时回落注册表默认（80）。
func nginxSpec(env config.Env, version string, ports []int, reader EnvReader) (engine.ContainerSpec, error) {
	ref, err := engine.ImageRefFor(string(model.KindNginx), version)
	if err != nil {
		return engine.ContainerSpec{}, err
	}
	spec := engine.ContainerSpec{
		Kind:    string(model.KindNginx),
		Version: version,
		Image:   ref,
	}
	list := ports
	if base, configured := nginxBasePort(reader, version); configured {
		list = append([]int{base}, ports...)
	} else if len(ports) == 0 && base > 0 {
		list = []int{base}
	}
	seen := make(map[int]bool, len(list))
	exposed := make([]string, 0, len(list))
	portMap := make(map[string]string, len(list))
	for _, p := range list {
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

// nginxBasePort 读 nginx 的服务端口：reader 为 nil（单测 / 未接配置）或该版本从未配过端口时，
// exists=false 交调用方回落注册表默认。
func nginxBasePort(reader EnvReader, version string) (int, bool) {
	sp, _ := Get(model.KindNginx)
	if reader == nil {
		return sp.HostPort, false
	}
	p, ok, err := reader.GetServicePort(string(model.KindNginx), version)
	if err != nil || !ok || p <= 0 {
		return sp.HostPort, false
	}
	return p, true
}

// portProto 把端口号格式化为 Docker 端口键，如 8090 → "8090/tcp"
func portProto(p int) string { return fmt.Sprintf("%d/tcp", p) }
