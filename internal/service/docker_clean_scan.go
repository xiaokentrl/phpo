// 扫描：把「这台机器上 Docker 还占着什么」数成 60 行清单。
// 浅扫只问 Docker 自己（容器 / 镜像 / 卷 / 网络 / 构建缓存 / 插件 / swarm / compose 的清单都在它那里）；
// 宿主上那 28 行（检查点目录、日志文件、veth、cgroup、containerd…）要「详细扫描」点一下才会去读，
// 而且只用管理员授权去**读**，不删任何东西。
// 数不出来的一律显示「这次没读到 + 为什么」，绝不给 0——给 0 等于说「这里没有东西」，那是假话。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"phpo/internal/engine"
	"phpo/internal/model"
)

// cleanContainerKeys 是「要靠容器清单才有数」的那几行；容器这一类问不上时，它们一起显示读不到。
var cleanContainerKeys = []string{
	"container.all", "container.running", "container.stopped", "container.paused",
	"container.orphan", "container.layer", "volume.bind",
}

// Scan 数一遍全部 60 行。deep=false 时只问 Docker；deep=true 时再去读宿主上的文件。
// 返回的 rows 顺序永远是 model.AllCleanRows 那一套，界面不用自己排序。
func (s *DockerCleanService) Scan(ctx context.Context, deep bool) (model.CleanScanReport, error) {
	info := s.eng.DockerInfo(ctx)
	plan := engine.PlanHostScan(info)

	// 进度分母：浅扫只走 Docker 那 10 段；详细扫描再加宿主自己的段数（PlanHostScan 给的就是这个数）。
	total := engine.DockerStagesTotal
	if deep && plan.Supported {
		total += plan.Stages
	}
	stage := s.cleanProgress(total)

	inv := s.eng.ScanDockerInventory(ctx, stage)

	var host engine.HostScanResult
	if deep {
		h, err := s.eng.ScanHost(ctx, info, stage)
		// 宿主不支持读（macOS / Windows）时 err 一定非空，但那 28 行仍然回来了——
		// 每一行都写着「Docker 的数据目录不在这台机器上」，这正是⑮要的效果，照用。
		if h.Rows != nil || err == nil || errors.Is(err, engine.ErrHostNotSupported) {
			host = h
		}
	}

	now := s.now()
	if host.ScannedAt.IsZero() {
		host.ScannedAt = now
	}
	s.mu.Lock()
	s.last = &cleanCache{info: info, inv: inv, host: host, deep: deep, scanned: now}
	s.mu.Unlock()

	rows := s.buildRows(inv, host, deep)
	rep := model.CleanScanReport{Rows: rows, ScannedAt: now, DeepScanned: deep}
	for _, r := range rows {
		rep.TotalCount += r.Count
		if r.HasBytes && !cleanAggregateRows[r.Key] {
			rep.TotalBytes += r.Bytes
		}
	}
	rep.Warnings = append(rep.Warnings, host.Warnings...)
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	return rep, nil
}

// HostScanSupported 回答「详细扫描这一颗按钮能不能点」：宿主不是 Linux 时读不到东西，点了也没数。
func (s *DockerCleanService) HostScanSupported(ctx context.Context) bool {
	return engine.PlanHostScan(s.eng.DockerInfo(ctx)).Supported
}

// RefreshRow 只重数一行：宿主那 28 行走 RescanHostRow（要提权就读一次），
// Docker 那 28 行重取一次清单后挑出这一行。行上的时间戳因此是各行的，不是整页的。
func (s *DockerCleanService) RefreshRow(ctx context.Context, key string) (model.CleanRow, error) {
	if msg, ok := cleanSilentRows[key]; ok {
		r := model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
		r.Message = msg
		return cleanDecide(r), nil
	}

	s.mu.Lock()
	cache := s.last
	s.mu.Unlock()

	if cleanHostRowKeys[key] {
		var info engine.DaemonInfo // 上一轮扫到的那份；还没扫过就现在问一次
		if cache != nil {
			info = cache.info
		} else {
			info = s.eng.DockerInfo(ctx)
		}
		h := s.eng.RescanHostRow(ctx, key, info)
		if cache != nil {
			cache.putHostRow(h)
		}
		return cleanDecide(cleanRowFromHost(h)), ctx.Err()
	}

	if cache == nil {
		// 还没整页扫过：先扫一次浅的，这一行才有数。
		rep, err := s.Scan(ctx, false)
		return cleanRowOf(rep, key), err
	}

	// nil 进度回调：重数一行不该再推一次整页进度。
	inv := s.eng.ScanDockerInventory(ctx, nil)
	row := s.dockerRows(inv, s.installedNames())[key]
	if row.Key == "" {
		row = model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
	}
	s.mu.Lock()
	cache.inv = inv
	s.mu.Unlock()
	return cleanDecide(row), ctx.Err()
}

