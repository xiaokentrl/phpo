// service 组规则（6）：install / uninstall / service-stop / service-start / update-config / service-config
// 控制流严格对齐原型：早退处 push 后 return（对应 case 内 break），非早退错误继续累积
package preflight

import (
	"strconv"
	"strings"

	"phpo/internal/config"
	"phpo/internal/store"
	"phpo/pkg/errs"
)

func (r *run) install() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	vv := config.ValidateVersion(c.Kind, c.Version)
	if !vv.Ok {
		r.errf("%s", vv.Msg)
		return
	}
	if contains(r.w.Snap.Installed[c.Kind], vv.Value) {
		r.errf("%s: %s %s", errs.VersionDup, c.Kind, vv.Value)
	}
	if c.Kind == "nginx" && len(r.w.Snap.Installed["nginx"]) > 0 {
		r.errf("%s", errs.NginxSingle)
	}
	// FIX #2：无密码长度校验
	if c.Port != nil {
		if s := strings.TrimSpace(asString(c.Port)); s != "" {
			if pp := r.validatePort(s, nil, nil, false); !pp.Ok {
				r.errf("%s", pp.Msg)
			}
		}
	}
	if c.Extensions != "" {
		for _, e := range splitExts(c.Extensions) {
			if !config.ValidateExt(e) {
				r.errf("%s: %s", errs.ExtInvalid, e)
				break
			}
		}
	}
}

func (r *run) uninstall() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	if !contains(r.w.Snap.Installed[c.Kind], c.Version) {
		r.errf("%s: %s %s", errs.NotInstalled, c.Kind, c.Version)
		return
	}
	if c.Kind == "php" {
		used := r.sitesUsingPHP(c.Version)
		if len(used) > 0 {
			r.errf("%s: PHP %s ← %s", errs.HasDependents, c.Version, strings.Join(used, ", "))
		}
		if len(r.w.Snap.Installed["php"]) <= 1 {
			r.errf("%s", errs.LastPhp)
		}
	}
	if c.Kind == "nginx" && len(r.w.Snap.Sites) > 0 {
		r.errf("%s: %d 个站点依赖 Nginx", errs.HasDependents, len(r.w.Snap.Sites))
	}
}

func (r *run) serviceStop() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	if !contains(r.w.Snap.Installed[c.Kind], c.Version) {
		r.errf("%s: %s %s", errs.NotInstalled, c.Kind, c.Version)
		return
	}
	if !r.w.isRunning(c.Kind, c.Version) {
		r.errf("%s: %s %s", errs.NotRunning, c.Kind, c.Version)
		return
	}
	// 依赖提示降级为 warning（能警告的不要阻止）
	if c.Kind == "php" {
		if used := r.sitesUsingPHP(c.Version); len(used) > 0 {
			r.warnf("以下站点正在使用 PHP %s：%s", c.Version, strings.Join(used, ", "))
		}
	}
	if c.Kind == "nginx" && len(r.w.Snap.Sites) > 0 {
		r.warnf("%d 个站点依赖 Nginx，停用后无法访问", len(r.w.Snap.Sites))
	}
	if c.Kind == "mysql" || c.Kind == "pgsql" || c.Kind == "redis" {
		r.warnf("停用 %s %s 将中断正在使用该服务的应用", c.Kind, c.Version)
	}
}

func (r *run) serviceStart() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	if !contains(r.w.Snap.Installed[c.Kind], c.Version) {
		r.errf("%s: %s %s", errs.NotInstalled, c.Kind, c.Version)
		return
	}
	if r.w.isRunning(c.Kind, c.Version) {
		r.errf("%s: %s %s", errs.IsRunning, c.Kind, c.Version)
		return
	}
}

func (r *run) updateConfig() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	if !contains(r.w.Snap.Installed[c.Kind], c.Version) {
		r.errf("%s: %s %s", errs.NotInstalled, c.Kind, c.Version)
		return
	}
	if c.Field == "port" {
		var cur string
		if c.Kind == "nginx" {
			cur = r.w.Snap.Env["NGINX_PORT"]
		} else {
			cur = r.w.Snap.Env[store.EnvKeyPort(c.Kind, c.Version)]
		}
		if pp := r.validatePort(asString(c.NewValue), parseIntSlice(cur), nil, false); !pp.Ok {
			r.errf("%s", pp.Msg)
		}
	}
	// FIX #2：无密码长度校验
}

func (r *run) serviceConfig() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	if !contains(r.w.Snap.Installed[c.Kind], c.Version) {
		r.errf("%s: %s %s", errs.NotInstalled, c.Kind, c.Version)
		return
	}
	if len(c.Files) == 0 {
		r.errf("%s", errs.ConfigEmpty)
	}
}

func (r *run) sitesUsingPHP(version string) []string {
	var out []string
	for _, s := range r.w.Snap.Sites {
		if s.PHP == version {
			out = append(out, s.Domain)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func splitExts(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseIntSlice 把 env 中的当前端口字符串转 []int（供 validatePort 的 exclude 用）
func parseIntSlice(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return []int{n}
}
