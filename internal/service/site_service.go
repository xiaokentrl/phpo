// SiteService：站点新增/删除（T403）——严格三段式（preflight 由上层裁决 → task 执行 → Apply 落地并广播 state:changed）
// 建站：建目录 → 校验并写 vhost（硬红线 2）→ 加 hosts（不可写仅警告）→ 落库。删站：根目录入回收站（7 天）→ 删 vhost → 落库删除。
// 域名零限制、根路径允许 WWW_ROOT 外（preflight 已降级为警告），建站幂等（重复建站不产生脏状态）。
package service

import (
	"context"
	"fmt"
	"path"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/internal/vhost"
)

// SiteStore 站点权威读写子集（*store.Store 满足）
type SiteStore interface {
	ListSites() ([]model.Site, error)
	UpsertSite(model.Site) error
	DeleteSite(domain string) error
	AddTrashItem(store.TrashItem) (int64, error)
	BuildSnapshot() (*model.Snapshot, error)
}

// SiteService 组合 vhost/hosts/回收站/任务引擎，落地站点生命周期
type SiteService struct {
	store    SiteStore
	vhosts   *vhost.Manager
	hosts    steps.HostsOps
	trash    *engine.Trash
	validate vhost.Validator
	reload   Reloader
	tasks    *task.Manager
	emitter  Emitter
	env      config.Env
	seq      atomic.Uint64
}

// Reloader 写盘后重载 nginx（真实实现走 docker exec；测试注入 noop）。nil 视为无需重载。
type Reloader interface {
	Reload(ctx context.Context) error
}

func NewSiteService(st SiteStore, vh *vhost.Manager, hosts steps.HostsOps, trash *engine.Trash, validate vhost.Validator, reload Reloader, tm *task.Manager, emitter Emitter, env config.Env) *SiteService {
	return &SiteService{store: st, vhosts: vh, hosts: hosts, trash: trash, validate: validate, reload: reload, tasks: tm, emitter: emitter, env: env}
}

// AddInput 建站入参；Root 为空时回落 {WWW_ROOT}/{domain}，Port 为 0 时回落 80
type AddInput struct {
	Domain      string
	Port        int
	PHP         string
	Root        string
	Rewrite     string
	RewriteRule string
}

