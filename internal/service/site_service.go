// SiteService：站点新增/删除（T403）——严格三段式（preflight 由上层裁决 → task 执行 → Apply 落地并广播 state:changed）
// 建站：建目录 → 校验并写 vhost（硬红线 2）→ 加 hosts（不可写仅警告）→ 落库。删站：根目录入回收站（7 天）→ 删 vhost → 落库删除。
// 降级：PHP 未就绪或端口被占用时站点照建（目录/hosts/落库、端口原样保留），仅跳过 vhost 落盘与端口发布，后续写操作自愈（§5.8）。
// 域名零限制、根路径允许 WWW_ROOT 外（preflight 已降级为警告），建站幂等（重复建站不产生脏状态）。
package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
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
	store     SiteStore
	vhosts    *vhost.Manager
	hosts     steps.HostsOps
	trash     *engine.Trash
	validate  vhost.Validator
	reload    Reloader
	tasks     *task.Manager
	emitter   Emitter
	env       config.Env
	publisher NginxPublisher
	seq       atomic.Uint64
}

// Reloader 写盘后重载 nginx（真实实现走 docker exec；测试注入 noop）。nil 视为无需重载。
type Reloader interface {
	Reload(ctx context.Context) error
}

// NginxPublisher 重发布站点端口到 nginx 容器（真实实现：LifecycleService.RepublishNginx）。
// nil 表示不重发布（单测 / 无 nginx 环境），保持既有站点写链路不变。
type NginxPublisher interface {
	RepublishNginx(ctx context.Context, ports []int) error
}

func NewSiteService(st SiteStore, vh *vhost.Manager, hosts steps.HostsOps, trash *engine.Trash, validate vhost.Validator, reload Reloader, tm *task.Manager, emitter Emitter, env config.Env) *SiteService {
	return &SiteService{store: st, vhosts: vh, hosts: hosts, trash: trash, validate: validate, reload: reload, tasks: tm, emitter: emitter, env: env}
}

