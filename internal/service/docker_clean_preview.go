// 预览：把用户勾中的那几行摊成「具体要动哪些东西」的一份确认清单，并发一张一次性凭据。
//
// 为什么要有这一步而不是点了「彻底清空」就动手：界面上那一行写的是「17 个卷 / 4.2 GB」，
// 而真正要删的是一个一个对象。用户必须在看清「到底是这 17 个」之后再点头——
// 这也是需求 ㉙ 那颗「同时卸载该版本」的落点：只有摊开对象，才知道这次会动到 phpo 自己的哪几个版本。
//
// 三条不编造的规矩：
//   - 这一行的数字是别处算过来的（容器可写层、镜像共用层、构建上下文），这里就不硬凑一份对象清单，
//     而是明说「没有单独可删的对象」——凑一份等于让用户去点一批不存在的东西。
//   - 宿主那 28 行只有点过「详细扫描」才有路径可读；没读过就直说没读过，不报 0 个。
//   - 每一项的体积只有 Docker 当场报过的才填；宿主上的单项摊不出体积（扫描只数到整行的总量），
//     宁可不填也不按「总量 ÷ 条数」分摊——那是编出来的数字，会让「已释放」虚高。
package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

// cleanPreviewTTL 是一次预览的有效期。
// 定这一条不是因为「安全」，而是因为对象会变：预览之后用户去命令行删了半个卷，
// 再拿旧凭据点清空就会拿一份过期的清单去动手。过期即重新预览，代价只是多点一次。
const cleanPreviewTTL = 10 * time.Minute

// cleanTrashedRows 是「删之前先挪进回收站、留 7 天」的那几行（需求 ④/⑳）。
// 判据只有一条：这一份东西是人放进去的、掉了就没了的数据或配置。
// 日志、临时目录、构建缓存、containerd 那一片不算——把它们挪进回收站等于磁盘一点没腾出来，
// 用户点「彻底清空」要的结果反而没拿到。
var cleanTrashedRows = map[string]bool{
	"volume.data": true, "volume.driver": true,
	"plugin.config": true, "plugin.data": true,
	"container.checkpoint": true,
}

// cleanNoObjectRows 是「有数字、但没有单独可删对象」的几行，值就是给用户的原话。
// 它们的档位仍是 SDK 档（数字真实），只是这份数字来自别处，摊不出逐颗开关。
var cleanNoObjectRows = map[string]string{
	"container.layer": "容器的可写层跟着容器一起走：删掉容器它就没了，这里没有第二份可单独删的东西",
	"image.layer":     "这一份是多个镜像共用的层，只有体积、没有单独的文件可删；要清掉它得删镜像本身",
	"build.context":   "构建上下文是构建缓存的一部分，Docker 只给「整份清掉」，没有按条删的接口；要清就在「构建缓存」那一行",
}

// dockerBuiltinNetworks 是 Docker 自己活着就要用的三张网络：删不掉，也不该在这里删。
// 「网络」那一行的总数会把它们算进去（这是真实数字），但确认清单里不列它们——
// 列了就是给用户一颗注定失败的开关。
var dockerBuiltinNetworks = map[string]bool{"host": true, "bridge": true, "null": true}

