// 密码策略测试：零校验 + 明文 .env 往返（任意字符 / 空值 / 超长均为合法密码）
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordPolicy(t *testing.T) {
	if DefaultPassword != "123456" {
		t.Fatalf("默认密码必须为 123456（§0.3），got %q", DefaultPassword)
	}
	p := GenPassword()
	if len(p) != 16 {
		t.Errorf("GenPassword 长度 = %d, 期望 16 位 hex", len(p))
	}
	if p == GenPassword() {
		t.Error("两次随机密码不应相同")
	}
}

func TestEnvFileRoundTripWithHostilePasswords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", ".env")
	in := EnvFile{
		"MYSQL_84_PASSWORD": "123456",
		"REDIS_8_PASSWORD":  "",                              // 空密码合法
		"PGSQL_17_PASSWORD": "with spaces and = equals sign", // 含空格与 =
		"WEIRD":             "#notacomment",                  // 值以 # 开头
	}
	if err := WriteEnvFile(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for k, w := range in {
		if got := out[k]; got != w {
			t.Errorf("%s 往返失败: %q → %q", k, w, got)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf(".env 权限应为 600, got %v %v", fi.Mode(), err)
	}
}

func TestReadEnvFileMissingIsEmpty(t *testing.T) {
	out, err := ReadEnvFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil || len(out) != 0 {
		t.Errorf("缺文件应返回空集: %v %v", out, err)
	}
}

func TestEnvFileValueLengthUnbounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	long := strings.Repeat("x", 100) // 100 位密码（§1.5：任意长度合法）
	if err := WriteEnvFile(path, EnvFile{"P": long}); err != nil {
		t.Fatal(err)
	}
	out, _ := ReadEnvFile(path)
	if out["P"] != long {
		t.Error("超长密码往返丢失")
	}
}
