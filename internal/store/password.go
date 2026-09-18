// 密码存取：env 表键 {KIND}_{VER}_PASSWORD（点号去除，与端口键同规则），明文零校验
package store

import (
	"strings"
)

// envVer 原型键规则：version 去点（8.4 → 84）
func envVer(version string) string { return strings.ReplaceAll(version, ".", "") }

func EnvKeyPassword(kind, version string) string {
	return strings.ToUpper(kind) + "_" + envVer(version) + "_PASSWORD"
}

func EnvKeyPort(kind, version string) string {
	return strings.ToUpper(kind) + "_" + envVer(version) + "_PORT"
}

// GetPassword 未设置时 exists=false（由调用方决定是否回落默认值）
func (s *Store) GetPassword(kind, version string) (string, bool, error) {
	return s.GetEnv(EnvKeyPassword(kind, version))
}

// SetPassword 明文直存：空串合法、任意长度合法、任意字符合法（§1.5）
func (s *Store) SetPassword(kind, version, password string) error {
	return s.SetEnv(EnvKeyPassword(kind, version), password)
}