// Preview 列出勾中那几行的具体对象，并登记一次性凭据供「彻底清空」校验。
//
// rows 为空即报错：界面上没勾任何一行却点了清空，那是操作走岔了，不该静默返回一张空单子。
// 第三档（phpo 没有安全删法）的行同样拒绝——那颗按钮本来就不该渲染出来（需求 ⑬）。
func (s *DockerCleanService) Preview(ctx context.Context, rows []string) (model.CleanPreview, error) {
	keys, err := s.cleanRowSelection(rows)
	if err != nil {
		return model.CleanPreview{}, err
	}

	// 预览要有数：没扫过就先扫一次浅的（宿主那 28 行此时会明说「还没读过」，不会假装是 0 个）。
	s.mu.Lock()
	cache := s.last
	s.mu.Unlock()
	if cache == nil {
		if _, err := s.Scan(ctx, false); err != nil && ctx.Err() != nil {
			return model.CleanPreview{}, err
		}
		s.mu.Lock()
		cache = s.last
		s.mu.Unlock()
	}
	if cache == nil {
		return model.CleanPreview{}, errors.New("这次没能读到 Docker 的清单，先重新扫描一次再预览")
	}

	hostRows := make(map[string]engine.HostRow, len(cache.host.Rows))
	for _, h := range cache.host.Rows {
		hostRows[h.Key] = h
	}
	installed := s.installedNames()

	var (
		targets  []model.CleanTarget
		warnings []string
	)
	seen := map[string]bool{} // 同一个对象可能被好几行同时列出来（容器既在「全部容器」也在「compose 项目」里）
	dup := 0
	for _, key := range keys {
		list, note := s.targetsFor(key, *cache, hostRows, installed)
		if note != "" {
			warnings = append(warnings, fmt.Sprintf("%s：%s", key, note))
		}
		for _, t := range list {
			// 每一项的界面 ID 是「类型 + 真正要删的那个引用」：同一个名字在 Docker 里可能属于
			// 两种东西（一个叫 foo 的卷和一个叫 foo 的网络各自都存在），只用名字会让用户勾中一个、
			// 界面把另一个也带走。删的时候再把这层前缀剥掉（见 cleanTargetRef）。
			t.ID = cleanTargetID(t.Kind, t.ID)
			if seen[t.ID] {
				dup++
				continue
			}
			seen[t.ID] = true
			t.Row = key
			targets = append(targets, t)
		}
	}
	if dup > 0 {
		warnings = append(warnings, fmt.Sprintf("有 %d 个对象同时属于好几行，这里只列一次——它们只会被删一遍", dup))
	}
	if len(targets) == 0 {
		return model.CleanPreview{}, errors.New("这几行里没有可以去删的对象（多半是还没点「详细扫描」，或这一行的占用本来就来自别处）")
	}

	pv := model.CleanPreview{
		Token:           s.newID("clean"),
		ExpiresAt:       s.now().UTC().Add(cleanPreviewTTL),
		Rows:            keys,
		Targets:         targets,
		Warnings:        warnings,
		ConsentRequired: hasDangerousTarget(targets),
		Installed:       s.installedTouched(targets, installed),
	}
	s.mu.Lock()
	s.token, s.preview = pv.Token, &pv
	s.mu.Unlock()

	s.auditOp("clean-preview", map[string]any{"rows": keys, "targets": len(targets)}, "ok")
	return pv, nil
}

// cleanRowSelection 收下界面勾中的行名：去重、保序（界面顺序即清单顺序），并拒掉两类不该来的行。
func (s *DockerCleanService) cleanRowSelection(rows []string) ([]string, error) {
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, raw := range rows {
		key := strings.TrimSpace(raw)
		if key == "" || seen[key] {
			continue
		}
		meta, ok := model.CleanRowMetaOf(key)
		if !ok {
			return nil, fmt.Errorf("清单里没有「%s」这一行，这次预览不做（行名对不上等于拿一份没人扫过的单子去删东西）", key)
		}
		if meta.Tier == model.TierInfo {
			return nil, fmt.Errorf("「%s」这一行 phpo 没有安全的删法，只给说明不给删除（界面不该给出这颗按钮）", key)
		}
		seen[key] = true
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil, errors.New("一行都没勾，先勾出要清掉哪几行再预览")
	}
	return out, nil
}

