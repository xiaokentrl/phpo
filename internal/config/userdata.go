// 应用用户数据目录（§4.2 / §8）：phpo.db、config.json、logs 的落地根；跨平台解析
// Linux ~/.config/phpo · macOS ~/Library/Application Support/phpo · Windows %APPDATA%\phpo
// 由 os.UserConfigDir 逐平台给出规范目录，再拼 phpo 子目录。
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// UserDataDir 返回应用用户数据根目录（不存在时由调用方按需创建）
func UserDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "phpo"), nil
}

// DBPath 返回 phpo.db 的绝对路径（位于用户数据目录内）
func DBPath() (string, error) {
	d, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "phpo.db"), nil
}

// ExpandHome 把路径前缀 `~`（或 `~/`）展开为当前用户主目录；其余原样返回。
// PHPO_HOME / WWW_ROOT 默认含 `~`（DefaultHome），落盘前须经此展开。
func ExpandHome(p string) string {
	if p == "" {
		return p
	}
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// ExpandEnvHomes 展开 Env 中 PHPO_HOME / WWW_ROOT 的 `~` 前缀并重新派生全路径
func ExpandEnvHomes(e Env) Env {
	return DerivePaths(ExpandHome(e.PHPOHome), ExpandHome(e.WWWRoot))
}
