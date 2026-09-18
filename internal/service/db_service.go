// 数据服务装配（MySQL / PostgreSQL）：密码 env 注入 + 宿主端口发布；数据持久由 MOUNTS 自动生成宿主 bind
// 密码策略 §1.5：明文、任意字符含空格、可为空；空密码走容器原生开关；未设回落默认 123456
package service

import (
	"strconv"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// EnvReader store.Store 天然满足：读某服务版本的明文密码与宿主发布端口；exists=false 表示未设置（调用方回落默认值）
type EnvReader interface {
	GetPassword(kind, version string) (string, bool, error)
	GetServicePort(kind, version string) (int, bool, error)
}

// DBService MySQL / PostgreSQL 安装装配策略（按种类参数化）
type DBService struct {
	kind   model.ServiceKind
	reader EnvReader
}

func NewDBService(kind model.ServiceKind, reader EnvReader) DBService {
	return DBService{kind: kind, reader: reader}
}

func (s DBService) Kind() model.ServiceKind { return s.kind }

func (s DBService) ContainerSpec(version string, env config.Env) (engine.ContainerSpec, error) {
	ref, err := engine.ImageRefFor(string(s.kind), version)
	if err != nil {
		return engine.ContainerSpec{}, err
	}
	sp, _ := Get(s.kind)
	spec := engine.ContainerSpec{
		Kind:    string(s.kind),
		Version: version,
		Image:   ref,
		Exposed: []string{sp.ContainerPort},
	}
	// 宿主发布端口：优先用该版本已落库的端口（多版本各占端口，§5.8 服务端口占用报错、不顺延），回落注册表默认
	hostPort := sp.HostPort
	if hp, ok := s.hostPort(version); ok {
		hostPort = hp
	}
	if hostPort > 0 {
		spec.PortMap = map[string]string{sp.ContainerPort: strconv.Itoa(hostPort)}
	}
	spec.Env = dbEnv(s.kind, s.password(version))
	// pgsql 官方镜像默认读 $PGDATA/postgresql.conf，忽略挂载到 /etc/postgresql 的配置；
	// 显式指向 config_file 才让渲染出的 postgresql.conf（内含 hba_file/data_directory）生效。
	if s.kind == model.KindPgsql {
		spec.Command = []string{"postgres", "-c", "config_file=/etc/postgresql/postgresql.conf"}
	}
	return spec, nil
}

// password 取明文；无 store（reader=nil）或未设置（exists=false）回落默认
func (s DBService) password(version string) string {
	if s.reader == nil {
		return config.DefaultPassword
	}
	v, exists, err := s.reader.GetPassword(string(s.kind), version)
	if err != nil || !exists {
		return config.DefaultPassword
	}
	return v
}

// hostPort 读该版本落库的宿主端口；无 store 或未设置返回 exists=false
func (s DBService) hostPort(version string) (int, bool) {
	if s.reader == nil {
		return 0, false
	}
	p, exists, err := s.reader.GetServicePort(string(s.kind), version)
	if err != nil || !exists || p <= 0 {
		return 0, false
	}
	return p, true
}

// dbEnv 按种类构造密码 env；空密码走容器原生开关，不拦截（§1.5）
func dbEnv(kind model.ServiceKind, password string) []string {
	switch kind {
	case model.KindMySQL:
		if password == "" {
			return []string{"MYSQL_ALLOW_EMPTY_PASSWORD=yes"}
		}
		return []string{"MYSQL_ROOT_PASSWORD=" + password}
	case model.KindPgsql:
		if password == "" {
			return []string{"POSTGRES_HOST_AUTH_METHOD=trust"}
		}
		return []string{"POSTGRES_PASSWORD=" + password}
	}
	return nil
}
