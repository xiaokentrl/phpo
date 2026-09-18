// 通用路径/域名/站点根/扩展名校验器（原型 1625–1705 行直译）
// 遵循最小限制原则：域名零限制、站点根允许 WWW_ROOT 外（降级 warning）
package config

import (
	"regexp"
	"strings"

	"phpo/pkg/errs"
)

var (
	reMultiSlash = regexp.MustCompile(`/+`)
	reTrailSlash = regexp.MustCompile(`/+$`)
	reTraversal  = regexp.MustCompile(`(^|/)\.\.(/|$)`)
	reDomain     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	reExt        = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

// NormPath 原型 normPath：trim → 折叠 // → 去尾部 /
func NormPath(p string) string {
	s := strings.TrimSpace(p)
	s = reMultiSlash.ReplaceAllString(s, "/")
	s = reTrailSlash.ReplaceAllString(s, "")
	return s
}

// HasTraversal 是否存在独立 .. 路径段
func HasTraversal(p string) bool {
	return reTraversal.MatchString(p)
}

type DomainCheck struct {
	Ok    bool
	Value string
	Msg   string
}

// ValidateDomain 域名零限制：仅格式 + 长度 ≤253；失败带原值
func ValidateDomain(domain string) DomainCheck {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" || len(d) > 253 {
		return DomainCheck{Msg: errs.DomainInvalid}
	}
	if !reDomain.MatchString(d) {
		return DomainCheck{Msg: errs.DomainInvalid + ": " + d}
	}
	return DomainCheck{Ok: true, Value: d}
}

type RootCheck struct {
	Ok         bool
	Value      string
	Msg        string
	OutsideWww bool // FIX #4：WWW_ROOT 外为 warning 而非 error
}

// ValidateSiteRoot 站点根校验；wwwRoot 为已 NormPath 的 WWW_ROOT
func ValidateSiteRoot(root, wwwRoot string) RootCheck {
	r := NormPath(root)
	if r == "" {
		return RootCheck{Msg: errs.RootEmpty}
	}
	if HasTraversal(r) {
		return RootCheck{Msg: errs.PathTraversal}
	}
	www := NormPath(wwwRoot)
	if !(r == www || strings.HasPrefix(r, www+"/")) {
		return RootCheck{Ok: true, Value: r, OutsideWww: true}
	}
	return RootCheck{Ok: true, Value: r}
}

// ValidateExt 扩展名格式（install/extensions 用）
func ValidateExt(name string) bool {
	return reExt.MatchString(strings.TrimSpace(name))
}
