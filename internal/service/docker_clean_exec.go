// 彻底清空：拿一次性凭据删掉用户勾中的那些东西——**一个任务、逐项一行、单颗失败不中断其余**（需求 ⑲）。
//
// 这一层干四件事，顺序不能换：
//
//  1. **核对现场**（需求 ⑧）：预览是几分钟前的，这几分钟里用户可能自己在命令行删过东西。
//     动手前重新问一次 Docker、重新看一眼宿主上的路径还在不在；已经不在了的那些记成 Skipped，
//     不进删除指令——把它们送去删只会换回一条看不懂的报错，而用户要的结果（「现在没有了」）其实已经成立。
//  2. **有数据的先留底**（需求 ④/⑳）：phpo 自己建的卷，先把数据目录挪进回收站（7 天可恢复），
//     挪成功才允许删 Docker 那头的记录；挪不动就这一卷不删，并在界面上说清为什么没删。
//     用户自己在命令行建的卷不在本条范围——替他把他选中的卷挪进 phpo 的回收站，等于动他的东西。
//  3. **删**：Docker 侧与宿主侧各一批，两批都复用 engine 已有的删除循环（同一套「单项失败不停整单」
//     与「每项回调一次」的口径，不另起第二套，需求 ⑨）。
//  4. **收口**：回收站登记（7 天）、审计一行、权威快照回流。
//
// 卸载（需求 ㉙）不在这些步骤里：它必须等删除任务返回之后**另起一单**——任务里再嵌套提交一单
// 会直接被拒（ErrBusy，§0.2 规则 11），所以它是 Execute 返回之后才做的事。
package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
)

// cleanResType 把 engine 的删除类型翻成逐项结果与审计用的资源类型。
//
// 两侧词表对不上是有原因的：`build.cache` 那一行在引擎侧叫 prune_build_cache
// （Docker 只有「整份清掉」这一条接口），在 model 侧叫 buildcache。这里给一张显式映射，
// 不在两侧任意一边改名字——改哪边都会把另一边已有的账目弄花。
// 宿主那五种（文件、网卡、命名空间、cgroup、组成员）在 model 侧没有对应资源类型，
// 原样透出类型名，界面上至少看得出「删的是哪一类东西」。
var cleanResType = map[string]model.ResourceType{
	engine.DelContainer:    model.ResContainer,
	engine.DelImage:        model.ResImage,
	engine.DelVolume:       model.ResVolume,
	engine.DelNetwork:      model.ResNetwork,
	engine.DelPlugin:       model.ResPlugin,
	engine.DelSwarmService: model.ResSwarmService,
	engine.DelSwarmConfig:  model.ResSwarmConfig,
	engine.DelSwarmSecret:  model.ResSwarmSecret,
	engine.DelBuildCache:   model.ResBuildCache,
}

// cleanItemType 取一项删除结果的资源类型；认不出的（宿主那五种）原样透出。
func cleanItemType(kind string) model.ResourceType {
	if rt, ok := cleanResType[kind]; ok {
		return rt
	}
	return model.ResourceType(kind)
}

