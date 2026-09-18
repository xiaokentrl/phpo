// 版本策略（总纲 §5.4 唯一校验规则）：非空 / 无 / \ / 无 .. / 无 \x00 / 长度 ≤128 / 首尾 trim
// 禁止字符集白名单；行为与原型 validateVersion（前端唯一界面来源.txt:1658）逐分支对齐
package config

import (
	"strings"
	"unicode"

	"phpo/pkg/errs"
)

// MaxVersionLen 版本号长度上限（§0.3 权威值 128）
const MaxVersionLen = 128

type VersionCheck struct {
	Ok    bool
	Value string
	Msg   string
}

func (v VersionCheck) Invalid() VersionCheck {
	return VersionCheck{Ok: false, Msg: errs.VersionInvalid + ": " + v.Value}
}

// ValidateVersion 仅做路径安全校验；kind 参数保留自原型签名（当前不参与判定）
func ValidateVersion(kind, version string) VersionCheck {
	v := strings.TrimSpace(version)
	_ = kind
	if v == "" {
		return VersionCheck{Ok: false, Msg: errs.VersionInvalid}
	}
	if v == "." || v == ".." || strings.Contains(v, "..") {
		return VersionCheck{Value: v}.Invalid()
	}
	if strings.ContainsAny(v, "/\\") || strings.ContainsRune(v, 0) || hasWSOrCtrl(v) {
		return VersionCheck{Value: v}.Invalid()
	}
	if len(v) > MaxVersionLen {
		return VersionCheck{Value: v}.Invalid()
	}
	return VersionCheck{Ok: true, Value: v}
}

// 原型正则 [\x00-\x1f\s]：控制字符 + Unicode 空白（unicode.White_Space 与 JS \s 集合一致）
func hasWSOrCtrl(s string) bool {
	for _, r := range s {
		if r <= 0x1f || r == 0x7f || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}