// targetsFor 列出一行的具体对象。note 非空表示这一行这次交不出对象清单，以及为什么（要摊到界面上）。
func (s *DockerCleanService) targetsFor(key string, cache cleanCache, hostRows map[string]engine.HostRow,
	installed map[string]bool,
) ([]model.CleanTarget, string) {
	inv, info := cache.inv, cache.info

	// 先挡三类「本来就没有对象」的，各自给一句人话。
	if msg, ok := cleanNoObjectRows[key]; ok {
		return nil, msg
	}
	if msg, ok := cleanSilentRows[key]; ok {
		return nil, msg
	}
	if cleanHostRowKeys[key] {
		h, ok := hostRows[key]
		if !ok || !cache.deep {
			return nil, msgHostNotScanned
		}
		// 只读到一半的行照样给对象，但要说清这份清单可能不全（需求 ㉘：不拿「读到的一半」冒充全部）。
		if h.Status == model.RowNoPerm {
			return hostTargets(h, key, info), "这一行有一部分目录没读到（授权被拒），下面列的是已经读到的那些"
		}
		if len(h.Paths) == 0 {
			return nil, h.Message
		}
		return hostTargets(h, key, info), ""
	}

	switch key {
	case "container.all", "container.running", "container.stopped", "container.paused", "container.orphan":
		if inv.Failed(engine.CatContainers) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatContainers))
		}
		var out []model.CleanTarget
		for _, c := range inv.Containers {
			switch key {
			case "container.running":
				if !c.Running {
					continue
				}
			case "container.stopped":
				if c.Running || c.Paused {
					continue
				}
			case "container.paused":
				if !c.Paused {
					continue
				}
			case "container.orphan":
				if !rowIsPhpo(c.Name) || installed[c.Name] {
					continue
				}
			}
			out = append(out, model.CleanTarget{
				Kind: engine.DelContainer, ID: c.Name, Name: c.Name,
				Size: c.SizeRw + c.SizeRootFs, InUse: c.Running, Foreign: !rowIsPhpo(c.Name),
			})
		}
		return out, ""

	case "image.all", "image.dangling", "image.unused", "image.multiarch":
		if inv.Failed(engine.CatImages) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatImages))
		}
		list := inv.Images
		if key == "image.dangling" {
			list = inv.DanglingImages()
		}
		if key == "image.unused" {
			if inv.Failed(engine.CatContainers) {
				return nil, "这一行要拿容器清单比对才知道有没有在用，容器这次没答上来"
			}
			list = inv.UnusedImages()
		}
		var out []model.CleanTarget
		for _, im := range list {
			if key == "image.multiarch" && im.ManifestCount <= 1 {
				continue
			}
			name := im.ID
			if len(im.Refs) > 0 {
				name = strings.Join(im.Refs, ", ")
			}
			out = append(out, model.CleanTarget{
				Kind: engine.DelImage, ID: im.ID, Name: name, Size: im.Size,
				InUse: im.Containers > 0, Foreign: !strings.HasPrefix(name, "phpo/"),
			})
		}
		return out, ""

	case "image.meta":
		if inv.Failed(engine.CatImages) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatImages))
		}
		// 一个标签就是一颗开关：删掉最后一个标签才算删掉那份镜像，Docker 自己掌握这一步。
		var out []model.CleanTarget
		for _, im := range inv.Images {
			for _, ref := range im.Refs {
				out = append(out, model.CleanTarget{
					Kind: engine.DelImage, ID: ref, Name: ref,
					Foreign: !strings.HasPrefix(ref, "phpo/"),
				})
			}
		}
		return out, ""

	case "volume.all", "volume.unused", "swarm.volume":
		if inv.Failed(engine.CatVolumes) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatVolumes))
		}
		list := inv.Volumes
		if key == "volume.unused" {
			if inv.Failed(engine.CatContainers) {
				return nil, "这一行要拿容器清单比对才知道有没有在用，容器这次没答上来"
			}
			list = inv.UnusedVolumes()
		}
		var out []model.CleanTarget
		for _, v := range list {
			if key == "swarm.volume" && v.Scope != "global" {
				continue
			}
			out = append(out, model.CleanTarget{
				Kind: engine.DelVolume, ID: v.Name, Name: v.Name,
				Size: v.Size, InUse: v.RefCount > 0, Foreign: !rowIsPhpo(v.Name),
			})
		}
		return out, ""

	case "network.all", "network.unused":
		if inv.Failed(engine.CatNetworks) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatNetworks))
		}
		list := inv.Networks
		if key == "network.unused" {
			list = inv.UnusedNetworks()
		}
		var out []model.CleanTarget
		skippedBuiltin := 0
		for _, n := range list {
			if dockerBuiltinNetworks[n.Name] {
				skippedBuiltin++
				continue
			}
			out = append(out, model.CleanTarget{
				Kind: engine.DelNetwork, ID: n.ID, Name: n.Name,
				InUse: n.Attached > 0 || n.Ingress, Foreign: !rowIsPhpo(n.Name),
			})
		}
		if skippedBuiltin > 0 && len(out) == 0 {
			return nil, fmt.Sprintf("这一行里只有 Docker 自带的网络（host / bridge / null，共 %d 张），删了 Docker 就起不来，这里不列它们", skippedBuiltin)
		}
		if skippedBuiltin > 0 {
			return out, fmt.Sprintf("另有 %d 张 Docker 自带的网络（host / bridge / null）没有列出来——删了 Docker 就起不来", skippedBuiltin)
		}
		return out, ""

	case "build.cache":
		if inv.Failed(engine.CatBuildCache) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatBuildCache))
		}
		// Docker 只有「整份清掉」这一条接口，没有按条删；所以这一行永远是一颗开关。
		var bytes int64
		for _, bc := range inv.BuildCache {
			bytes += bc.Size
		}
		return []model.CleanTarget{{Kind: engine.DelBuildCache, ID: "all",
			Name: fmt.Sprintf("整份构建缓存（%d 条记录）", len(inv.BuildCache)), Size: bytes}}, ""

	case "plugin.all":
		if inv.Failed(engine.CatPlugins) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatPlugins))
		}
		var out []model.CleanTarget
		for _, p := range inv.Plugins {
			out = append(out, model.CleanTarget{
				Kind: engine.DelPlugin, ID: p.Name, Name: p.Name,
				InUse: p.Enabled, Foreign: !rowIsPhpo(p.Name),
			})
		}
		return out, ""

	case "swarm.service":
		if inv.Failed(engine.CatSwarmServices) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatSwarmServices))
		}
		out := make([]model.CleanTarget, 0, len(inv.Services))
		for _, sv := range inv.Services {
			out = append(out, swarmTarget(engine.DelSwarmService, sv))
		}
		return out, ""

	case "swarm.config":
		if inv.Failed(engine.CatSwarmConfigs) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(engine.CatSwarmConfigs))
		}
		out := make([]model.CleanTarget, 0, len(inv.Configs))
		for _, cf := range inv.Configs {
			out = append(out, swarmTarget(engine.DelSwarmConfig, cf))
		}
		return out, ""

	case "swarm.stack", "compose.project":
		label := engine.SwarmStackLabel()
		cat := engine.CatSwarmServices
		if key == "compose.project" {
			label, cat = engine.ComposeProjectLabel(), engine.CatContainers
		}
		if inv.Failed(cat) {
			return nil, fmt.Sprintf("这次没读到（Docker 没答上来）：%s", inv.Reason(cat))
		}
		groups := inv.Groups(label)
		var out []model.CleanTarget
		for _, g := range groups {
			for _, name := range g.Containers {
				out = append(out, model.CleanTarget{Kind: engine.DelContainer, ID: name,
					Name: fmt.Sprintf("%s · 容器 %s", g.Name, name), Foreign: !rowIsPhpo(name)})
			}
			for _, ref := range g.Images {
				out = append(out, model.CleanTarget{Kind: engine.DelImage, ID: ref,
					Name: fmt.Sprintf("%s · 镜像 %s", g.Name, ref)})
			}
			for _, name := range g.Volumes {
				out = append(out, model.CleanTarget{Kind: engine.DelVolume, ID: name,
					Name: fmt.Sprintf("%s · 数据卷 %s", g.Name, name), Foreign: !rowIsPhpo(name)})
			}
			for _, name := range g.Networks {
				if dockerBuiltinNetworks[name] {
					continue
				}
				out = append(out, model.CleanTarget{Kind: engine.DelNetwork, ID: name,
					Name: fmt.Sprintf("%s · 网络 %s", g.Name, name), Foreign: !rowIsPhpo(name)})
			}
			for _, name := range g.Services {
				out = append(out, model.CleanTarget{Kind: engine.DelSwarmService, ID: name,
					Name: fmt.Sprintf("%s · 服务 %s", g.Name, name)})
			}
		}
		return out, ""
	}
	return nil, "这一行还没有可列出的对象"
}

