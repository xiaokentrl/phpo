// SHA256 校验：命中前 / 提升时 / 定期扫描三处复用（§5.14.5）
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// FileSHA256 计算文件内容 sha256（hex，无算法前缀）
func FileSHA256(path string) (string, error) {
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

// matches 期望值兼容 "sha256:xxx" 前缀与裸 hex
func matches(computed, want string) bool {
	if want == "" {
		return false // 无记录值视为不匹配（防误命中）
	}
	if len(want) > 7 && want[:7] == "sha256:" {
		want = want[7:]
	}
	return computed == want
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}
