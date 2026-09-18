// .env 明文读写：~/phpo/.env 的解析与序列化（密码含任意字符，仅按首个 = 分割）
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type EnvFile map[string]string

// ParseEnv 逐行解析 KEY=VALUE；忽略空行与 # 注释；值保留原样（不引号化、不 trim）
func ParseEnv(r *bufio.Reader) (EnvFile, error) {
	out := EnvFile{}
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
				if k, v, ok := strings.Cut(line, "="); ok {
					out[strings.TrimSpace(k)] = v
				}
			}
		}
		if err != nil {
			if err == os.ErrClosed || err.Error() == "EOF" {
				break
			}
			return nil, err
		}
	}
	return out, nil
}

// ReadEnvFile 读取并解析；文件不存在返回空集（首装场景）
func ReadEnvFile(path string) (EnvFile, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return EnvFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseEnv(bufio.NewReader(f))
}

// WriteEnvFile 原子写（0600；Windows 由 NTFS ACL 保证，§8）；键按字典序输出保证 diff 稳定
func WriteEnvFile(path string, env EnvFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, env[k]))
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
