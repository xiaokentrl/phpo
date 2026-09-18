// Redis 服务装配：redis:{version}，密码走容器 env（对齐 mysql/pgsql），命令 --requirepass 覆盖配置占位
// redis.conf 里的 requirepass ${REDIS_...} 是 SSOT 展示用；redis 不展开 env，故由启动命令注入实际密码。
package service

import (
	"strconv"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// RedisService Redis 种类的安装装配策略
type RedisService struct {
	reader EnvReader
}

func NewRedisService(reader EnvReader) RedisService {
	return RedisService{reader: reader}
}

func (RedisService) Kind() model.ServiceKind { return model.KindRedis }

func (s RedisService) ContainerSpec(version string, env config.Env) (engine.ContainerSpec, error) {
	ref, err := engine.ImageRefFor(string(model.KindRedis), version)
	if err != nil {
		return engine.ContainerSpec{}, err
	}
	sp, _ := Get(model.KindRedis)
	spec := engine.ContainerSpec{
		Kind:    string(model.KindRedis),
		Version: version,
		Image:   ref,
		Exposed: []string{sp.ContainerPort},
	}
	hostPort := sp.HostPort
	if hp, ok := s.hostPort(version); ok {
		hostPort = hp
	}
	if hostPort > 0 {
		spec.PortMap = map[string]string{sp.ContainerPort: strconv.Itoa(hostPort)}
	}

	password := s.password(version)
	spec.Env = []string{"REDIS_PASSWORD=" + password}
	// 命令优先于配置文件：有密码走 requirepass；空密码关保护模式以允许容器网络内匿名连接（§1.5 空密码合法）
	if password == "" {
		spec.Command = []string{"sh", "-c", `exec redis-server /usr/local/etc/redis/redis.conf --requirepass "" --protected-mode no`}
	} else {
		spec.Command = []string{"sh", "-c", `exec redis-server /usr/local/etc/redis/redis.conf --requirepass "$REDIS_PASSWORD"`}
	}
	return spec, nil
}

func (s RedisService) password(version string) string {
	if s.reader == nil {
		return config.DefaultPassword
	}
	v, exists, err := s.reader.GetPassword(string(model.KindRedis), version)
	if err != nil || !exists {
		return config.DefaultPassword
	}
	return v
}

func (s RedisService) hostPort(version string) (int, bool) {
	if s.reader == nil {
		return 0, false
	}
	p, exists, err := s.reader.GetServicePort(string(model.KindRedis), version)
	if err != nil || !exists || p <= 0 {
		return 0, false
	}
	return p, true
}