// Execute 按一次性凭据执行一次彻底清空。
//
// 凭据不对时返回 nil report（这次什么都没做）；凭据有效就一定带回 report，哪怕任务被取消——
// 用户点开抽屉要看的是「哪几项动了、哪几项没动」，不是一个光秃秃的成败。
func (s *DockerCleanService) Execute(ctx context.Context, req model.CleanRequest) (*model.CleanExecuteReport, error) {
	pv, chosen, err := s.takeClean(req)
	if err != nil {
		return nil, err
	}

	id := s.newID("docker-clean")
	rep := &model.CleanExecuteReport{
		TaskID:  id,
		Items:   make([]model.CleanedItem, 0, len(chosen)),
		Trashed: make([]string, 0),
	}
	run := &cleanRun{targets: chosen}

	t := &task.Task{
		ID:          id,
		Label:       fmt.Sprintf("清理 Docker 全量资源（%d 项）", len(chosen)),
		LabelCode:   task.MsgTaskDockerClean,
		LabelParams: map[string]string{"n": strconv.Itoa(len(chosen))},
		Meta:        model.TaskMeta{Type: "docker-clean"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "核对现场", Exec: func(ctx context.Context, log task.StepLog) error {
				run.check(ctx, s, log, rep)
				return nil
			}},
			&task.FuncStep{StepName: "把 phpo 自己卷里的数据挪进回收站", Exec: func(ctx context.Context, log task.StepLog) error {
				run.backUpVolumeData(ctx, s, log, rep)
				return nil
			}},
			&task.FuncStep{StepName: "删除 Docker 侧的对象", Exec: func(ctx context.Context, log task.StepLog) error {
				run.deleteDocker(ctx, s, log, rep)
				return nil
			}},
			&task.FuncStep{StepName: "删除宿主上的文件与内核对象", Exec: func(ctx context.Context, log task.StepLog) error {
				run.deleteHost(ctx, s, log, rep)
				return nil
			}},
		},
		Apply: func() error {
			s.auditOp("docker-clean", map[string]any{
				"rows": pv.Rows, "asked": len(chosen), "removed": rep.Removed,
				"failed": rep.Failed, "skipped": rep.Skipped, "freedBytes": rep.FreedBytes,
			}, cleanAuditStatus(rep))
			s.emitState()
			return nil
		},
	}

	status, runErr := s.tasks.Run(ctx, t)
	rep.Status = status
	if runErr != nil {
		return rep, runErr
	}

	// 卸载另起一单（需求 ㉙）：这一单已经返回、队列空了，此刻才允许再提交。
	if req.Uninstall && len(pv.Installed) > 0 {
		if err := s.uninstallAfterClean(ctx, pv.Installed, rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// takeClean 校验并**当场作废**这张一次性凭据。
//
// 为什么要作废：同一份预览点两次「彻底清空」，第二次拿的是几分钟前的清单——
// 那批对象可能已经被第一次删掉了，也可能被用户自己动过。宁可让他重新预览一次
// （多按一颗按钮），也不能让一份旧单子再去删一次。
func (s *DockerCleanService) takeClean(req model.CleanRequest) (*model.CleanPreview, []model.CleanTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pv := s.preview
	if pv == nil || req.Token == "" || pv.Token != req.Token {
		return nil, nil, errors.New("这张预览已经用过了（一次凭据只能清空一次），请重新预览")
	}
	if !s.now().UTC().Before(pv.ExpiresAt) {
		s.token, s.preview = "", nil
		return nil, nil, fmt.Errorf("这份预览放了超过 %d 分钟，Docker 上的东西这几分钟里可能已经变了——请重新预览再清空",
			int(cleanPreviewTTL.Minutes()))
	}
	if pv.ConsentRequired && !req.Consent {
		return nil, nil, errors.New("你勾的这几行里有危险项（容器的网卡、网络命名空间、CNI 留下的网卡与配置、docker 组的成员）——" +
			"删了正在跑的容器会断网，Docker 也可能起不来。确认知道这个后果再清空")
	}

	want := make(map[string]bool, len(req.IDs))
	for _, id := range req.IDs {
		if id != "" {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return nil, nil, errors.New("一项都没勾，先在这份清单里勾出要删哪些")
	}

	chosen := make([]model.CleanTarget, 0, len(want))
	known := make(map[string]bool, len(pv.Targets))
	for _, tg := range pv.Targets {
		known[tg.ID] = true
		if want[tg.ID] {
			chosen = append(chosen, tg)
		}
	}
	// 清单外的 ID 一律拒：拿一份没人预览过的单子去删东西，等于跳过用户点头那一步。
	for _, id := range req.IDs {
		if id != "" && !known[id] {
			return nil, nil, fmt.Errorf("这次预览里没有「%s」这一项，清空不做（对象可能已经变了，请重新预览）", id)
		}
	}

	s.token, s.preview = "", nil
	return pv, chosen, nil
}

// cleanRun 攒着这一单的现场：核对完的目标、分成两批的删除指令、这次重新读到的清单。
type cleanRun struct {
	targets []model.CleanTarget
	docker  []model.CleanTarget
	host    []model.CleanTarget
	info    engine.DaemonInfo
	inv     engine.DockerInventory
}

// check 重新问一次 Docker 与宿主，把已经不在了的那批挑出来（需求 ⑧），并顺手把两批指令分好。
//
// 「这一类这次没读到」**不算**「已经不在了」：没读到只是不知道，
// 把它当成没了就等于在界面上报一笔没做过的删除。
func (r *cleanRun) check(ctx context.Context, s *DockerCleanService, log task.StepLog, rep *model.CleanExecuteReport) {
	r.info = s.eng.DockerInfo(ctx)
	r.inv = s.eng.ScanDockerInventory(ctx, nil)
	present := cleanPresent(r.inv)

	kept := make([]model.CleanTarget, 0, len(r.targets))
	for _, tg := range r.targets {
		gone := false
		switch {
		case tg.Kind == engine.DelBuildCache:
			// 整份清扫不认具体对象，只有「这次到底读到了构建缓存没有」这一说，读不到就照常交给它。
		case cleanHostRowKeys[tg.Row]:
			// 组成员那一行的 ID 是裸用户名不是路径；在不在组里由引擎现读 /etc/group 判，
			// 这里不替它下「已经不在了」的结论。
			if tg.Row != "system.group" {
				if _, err := os.Lstat(cleanTargetRef(tg.ID)); err != nil {
					gone = errors.Is(err, fs.ErrNotExist)
				}
			}
		default:
			// 界面 ID 本身就是「类型 + 引用」，与上面那张「还在不在」的表同一个键法，直接对得上。
			gone = !present[tg.ID]
		}
		if gone {
			rep.Skipped++
			// OK=true 配上这一句说明：界面须把它画成「跳过」那种行，不是红色失败。
			rep.Items = append(rep.Items, model.CleanedItem{
				Type: cleanItemType(tg.Kind), Name: tg.Name, OK: true, Error: "动手时已经不在了，本次没再动它",
			})
			log.Log(string(model.LogDim), "已经不在了，跳过: "+tg.Name)
			continue
		}
		if cleanHostRowKeys[tg.Row] {
			r.host = append(r.host, tg)
			continue
		}
		r.docker = append(r.docker, tg)
		kept = append(kept, tg)
	}
	log.Log(string(model.LogMeta), fmt.Sprintf("核对现场：本次要动 %d 项，另有 %d 项在预览之后已经不在了",
		len(kept), rep.Skipped))
}

// backUpVolumeData 把 phpo 自己的卷的数据目录先挪进回收站（需求 ④/⑳）。
//
// 只有挪成功的那几个才留在待删清单里；挪不动（授权被拒、目录读不动）就把这一卷从这一批里摘掉，
// 并逐行说清为什么没删——「删了但一份底都没留」正是这条需求不接受的结果。
// 数据目录的位置只认 Docker 报回的 Mountpoint：服务层自己拼一次路径，就多一处能把「这个卷」
// 拼成「那个目录」的地方。
func (r *cleanRun) backUpVolumeData(ctx context.Context, s *DockerCleanService, log task.StepLog, rep *model.CleanExecuteReport) {
	rest := make([]model.CleanTarget, 0, len(r.docker))
	for _, tg := range r.docker {
		name := cleanTargetRef(tg.ID)
		if tg.Kind != engine.DelVolume || !rowIsPhpo(name) {
			rest = append(rest, tg)
			continue
		}
		mp := r.volumeMount(name)
		if mp == "" {
			r.dropVolume(log, rep, tg, "Docker 没报出这个卷的数据目录，为了不删掉一份找不回来的数据，这一卷本次没动")
			continue
		}
		ops := []engine.DeleteOp{{Row: "volume.data", Kind: engine.DelHostPath, Ref: mp,
			NeedsRoot: !r.info.Rootless, TrashIt: true}}
		res := s.eng.DeleteHostObjects(ctx, ops, r.info, s.trash, nil)
		if len(res) != 1 {
			r.dropVolume(log, rep, tg, "回收站没有给出这一卷的处理结果，本次没动它")
			continue
		}
		if res[0].Err != nil {
			r.dropVolume(log, rep, tg, "数据没能挪进回收站，这一卷本次没删："+res[0].Err.Error())
			continue
		}
		s.registerTrash(log, rep, "volume.data", mp, res[0].TrashPath)
		rest = append(rest, tg)
	}
	r.docker = rest
}

// dropVolume 记下「这一卷本次没动」：一行 err + 一条失败结果，整单其余照常继续（需求 ⑲）。
func (r *cleanRun) dropVolume(log task.StepLog, rep *model.CleanExecuteReport, tg model.CleanTarget, why string) {
	rep.Failed++
	rep.Items = append(rep.Items, model.CleanedItem{Type: model.ResVolume, Name: tg.Name, Error: why})
	log.Log(string(model.LogErr), why+"（"+tg.Name+"）")
}

// volumeMount 从这次重新读到的清单里取一个卷的数据目录。
func (r *cleanRun) volumeMount(name string) string {
	for _, v := range r.inv.Volumes {
		if v.Name == name {
			return v.Mountpoint
		}
	}
	return ""
}

// deleteDocker 走 Docker 那一批，逐项一行。
func (r *cleanRun) deleteDocker(ctx context.Context, s *DockerCleanService, log task.StepLog, rep *model.CleanExecuteReport) {
	if len(r.docker) == 0 {
		return
	}
	ops := make([]engine.DeleteOp, 0, len(r.docker))
	for _, tg := range r.docker {
		ops = append(ops, engine.DeleteOp{Row: tg.Row, Kind: tg.Kind, Ref: cleanTargetRef(tg.ID), Size: tg.Size})
	}
	i := 0
	// 引擎保证「每项完成回调一次、回执与指令等长同序」，所以这里只按回调落账，不再遍历一遍回执。
	s.eng.DeleteDockerObjects(ctx, ops, func(d engine.DeleteResult) {
		if i < len(r.docker) {
			s.tallyOne(log, rep, r.docker[i], d)
			i++
		}
	})
}

// deleteHost 走宿主那一批（文件、网卡、命名空间、cgroup、docker 组成员）。
//
// Kind 在这里只是占位：真正用哪种删法由引擎的分类门现算（服务层传来的不作数）。
// Ref 只能是预览里那份扫描回来的原文。
func (r *cleanRun) deleteHost(ctx context.Context, s *DockerCleanService, log task.StepLog, rep *model.CleanExecuteReport) {
	if len(r.host) == 0 {
		return
	}
	ops := make([]engine.DeleteOp, 0, len(r.host))
	for _, tg := range r.host {
		ops = append(ops, engine.DeleteOp{Row: tg.Row, Kind: tg.Kind, Ref: cleanTargetRef(tg.ID), Size: tg.Size,
			NeedsRoot: tg.NeedsRoot, TrashIt: cleanTrashedRows[tg.Row]})
	}
	i := 0
	s.eng.DeleteHostObjects(ctx, ops, r.info, s.trash, func(d engine.DeleteResult) {
		if i < len(r.host) {
			s.tallyOne(log, rep, r.host[i], d)
			if d.Err == nil && d.TrashPath != "" {
				s.registerTrash(log, rep, d.Row, cleanTargetRef(r.host[i].ID), d.TrashPath)
			}
			i++
		}
	})
}

// tallyOne 落一条逐项结果：一行日志 + 一次事件 + 一个计数。
//
// 取消的那几条**既不算删掉也不算跳过**：它们根本没被动过，报成「已删除」是假账，
// 报成「已经不在了」也是假账，只能按「这次没做完」记进 failed 并给一行 dim。
func (s *DockerCleanService) tallyOne(log task.StepLog, rep *model.CleanExecuteReport, tg model.CleanTarget, res engine.DeleteResult) {
	name := tg.Name
	if name == "" {
		name = res.Ref
	}
	item := model.CleanedItem{Type: cleanItemType(res.Kind), Name: name}
	switch {
	case res.Err == nil:
		item.OK = true
		rep.Removed++
		rep.FreedBytes += res.Freed
		if res.Freed > 0 {
			log.Log(string(model.LogOk), fmt.Sprintf("已删除 %s：%s（腾出 %d 字节）", res.Kind, name, res.Freed))
		} else {
			log.Log(string(model.LogOk), "已删除 "+res.Kind+"："+name)
		}
		s.em.Emit("docker:cleanup", map[string]any{"stage": "removed", "resource": name, "action": res.Kind})
	case errors.Is(res.Err, context.Canceled):
		item.Error = res.Err.Error()
		rep.Failed++
		log.Log(string(model.LogDim), "这次取消，没动它: "+name)
	default:
		item.Error = res.Err.Error()
		rep.Failed++
		log.Log(string(model.LogErr), fmt.Sprintf("删除失败 %s：%s —— %s", res.Kind, name, res.Err))
	}
	rep.Items = append(rep.Items, item)
}

// registerTrash 把挪进回收站的那一份登记 7 天（§5.13.7）。
//
// 登记前先看落点在不在：源本来就不在时回收站里其实什么都没有，
// 记一条假的「7 天内可恢复」比不记更糟——用户会以为自己还能回来找。
func (s *DockerCleanService) registerTrash(log task.StepLog, rep *model.CleanExecuteReport, row, orig, dest string) {
	if dest == "" {
		return
	}
	if _, err := os.Stat(dest); err != nil {
		log.Log(string(model.LogDim), "回收站里没有这个落点，不登记: "+dest)
		return
	}
	if _, err := s.store.AddTrashItem(store.TrashItem{Kind: row, OrigPath: orig, TrashPath: dest}); err != nil {
		log.Log(string(model.LogErr), "回收站登记失败（东西已经在回收站目录里）: "+dest+" —— "+err.Error())
		return
	}
	rep.Trashed = append(rep.Trashed, dest)
	log.Log(string(model.LogDim), "已放进回收站，7 天内可恢复: "+orig+" → "+dest)
}

// uninstallAfterClean 在删除任务返回之后另起一单卸载（需求 ㉙，§0.2 规则 11）。
//
// 这颗开关默认不勾：删掉容器/镜像只让服务卡片显示成「缺失态」，「已安装」这本账不动；
// 用户勾了才走到这里。失败要说清是哪一个版本，且不影响本次已经完成的清理。
func (s *DockerCleanService) uninstallAfterClean(ctx context.Context, list []model.CleanInstalled, rep *model.CleanExecuteReport) error {
	var bad []string
	for _, in := range list {
		err := s.uninst.Remove(ctx, model.ServiceKind(in.Kind), in.Version)
		name := "卸载 " + in.Kind + " " + in.Version
		item := model.CleanedItem{Type: model.ResContainer, Name: name, OK: err == nil}
		if err != nil {
			item.Error = err.Error()
			bad = append(bad, fmt.Sprintf("%s %s：%s", in.Kind, in.Version, err))
		}
		rep.Items = append(rep.Items, item)
	}
	if len(bad) > 0 {
		return fmt.Errorf("资源已清理，但这几个版本没能卸载：%s", strings.Join(bad, "；"))
	}
	return nil
}

// cleanAuditStatus 把一次清理的收尾折成审计那一列的字：全删掉了才算 ok，有没删掉的就是 partial。
func cleanAuditStatus(rep *model.CleanExecuteReport) string {
	if rep.Failed > 0 {
		return "partial"
	}
	return "ok"
}

// cleanPresent 把这次读到的 Docker 对象摊成一张「还在不在」的查找表。
//
// 一个对象可能同时以 ID 和名字/标签被认（镜像就是这种：勾的是标签，清单里也有 ID），
// 两种都登记进去，否则会把一个明明还在的镜像说成「已经不在了」。
// 某一种这次没读到，就不给它的对象下任何结论——表里不出现，判定处按「不知道」处理。
func cleanPresent(inv engine.DockerInventory) map[string]bool {
	out := map[string]bool{}
	add := func(kind, ref string) {
		if ref != "" {
			out[cleanTargetID(kind, ref)] = true
		}
	}
	if !inv.Failed(engine.CatContainers) {
		for _, c := range inv.Containers {
			add(engine.DelContainer, c.Name)
		}
	}
	if !inv.Failed(engine.CatImages) {
		for _, im := range inv.Images {
			add(engine.DelImage, im.ID)
			for _, ref := range im.Refs {
				add(engine.DelImage, ref)
			}
		}
	}
	if !inv.Failed(engine.CatVolumes) {
		for _, v := range inv.Volumes {
			add(engine.DelVolume, v.Name)
		}
	}
	if !inv.Failed(engine.CatNetworks) {
		for _, n := range inv.Networks {
			add(engine.DelNetwork, n.ID)
			add(engine.DelNetwork, n.Name)
		}
	}
	if !inv.Failed(engine.CatPlugins) {
		for _, p := range inv.Plugins {
			add(engine.DelPlugin, p.Name)
			add(engine.DelPlugin, p.Reference)
		}
	}
	if !inv.Failed(engine.CatSwarmServices) {
		for _, sv := range inv.Services {
			add(engine.DelSwarmService, sv.ID)
			add(engine.DelSwarmService, sv.Name)
		}
	}
	if !inv.Failed(engine.CatSwarmConfigs) {
		for _, cf := range inv.Configs {
			add(engine.DelSwarmConfig, cf.ID)
			add(engine.DelSwarmConfig, cf.Name)
		}
	}
	return out
}
