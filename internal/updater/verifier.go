// 升级包完整性校验（硬红线 #6）：SHA256 + Ed25519 双校验，缺一不可
// 签名约定：signature = base64(Ed25519(sha256Hex))，被签消息为下载文件的 SHA256 小写 hex
package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

var (
	ErrChecksum    = errors.New("updater: SHA256 校验失败")
	ErrSignature   = errors.New("updater: 签名校验失败")
	ErrNoPublicKey = errors.New("updater: 未配置签名公钥")
	ErrKeyLength   = errors.New("updater: Ed25519 公钥长度错误")
)

// SHA256File 计算下载包 sha256（hex）
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum 比对实际与期望 SHA256（大小写无关）
func VerifyChecksum(gotHex, wantHex string) error {
	if !strings.EqualFold(gotHex, wantHex) || wantHex == "" {
		return ErrChecksum
	}
	return nil
}

// VerifySignature 校验 Ed25519 签名（对 sha256Hex 签名）
func VerifySignature(pub ed25519.PublicKey, sha256Hex, signatureB64 string) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrKeyLength
	}
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return ErrSignature
	}
	if !ed25519.Verify(pub, []byte(strings.ToLower(sha256Hex)), sig) {
		return ErrSignature
	}
	return nil
}

// VerifyPackage 对已下载文件执行双校验；pub 为 nil 时拒绝（升级包必须验签）
func VerifyPackage(path, wantSHA string, pub ed25519.PublicKey, signatureB64 string) error {
	if pub == nil {
		return ErrNoPublicKey
	}
	got, err := SHA256File(path)
	if err != nil {
		return err
	}
	if err := VerifyChecksum(got, wantSHA); err != nil {
		return err
	}
	return VerifySignature(pub, got, signatureB64)
}