// swarmTarget 列一个 swarm 对象。Ref **只能是 ID**：ServiceRemove / ConfigRemove 认的是 ID，
// 拿名字过去会删不掉又报一句看不懂的原因。
func swarmTarget(kind string, n engine.InvNamed) model.CleanTarget {
	return model.CleanTarget{Kind: kind, ID: n.ID, Name: n.Name, Foreign: !rowIsPhpo(n.Name)}
}

// hostTargets 把宿主行摊成逐项对象。
//
// Ref 一律用扫描回来的路径原文——服务层不拼路径（拼一次就多一处能把「删这个」写成「删那个」的地方）。
// system.group 那一行的原文是裸用户名，不是路径，这恰好是它要的形状。
// 体积这里**不填**：宿主扫描只数得出整行的总量，摊到单项上就是编的数字（需求 ㉘）。
func hostTargets(h engine.HostRow, key string, info engine.DaemonInfo) []model.CleanTarget {
	// 授权被拒过、或这台机器的 Docker 是 rootful 守护进程写出来的东西，都得提权才动得了。
	needsRoot := h.Status == model.RowNoPerm || !info.Rootless
	if key == "system.group" {
		needsRoot = true // 改用户组本来就要授权，跟这台机器的 Docker 是不是 rootless 无关
	}
	out := make([]model.CleanTarget, 0, len(h.Paths))
	for _, p := range h.Paths {
		ph := p
		if key == "log.rotate" || key == "log.container" || key == "log.daemon" || key == "log.journald" {
			ph = filepath.Base(p)
		}
		out = append(out, model.CleanTarget{
			Kind: engine.DelHostPath, ID: p, Name: ph,
			InUse: false, Foreign: !rowIsPhpo(filepath.Base(p)),
			NeedsRoot: needsRoot,
		})
	}
	return out
}

