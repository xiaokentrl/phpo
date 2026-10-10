// SiteService：站点新增/删除（T403）——严格三段式（preflight 由上层裁决 → task 执行 → Apply 落地并广播 state:changed）
// 建站：建目录 → 校验并写 vhost（硬红线 2）→ 加 hosts（不可写仅警告）→ 落库。删站：根目录入回收站（7 天）→ 删 vhost → 落库删除。
// 降级分两种（§5.8）：① PHP 未装或 nginx 未装——上游解析不了、连校验都问不到，vhost 暂不落盘（目录/hosts/落库照常、端口原样保留）；
// ② 端口被「不能确认是自己站点」的东西占着——vhost 照常落盘，只是这一个宿主端口暂不发布，站点标为降级并提示占用者。
// 两种都会在后续任一站点写操作、或 Nginx 启动后的补齐里自愈。
// 域名零限制、根路径允许 WWW_ROOT 外（preflight 已降级为警告），建站幂等（重复建站不产生脏状态）。
package service

import (
	"context"
	"errors"
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
	portpkg "phpo/pkg/port"
)

// SiteStore 站点权威读写子集（*store.Store 满足）
type SiteStore interface {
	ListSites() ([]model.Site, error)
	UpsertSite(model.Site) error
	DeleteSite(domain string) error
	AddTrashItem(store.TrashItem) (int64, error)
	BuildSnapshot() (*model.Snapshot, error)
	SetSitePortBlocks(map[string]string)
	SetSiteOrder(domains []string) error
}

