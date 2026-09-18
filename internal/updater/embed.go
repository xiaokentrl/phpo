// 发布公钥嵌入（硬红线 6）：go:embed 随包分发，用于校验升级包 Ed25519 签名
// 当前为占位公钥；T702 签名发布流程将以 build/signing/public.key 的真实公钥替换本文件
package updater

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"strings"
)

//go:embed signing
var signingFS embed.FS

// PublicKey 解析嵌入的 Ed25519 公钥；缺失或长度不符返回 nil（VerifyPackage 据此拒绝一切升级）
func PublicKey() ed25519.PublicKey {
	b, err := signingFS.ReadFile("signing/public.key")
	if err != nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(raw)
}
