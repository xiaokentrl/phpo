// site 组规则（6）：site-add / site-remove / site-port / site-vhost / php-switch / rewrite
package preflight

import (
	"strings"

	"phpo/internal/config"
	"phpo/pkg/errs"
)

func (r *run) siteAdd() {
	c := r.c
	// 建站的唯一服务门禁是 nginx（未装则无法发布站点）；未运行只降级，PHP/MySQL 等其余服务缺失也只降级不阻断
	if len(r.w.Snap.Installed["nginx"]) == 0 {
		r.errf("%s", errs.NginxNeeded)
		return
	}
	if len(r.w.Snap.Running["nginx"]) == 0 {
		r.warnf("%s", nginxNotRunningWarn())
	}
	dd := config.ValidateDomain(c.Domain)
	if !dd.Ok {
		r.errf("%s", dd.Msg)
		return
	}
	if r.findSite(dd.Value) != nil {
		r.errf("%s: %s", errs.DomainExists, dd.Value)
	}
	// 站点端口冲突：保留用户所填端口、站点照常创建、配置照常落盘，只把这一个端口暂不发布（站点降级），
	// 不顺延、不阻断建站（§5.8）。被自己的 Nginx 发布过的端口不算占用，按 server_name 分流复用、连告警都不给。
	if c.Port != nil {
		warnRootlessPrivilegedPort(r, c.Port)
		pp := r.validatePort(asString(c.Port), nil, nil, conflictKeepWarn)
		if !pp.Ok {
			r.errf("%s", pp.Msg)
		} else if pp.Occupied {
			r.warnf("%s（站点仍会创建，站点配置照常落盘，只是这个端口暂不发布；腾出该端口或改用空闲端口后自动补齐）", pp.Msg)
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
	if !r.nginxServingWarn() {
		return
	}
	// FIX #5：改已有站点的端口仍支持自动顺延（排除自身域名与当前端口）
	pp := r.validatePort(asString(c.NewValue), []int{site.Port}, []string{c.Domain}, conflictAdvance)
	if !pp.Ok {
		r.errf("%s", pp.Msg)
	} else {
		// rootless 特权端口警告按「最终生效端口」判定：顺延结果也可能落在 <1024（占用表不知道 rootless 限制）
		warnRootlessPrivilegedPort(r, pp.Value)
		if pp.Adjusted {
			r.setAdjustedPort(pp.Value)
			r.warnf("%s", advanceMsg(pp.Original, pp.Value))
		}
	}
}

func (r *run) siteVhost() {
	c := r.c
	if r.findSite(c.Domain) == nil {
		r.errf("%s: %s", errs.SiteMissing, c.Domain)
		return
	}
	if !r.nginxServing() {
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
	if !r.nginxServingWarn() {
		return
	}
	if !contains(r.w.Snap.Installed["php"], c.NewPhp) {
		r.errf("%s: PHP %s", errs.NotInstalled, c.NewPhp)
		return
	}
	// 目标容器未运行只警告不阻止：切换链路会先自动启动它（备好上游再 reload nginx），启动失败才在任务里报错
	if !contains(r.w.Snap.Running["php"], c.NewPhp) {
		r.warnf("PHP %s 容器未运行，切换时将自动启动", c.NewPhp)
	}
}

func (r *run) rewrite() {
	c := r.c
	if r.findSite(c.Domain) == nil {
		r.errf("%s: %s", errs.SiteMissing, c.Domain)
		return
	}
	if !r.nginxServingWarn() {
		return
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

// nginxServing 手改正文（site-vhost）的服务门禁：nginx 必须已装且运行。
// 这一条只管「用户自己写的那份配置」——它没有模板兜底，写坏了 nginx 就起不来，所以必须先过运行中容器的
// nginx -t（硬红线 2），容器停了这条链必然失败，故拦截并给出可恢复的下一步。
// 模板生成的那三处（改端口 / 切 PHP / 伪静态）不走这里，改走 nginxServingWarn：没在跑只告警、照常落盘。
// 返回 false 表示已记错误，调用方应直接 return。
func (r *run) nginxServing() bool {
	if len(r.w.Snap.Installed["nginx"]) == 0 {
		r.errf("%s", errs.NginxNeeded)
		return false
	}
	if len(r.w.Snap.Running["nginx"]) == 0 {
		r.errf("%s", nginxNotServingErr())
		return false
	}
	return true
}

// nginxServingWarn 模板生成型改动（改端口 / 切 PHP / 伪静态）的服务门禁：Nginx 没在跑只告警、不拦。
// 这三处写的都是模板生成的配置，Nginx 没起来时先落盘、等它启动后重新校验再发布端口（硬红线 2 的登记例外，见 §3.3）；
// 手动改正文（site-vhost）不在此列——那是用户自己写的配置，必须先过运行中的 Nginx 校验。
// 返回 false 表示已记错误（Nginx 没装），调用方应直接 return。
func (r *run) nginxServingWarn() bool {
	if len(r.w.Snap.Installed["nginx"]) == 0 {
		r.errf("%s", errs.NginxNeeded)
		return false
	}
	if len(r.w.Snap.Running["nginx"]) == 0 {
		r.warnf("%s", nginxNotServingWarn())
	}
	return true
}

// nginxNotRunningWarn nginx 已装但未运行的建站降级告警；文案与前端 usePreflight.ts 逐字对齐
func nginxNotRunningWarn() string {
	return errs.NotRunning + ": Nginx（站点仍会创建，站点配置先落盘但未经 Nginx 校验；启动 Nginx 后重新校验并补齐端口发布）"
}

// nginxNotServingWarn 模板生成型改动（改端口 / 切 PHP / 伪静态）在 Nginx 未运行时的告警；文案与前端 usePreflight.ts 逐字对齐
func nginxNotServingWarn() string {
	return errs.NotRunning + ": Nginx（站点配置仍会落盘，但未经 Nginx 校验、端口暂不发布；启动 Nginx 后自动重新校验并补齐）"
}

// nginxNotServingErr nginx 未运行时的编辑站点拦截文案；文案与前端 usePreflight.ts 逐字对齐
func nginxNotServingErr() string {
	return errs.NotRunning + ": Nginx（vhost 改动须经运行中的 Nginx 校验后才能落盘，请先启动 Nginx 再重试）"
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