// SiteService 组合 vhost/hosts/回收站/任务引擎，落地站点生命周期
type SiteService struct {
	store      SiteStore
	vhosts     *vhost.Manager
	hosts      steps.HostsOps
	trash      *engine.Trash
	validate   vhost.Validator
	reload     Reloader
	tasks      *task.Manager
	emitter    Emitter
	env        config.Env
	publisher  NginxPublisher
	phpStarter PHPStarter
	portBinder PortBinder
	portBlocks map[string]string
	seq        atomic.Uint64
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

// PortBinder 问一次「这些宿主端口此刻绑不绑得上」，返回 端口 → 占用者的人话描述。
// 已由自家 nginx 发布的那些不在询问范围——Docker 本来就占着它，再探必自我误判。
type PortBinder interface {
	BlockedSitePorts(ctx context.Context, ports []int) map[int]string
}

// SetPortBinder 注入宿主端口实测绑定器（di 装配期调用）；未注入则只做逻辑档位，不实测。
func (s *SiteService) SetPortBinder(b PortBinder) { s.portBinder = b }

// PHPStarter 确保 php/{version} 容器在跑（真实实现走 LifecycleService.Start：StartContainer 幂等、
// 稳定 running 验证、状态落库广播）；nil 表示无法启动（单测 / 未注入），切换链路仅跳过该步。
type PHPStarter func(ctx context.Context, version string, log task.StepLog) error

// SetPHPStarter 注入切换目标容器的启动器（di 装配期调用）。
func (s *SiteService) SetPHPStarter(fn PHPStarter) { s.phpStarter = fn }

// ensurePHPRunningStep 切换目标版本的上游备妥步：排在写盘之前——先让上游就绪再 reload nginx，
// 否则切换完成到容器就绪之间站点是 502 窗口。starter 未注入时是空步（老测试零改动）。
func (s *SiteService) ensurePHPRunningStep(version string) task.Step {
	return &task.FuncStep{
		BaseStep: task.BaseStep{StepName: "启动 PHP " + version + "（切换目标）"},
		Exec: func(ctx context.Context, log task.StepLog) error {
			if s.phpStarter == nil {
				return nil
			}
			return s.phpStarter(ctx, version, log)
		},
	}
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
	stepsList := []task.Step{steps.NewPrepareSiteDir("创建站点目录", s.env, site.Root)}
	// 建站即在站点根目录放一份示例 index.php：站点没有自己的源码时，访问者看到的是这份而不是 nginx 的默认页。
	// 已存在（用户自己的源码）一律不覆盖——这一步无条件下发，降级态也有站点目录，照样该有入口页。
	stepsList = append(stepsList, steps.NewWriteSiteIndex("写入 index.php", site.Root))
	// 两种降级分开处理（§5.8）：① PHP 未装或 nginx 未装——连 nginx -t 都问不到，vhost 暂不落盘（目录/hosts/落库照常、端口原样保留）；
	// ② 端口被「不能确认是自己站点」的东西占着——vhost 照常落盘，只是这一个宿主端口暂不发布，
	// 站点标为降级并在日志里如实说出占用者（不阻断、不改用户所填端口）。两种都经后续任一站点写操作或 Nginx 启动后的补齐自愈。
	snap, landReady := s.landReady(site)
	if landReady {
		stepsList = append(stepsList, steps.NewWriteVHost("生成 vhost", s.vhosts, s.validatorFor(snap, false), domain, content))
	}
	stepsList = append(stepsList, steps.NewAddHosts("写入 hosts", s.hosts, domain))
	if landReady {
		ports, blocked := s.publishPlan(ctx, mustSites(s.store), domain, site.Port)
		if rp := s.republishStep(ports, blocked[domain]); rp != nil {
			stepsList = append(stepsList, rp)
		}
	}
	t := &task.Task{
		ID:          s.newID("site-add"),
		Label:       "创建站点 " + domain,
		LabelCode:   task.MsgTaskSiteAdd,
		LabelParams: map[string]string{"domain": domain},
		Meta:        model.TaskMeta{Type: "site-add", Domain: domain},
		Steps:       stepsList,
		Apply:       func() error { return s.commitUpsert(ctx, site) },
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// Remove 删站：根目录入回收站（保留 7 天）→ 删 vhost 文件 → 回收 hosts 条目 → 落库删除；源码目录可经回收站恢复
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
		steps.NewRemoveHosts("回收 hosts", s.hosts, domain),
	}
	// 删站点这一单里，被删的那颗域名既不在发布集（exceptDomain=domain）也不新增端口（addPort=0），
	// 所以 blocked[domain] 必然是空——降级提示只属于「创建/改端口」那两路，这里传空串。
	ports, _ := s.publishPlan(ctx, mustSites(s.store), domain, 0)
	if rp := s.republishStep(ports, ""); rp != nil {
		stepsList = append(stepsList, rp)
	}
	id := s.newID("site-remove")
	t := &task.Task{
		ID:          id,
		Label:       "删除站点 " + domain,
		LabelCode:   task.MsgTaskSiteRemove,
		LabelParams: map[string]string{"domain": domain},
		Meta:        model.TaskMeta{Type: "site-remove", Domain: domain},
		Steps:       stepsList,
		Apply: func() error {
			trashPath = trashStep.TrashPath()
			if err := s.commitRemove(ctx, domain, site.Root, trashPath); err != nil {
				return err
			}
			s.healFreed(id, ctx)
			return nil
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// ReconcileServe 就绪入口（nginx 安装/启动、PHP 装重建、数据服务卸载后）：补齐降级站点，
// 并额外用权威发布集校对一次宿主端口——停机期间的站点写操作可能没发布成功，容器绑的端口已与站点不符。
// 端口被占而降级的站点也在这里恢复：重新核一次占用表，腾出来的端口随本次重发布回到宿主。
func (s *SiteService) ReconcileServe(ctx context.Context) error {
	return s.reconcileServe(ctx, true)
}

// reconcileServe 补齐降级站点：把「本应对外服务但 conf 缺失」的站点正文写盘、重载 nginx，再按需在发布集
// 变化后重绑宿主端口。幂等：已落盘或仍不就绪（PHP／nginx 缺失）的站点跳过补写。
// checkPorts=true 时即使没有站点待补 vhost 也校对一次宿主端口（容器绑的集可能已与站点不符）；
// false 只在补写过 vhost 后才发布——站点写链路自身已在步骤里发布过一份并集，避免同一次操作重复重建 nginx。
// 补写盘走的是「校验器已就绪」那一条：nginx 在跑就照常 nginx -t，nginx 没跑则跳过校验（§5.8 的硬红线 2 例外）。
// 端口被占的站点不在这条路上补写——它的 conf 早就落盘了，要恢复的是宿主端口：这里重算一次 blocked 集，
// 把腾出来的端口经重发布带回宿主，并把降级标记的变化随快照回流（不重刷一遍已落盘的 conf）。
// 单站写失败不阻断其余站点，失败域名聚合为一个错误交由调用方记日志——站点维持降级，后续任一站点写操作仍可自愈。
// 由 nginx 安装/启动任务内联调用，故自身不再产出 task（task.Manager 单飞，嵌套运行会 ErrBusy）。
func (s *SiteService) reconcileServe(ctx context.Context, checkPorts bool) error {
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
		if !siteLandReady(snap, st) {
			continue
		}
		content := s.vhosts.Get(st.Domain) // 取缓存正文（保留手改），无缓存则按站点重算
		if content == "" {
			continue
		}
		if _, e := os.Stat(s.vhosts.Path(st.Domain)); e == nil {
			// 已落盘 ≠ 已经对：模板生成的那一份可能是**上一个容器引擎**时期写的（v2.9.16 前 nginx 运行时
			// DNS 写死 127.0.0.11，换到 Podman 后那一口没人应答，nginx 解析不到 php-{ver}-fpm，站点常年 502，
			// 而用户什么都没做错）。这里现算一次，与磁盘上那份不一致就重刷——手改正文不动（那是用户的意图，
			// 自愈不得替他改）。
			if s.vhosts.Customized(st.Domain) {
				continue
			}
			have, e := os.ReadFile(s.vhosts.Path(st.Domain))
			want := s.vhosts.Regenerate(st.Domain)
			if e != nil || want == "" || string(have) == want {
				continue // 读不动 / 渲染不出东西 / 本来就是这一份：什么都不动
			}
			content = want
		}
		if e := s.vhosts.Save(ctx, s.validatorFor(snap, false), st.Domain, content); e != nil {
			failed = append(failed, st.Domain)
			continue
		}
		healed = append(healed, st.Domain)
	}
	if len(healed) > 0 && s.reload != nil {
		if err := s.reload.Reload(ctx); err != nil {
			return fmt.Errorf("补齐 %d 个站点后重载 nginx 失败: %w", len(healed), err)
		}
	}
	// 端口校对与补写盘解耦：就绪入口即使没有站点待补 vhost，在跑容器的宿主端口集也可能已与站点不符
	// （停机期间删过站、重发布当时被跳过）。是否真重建由 RepublishNginx 按发布集判等决定。
	if s.publisher != nil && (checkPorts || len(healed) > 0) {
		ports, _ := s.publishPlan(ctx, sites, "", 0)
		if err := s.publisher.RepublishNginx(ctx, ports); err != nil {
			return err
		}
	}
	// 端口腾出来了即不再降级，这份标记变化也要随快照回流——不能只在补写过 vhost 那一档才回流。
	blocksChanged := s.refreshPortBlocks(ctx)
	if len(healed) == 0 && !blocksChanged {
		if len(failed) > 0 {
			return fmt.Errorf("部分站点 vhost 补齐失败: %s", strings.Join(failed, ", "))
		}
		return nil
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

// siteLandReady 能不能把 vhost 落盘：nginx 已装（才有 nginx -t 可问）且所选 PHP 已装（上游 php-{ver}-fpm:9000 解析得了）。
// 端口被占不再拦落盘——配置照常写、只是那一个宿主端口暂不发布（降级）；nginx 没在跑也不再拦落盘，
// 只是这一轮的正文未经校验（见 validatorFor 与硬红线 2 的登记例外），启动 Nginx 后由 ReconcileServe 重新校验补齐。
func siteLandReady(snap *model.Snapshot, site model.Site) bool {
	if len(snap.Installed["nginx"]) == 0 {
		return false
	}
	return snap.HasVersion("php", site.PHP)
}

// landReady 读一次权威快照再判落盘就绪，并把那份快照交给调用方（校验档位与端口档位共用同一份，不重复问库）。
// 权威快照不可读按不就绪处理（宁可降级也不写出跑不通的 vhost）。
func (s *SiteService) landReady(site model.Site) (*model.Snapshot, bool) {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return nil, false
	}
	return snap, siteLandReady(snap, site)
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
	return s.writeVHost(ctx, "site-port", "改端口 "+domain, task.MsgTaskSitePort, map[string]string{"domain": domain}, func(m *vhost.Manager) string {
		return m.ApplyPort(domain, port)
	}, domain, false)
}

// SwitchPHP 切换 PHP（T405，硬红线 1 精确上游）。前置「备好上游」步：目标容器没起就先启动——
// 先让上游就绪再 reload nginx，否则切换完成到容器就绪之间站点是 502 窗口（追加需求）。
func (s *SiteService) SwitchPHP(ctx context.Context, domain, php string) error {
	return s.writeVHost(ctx, "php-switch", "切换 PHP "+php+" · "+domain, task.MsgTaskSitePhp, map[string]string{"domain": domain, "php": php}, func(m *vhost.Manager) string {
		return m.ApplyPhp(domain, php)
	}, domain, false, s.ensurePHPRunningStep(php))
}

// SetRewrite 改伪静态（T406）
func (s *SiteService) SetRewrite(ctx context.Context, domain, preset, rule string) error {
	return s.writeVHost(ctx, "rewrite", "伪静态 "+preset+" · "+domain, task.MsgTaskSiteRewrite, map[string]string{"domain": domain, "preset": preset}, func(m *vhost.Manager) string {
		return m.ApplyRewrite(domain, preset, rule)
	}, domain, false)
}

// SetVhostContent 手改 vhost 正文（T406）；写前 nginx -t 必过（硬红线 2）
func (s *SiteService) SetVhostContent(ctx context.Context, domain, content string) error {
	return s.writeVHost(ctx, "site-vhost", "编辑 vhost · "+domain, task.MsgTaskSiteVhost, map[string]string{"domain": domain}, func(m *vhost.Manager) string {
		return m.SetContent(domain, content)
	}, domain, true)
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
		ID:          s.newID("hosts"),
		Label:       "加 hosts · " + domain,
		LabelCode:   task.MsgTaskSiteHosts,
		LabelParams: map[string]string{"domain": domain},
		Meta:        model.TaskMeta{Type: "hosts-add", Domain: domain},
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

// writeVHost 通用编排：把权威站点灌入管理器→mutate 得新正文→写盘(校验)+reload→落库+广播。
// pre 为可变前置步（SwitchPHP 的「备好上游」用），排在写盘之前执行。
// strict 为真时一律要 nginx -t 才落盘（用户手改 vhost 正文走这条）；为假时 nginx 没在跑就跳过校验先落盘，
// 等 nginx 起来后由 ReconcileServe 重新校验补齐（§5.8 的硬红线 2 例外）。
// labelCode / labelParams 是这次操作在队列行与抽屉标题上的可翻译名字（多语言 Phase 2）；
// label 仍是中文原文，账本与不认码的界面都用它。
func (s *SiteService) writeVHost(ctx context.Context, op, label, labelCode string, labelParams map[string]string, mutate func(*vhost.Manager) string, domain string, strict bool, pre ...task.Step) error {
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
	snap, snapErr := s.store.BuildSnapshot()
	if snapErr != nil {
		snap = nil
	}
	stepList := append([]task.Step{}, pre...)
	stepList = append(stepList,
		steps.NewWriteVHost("写入 vhost", s.vhosts, s.validatorFor(snap, strict), domain, content),
		&task.FuncStep{StepName: "重载 Nginx", Exec: func(ctx context.Context, _ task.StepLog) error {
			if s.reload == nil {
				return nil
			}
			return s.reload.Reload(ctx)
		}},
	)
	ports, blocked := s.publishPlan(ctx, sites, domain, updated.Port)
	if rp := s.republishStep(ports, blocked[domain]); rp != nil {
		stepList = append(stepList, rp)
	}
	id := s.newID(op)
	t := &task.Task{
		ID:          id,
		Label:       label,
		LabelCode:   labelCode,
		LabelParams: labelParams,
		Meta:        model.TaskMeta{Type: op, Domain: domain},
		Steps:       stepList,
		Apply: func() error {
			if err := s.store.UpsertSite(updated); err != nil {
				return err
			}
			s.refreshPortBlocks(ctx)
			if err := s.emit(); err != nil {
				return err
			}
			s.healFreed(id, ctx)
			return nil
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// healFreed 本次站点写操作让出的端口（删站、改端口）可能正卡着别的降级站点：落库后顺手补齐它们。
// 只能挂在 Apply 段末尾——步骤阶段库里仍是旧占用表，那时补齐会照旧判定「端口被占」而空跑。
// 不校对宿主端口（checkPorts=false）：本次操作已按当轮并集发布过，只有真补齐了 vhost 才需再发一次。
// 失败只记一行 task:log 不上抛：本次删除/改动已成功，让补齐失败回滚整任务与本意相反（§3.2 原则 7）。
func (s *SiteService) healFreed(taskID string, ctx context.Context) {
	if err := s.reconcileServe(ctx, false); err != nil {
		task.Logf(s.emitter, taskID, model.LogErr, "补齐其他降级站点失败: "+err.Error())
	}
}

// commitUpsert applyStateChange：写库 + 校准缓存 + 重算端口降级 + 广播新快照
// 降级集必须在 emit 之前落：快照的 site.Health 里那颗降级标记就是从这份表派生的（硬红线 4：界面只认快照）。
func (s *SiteService) commitUpsert(ctx context.Context, site model.Site) error {
	if err := s.store.UpsertSite(site); err != nil {
		return err
	}
	s.vhosts.Sync(append(mustSites(s.store), site))
	s.vhosts.Regenerate(site.Domain)
	s.refreshPortBlocks(ctx)
	return s.emit()
}

// commitRemove applyStateChange：登记回收站条目 + 删库 + 清 vhost 缓存 + 重算端口降级 + 广播
func (s *SiteService) commitRemove(ctx context.Context, domain, origRoot, trashPath string) error {
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
	s.refreshPortBlocks(ctx)
	return s.emit()
}

// ReorderSites 持久化站点展示顺序（拖拽排序）：顺序落 config.yaml（与镜像源保存同类——纯配置写，
// 不动 Docker 状态，不走任务队列），成功后广播 state:changed 让快照按新序回流。
func (s *SiteService) ReorderSites(domains []string) error {
	if err := s.store.SetSiteOrder(domains); err != nil {
		return err
	}
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

// PublishPorts 当前应发布给 nginx 的权威站点端口集（实现 NginxPortSource）。
// 装/重建 nginx 与站点写链路走同一判据，避免「nginx 按另一套端口起来、站点绑不上」。
// 判据复用 publishPlan：端口被外面占着而降级的站点不在这份清单里，nginx 不去抢绑它。
func (s *SiteService) PublishPorts() []int {
	ports, _ := s.publishPlan(context.Background(), mustSites(s.store), "", 0)
	return ports
}

// republishStep 发布站点端口到 nginx。conflict 非空即本次有站点的端口被「不能确认是自己站点」的东西占着：
// 先如实说那一句（meta 级——不是 err，任务本身是成功的，§5.6 的失败原因取最后一条 err 行），再照常发布其余端口。
// 发布失败只落一行 err 后 return nil——vhost 已经落盘、站点已经建好，把 nginx 侧的端口问题判死整单，
// 等于撤回已经做对的那部分（§0.2 规则 16 / §5.16.3 同口径）。腾出端口后由 ReconcileServe 补齐。
func (s *SiteService) republishStep(ports []int, conflict string) task.Step {
	if s.publisher == nil && conflict == "" {
		return nil
	}
	return &task.FuncStep{StepName: "发布站点端口到 Nginx", Exec: func(ctx context.Context, log task.StepLog) error {
		if conflict != "" {
			log.Log(string(model.LogMeta), conflict)
		}
		if s.publisher == nil {
			return nil
		}
		if err := s.publisher.RepublishNginx(ctx, ports); err != nil {
			log.Log(string(model.LogErr), "站点端口未发布到 Nginx: "+err.Error())
		}
		return nil
	}}
}

// publishPorts 是读不到权威快照时退回的保守口径：只取 vhost 已落盘站点的端口并集，并剔除数据服务占用的端口。
// 占用者是自家 nginx（可复用）还是外面的进程（要降级），判据全在快照那张占用表里；快照读不到就不猜，
// 降级表给空、照常发布。正常路径走 publishPlan，这里只保底。
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

// publishPlan = 该发布给 nginx 的宿主端口集 + 哪些站点因端口绑不上而降级（域名 → 人话原因）。
// 占用只分两档：占用者是别的站点（= 自家 nginx 在监听，按 server_name 分流即可复用，不提示、不降级）；
// 其余一律「不能确认」——数据服务占的、别的进程占的、探测说绑不上的，配置照常落盘、只不发布这一个端口。
// 快照读不到时不猜：退回旧的「按落盘站点取端口并集」口径，blocked 给空。
func (s *SiteService) publishPlan(ctx context.Context, sites []model.Site, exceptDomain string, addPort int) ([]int, map[string]string) {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return s.publishPorts(sites, exceptDomain, addPort), map[string]string{}
	}
	hard := map[int]string{}
	for p, owner := range store.CollectUsedPorts(snap, []string{exceptDomain}) {
		if strings.HasPrefix(owner, "site ") {
			continue // 自家 nginx 正在监听，复用即可
		}
		hard[p] = owner
	}
	cand := make([]int, 0, len(sites)+1)
	seen := map[int]bool{}
	for _, st := range sites {
		if st.Domain == exceptDomain || st.Port <= 0 || hard[st.Port] != "" || seen[st.Port] {
			continue
		}
		seen[st.Port] = true
		cand = append(cand, st.Port)
	}
	if addPort > 0 && !seen[addPort] && hard[addPort] == "" {
		cand = append(cand, addPort)
	}
	probed := map[int]string{}
	if s.portBinder != nil && len(cand) > 0 {
		probed = s.portBinder.BlockedSitePorts(ctx, cand)
	}
	blocked := map[string]string{}
	keep := make([]model.Site, 0, len(sites))
	for _, st := range sites {
		if st.Domain == exceptDomain || st.Port <= 0 {
			continue
		}
		if _, e := os.Stat(s.vhosts.Path(st.Domain)); e != nil {
			continue // 没落盘就没有可发布的端口
		}
		why := hard[st.Port]
		if why == "" {
			why = probed[st.Port]
		}
		if why != "" {
			blocked[st.Domain] = portBlockedMsg(st.Port, why)
			continue
		}
		keep = append(keep, st)
	}
	ports := sitePorts(keep, "", 0)
	if addPort > 0 {
		why := hard[addPort]
		if why == "" {
			why = probed[addPort]
		}
		if why != "" {
			blocked[exceptDomain] = portBlockedMsg(addPort, why)
		} else if !containsPort(ports, addPort) {
			ports = append(ports, addPort)
		}
	}
	return ports, blocked
}

// OwnerRootlessPrivileged 是「这个端口不是被谁占了，而是 rootless 模式的容器引擎自己绑不了 1024
// 以下的特权端口」这一事实的记号，由装配层的端口实测写进占用表。为什么要单独立一个记号：拿它当
// 「其他程序占用」报给用户就是假话——本机并没有别的东西在听 80，用户按提示去「腾出端口」也腾不出
// 任何东西，真正要动的是引擎的特权端口限制。
const OwnerRootlessPrivileged = "rootless:privileged-port"

// ClassifyProbe 把一次「phpo 自己能不能绑上这个宿主端口」的实测结果归成档，交回占用者记号。
// 返回 degraded=false 表示这一档不该把站点标成降级。为什么要分这么细：这个探针是在 phpo 自己进程里
// listen，量的是 phpo 的权限，不是容器引擎的权限——把「phpo 问不出结果」报成「被其他程序占了」，
// 用户就会去腾一个根本不存在的东西。
//
//	绑得上            → 不降级
//	明确已被占用        → 「其他程序」，降级（真有人听着这个口）
//	权限不足 + rootless 引擎 + 端口 <1024 → OwnerRootlessPrivileged，降级（是引擎绑不了特权端口，不是被人占）
//	其余报错           → 不降级（探测能力不足不得变成阻断，§5.8）
func ClassifyProbe(err error, p int, rootless bool) (string, bool) {
	switch {
	case err == nil:
		return "", false
	case portpkg.InUse(err):
		return "其他程序", true
	case errors.Is(err, os.ErrPermission) && p < 1024 && rootless:
		return OwnerRootlessPrivileged, true
	}
	return "", false
}

// portBlockedMsg 给人话一句：这个端口此刻归谁、这次只做了哪一半、怎么回来。
// rootless 那一档不说「被占用」（那是假话），改成说清两件事：绑不上的真正原因 + 该动哪里。
func portBlockedMsg(port int, owner string) string {
	if owner == OwnerRootlessPrivileged {
		return fmt.Sprintf("端口 %d 绑不上：rootless 模式的容器引擎不能绑定 1024 以下的端口（不是别的程序占了它）。"+
			"站点配置已落盘，只是这个端口暂不发布。要放开任选其一：给引擎允许特权端口 "+
			"sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80（写进 /etc/sysctl.d/ 下次开机仍然有效），"+
			"或把站点端口改成 1024 以上。改好后任意一次站点保存、或重启容器引擎，这个端口就自动补上发布。", port)
	}
	return fmt.Sprintf("端口 %d 已被 %s 占用：站点配置已落盘，只是这个端口暂不发布（腾出后自动补齐）", port, owner)
}

// validatorFor 这一轮的 nginx -t 该不该做：手改正文（strict）必须过；nginx 在跑才有 -t 可问；
// 快照读不到时照旧校验（不放松）。没在跑就跳过——这是给硬红线 2 登记的一处例外，
// 只用于 phpo 自己按模板渲染出的配置，且启动 Nginx 后由 ReconcileServe 重新校验补齐。
func (s *SiteService) validatorFor(snap *model.Snapshot, strict bool) vhost.Validator {
	if snap == nil || strict || len(snap.Running["nginx"]) > 0 {
		return s.validate
	}
	return nil
}

// refreshPortBlocks 落库之后重算一次降级表（域名 → 原因），写回存储并同步本服务缓存；返回「跟上次比是否变了」。
// 只能在写库之后调：步骤阶段库里仍是旧占用表（healFreed 同一原因）。
func (s *SiteService) refreshPortBlocks(ctx context.Context) bool {
	_, blocked := s.publishPlan(ctx, mustSites(s.store), "", 0)
	changed := !sameBlocks(s.portBlocks, blocked)
	s.portBlocks = blocked
	s.store.SetSitePortBlocks(blocked)
	return changed
}

func sameBlocks(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func containsPort(ports []int, p int) bool {
	for _, v := range ports {
		if v == p {
			return true
		}
	}
	return false
}
