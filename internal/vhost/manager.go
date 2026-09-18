// vhost 管理器：VHosts 九方法后端化（compute/get/regenerate/setContent/applyPort/applyPhp/applyRewrite/remove/rebuildAll）
// 纯内容层：只依赖 config（路径映射）与 template（渲染），缓存站点列表 + 已改写正文；文件落盘在 sync.go
package vhost

import (
	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/template"
)

// Manager 维护 vhost 正文缓存与站点元数据；env 提供 host↔container 路径映射
type Manager struct {
	env    config.Env
	sites  map[string]*model.Site // 按域名索引的运行态站点
	vhosts map[string]string      // 域名 → 当前 vhost 正文（手改或计算）
}

// New 构造空管理器；站点缓存由 Sync 从权威快照灌入
func New(env config.Env) *Manager {
	return &Manager{env: env, sites: map[string]*model.Site{}, vhosts: map[string]string{}}
}

// Sync 用权威站点列表替换缓存（保留仍存在的域名的已改写正文与 customized 标记）
func (m *Manager) Sync(sites []model.Site) {
	next := make(map[string]*model.Site, len(sites))
	for i := range sites {
		st := sites[i]
		if old, ok := m.sites[st.Domain]; ok {
			st.VhostCustomized = old.VhostCustomized
			st.RewriteRule = orKeep(st.RewriteRule, old.RewriteRule)
		}
		next[st.Domain] = &st
	}
	m.sites = next
	// 清理孤儿正文
	for d := range m.vhosts {
		if _, ok := next[d]; !ok {
			delete(m.vhosts, d)
		}
	}
}

func orKeep(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// compute 复刻 defaultVhost：规则取 preset（custom 非空用之），root 映射到容器路径，上游 php-{ver}-fpm
func (m *Manager) compute(site *model.Site) string {
	if site == nil {
		return ""
	}
	rule := RuleFor(site.Rewrite, site.RewriteRule)
	content, err := template.RenderVhost(template.VhostInput{
		Domain:        site.Domain,
		Port:          site.Port,
		ContainerRoot: m.env.HostToContainer(site.Root),
		Upstream:      "php-" + site.PHP + "-fpm",
		Rule:          rule,
	})
	if err != nil {
		return ""
	}
	return content
}

// Compute 暴露纯计算（不落缓存），供 preflight 预览与测试
func (m *Manager) Compute(site model.Site) string {
	st := site
	return m.compute(&st)
}

// Get 返回缓存正文；无缓存则即时计算（VHosts.get）
func (m *Manager) Get(domain string) string {
	site := m.sites[domain]
	if site == nil {
		return ""
	}
	if c, ok := m.vhosts[domain]; ok && c != "" {
		return c
	}
	return m.compute(site)
}

// Regenerate 丢弃手改、按站点重算正文（VHosts.regenerate）
func (m *Manager) Regenerate(domain string) string {
	site := m.sites[domain]
	if site == nil {
		delete(m.vhosts, domain)
		return ""
	}
	m.vhosts[domain] = m.compute(site)
	site.VhostCustomized = false
	return m.vhosts[domain]
}

// SetContent 手改正文落缓存，并回读 port/root 同步站点元数据（VHosts.setContent）
func (m *Manager) SetContent(domain, content string) string {
	site := m.sites[domain]
	if site == nil {
		return ""
	}
	m.vhosts[domain] = content
	site.VhostCustomized = true
	p := Parse(content)
	if p.Port != 0 {
		site.Port = p.Port
	}
	if p.Root != "" {
		site.Root = m.env.ContainerToHost(p.Root)
	}
	return content
}

// ApplyPort 改端口：手改态就地替换 listen，否则重算（VHosts.applyPort）
func (m *Manager) ApplyPort(domain string, port int) string {
	site := m.sites[domain]
	if site == nil {
		return ""
	}
	site.Port = port
	if site.VhostCustomized {
		if c, ok := m.vhosts[domain]; ok && c != "" {
			m.vhosts[domain] = ReplaceListen(c, port)
			return m.vhosts[domain]
		}
	}
	m.vhosts[domain] = m.compute(site)
	site.VhostCustomized = false
	return m.vhosts[domain]
}

// ApplyPhp 切 PHP：手改态就地替换上游（硬红线 1），否则重算（VHosts.applyPhp）
func (m *Manager) ApplyPhp(domain, php string) string {
	site := m.sites[domain]
	if site == nil {
		return ""
	}
	site.PHP = php
	if site.VhostCustomized {
		if c, ok := m.vhosts[domain]; ok && c != "" {
			m.vhosts[domain] = ReplacePhpUpstream(c, php)
			return m.vhosts[domain]
		}
	}
	m.vhosts[domain] = m.compute(site)
	site.VhostCustomized = false
	return m.vhosts[domain]
}

// ApplyRewrite 改伪静态：更新预设与自定义规则后重算（VHosts.applyRewrite）
func (m *Manager) ApplyRewrite(domain, preset, rule string) string {
	site := m.sites[domain]
	if site == nil {
		return ""
	}
	site.Rewrite = preset
	site.RewriteRule = rule
	m.vhosts[domain] = m.compute(site)
	site.VhostCustomized = false
	return m.vhosts[domain]
}

// Site 返回缓存中的站点元数据副本（含 SetContent 回读后的 port/root）；不存在返回 false
func (m *Manager) Site(domain string) (model.Site, bool) {
	site := m.sites[domain]
	if site == nil {
		return model.Site{}, false
	}
	return *site, true
}

// Remove 移除某域名的 vhost 缓存（VHosts.remove）
func (m *Manager) Remove(domain string) {
	delete(m.vhosts, domain)
}

// RebuildAll 清孤儿 + 对非手改站点重算（VHosts.rebuildAll）
func (m *Manager) RebuildAll() {
	for d := range m.vhosts {
		if _, ok := m.sites[d]; !ok {
			delete(m.vhosts, d)
		}
	}
	for domain, site := range m.sites {
		cached, has := m.vhosts[domain]
		if !site.VhostCustomized || !has || cached == "" {
			m.vhosts[domain] = m.compute(site)
			site.VhostCustomized = false
		}
	}
}
