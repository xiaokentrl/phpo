// 密码策略（总纲 §1.5）：明文 · 默认 123456 · 可修改 · 可为空 · 长度不校验 · UI 可查看
// 明确禁止：长度/复杂度/非空校验，keyring、AES、bcrypt、argon2 等任何加密路径
package config

import (
	"crypto/rand"
	"encoding/hex"
)

// DefaultPassword 原型 DEFAULT_PASSWORD 直译
const DefaultPassword = "123456"

// GenPassword 随机初始密码：16 位十六进制（原型 genPassword 语义：8 字节随机数 hex 编码）
func GenPassword() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 系统随机源不可用属环境级故障，回落默认值（仍零校验放行）
		return DefaultPassword
	}
	return hex.EncodeToString(b[:])
}
