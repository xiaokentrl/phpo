// PHP 服务装配：php:{version}-fpm，仅容器内暴露 9000；不发布宿主端口
// 站点经 nginx fastcgi 走网络别名 php-{version}-fpm:9000 命中（硬红线 1）
package service

import (
	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// PHPService PHP 种类的安装装配策略
type PHPService struct{}

func (PHPService) Kind() model.ServiceKind { return model.KindPHP }

func (PHPService) ContainerSpec(version string, _ config.Env) (engine.ContainerSpec, error) {
	ref, err := engine.ImageRefFor(string(model.KindPHP), version)
	if err != nil {
		return engine.ContainerSpec{}, err
	}
	sp, _ := Get(model.KindPHP)
	return engine.ContainerSpec{
		Kind:    string(model.KindPHP),
		Version: version,
		Image:   ref,
		Exposed: []string{sp.ContainerPort}, // 9000/tcp；PortMap 留空即不发布宿主
	}, nil
}
