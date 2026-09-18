// site 组规则（6）：site-add / site-remove / site-port / site-vhost / php-switch / rewrite
package preflight

import (
	"strings"

	"phpo/internal/config"
	"phpo/pkg/errs"
)

func (r *run) siteAdd() {
	c := r.c
	if len(r.w.Snap.Installed["php"]) == 0 {
		r.errf("%s", errs.PhpNeeded)
		return
	}
	if len(r.w.Snap.Installed["nginx"]) == 0 {
		r.errf("%s", errs.NginxNeeded)
		return
	}
	dd := config.ValidateDomain(c.Domain)
	if !dd.Ok {
		r.errf("%s", dd.Msg)
		return
	}
	if r.findSite(dd.Value) != nil {
		r.errf("%s: %s", errs.DomainExists, dd.Value)
	}
	// 站点端口冲突：顺延首个可用空位（无窗口上限，不报错）
	if c.Port != nil {
		pp := r.validatePort(asString(c.Port), nil, nil, true)
		if !pp.Ok {
			r.errf("%s", pp.Msg)
		} else if pp.Adjusted {
			r.setAdjustedPort(pp.Value)
			r.warnf("%s", advanceMsg(pp.Original, pp.Value))
		}
	}
	if c.PHP != "" && !contains(r.w.Snap.Installed["php"], c.PHP) {
		r.errf("%s: PHP %s", errs.NotInstalled, c.PHP)
	}
	if c.Root != "" {
		rr := config.ValidateSiteRoot(c.Root, r.w.Snap.Env["WWW_ROOT"])
		if !rr.Ok {
			r.errf("%s", rr.Msg)
		} else {
			// FIX #4：WWW_ROOT 之外为 warning 而非 error
			if rr.OutsideWww {
				r.warnf("%s: %s", errs.RootOutsideWww, rr.Value)
			}
			if r.siteRootUsed(rr.Value) {
				r.errf("%s: %s", errs.RootDuplicated, rr.Value)
			}
		}
	}
}

func (r *run) siteRemove() {
	if r.findSite(r.c.Domain) == nil {
		r.errf("%s: %s", errs.SiteMissing, r.c.Domain)
	}
}

func (r *run) sitePort() {
	c := r.c
	site := r.findSite(c.Domain)
	if site == nil {
		r.errf("%s: %s", errs.SiteMissing, c.Domain)
		return
	}
	// FIX #5：站点端口编辑同样支持自动顺延（排除自身域名与当前端口）
	pp := r.validatePort(asString(c.NewValue), []int{site.Port}, []string{c.Domain}, true)
	if !pp.Ok {
		r.errf("%s", pp.Msg)
	} else if pp.Adjusted {
		r.setAdjustedPort(pp.Value)
		r.warnf("%s", advanceMsg(pp.Original, pp.Value))
	}
}

func (r *run) siteVhost() {
	c := r.c
	if r.findSite(c.Domain) == nil {
		r.errf("%s: %s", errs.SiteMissing, c.Domain)
		return
	}
	if c.Content != nil && strings.TrimSpace(*c.Content) == "" {
		r.errf("%s", errs.ConfigEmpty)
	}
	if c.PHP != "" && !contains(r.w.Snap.Installed["php"], c.PHP) {
		r.warnf("%s: PHP %s（站点配置仍可保存，但需安装该版本才能生效）", errs.NotInstalled, c.PHP)
	}
}

func (r *run) phpSwitch() {
	c := r.c
	if r.findSite(c.Domain) == nil {
		r.errf("%s: %s", errs.SiteMissing, c.Domain)
		return
	}
	if !contains(r.w.Snap.Installed["php"], c.NewPhp) {
		r.errf("%s: PHP %s", errs.NotInstalled, c.NewPhp)
	}
}

func (r *run) rewrite() {
	c := r.c
	if r.findSite(c.Domain) == nil {
		r.errf("%s: %s", errs.SiteMissing, c.Domain)
		return
	}
	if len(r.w.Snap.Installed["nginx"]) == 0 {
		r.errf("%s", errs.NginxNeeded)
	}
}

func (r *run) findSite(domain string) *siteView {
	for i := range r.w.Snap.Sites {
		if r.w.Snap.Sites[i].Domain == domain {
			s := r.w.Snap.Sites[i]
			return &siteView{Domain: s.Domain, Port: s.Port, Root: s.Root}
		}
	}
	return nil
}

func (r *run) siteRootUsed(normRoot string) bool {
	for _, s := range r.w.Snap.Sites {
		if config.NormPath(s.Root) == normRoot {
			return true
		}
	}
	return false
}

type siteView struct {
	Domain string
	Port   int
	Root   string
}

func advanceMsg(from, to int) string {
	return strings.NewReplacer(
		"{from}", itoa(from),
		"{to}", itoa(to),
	).Replace(errs.PortAdvance)
}
