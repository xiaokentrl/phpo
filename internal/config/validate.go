// 通用路径/域名/站点根/扩展名校验器（原型 1625–1705 行直译）
// 遵循最小限制原则：域名零限制、站点根允许 WWW_ROOT 外（降级 warning）
package config

import (
	"fmt"
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
	// reTraversalWin 自定义根可落在 Windows（`D:\phpo\..\x`），斜杠正则盖不住反斜杠分段
	reTraversalWin = regexp.MustCompile(`(\\|^)\.\.(\\|$)`)
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
	if r != www && !strings.HasPrefix(r, www+"/") {
		return RootCheck{Ok: true, Value: r, OutsideWww: true}
	}
	return RootCheck{Ok: true, Value: r}
}

// ValidateExt 扩展名格式（install/extensions 用）
func ValidateExt(name string) bool {
	return reExt.MatchString(strings.TrimSpace(name))
}

// ---- Docker 镜像源（参与 §5.14.3 优先级里「联网拉取」那一级）：只认「主机[:端口]」这一种形状 ----

// ErrRegistryHost 镜像源地址不合形的统一前缀；写路径与 preflight 共用同一判据（§0.2 #14）
const ErrRegistryHost = "镜像源地址不正确"

// reRegistryHost 主机名或「主机:端口」：允许字母数字、连字符、点、IPv6 方括号内的冒号另计（不支持字面 IPv6，最小限制够用）
var reRegistryHost = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?(:[0-9]{1,5})?$`)

// NormalizeRegistryHost 将用户填的一行归一成镜像源主机名：去空白、剥掉 http(s):// 前缀、剥掉结尾斜杠与末尾 /v2。
// 不要求绝对地址、不要求已存在、不校验可达性（最小限制原则）——填了连不上的源，拉取时会逐项点名并回落原始镜像名。
func NormalizeRegistryHost(s string) string {
	h := strings.TrimSpace(s)
	for _, p := range []string{"https://", "http://"} {
		h = strings.TrimPrefix(h, p)
	}
	h = strings.TrimRight(h, "/")
	h = strings.TrimSuffix(h, "/v2")
	return strings.TrimSpace(h)
}

// ValidateRegistryHost 归一并校验单个镜像源主机名：非空、无空白字符、无路径分段、字符集合规。
// 返回归一后的值；非法时 error 带原值（供界面与 preflight 同一判据，不另立标准）。
func ValidateRegistryHost(s string) (string, error) {
	h := NormalizeRegistryHost(s)
	switch {
	case h == "":
		return "", fmt.Errorf("%s: 不能为空", ErrRegistryHost)
	case strings.ContainsAny(h, " \t\x00"):
		return "", fmt.Errorf("%s: %s（不能含空白字符）", ErrRegistryHost, s)
	case strings.Contains(h, "/"):
		return "", fmt.Errorf("%s: %s（只填主机名或「主机:端口」，不要带路径）", ErrRegistryHost, s)
	case !reRegistryHost.MatchString(h):
		return "", fmt.Errorf("%s: %s", ErrRegistryHost, s)
	}
	return h, nil
}

// ValidateRegistryHosts 逐行校验并归一镜像源清单：忽略空行、去重（保序）。
// 返回的切片永远非 nil（清空即「不改写、直连官方」）；任一项非法即报错并点名首个坏值。
func ValidateRegistryHosts(hosts []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, h := range hosts {
		if strings.TrimSpace(h) == "" {
			continue
		}
		v, err := ValidateRegistryHost(h)
		if err != nil {
			return nil, err
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}

// normalizeRegistryHosts 读盘侧归一：用户手改 config.yaml 可能留下坏值，此处静默剔除
// （读配置不得报错把整个应用卡住），严格裁决在写入路径（ValidateRegistryHosts / preflight）。
func normalizeRegistryHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := map[string]bool{}
	for _, h := range hosts {
		if strings.TrimSpace(h) == "" {
			continue
		}
		v, err := ValidateRegistryHost(h)
		if err != nil || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// checkRootPath 可自定义根（缓存根 / 备份根 / 数据目录）的唯一校验：路径安全（硬红线 3）。
// 遵循最小限制原则——不要求绝对路径、不限字符集、不要求目录已存在；只规范化并拒绝 `..` 穿越段。
// 返回规范化后的值（空串表示「未自定义 / 清除自定义」）。
func checkRootPath(p string) (string, error) {
	s := NormPath(p)
	if s == "" {
		return "", nil
	}
	if HasTraversal(s) || reTraversalWin.MatchString(s) || strings.ContainsRune(s, 0) {
		return "", fmt.Errorf("%s", errs.PathTraversal)
	}
	return s, nil
}

// ValidateRootPath checkRootPath 的导出口：preflight 与配置写盘共用同一判据，不另立标准（§0.2 #14）
func ValidateRootPath(p string) (string, error) { return checkRootPath(p) }
