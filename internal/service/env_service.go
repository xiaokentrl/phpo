// EnvService：服务密码/端口 env 读写（T504）——明文零校验落库，写入后广播 state:changed 供前端回流（硬红线 4：后端唯一权威）
// 密码策略 §1.5：默认 123456、可修改、可为空、长度不校验、UI 可查看；端口按版本落库供 DBService/RedisService 读取（§5.8 占用报错不顺延）
package service

import (
	"phpo/internal/config"
	"phpo/internal/model"
)

// EnvStore 密码/端口 env 读写子集（*store.Store 满足）
type EnvStore interface {
	GetPassword(kind, version string) (string, bool, error)
	SetPassword(kind, version, password string) error
	GetServicePort(kind, version string) (int, bool, error)
	SetServicePort(kind, version string, port int) error
	BuildSnapshot() (*model.Snapshot, error)
}

// EnvService 组合 store 与发射器；写入即回流
type EnvService struct {
	store   EnvStore
	emitter Emitter
}

func NewEnvService(st EnvStore, emitter Emitter) *EnvService {
	return &EnvService{store: st, emitter: emitter}
}

// GetPassword 读明文供 UI 回显；未设置回落默认（与容器实际生效值一致）
func (e *EnvService) GetPassword(kind model.ServiceKind, version string) (string, error) {
	v, exists, err := e.store.GetPassword(string(kind), version)
	if err != nil {
		return "", err
	}
	if !exists {
		return config.DefaultPassword, nil
	}
	return v, nil
}

// SetPassword 明文直存（空串/任意长度/任意字符合法，零校验零加密），随后广播
func (e *EnvService) SetPassword(kind model.ServiceKind, version, password string) error {
	if err := e.store.SetPassword(string(kind), version, password); err != nil {
		return err
	}
	return e.emit()
}

// GetPort 读某版本落库的宿主端口；未设置返回 0（调用方回落注册表默认）
func (e *EnvService) GetPort(kind model.ServiceKind, version string) (int, error) {
	p, _, err := e.store.GetServicePort(string(kind), version)
	return p, err
}

// SetPort 落库某版本宿主端口，随后广播
func (e *EnvService) SetPort(kind model.ServiceKind, version string, port int) error {
	if err := e.store.SetServicePort(string(kind), version, port); err != nil {
		return err
	}
	return e.emit()
}

// emit 拉取权威快照并广播 state:changed
func (e *EnvService) emit() error {
	snap, err := e.store.BuildSnapshot()
	if err != nil {
		return err
	}
	e.emitter.Emit("state:changed", map[string]any{"snapshot": snap})
	return nil
}
