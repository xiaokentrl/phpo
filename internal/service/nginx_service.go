// Nginx 服务装配：nginx:{version}，容器内 80 发布到宿主默认站点端口（占用则建站时顺延，§5.8）
package service

import (
	"strconv"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// NginxService Nginx 种类的安装装配策略（单例）
type NginxService struct{}

func (NginxService) Kind() model.ServiceKind { return model.KindNginx }

func (NginxService) ContainerSpec(version string, _ config.Env) (engine.ContainerSpec, error) {
	ref, err := engine.ImageRefFor(string(model.KindNginx), version)
	if err != nil {
		return engine.ContainerSpec{}, err
	}
	sp, _ := Get(model.KindNginx)
	spec := engine.ContainerSpec{
		Kind:    string(model.KindNginx),
		Version: version,
		Image:   ref,
		Exposed: []string{sp.ContainerPort},
	}
	if sp.HostPort > 0 {
		spec.PortMap = map[string]string{sp.ContainerPort: strconv.Itoa(sp.HostPort)}
	}
	return spec, nil
}