// Add 幂等建站：产出一个多步 task，经 task.Manager 执行；Apply 段落库并广播 state:changed
func (s *SiteService) Add(ctx context.Context, in AddInput) error {
	site := s.normalize(in)
	content := s.vhostContent(site)

	domain := site.Domain
	t := &task.Task{
		ID:    s.newID("site-add"),
		Label: "创建站点 " + domain,
		Meta:  model.TaskMeta{Type: "site-add", Domain: domain},
		Steps: []task.Step{
			steps.NewPrepareSiteDir("创建站点目录", s.env, site.Root),
			steps.NewWriteVHost("生成 vhost", s.vhosts, s.validate, domain, content),
			steps.NewAddHosts("写入 hosts", s.hosts, domain),
		},
		Apply: func() error { return s.commitUpsert(site) },
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// Remove 删站：根目录入回收站（保留 7 天）→ 删 vhost 文件 → 落库删除；源码目录可经回收站恢复
func (s *SiteService) Remove(ctx context.Context, domain string) error {
	site, ok := s.find(domain)
	if !ok {
		return fmt.Errorf("站点不存在: %s", domain)
	}
	prevContent := s.vhosts.Get(domain)

	var trashPath string
	trashStep := steps.NewTrashSiteDir("站点目录入回收站", s.trash, site.Root)
	t := &task.Task{
		ID:    s.newID("site-remove"),
		Label: "删除站点 " + domain,
		Meta:  model.TaskMeta{Type: "site-remove", Domain: domain},
		Steps: []task.Step{
			trashStep,
			steps.NewDeleteVHostFile("移除 vhost", s.vhosts, domain, prevContent),
		},
		Apply: func() error {
			trashPath = trashStep.TrashPath()
			return s.commitRemove(domain, site.Root, trashPath)
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// ---- 内部助手 ----

// normalize 填充默认根路径与端口，并归一化域名（零限制：不校验字符集）
func (s *SiteService) normalize(in AddInput) model.Site {
	port := in.Port
	if port == 0 {
		port = config.DefaultSitePort
	}
	root := in.Root
	if root == "" {
		root = path.Join(s.env.WWWRoot, in.Domain)
	}
	rewrite := in.Rewrite
	if rewrite == "" {
		rewrite = "none"
	}
	return model.Site{
		Domain: in.Domain, Port: port, PHP: in.PHP, Root: root,
		Rewrite: rewrite, RewriteRule: in.RewriteRule,
	}
}

// vhostContent 把新站点并入管理器缓存后取其 vhost 正文（供写盘）
func (s *SiteService) vhostContent(site model.Site) string {
	sites, _ := s.store.ListSites()
	merged := make([]model.Site, 0, len(sites)+1)
	for _, st := range sites {
		if st.Domain != site.Domain {
			merged = append(merged, st)
		}
	}
	merged = append(merged, site)
	s.vhosts.Sync(merged)
	return s.vhosts.Get(site.Domain)
}

// find 从权威库取站点
func (s *SiteService) find(domain string) (model.Site, bool) {
	sites, _ := s.store.ListSites()
	for _, st := range sites {
		if st.Domain == domain {
			return st, true
		}
	}
	return model.Site{}, false
}

// ---- vhost 写操作（改端口 / 切 PHP / 伪静态 / 手改正文）----
// 统一走三段式：mutate 计算新正文 + 更新管理器内站点元数据 → task[写盘(校验)→reload] → Apply 落库并广播

// SetPort 改站点端口（T404）
func (s *SiteService) SetPort(ctx context.Context, domain string, port int) error {
	return s.writeVHost(ctx, "site-port", "改端口 "+domain, func(m *vhost.Manager) string {
		return m.ApplyPort(domain, port)
	}, domain)
}

// SwitchPHP 切换 PHP（T405，硬红线 1 精确上游）
func (s *SiteService) SwitchPHP(ctx context.Context, domain, php string) error {
	return s.writeVHost(ctx, "php-switch", "切换 PHP "+php+" · "+domain, func(m *vhost.Manager) string {
		return m.ApplyPhp(domain, php)
	}, domain)
}

// SetRewrite 改伪静态（T406）
func (s *SiteService) SetRewrite(ctx context.Context, domain, preset, rule string) error {
	return s.writeVHost(ctx, "rewrite", "伪静态 "+preset+" · "+domain, func(m *vhost.Manager) string {
		return m.ApplyRewrite(domain, preset, rule)
	}, domain)
}

// SetVhostContent 手改 vhost 正文（T406）；写前 nginx -t 必过（硬红线 2）
func (s *SiteService) SetVhostContent(ctx context.Context, domain, content string) error {
	return s.writeVHost(ctx, "site-vhost", "编辑 vhost · "+domain, func(m *vhost.Manager) string {
		return m.SetContent(domain, content)
	}, domain)
}

// writeVHost 通用编排：把权威站点灌入管理器→mutate 得新正文→写盘(校验)+reload→落库+广播
func (s *SiteService) writeVHost(ctx context.Context, op, label string, mutate func(*vhost.Manager) string, domain string) error {
	sites, _ := s.store.ListSites()
	s.vhosts.Sync(sites)
	content := mutate(s.vhosts)
	if content == "" {
		return fmt.Errorf("站点不存在: %s", domain)
	}
	updated, ok := s.vhosts.Site(domain)
	if !ok {
		return fmt.Errorf("站点状态缺失: %s", domain)
	}
	stepList := []task.Step{
		steps.NewWriteVHost("写入 vhost", s.vhosts, s.validate, domain, content),
		&task.FuncStep{StepName: "重载 Nginx", Exec: func(ctx context.Context, _ task.StepLog) error {
			if s.reload == nil {
				return nil
			}
			return s.reload.Reload(ctx)
		}},
	}
	t := &task.Task{
		ID:    s.newID(op),
		Label: label,
		Meta:  model.TaskMeta{Type: op, Domain: domain},
		Steps: stepList,
		Apply: func() error {
			if err := s.store.UpsertSite(updated); err != nil {
				return err
			}
			return s.emit()
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// commitUpsert applyStateChange：写库 + 校准缓存 + 广播新快照
func (s *SiteService) commitUpsert(site model.Site) error {
	if err := s.store.UpsertSite(site); err != nil {
		return err
	}
	s.vhosts.Sync(append(mustSites(s.store), site))
	s.vhosts.Regenerate(site.Domain)
	return s.emit()
}

// commitRemove applyStateChange：登记回收站条目 + 删库 + 清 vhost 缓存 + 广播
func (s *SiteService) commitRemove(domain, origRoot, trashPath string) error {
	if trashPath != "" {
		if _, err := s.store.AddTrashItem(store.TrashItem{Kind: "site", OrigPath: origRoot, TrashPath: trashPath}); err != nil {
			return err
		}
	}
	if err := s.store.DeleteSite(domain); err != nil {
		return err
	}
	s.vhosts.Remove(domain)
	s.vhosts.Sync(mustSites(s.store))
	return s.emit()
}

// emit 拉取权威快照并广播 state:changed（硬红线 4：后端唯一权威）
func (s *SiteService) emit() error {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return err
	}
	s.emitter.Emit("state:changed", map[string]any{"snapshot": snap})
	return nil
}

func mustSites(st SiteStore) []model.Site {
	sites, _ := st.ListSites()
	if sites == nil {
		return []model.Site{}
	}
	return sites
}

func (s *SiteService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}