// buildRows 按 model.AllCleanRows 的顺序拼出 60 行：数字来自两份取数结果，静态属性来自名单。
func (s *DockerCleanService) buildRows(inv engine.DockerInventory, host engine.HostScanResult, deep bool) []model.CleanRow {
	dockerRows := s.dockerRows(inv, s.installedNames())
	hostRows := make(map[string]engine.HostRow, len(host.Rows))
	for _, h := range host.Rows {
		hostRows[h.Key] = h
	}

	out := make([]model.CleanRow, 0, len(model.AllCleanRows))
	for _, meta := range model.AllCleanRows {
		key := meta.Key

		// 数不出独立占用又不会单独删的东西：只说清数字在哪一行，不给 0。
		if msg, ok := cleanSilentRows[key]; ok {
			r := model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
			r.Message = msg
			out = append(out, cleanDecide(r))
			continue
		}

		if cleanHostRowKeys[key] {
			if h, ok := hostRows[key]; ok {
				out = append(out, cleanDecide(cleanRowFromHost(h)))
				continue
			}
			// 浅扫时宿主那 28 行到这里：不是「没有」，是「还没去读」。
			r := model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
			if !deep {
				r.Message = msgHostNotScanned
			}
			out = append(out, cleanDecide(r))
			continue
		}

		r, ok := dockerRows[key]
		if !ok {
			r = model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
		}
		out = append(out, cleanDecide(r))
	}
	return out
}

// cleanRowFromHost 把宿主采集到的那一行搬到界面上的行：状态、数字、说明、时间戳一律照搬。
func cleanRowFromHost(h engine.HostRow) model.CleanRow {
	r := model.NewCleanRow(h.Key, h.Status, h.Count, h.Bytes, h.HasBytes)
	r.Message = h.Message
	if !h.ScannedAt.IsZero() {
		r.ScannedAt = h.ScannedAt
	}
	return r
}

// cleanDecide 收尾那颗「能不能删」的闸门：第三档（没有安全删法）永远不给按钮，
// 数不到东西的行也不给——按钮点了没东西可删，比不给按钮更扰人。
func cleanDecide(r model.CleanRow) model.CleanRow {
	r.Deletable = r.Tier != model.TierInfo && r.Status == model.RowOK
	return r
}

// cleanFailRow 是「这一类问 Docker 没答上来」的行：给出原因，但不给 0。
func cleanFailRow(key string, inv engine.DockerInventory, cats ...string) model.CleanRow {
	r := model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
	for _, c := range cats {
		if inv.Failed(c) {
			r.Message = fmt.Sprintf("Docker 这次没答上来（%s）：%s", c, inv.Reason(c))
			return r
		}
	}
	r.Message = "Docker 这次没答上来"
	return r
}

