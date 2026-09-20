// site 组规则（6）：site-add / site-remove / site-port / site-vhost / php-switch / rewrite
package preflight

import (
	"strings"

	"phpo/internal/config"
	"phpo/pkg/errs"
)

func (r *run) siteAdd() {
	c := r.c
	// 建站不设任何服务门禁：nginx 未就绪（未装或未运行）只降级告警，站点照常创建（§5.8）
	if len(r.w.Snap.Installed["nginx"]) == 0 {
		r.warnf("%s", nginxPendingWarn(errs.NotInstalled))
	} else if len(r.w.Snap.Running["nginx"]) == 0 {
		r.warnf("%s", nginxPendingWarn(errs.NotRunning))
	}
	dd := config.ValidateDomain(c.Domain)
	if !dd.Ok {
		r.errf("%s", dd.Msg)
		return
	}
	if r.findSite(dd.Value) != nil {
		r.errf("%s: %s", errs.DomainExists, dd.Value)
	}
	// 站点端口冲突：保留用户所填端口，只告警并降级（vhost 暂不落盘、端口暂不发布），不顺延、不阻断建站（§5.8）
	if c.Port != nil {
		pp := r.validatePort(asString(c.Port), nil, nil, conflictKeepWarn)
		if !pp.Ok {
			r.errf("%s", pp.Msg)
		} else if pp.Occupied {
			r.warnf("%s（站点仍会创建，但端口暂不发布、vhost 暂不落盘；腾出该端口或改用空闲端口后生效）", pp.Msg)
		}
	}
	// 无可用 PHP：站点仍建，但 vhost 暂不落盘（上游容器不存在则 nginx -t 必失败，硬红线 2）
	if !r.w.Snap.HasVersion("php", c.PHP) {
		r.warnf("%s: PHP %s（站点仍会创建，安装或切换到可用 PHP 版本后生效）", errs.NotInstalled, phpLabel(c.PHP))
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
	// FIX #5：改已有站点的端口仍支持自动顺延（排除自身域名与当前端口）
	pp := r.validatePort(asString(c.NewValue), []int{site.Port}, []string{c.Domain}, conflictAdvance)
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

// nginxPendingWarn nginx 未就绪（未装 / 未运行）的建站降级告警；文案与前端 usePreflight.ts 逐字对齐
func nginxPendingWarn(code string) string {
	act := "安装"
	if code == errs.NotRunning {
		act = "启动"
	}
	return code + ": Nginx（站点仍会创建，vhost 暂不落盘、端口暂不发布；" + act + " Nginx 后自动补齐）"
}

func advanceMsg(from, to int) string {
	return strings.NewReplacer(
		"{from}", itoa(from),
		"{to}", itoa(to),
	).Replace(errs.PortAdvance)
}

// phpLabel 告警文案用：未选版本时给出可读占位
func phpLabel(php string) string {
	if php == "" {
		return "未指定"
	}
	return php
}
