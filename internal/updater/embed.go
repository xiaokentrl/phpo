// 发布公钥嵌入（硬红线 6）：go:embed 随包分发，用于校验升级包 Ed25519 签名
// 当前为占位公钥。配钥流程（一次性，离线）：scripts/sign-release.sh genkey 生成私钥 →
// 私钥存 CI secret PHPO_SIGNING_KEY → sign-release.sh pubkey 导出真实公钥写入本文件并提交；
// release.yml 在签名前反推公钥并与本文件比对，不匹配（占位未替换）即拒绝发布，杜绝客户端验签全失败。
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