// dockerRows 算出「Docker 自己能答的那 28 行」。取数失败的类别整批降级成读不到。
func (s *DockerCleanService) dockerRows(inv engine.DockerInventory, installed map[string]bool) map[string]model.CleanRow {
	out := make(map[string]model.CleanRow, len(cleanContainerKeys)+21)
	ok := func(key string, count int, bytes int64, hasBytes bool) {
		out[key] = model.NewCleanRow(key, model.RowOK, count, bytes, hasBytes)
	}

	// ---- 容器（volume.bind 的数字也来自容器的挂载清单）----
	for _, k := range cleanContainerKeys {
		out[k] = cleanFailRow(k, inv, engine.CatContainers)
	}
	if !inv.Failed(engine.CatContainers) {
		var totalBytes, rwBytes int64
		running, stopped, paused, orphans := 0, 0, 0, 0
		binds := map[string]bool{}
		for _, c := range inv.Containers {
			totalBytes += c.SizeRw + c.SizeRootFs
			rwBytes += c.SizeRw
			switch {
			case c.Running:
				running++
			case c.Paused:
				paused++
			default:
				stopped++
			}
			if rowIsPhpo(c.Name) && !installed[c.Name] {
				orphans++
			}
			for _, b := range c.Binds {
				binds[b] = true
			}
		}
		ok("container.all", len(inv.Containers), totalBytes, true)
		ok("container.running", running, 0, false)
		ok("container.stopped", stopped, 0, false)
		ok("container.paused", paused, 0, false)
		ok("container.orphan", orphans, 0, false)
		ok("container.layer", len(inv.Containers), rwBytes, true)
		ok("volume.bind", len(binds), 0, false)
	}

	// ---- 镜像 ----
	for _, k := range []string{"image.all", "image.dangling", "image.unused", "image.meta", "image.multiarch", "image.layer"} {
		out[k] = cleanFailRow(k, inv, engine.CatImages)
	}
	if !inv.Failed(engine.CatImages) {
		var sumBytes, sumRefs, multi int64
		for _, im := range inv.Images {
			sumBytes += im.Size
			sumRefs += int64(len(im.Refs))
			if im.ManifestCount > 1 {
				multi++
			}
		}
		ok("image.all", len(inv.Images), sumBytes, true)
		ok("image.meta", int(sumRefs), 0, false)
		ok("image.multiarch", int(multi), 0, false)

		var dBytes int64
		for _, im := range inv.DanglingImages() {
			dBytes += im.Size
		}
		ok("image.dangling", len(inv.DanglingImages()), dBytes, true)

		// 「没容器在用的镜像」要同时有容器清单才判得出，容器那一类问不上就不算这一行。
		if inv.Failed(engine.CatContainers) {
			out["image.unused"] = cleanFailRow("image.unused", inv, engine.CatContainers, engine.CatImages)
		} else {
			var uBytes int64
			unused := inv.UnusedImages()
			for _, im := range unused {
				uBytes += im.Size
			}
			ok("image.unused", len(unused), uBytes, true)
		}

		// 层重叠体积只在 DiskUsage 那一次应答里有；拿不到就是拿不到，不当成 0。
		if inv.LayersOK {
			r := model.NewCleanRow("image.layer", model.RowOK, 0, inv.LayersSize, true)
			r.Message = "这一份是多个镜像共用的层，只有体积、没有单独的文件可数"
			out["image.layer"] = r
		} else {
			r := cleanFailRow("image.layer", inv, engine.CatLayers)
			if r.Message == "Docker 这次没答上来" {
				r.Message = "共用层的体积这次没拿到（Docker 的磁盘用量那一段没答上来）"
			}
			out["image.layer"] = r
		}
	}

	// ---- 卷 ----
	for _, k := range []string{"volume.all", "volume.unused", "swarm.volume"} {
		out[k] = cleanFailRow(k, inv, engine.CatVolumes)
	}
	if !inv.Failed(engine.CatVolumes) {
		var vBytes int64
		hasSize := false
		globals := 0
		for _, v := range inv.Volumes {
			if v.HasSize {
				vBytes += v.Size
				hasSize = true
			}
			if v.Scope == "global" {
				globals++
			}
		}
		ok("volume.all", len(inv.Volumes), vBytes, hasSize)
		ok("swarm.volume", globals, 0, false)

		if inv.Failed(engine.CatContainers) {
			out["volume.unused"] = cleanFailRow("volume.unused", inv, engine.CatContainers, engine.CatVolumes)
		} else {
			var uvBytes int64
			unused := inv.UnusedVolumes()
			for _, v := range unused {
				if v.HasSize {
					uvBytes += v.Size
				}
			}
			ok("volume.unused", len(unused), uvBytes, true)
		}
	}

	// ---- 网络 ----
	for _, k := range []string{"network.all", "network.unused"} {
		out[k] = cleanFailRow(k, inv, engine.CatNetworks)
	}
	if !inv.Failed(engine.CatNetworks) {
		ok("network.all", len(inv.Networks), 0, false)
		unused := inv.UnusedNetworks()
		ok("network.unused", len(unused), 0, false)
	}

	// ---- 构建缓存 ----
	out["build.cache"] = cleanFailRow("build.cache", inv, engine.CatBuildCache)
	out["build.context"] = cleanFailRow("build.context", inv, engine.CatBuildCache)
	if !inv.Failed(engine.CatBuildCache) {
		var cBytes int64
		ctxCnt := 0
		for _, bc := range inv.BuildCache {
			cBytes += bc.Size
			if strings.HasPrefix(bc.Type, "source") {
				ctxCnt++
			}
		}
		ok("build.cache", len(inv.BuildCache), cBytes, true)
		ok("build.context", ctxCnt, 0, false)
	}

	// ---- 插件 ----
	out["plugin.all"] = cleanFailRow("plugin.all", inv, engine.CatPlugins)
	if !inv.Failed(engine.CatPlugins) {
		ok("plugin.all", len(inv.Plugins), 0, false)
	}

	// ---- swarm ----
	out["swarm.service"] = cleanFailRow("swarm.service", inv, engine.CatSwarmServices)
	out["swarm.stack"] = cleanFailRow("swarm.stack", inv, engine.CatSwarmServices)
	if !inv.Failed(engine.CatSwarmServices) {
		ok("swarm.service", len(inv.Services), 0, false)
		stacks := inv.Groups(engine.SwarmStackLabel())
		ok("swarm.stack", len(stacks), groupBytes(stacks), true)
	}
	// 这一行的界面名字是「配置 / 密钥」：密文只采进清单却不数进来，数量就只兑现了一半承诺。
	// 两类任一类没读到都算这一行读不到——把「不知道」画成「没有」是 §5.11 不接受的。
	out["swarm.config"] = cleanFailRow("swarm.config", inv, engine.CatSwarmConfigs, engine.CatSwarmSecrets)
	out["compose.config"] = cleanFailRow("compose.config", inv, engine.CatSwarmConfigs)
	if !inv.Failed(engine.CatSwarmConfigs) && !inv.Failed(engine.CatSwarmSecrets) {
		ok("swarm.config", len(inv.Configs)+len(inv.Secrets), 0, false)
	}
	if !inv.Failed(engine.CatSwarmConfigs) {
		composeCfg := 0
		for _, c := range inv.Configs {
			if c.Labels[engine.ComposeProjectLabel()] != "" {
				composeCfg++
			}
		}
		ok("compose.config", composeCfg, 0, false)
	}
	out["swarm.node"] = cleanFailRow("swarm.node", inv, engine.CatSwarmNodes)
	out["swarm.leave"] = cleanFailRow("swarm.leave", inv, engine.CatSwarmNodes)
	if !inv.Failed(engine.CatSwarmNodes) {
		// swarm 节点这一行不给按钮（第三档：退出 swarm 要动的是用户的集群，不是本机一份文件）。
		out["swarm.node"] = model.NewCleanRow("swarm.node", model.RowOK, len(inv.Nodes), 0, false)
		cnt := 0
		if len(inv.Nodes) > 0 {
			cnt = 1
		}
		out["swarm.leave"] = model.NewCleanRow("swarm.leave", model.RowOK, cnt, 0, false)
	}

	// ---- compose 项目（按容器上的 compose 标签归组，所以容器那一类问不上就不算）----
	out["compose.project"] = cleanFailRow("compose.project", inv, engine.CatContainers)
	if !inv.Failed(engine.CatContainers) {
		projects := inv.Groups(engine.ComposeProjectLabel())
		ok("compose.project", len(projects), groupBytes(projects), true)
	}

	return out
}

// groupBytes 把一组资源的占用相加，用于「按标签归组」的那两行（swarm 栈 / compose 项目）。
func groupBytes(groups []engine.InvGroup) int64 {
	var sum int64
	for _, g := range groups {
		sum += g.Bytes
	}
	return sum
}

// cleanRowOf 从一份扫描结果里取一行；没有这行时返回一个空行，让调用方按「没数到」处理。
func cleanRowOf(r model.CleanScanReport, key string) model.CleanRow {
	for _, row := range r.Rows {
		if row.Key == key {
			return row
		}
	}
	return model.NewCleanRow(key, model.RowUnavailable, 0, 0, false)
}

// putHostRow 用一行重扫的结果覆盖缓存里的那一行（宿主行的时间戳由此逐行独立）。
func (c *cleanCache) putHostRow(h engine.HostRow) {
	for i, old := range c.host.Rows {
		if old.Key == h.Key {
			c.host.Rows[i] = h
			return
		}
	}
	c.host.Rows = append(c.host.Rows, h)
}