// SetNginxPublisher 注入端口重发布器（di 装配期调用）；未注入则站点写链路不触 nginx 重建。
func (s *SiteService) SetNginxPublisher(p NginxPublisher) { s.publisher = p }

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
	stepsList := []task.Step{steps.NewPrepareSiteDir("创建站点目录", s.env, site.Root)}
	// 降级态：所选 PHP 未装（上游 php-{ver}-fpm:9000 无法解析，nginx -t 必失败，硬红线 2）
	// 或所选端口已被占用（发布即让 nginx 绑不上）。两者都只跳过「写 vhost + 发布端口」，
	// 站点目录/hosts/落库照常、端口原样保留；经 SwitchPHP / SetPort / SetRewrite 的 writeVHost 链路自愈。
	ready := s.serveReady(site)
	if ready {
		stepsList = append(stepsList, steps.NewWriteVHost("生成 vhost", s.vhosts, s.validate, domain, content))
	}
	stepsList = append(stepsList, steps.NewAddHosts("写入 hosts", s.hosts, domain))
	if ready {
		if rp := s.republishStep(s.publishPorts(mustSites(s.store), domain, site.Port)); rp != nil {
			stepsList = append(stepsList, rp)
		}
	}
	t := &task.Task{
		ID:    s.newID("site-add"),
		Label: "创建站点 " + domain,
		Meta:  model.TaskMeta{Type: "site-add", Domain: domain},
		Steps: stepsList,
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
	stepsList := []task.Step{
		trashStep,
		steps.NewDeleteVHostFile("移除 vhost", s.vhosts, domain, prevContent),
	}
	if rp := s.republishStep(s.publishPorts(mustSites(s.store), domain, 0)); rp != nil {
		stepsList = append(stepsList, rp)
	}
	t := &task.Task{
		ID:    s.newID("site-remove"),
		Label: "删除站点 " + domain,
		Meta:  model.TaskMeta{Type: "site-remove", Domain: domain},
		Steps: stepsList,
		Apply: func() error {
			trashPath = trashStep.TrashPath()
			return s.commitRemove(domain, site.Root, trashPath)
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// ReconcileServe 补齐降级站点：nginx 就绪（安装/启动）后，把「本应对外服务但 conf 缺失」的站点正文写盘、
// 重载 nginx 并重发布站点端口。幂等：已落盘或仍不就绪（PHP 缺失 / 端口被占）的站点跳过，不重复触发发布。
// 单站写失败不阻断其余站点，失败域名聚合为一个错误交由调用方记日志——站点维持降级，后续任一站点写操作仍可自愈。
// 由 nginx 安装/启动任务内联调用，故自身不再产出 task（task.Manager 单飞，嵌套运行会 ErrBusy）。
func (s *SiteService) ReconcileServe(ctx context.Context) error {
	sites, err := s.store.ListSites()
	if err != nil {
		return err
	}
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return err
	}
	s.vhosts.Sync(sites)

	var healed, failed []string
	for _, st := range sites {
		if !siteServeReady(snap, st) {
			continue
		}
		if _, e := os.Stat(s.vhosts.Path(st.Domain)); e == nil {
			continue // 已落盘，无需补齐
		}
		content := s.vhosts.Get(st.Domain) // 取缓存正文（保留手改），无缓存则按站点重算
		if content == "" {
			continue
		}
		if e := s.vhosts.Save(ctx, s.validate, st.Domain, content); e != nil {
			failed = append(failed, st.Domain)
			continue
		}
		healed = append(healed, st.Domain)
	}
	if len(healed) == 0 {
		if len(failed) > 0 {
			return fmt.Errorf("部分站点 vhost 补齐失败: %s", strings.Join(failed, ", "))
		}
		return nil
	}
	if s.reload != nil {
		if err := s.reload.Reload(ctx); err != nil {
			return fmt.Errorf("补齐 %d 个站点后重载 nginx 失败: %w", len(healed), err)
		}
	}
	if s.publisher != nil {
		if err := s.publisher.RepublishNginx(ctx, s.publishPorts(sites, "", 0)); err != nil {
			return err
		}
	}
	if err := s.emit(); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("已补齐 %s，但部分站点 vhost 补齐失败: %s", strings.Join(healed, ", "), strings.Join(failed, ", "))
	}
	return nil
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

// serveReady 判定站点能否立即对外服务：nginx 已装且在运行、所选 PHP 已安装（否则 nginx -t 必失败，
// 硬红线 2）、所选端口未被占用（占用判据与 preflight 同源——store.CollectUsedPorts，排除自身域名）。
// 权威快照不可读按未就绪处理（宁可降级也不写出跑不通的 vhost）。
func (s *SiteService) serveReady(site model.Site) bool {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return false
	}
	return siteServeReady(snap, site)
}

// siteServeReady 就绪判定的纯函数版：复用同一份快照，供建站与补齐共用
func siteServeReady(snap *model.Snapshot, site model.Site) bool {
	if len(snap.Installed["nginx"]) == 0 || len(snap.Running["nginx"]) == 0 {
		return false
	}
	if !snap.HasVersion("php", site.PHP) {
		return false
	}
	_, occupied := store.CollectUsedPorts(snap, []string{site.Domain})[site.Port]
	return !occupied
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

// AddHosts 手动补写系统 hosts（站点列表「加 hosts」按钮）：建站时提权被拒的站点靠此自愈。
// 返回值是给 UI 的人话警告：空串 = 已生效或本就幂等；非空 = 未生效原因（如需以管理员身份运行），
// 按 §3.2 原则 7「能警告的不要阻止」不作为 error。
// 不新增 preflight action（§0.3 冻结 17 条）：此处唯一前置是站点存在，无端口/路径/版本可裁决；
// 仍走 task 三段式（硬红线 5），Apply 段广播快照，hosts 列由后端探针回流（硬红线 4）。
func (s *SiteService) AddHosts(ctx context.Context, domain string) (string, error) {
	sites, err := s.store.ListSites()
	if err != nil {
		return "", err
	}
	found := false
	for _, st := range sites {
		if st.Domain == domain {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("站点不存在: %s", domain)
	}
	warning := ""
	t := &task.Task{
		ID:    s.newID("hosts"),
		Label: "加 hosts · " + domain,
		Meta:  model.TaskMeta{Type: "hosts-add", Domain: domain},
		Steps: []task.Step{&task.FuncStep{StepName: "写入 hosts", Exec: func(_ context.Context, log task.StepLog) error {
			res, e := s.hosts.Add(domain)
			if e != nil {
				log.Log("err", "hosts 写入失败: "+e.Error())
				return e
			}
			if res.Warning != "" {
				log.Log("err", res.Warning)
				warning = res.Warning
				return nil
			}
			if res.Changed {
				log.Log("ok", "已添加 hosts: 127.0.0.1 "+domain)
			} else {
				log.Log("dim", "hosts 已存在，跳过")
			}
			return nil
		}}},
		Apply: func() error { return s.emit() },
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return "", err
	}
	return warning, nil
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
	if rp := s.republishStep(s.publishPorts(sites, domain, updated.Port)); rp != nil {
		stepList = append(stepList, rp)
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

// republishStep 产出「重发布站点端口到 nginx」步骤；publisher 为 nil 时返回 nil（调用方据此不加入任务）。
func (s *SiteService) republishStep(ports []int) task.Step {
	if s.publisher == nil {
		return nil
	}
	return &task.FuncStep{StepName: "发布站点端口到 Nginx", Exec: func(ctx context.Context, _ task.StepLog) error {
		return s.publisher.RepublishNginx(ctx, ports)
	}}
}

// publishPorts 汇总应发布给 nginx 的宿主端口：只计入 vhost 已落盘的站点，并剔除数据服务占用的端口。
// 降级站点（PHP 未就绪 / 端口被占）库里仍记着端口，但没有 conf——发布出去只会让 nginx 容器去抢绑
// 一个没人服务、甚至已被 mysql 等占用的宿主端口，绑不上即全站瘫痪（§5.8）。
func (s *SiteService) publishPorts(sites []model.Site, exceptDomain string, addPort int) []int {
	out := make([]model.Site, 0, len(sites))
	for _, st := range sites {
		if _, err := os.Stat(s.vhosts.Path(st.Domain)); err != nil {
			continue
		}
		out = append(out, st)
	}
	ports := sitePorts(out, exceptDomain, addPort)
	svc := map[int]bool{}
	if snap, err := s.store.BuildSnapshot(); err == nil {
		for p := range store.CollectServicePorts(snap) {
			svc[p] = true
		}
	}
	keep := make([]int, 0, len(ports))
	for _, p := range ports {
		if !svc[p] {
			keep = append(keep, p)
		}
	}
	return keep
}

// sitePorts 汇总应发布端口并集：取现有站点端口（排除 exceptDomain），再并入 addPort（<=0 忽略）；去重。
func sitePorts(sites []model.Site, exceptDomain string, addPort int) []int {
	seen := make(map[int]bool, len(sites)+1)
	out := make([]int, 0, len(sites)+1)
	for _, st := range sites {
		if st.Domain == exceptDomain || st.Port <= 0 || seen[st.Port] {
			continue
		}
		seen[st.Port] = true
		out = append(out, st.Port)
	}
	if addPort > 0 && !seen[addPort] {
		out = append(out, addPort)
	}
	return out
}