// hasDangerousTarget 判断这次勾选里有没有危险项（那 4 行）——只有这份名单参与判定，
// 行上的 risk 徽标只是颜色，不做任何决定（需求 ⑭/㉖）。
func hasDangerousTarget(targets []model.CleanTarget) bool {
	for _, t := range targets {
		if model.IsDangerousCleanRow(t.Row) {
			return true
		}
	}
	return false
}

// installedTouched 说出「这次清理会动到 phpo 自己装过的哪几个版本」。
//
// 它只为确认框里那颗默认不勾的「同时卸载」服务（需求 ㉙）：默认只把服务卡片标成缺失态，
// 不擅自改「已安装」这本账；用户真想卸才另起一单。判据是 phpo 命名空间的名字，
// 拿快照里那份已装清单现比，不靠字符串猜版本。
func (s *DockerCleanService) installedTouched(targets []model.CleanTarget, installed map[string]bool) []model.CleanInstalled {
	snap, err := s.store.BuildSnapshot()
	if err != nil || snap == nil {
		return nil
	}
	var out []model.CleanInstalled
	for _, kind := range sortedKeys(snap.Installed) {
		for _, ver := range snap.Installed[kind] {
			ref := engine.ContainerRef{Kind: kind, Version: ver}
			prefix := dockerutil.NamespacePrefix + kind + "-" + ver + "-"
			for _, t := range targets {
				ref0 := cleanTargetRef(t.ID)
				if t.Name == ref.Name() || ref0 == ref.Name() || strings.HasPrefix(t.Name, prefix) ||
					strings.HasPrefix(ref0, prefix) {
					out = append(out, model.CleanInstalled{Kind: kind, Version: ver})
					break
				}
			}
		}
	}
	return out
}

// cleanTargetID 给界面上每一项一个唯一的身份：类型 + 真正要删的那个引用。
func cleanTargetID(kind, ref string) string { return kind + "|" + ref }

// cleanTargetRef 从那份身份里取回要删的引用原文（引擎只认这个）。
// 只切第一颗分隔符：路径与标签里本来就可能带分隔符，切多了一次就把用户的名字改短了。
func cleanTargetRef(id string) string {
	if i := strings.Index(id, "|"); i >= 0 {
		return id[i+1:]
	}
	return id
}
