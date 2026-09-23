// service 组规则（6）：install / uninstall / service-stop / service-start / update-config / service-config
// 控制流严格对齐原型：早退处 push 后 return（对应 case 内 break），非早退错误继续累积
package preflight

import (
	"strconv"
	"strings"

	"phpo/internal/config"
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
			if pp := r.validatePort(s, nil, nil, conflictBlock); !pp.Ok {
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
		// 依赖站点与「最后一个版本」都只告警、不阻止（§0.2 规则 16 / §1.11 最小限制）：
		// 卸空 PHP 是合法诉求，把后果说清楚即可，拦下点击才是替程序员做决定。
		if used := r.sitesUsingPHP(c.Version); len(used) > 0 {
			r.warnf("以下站点正在使用 PHP %s：%s —— 卸载后这些站点的 vhost 上游失效", c.Version, strings.Join(used, ", "))
		}
	}
	if c.Kind == "nginx" && len(r.w.Snap.Sites) > 0 {
		// 与 PHP 同口径降级为警告（§1.11）：vhost 仍在盘上，重装 nginx 即恢复，不是不可逆后果
		r.warnf("%d 个站点依赖 Nginx，卸载后这些站点无法访问", len(r.w.Snap.Sites))
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
		// 当前端口一律按版本键读（config.FlatEnv 只产出 {KIND}_{VER}_PORT，不存在无关键的 NGINX_PORT）；
		// 读得出「自己现在占着哪个端口」，改回同一端口才不会被当成冲突（§5.8 服务端口占用报错的口径）
		cur := r.w.Snap.Env[config.EnvKeyPort(c.Kind, c.Version)]
		if pp := r.validatePort(asString(c.NewValue), parseIntSlice(cur), nil, conflictBlock); !pp.Ok {
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
