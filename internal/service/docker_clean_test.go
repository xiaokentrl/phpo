// 全量清理服务层的用例：预览摊得出具体对象、一次性凭据只用一次、危险项要知情同意、
// 动手前核对现场、phpo 自己的卷先留底再删、单颗失败不中断整单、卸载另起一单。
//
// 全部走假件：不需要 Docker 守护进程，也不碰用户机器上的任何真实目录（宿主路径一律造在 t.TempDir() 里）。
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
)

// ---- 假件 ----

// dcEngine 是清点与删除的假出口：清单逐次给（预览一份、动手时再一份），删除只记账不真删。
type dcEngine struct {
	info   engine.DaemonInfo
	invs   []engine.DockerInventory
	host   engine.HostScanResult
	scanNo int

	dockerOps []engine.DeleteOp
	hostOps   []engine.DeleteOp
	failRefs  map[string]bool
	// trashRoot 是假件里「挪进回收站」的落点根目录（用 t.TempDir()，不碰用户的回收站）。
	trashRoot string
	// hostFails 让某一条宿主指令失败（演「数据挪不进回收站」那一档）
	hostFails map[string]bool
}

func (e *dcEngine) ScanDockerInventory(context.Context, engine.StageFn) engine.DockerInventory {
	inv := e.invs[len(e.invs)-1]
	if e.scanNo < len(e.invs) {
		inv = e.invs[e.scanNo]
	}
	e.scanNo++
	return inv
}

func (e *dcEngine) DockerInfo(context.Context) engine.DaemonInfo { return e.info }

func (e *dcEngine) ScanHost(context.Context, engine.DaemonInfo, engine.StageFn) (engine.HostScanResult, error) {
	return e.host, nil
}

func (e *dcEngine) RescanHostRow(_ context.Context, key string, _ engine.DaemonInfo) engine.HostRow {
	for _, h := range e.host.Rows {
		if h.Key == key {
			return h
		}
	}
	return engine.HostRow{Key: key, Status: model.RowUnavailable, Message: "假件没有这一行"}
}

func (e *dcEngine) HostPathFacts([]string) []engine.HostPathFact { return nil }

func (e *dcEngine) DeleteDockerObjects(_ context.Context, ops []engine.DeleteOp, onItem engine.ItemFn) []engine.DeleteResult {
	out := make([]engine.DeleteResult, 0, len(ops))
	for _, op := range ops {
		e.dockerOps = append(e.dockerOps, op)
		res := engine.DeleteResult{Row: op.Row, Kind: op.Kind, Ref: op.Ref, Freed: op.Size}
		if e.failRefs[op.Ref] {
			res.Freed, res.Err = 0, errors.New("镜像正被容器占用")
		}
		out = append(out, res)
		if onItem != nil {
			onItem(res)
		}
	}
	return out
}

func (e *dcEngine) DeleteHostObjects(_ context.Context, ops []engine.DeleteOp, _ engine.DaemonInfo,
	_ *engine.Trash, onItem engine.ItemFn,
) []engine.DeleteResult {
	out := make([]engine.DeleteResult, 0, len(ops))
	for _, op := range ops {
		e.hostOps = append(e.hostOps, op)
		res := engine.DeleteResult{Row: op.Row, Kind: engine.DelHostPath, Ref: op.Ref, Freed: op.Size}
		if e.hostFails[op.Ref] {
			res.Freed, res.Err = 0, errors.New("挪动它需要管理员授权")
		} else if op.TrashIt {
			// 落点要真在盘上：服务层登记回收站前会看一眼，落点不在就不登记（不记一条假的「可恢复」）。
			dest := filepath.Join(e.trashRoot, filepath.Base(op.Ref))
			if err := os.MkdirAll(dest, 0o777); err != nil {
				res.Freed, res.Err = 0, err
			} else {
				res.Freed, res.TrashPath = 0, dest
			}
		}
		out = append(out, res)
		if onItem != nil {
			onItem(res)
		}
	}
	return out
}

type dcStore struct {
	snap    *model.Snapshot
	trash   []store.TrashItem
	trashNo int
}

func (s *dcStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, nil }

func (s *dcStore) AddTrashItem(it store.TrashItem) (int64, error) {
	s.trashNo++
	if it.OrigPath == "" || it.TrashPath == "" {
		return 0, errors.New("回收站登记缺路径")
	}
	if _, err := os.Stat(it.TrashPath); err != nil {
		return 0, fmt.Errorf("回收站里没有这个落点: %s", it.TrashPath)
	}
	it.ID = int64(s.trashNo)
	s.trash = append(s.trash, it)
	return it.ID, nil
}

func (s *dcStore) AppendOperation(model.Operation) error { return nil }

type dcAudit struct{ ops []model.Operation }

func (a *dcAudit) Log(op model.Operation) error {
	a.ops = append(a.ops, op)
	return nil
}

type dcUninst struct {
	calls []string
	fail  map[string]bool
	// doneAtCall 记下被调用那一刻「已经收尾的任务数」——用来证明卸载是在删除任务返回之后才发生的。
	doneAtCall []int
	probe      func() int
}

func (u *dcUninst) Remove(_ context.Context, kind model.ServiceKind, version string) error {
	key := string(kind) + ":" + version
	u.calls = append(u.calls, key)
	if u.probe != nil {
		u.doneAtCall = append(u.doneAtCall, u.probe())
	}
	if u.fail[key] {
		return errors.New("容器还在被站点使用")
	}
	return nil
}

// ---- 取材助手 ----

func dcContainer(name string, running, paused bool, size int64) engine.InvContainer {
	return engine.InvContainer{ID: "id-" + name, Name: name, Running: running, Paused: paused, SizeRw: size}
}

func dcVolume(name, mount string, size int64) engine.InvVolume {
	return engine.InvVolume{Name: name, Driver: "local", Mountpoint: mount, Size: size, HasSize: true}
}

// dcSvc 起一套服务：假引擎 + 一份「phpo 装了 php 8.4」的快照 + 真回收站目录。
func dcSvc(t *testing.T, eng *dcEngine, uninst *dcUninst) (*DockerCleanService, *dcStore, *dcAudit, *fakeEmitter) {
	t.Helper()
	st := &dcStore{snap: &model.Snapshot{
		Installed: map[string][]string{"php": {"8.4"}},
		Sites:     []model.Site{},
	}}
	audit := &dcAudit{}
	em := &fakeEmitter{}
	trash := engine.NewTrash(t.TempDir() + "/trash")
	eng.trashRoot = filepath.Join(t.TempDir(), "landed")
	svc := NewDockerCleanService(eng, st, audit, uninst, trash, em, task.NewManager(em))
	return svc, st, audit, em
}

// dcPreview 先扫一遍再预览，把两次调用都收在这一个助手里。
func dcPreview(t *testing.T, svc *DockerCleanService, deep bool, rows ...string) (model.CleanPreview, error) {
	t.Helper()
	if _, err := svc.Scan(context.Background(), deep); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	return svc.Preview(context.Background(), rows)
}

// dcRoot 是这一条用例自己的宿主根（同一个 TempDir 下拼层级，不碰用户机器上的真实目录）。
var dcCaseRoot = map[*testing.T]string{}

func dcRoot(t *testing.T) string {
	t.Helper()
	if p, ok := dcCaseRoot[t]; ok {
		return p
	}
	p := filepath.Join(t.TempDir(), "host")
	if err := os.MkdirAll(p, 0o777); err != nil {
		t.Fatalf("造宿主根失败: %v", err)
	}
	dcCaseRoot[t] = p
	return p
}

// dcExist 造一个「此刻确实在盘上」的宿主路径——现场核对那一趟要看它还在不在。
func dcExist(t *testing.T, sub string) string {
	t.Helper()
	p := filepath.Join(dcRoot(t), filepath.FromSlash(sub))
	if err := os.MkdirAll(p, 0o777); err != nil {
		t.Fatalf("造目录失败: %v", err)
	}
	return p
}

// ---- 预览 ----

// TestCleanPreview_NamesEveryObject 是这一步存在的理由：
// 界面那一行写的是「3 个容器」，确认框必须摊开成三个名字，谁都不该被含糊地一起带走。
func TestCleanPreview_NamesEveryObject(t *testing.T) {
	eng := &dcEngine{
		info: engine.DaemonInfo{OK: true, Rootless: true},
		invs: []engine.DockerInventory{{
			Containers: []engine.InvContainer{
				dcContainer("phpo-php-8.4", true, false, 10),
				dcContainer("nginx-web", false, false, 20),
			},
			Images:   []engine.InvImage{{ID: "sha256:aaa", Refs: []string{"php:8.4-fpm", "local/wp:1"}, Size: 100}},
			Volumes:  []engine.InvVolume{dcVolume("phpo-mysql-8.4-data", "/var/lib/docker/volumes/x/_data", 5)},
			Networks: []engine.InvNetwork{{ID: "n1", Name: "bridge"}, {ID: "n2", Name: "phpo-network"}},
			BuildCache: []engine.InvBuildCache{
				{ID: "b1", Size: 30}, {ID: "b2", Size: 70},
			},
		}},
	}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	pv, err := dcPreview(t, svc, false,
		"container.all", "image.all", "volume.all", "network.all", "build.cache")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if len(pv.Targets) != 6 {
		t.Fatalf("应摊出 6 项（2 容器 + 1 镜像 + 1 卷 + 1 网络 + 1 份构建缓存），实得 %d：%s",
			len(pv.Targets), dcNames(pv.Targets))
	}
	// 构建缓存只有「整份清掉」这一条接口，所以永远是一颗开关，不是两条记录。
	cache := dcFind(pv.Targets, engine.DelBuildCache)
	if cache == nil {
		t.Fatal("构建缓存这一行应给出那一项整份清扫")
	}
	if cache.Name != "整份构建缓存（2 条记录）" || cache.Size != 100 {
		t.Fatalf("整份清扫要带上条数与总体积，实得 %q / %d", cache.Name, cache.Size)
	}
	// Docker 自带的 bridge 不列进确认清单：那颗开关点下去注定失败，还删掉的是 Docker 自己的东西。
	for _, tg := range pv.Targets {
		if tg.Kind == engine.DelNetwork && tg.ID == "network|bridge" {
			t.Fatal("Docker 自带的 bridge 不该被列进确认清单")
		}
	}
	if len(pv.Warnings) == 0 || !strings.Contains(pv.Warnings[0], "host / bridge / null") {
		t.Fatalf("跳过自带网络这件事必须说出来，不能让用户以为漏数了：%v", pv.Warnings)
	}
	// Foreign 是「这不是 phpo 建的」——确认框要靠它把 phpo 自己的东西单独标出来。
	web := dcFindByRef(pv.Targets, "nginx-web")
	if web == nil || !web.Foreign {
		t.Fatalf("用户自己建的容器要标成外来，实得 %+v", web)
	}
	if mine := dcFindByRef(pv.Targets, "phpo-php-8.4"); mine == nil || mine.Foreign {
		t.Fatalf("phpo 自己建的容器不该标外来，实得 %+v", mine)
	}
	// 「同时卸载」那颗开关只在这次真会动到已装版本时才出现。
	if len(pv.Installed) != 1 || pv.Installed[0].Kind != "php" || pv.Installed[0].Version != "8.4" {
		t.Fatalf("这次动到了 phpo 的 php 8.4，应带出一条可卸载项，实得 %+v", pv.Installed)
	}
	if pv.ConsentRequired {
		t.Fatal("没勾危险项，不该要求知情同意")
	}
}

// TestCleanPreview_HostRowsNeedDeepScan 说的是浅扫时不许假装数过了。
//
// 宿主那 28 行只有点过「详细扫描」才有路径可读。浅扫完就预览，
// 要么给出真路径、要么明说还没读——给一张空单子或凭印象拼出来的路径都删错东西。
func TestCleanPreview_HostRowsNeedDeepScan(t *testing.T) {
	hostPath := dcExist(t, "volumes") + "/abc"
	eng := &dcEngine{
		info: engine.DaemonInfo{OK: true, Rootless: true, Root: filepath.Dir(hostPath)},
		invs: []engine.DockerInventory{{}},
		host: engine.HostScanResult{Rows: []engine.HostRow{{
			Key: "volume.data", Status: model.RowOK, Count: 1, Paths: []string{hostPath},
		}}},
	}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	// 浅扫一次（没读宿主）就预览这一行：不该摊出任何东西。
	if _, err := dcPreview(t, svc, false, "volume.data"); err == nil {
		t.Fatal("浅扫时宿主行没有可读的路径，预览必须拒绝而不是给一张空单子")
	} else if !strings.Contains(err.Error(), "详细扫描") {
		t.Fatalf("要告诉用户下一步是点「详细扫描」，实得 %q", err)
	}

	pv, err := dcPreview(t, svc, true, "volume.data")
	if err != nil {
		t.Fatalf("深扫后预览失败: %v", err)
	}
	if len(pv.Targets) != 1 || pv.Targets[0].ID != "host_path|"+hostPath {
		t.Fatalf("宿主项的引用只能是扫描回来的路径原文，实得 %+v", pv.Targets)
	}
	// rootless 的这台机器上这些目录属主是自己，不需要授权；rootful 才要。
	if pv.Targets[0].NeedsRoot {
		t.Fatal("rootless 守护进程写出来的目录属主是当前用户，不该标成要授权")
	}
}

// TestCleanPreview_RefusesRowsWithNoObjects 挡住三类不该来预览的行。
func TestCleanPreview_RefusesRowsWithNoObjects(t *testing.T) {
	eng := &dcEngine{
		info: engine.DaemonInfo{OK: true, Rootless: true},
		invs: []engine.DockerInventory{{
			Containers: []engine.InvContainer{dcContainer("phpo-php-8.4", true, false, 10)},
		}},
	}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	if _, err := dcPreview(t, svc, false); err == nil || !strings.Contains(err.Error(), "一行都没勾") {
		t.Fatalf("一行都没勾要拦住，实得 %v", err)
	}
	if _, err := dcPreview(t, svc, false, "network.iptables"); err == nil ||
		!strings.Contains(err.Error(), "没有安全的删法") {
		t.Fatalf("第三档的行（界面本不该给出按钮）要拒，实得 %v", err)
	}
	if _, err := dcPreview(t, svc, false, "container.nope"); err == nil ||
		!strings.Contains(err.Error(), "清单里没有") {
		t.Fatalf("行名对不上要拒——拿一份没人扫过的单子去删东西是危险的，实得 %v", err)
	}
	// 数字真实但没有单独可删对象的行：要说清占用来自哪里，而不是凑一份假清单。
	pv, err := dcPreview(t, svc, false, "container.all", "container.layer")
	if err != nil {
		t.Fatalf("混合勾选应照常给出容器那几项: %v", err)
	}
	if len(pv.Targets) != 1 {
		t.Fatalf("可写层不该凑出对象，实得 %+v", pv.Targets)
	}
	if !strings.Contains(strings.Join(pv.Warnings, "|"), "跟着容器一起走") {
		t.Fatalf("要逐行说明这一行为什么交不出清单，实得 %v", pv.Warnings)
	}
}

// TestCleanPreview_DangerNeedsConsent 锁住那 4 行危险项（需求 ⑭/㉖/㉚）。
//
// 判据只有那份名单，行上的 risk 徽标不参与：徽标是颜色，名单才是闸门。
func TestCleanPreview_DangerNeedsConsent(t *testing.T) {
	usr := "webdev"
	eng := &dcEngine{
		info: engine.DaemonInfo{OK: true, Rootless: true},
		invs: []engine.DockerInventory{{
			Containers: []engine.InvContainer{dcContainer("phpo-php-8.4", true, false, 10)},
			Networks:   []engine.InvNetwork{{ID: "n2", Name: "phpo-network"}},
		}},
		host: engine.HostScanResult{Rows: []engine.HostRow{
			{Key: "system.group", Status: model.RowOK, Count: 1, Paths: []string{usr}},
		}},
	}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	pv, err := dcPreview(t, svc, true, "system.group")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if !pv.ConsentRequired {
		t.Fatal("docker 组这一行是危险项，必须要求知情同意")
	}
	// 组成员这一行不给授权也过不了：改用户组本来就要 root。
	if len(pv.Targets) != 1 || !pv.Targets[0].NeedsRoot {
		t.Fatalf("移出 docker 组要提权，实得 %+v", pv.Targets)
	}

	// 没点头就提交：拒掉，并且**不作废**这张凭据——用户看完后果还回来接着清空。
	if _, err := svc.Execute(context.Background(), model.CleanRequest{Token: pv.Token, IDs: []string{pv.Targets[0].ID}}); err == nil ||
		!strings.Contains(err.Error(), "危险项") {
		t.Fatalf("危险项没确认就该拦住，实得 %v", err)
	}
	rep, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv.Token, IDs: []string{pv.Targets[0].ID}, Consent: true})
	if err != nil {
		t.Fatalf("确认之后应放行: %v", err)
	}
	if rep.Removed != 1 {
		t.Fatalf("这一项应删掉，实得 %+v", rep)
	}
}

// ---- 执行 ----

// TestCleanExecute_OneTokenOnce 锁住「一次凭据只能用一次」。
//
// 同一份预览点两次清空，第二次拿的是几分钟前的清单：那批对象可能已经被第一次删掉了，
// 也可能被用户自己在命令行动过。让他重新预览一次，比让一份旧单子再去删一次便宜得多。
func TestCleanExecute_OneTokenOnce(t *testing.T) {
	eng := &dcEngine{
		info: engine.DaemonInfo{OK: true, Rootless: true},
		invs: []engine.DockerInventory{{
			Containers: []engine.InvContainer{dcContainer("nginx-web", false, false, 20)},
		}},
	}
	svc, _, audit, em := dcSvc(t, eng, &dcUninst{})

	pv, err := dcPreview(t, svc, false, "container.all")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	req := model.CleanRequest{Token: pv.Token, IDs: []string{pv.Targets[0].ID}}
	if _, err := svc.Execute(context.Background(), req); err != nil {
		t.Fatalf("第一次清空应成功: %v", err)
	}
	if _, err := svc.Execute(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "已经用过") {
		t.Fatalf("同一张凭据不该能用两次，实得 %v", err)
	}
	// 清单外的 ID：等于跳过用户点头那一步，一律拒。
	pv2, err := dcPreview(t, svc, false, "container.all")
	if err != nil {
		t.Fatalf("重新预览失败: %v", err)
	}
	if _, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv2.Token, IDs: []string{"volume|not-in-this-preview"}}); err == nil ||
		!strings.Contains(err.Error(), "这次预览里没有") {
		t.Fatalf("清单外的 ID 要拒，实得 %v", err)
	}
	// 过期也要拒，而且拒完就把这张作废（下一次拿这张再来点，只会得到「已经用过」）。
	svc.now = func() time.Time { return pv2.ExpiresAt.Add(time.Minute) }
	if _, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv2.Token, IDs: dcIDs(pv2.Targets)}); err == nil ||
		!strings.Contains(err.Error(), "请重新预览") {
		t.Fatalf("过期的预览要拒，实得 %v", err)
	}
	// 一次清空 = 一个任务（需求 ⑲）：收尾事件只该有一次。
	if n := dcCount(em.events, "task:done"); n != 1 {
		t.Fatalf("成功的清空应只产生一个任务，实得 task:done %d 次", n)
	}
	if !dcHasOp(audit.ops, "docker-clean") {
		t.Fatalf("清空要落一行审计，实得 %+v", audit.ops)
	}
}

// TestCleanExecute_SkipsWhatVanishedAfterPreview 是需求 ⑧ 的那一步：动手前核对现场。
//
// 预览之后用户自己用 docker rm 删掉了一个容器。这一项要落到 Skipped 并逐行说明，
// 而不是被送去删之后再回报一条看不懂的失败——用户要的结果（「现在没有了」）其实已经成立。
func TestCleanExecute_SkipsWhatVanishedAfterPreview(t *testing.T) {
	two := engine.DockerInventory{Containers: []engine.InvContainer{
		dcContainer("a", false, false, 1), dcContainer("b", false, false, 2),
	}}
	one := engine.DockerInventory{Containers: []engine.InvContainer{dcContainer("a", false, false, 1)}}
	eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true}, invs: []engine.DockerInventory{two, one}}
	svc, _, _, em := dcSvc(t, eng, &dcUninst{})

	pv, err := dcPreview(t, svc, false, "container.all")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	ids := []string{pv.Targets[0].ID, pv.Targets[1].ID}
	rep, err := svc.Execute(context.Background(), model.CleanRequest{Token: pv.Token, IDs: ids})
	if err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if rep.Skipped != 1 || rep.Removed != 1 || rep.Failed != 0 {
		t.Fatalf("应删掉 1 项、跳过 1 项、不报失败，实得 %+v", rep)
	}
	if len(eng.dockerOps) != 1 || eng.dockerOps[0].Ref != "a" {
		t.Fatalf("已经不在了的那一项不该被送去删，实得 %+v", eng.dockerOps)
	}
	if rep.FreedBytes != 1 {
		t.Fatalf("只按真删掉的那一项记账，实得 %d", rep.FreedBytes)
	}
	if !strings.Contains(dcJoined(em.logs), "已经不在了，跳过: b") {
		t.Fatalf("跳过要逐行看得见，实得 %v", em.logs)
	}
}

// TestCleanExecute_KeepsPhpoVolumeDataFirst 是需求 ④/⑳：有数据的先留底，留不住就不删。
func TestCleanExecute_KeepsPhpoVolumeDataFirst(t *testing.T) {
	mp := dcExist(t, "volumes") + "/phpo-mysql-8.4-data"
	inv := engine.DockerInventory{Volumes: []engine.InvVolume{
		dcVolume("phpo-mysql-8.4-data", mp, 4096),
		dcVolume("someone-elses-volume", "/var/lib/docker/volumes/other/_data", 1),
	}}
	t.Run("能留底：先挪进回收站，再删 Docker 那头的记录", func(t *testing.T) {
		eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true},
			invs: []engine.DockerInventory{inv, inv}}
		svc, st, _, _ := dcSvc(t, eng, &dcUninst{})
		pv, err := dcPreview(t, svc, false, "volume.all")
		if err != nil {
			t.Fatalf("预览失败: %v", err)
		}
		rep, err := svc.Execute(context.Background(), model.CleanRequest{Token: pv.Token, IDs: dcIDs(pv.Targets)})
		if err != nil {
			t.Fatalf("清空失败: %v", err)
		}
		if len(eng.hostOps) != 1 {
			t.Fatalf("phpo 自己的卷要先走一次回收站，实得 %+v", eng.hostOps)
		}
		first := eng.hostOps[0]
		if first.Row != "volume.data" || !first.TrashIt || first.Ref != mp {
			t.Fatalf("留底的是这个卷的数据目录本身，路径不能重新拼，实得 %+v", first)
		}
		if first.NeedsRoot {
			t.Fatal("rootless 的机器上这些目录属主是自己，不该多要一次授权")
		}
		if len(eng.dockerOps) != 2 {
			t.Fatalf("留过底的卷与别人建的卷都该走 SDK 删除，实得 %+v", eng.dockerOps)
		}
		if rep.Removed != 2 || rep.Failed != 0 {
			t.Fatalf("两个卷都应删掉，实得 %+v", rep)
		}
		// 只有 phpo 自己的卷进回收站：别人的卷不归 phpo 代管。
		if len(st.trash) != 1 || st.trash[0].Kind != "volume.data" || st.trash[0].OrigPath != mp {
			t.Fatalf("回收站应只登记这一卷，实得 %+v", st.trash)
		}
	})

	t.Run("留不住底：这一卷不删，其余照常删完", func(t *testing.T) {
		eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true},
			invs: []engine.DockerInventory{inv, inv}, hostFails: map[string]bool{mp: true}}
		svc, st, _, em := dcSvc(t, eng, &dcUninst{})
		pv, err := dcPreview(t, svc, false, "volume.all")
		if err != nil {
			t.Fatalf("预览失败: %v", err)
		}
		rep, err := svc.Execute(context.Background(), model.CleanRequest{Token: pv.Token, IDs: dcIDs(pv.Targets)})
		if err != nil {
			t.Fatalf("单项没做成不该判死整单: %v", err)
		}
		for _, op := range eng.dockerOps {
			if op.Ref == "phpo-mysql-8.4-data" {
				t.Fatal("数据没能挪进回收站却还去删这个卷，等于删掉一份找不回来的数据")
			}
		}
		if rep.Removed != 1 || rep.Failed != 1 {
			t.Fatalf("应删成 1 项、失败 1 项，实得 %+v", rep)
		}
		if len(st.trash) != 0 {
			t.Fatalf("没挪成功就不该登记回收站，实得 %+v", st.trash)
		}
		if !strings.Contains(dcJoined(em.logs), "数据没能挪进回收站") {
			t.Fatalf("要逐行说清这一卷为什么没删，实得 %v", em.logs)
		}
	})
}

// TestCleanExecute_HostOpsKeepRawPathAndTrashRules 锁住调用引擎的那份契约。
//
// 两件事最容易在这一层写错：Kind 与 Ref。Kind 只是占位（真正用哪种删法由引擎的分类门现算），
// Ref 只能是扫描回来的路径原文；「先进回收站」只对那几行有数据的东西开，
// 把日志和缓存挪进回收站等于磁盘一点没腾出来。
func TestCleanExecute_HostOpsKeepRawPathAndTrashRules(t *testing.T) {
	base := dcExist(t, "root")
	volData := dcExist(t, "root/volumes/abc")
	logs := dcExist(t, "root/containers/xyz/00e3-json.log")
	cni := dcExist(t, "root/cni/net.d/10-x.conflist")
	group := "webdev"

	host := engine.HostScanResult{Rows: []engine.HostRow{
		{Key: "volume.data", Status: model.RowOK, Paths: []string{volData}},
		{Key: "log.container", Status: model.RowOK, Paths: []string{logs}},
		{Key: "network.cni", Status: model.RowOK, Paths: []string{cni}},
		{Key: "system.group", Status: model.RowOK, Paths: []string{group}},
	}}
	eng := &dcEngine{
		info: engine.DaemonInfo{OK: true, Rootless: false, Root: base},
		invs: []engine.DockerInventory{{}, {}},
		host: host,
	}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	pv, err := dcPreview(t, svc, true, "volume.data", "log.container", "network.cni", "system.group")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if _, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv.Token, IDs: dcIDs(pv.Targets), Consent: true}); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if len(eng.hostOps) != 4 {
		t.Fatalf("四项宿主对象应各一条指令，实得 %+v", eng.hostOps)
	}
	byRow := map[string]engine.DeleteOp{}
	for _, op := range eng.hostOps {
		byRow[op.Row] = op
	}
	if byRow["volume.data"].Ref != volData || !byRow["volume.data"].TrashIt {
		t.Fatalf("卷数据要按原路径进回收站，实得 %+v", byRow["volume.data"])
	}
	if byRow["log.container"].Ref != logs || byRow["log.container"].TrashIt {
		t.Fatalf("日志直接删——挪进回收站等于没腾出磁盘，实得 %+v", byRow["log.container"])
	}
	if byRow["network.cni"].Ref != cni || byRow["network.cni"].TrashIt {
		t.Fatalf("CNI 配置按原路径删，不进回收站，实得 %+v", byRow["network.cni"])
	}
	// 组成员那一行的原文是裸用户名，不是路径。
	if byRow["system.group"].Ref != group || !byRow["system.group"].NeedsRoot {
		t.Fatalf("移出 docker 组给的是用户名，且本来就要授权，实得 %+v", byRow["system.group"])
	}
	// rootful 的这台机器：宿主那几片都是 root 写的，逐项都要提权。
	for row, op := range byRow {
		if !op.NeedsRoot {
			t.Fatalf("rootful 守护进程写出来的东西属主是 root，%s 这一项必须提权", row)
		}
	}
}

// TestCleanExecute_UninstallIsSecondTask 锁住需求 ㉙ 的那颗默认不勾的开关。
//
// 卸载不能塞进删除那一单里：任务里再嵌套提交一单会直接被拒（ErrBusy）。
// 默认只让服务卡片显示成「缺失态」，「已安装」这本账不动；勾了才另起一单。
func TestCleanExecute_UninstallIsSecondTask(t *testing.T) {
	inv := engine.DockerInventory{Containers: []engine.InvContainer{dcContainer("phpo-php-8.4", true, false, 10)}}
	eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true}, invs: []engine.DockerInventory{inv, inv}}
	un := &dcUninst{}
	svc, _, _, em := dcSvc(t, eng, un)
	// 假件自己不会开任务，所以「卸载排在删除之后」只能这样证：被调用时删除那一单已经收尾。
	un.probe = func() int { return dcCount(em.events, "task:done") }

	pv, err := dcPreview(t, svc, false, "container.all")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if len(pv.Installed) != 1 {
		t.Fatalf("这次动到了 php 8.4，应带出可卸载项，实得 %+v", pv.Installed)
	}

	// 没勾「同时卸载」：一次都不该调 Remove。
	if _, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv.Token, IDs: dcIDs(pv.Targets)}); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if len(un.calls) != 0 {
		t.Fatalf("没勾那颗开关就不该卸载任何东西，实得 %v", un.calls)
	}

	// 勾了：卸载是删除任务返回之后的另一单，收尾事件因此是两次。
	pv2, err := dcPreview(t, svc, false, "container.all")
	if err != nil {
		t.Fatalf("重新预览失败: %v", err)
	}
	rep, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv2.Token, IDs: dcIDs(pv2.Targets), Uninstall: true})
	if err != nil {
		t.Fatalf("带卸载的清空失败: %v", err)
	}
	if len(un.calls) != 1 || un.calls[0] != "php:8.4" {
		t.Fatalf("应另起一单卸载 php 8.4，实得 %v", un.calls)
	}
	// 到这里只调过一次卸载（第一单没勾那颗开关）。被调用时删除那一单已经收尾，才说明它没挤进删除单里。
	if len(un.doneAtCall) != 1 || un.doneAtCall[0] < 2 {
		t.Fatalf("卸载必须排在删除任务返回之后：调用时的收尾任务数 %v，应为 2（两次清空各一单）", un.doneAtCall)
	}
	if rep.Removed != 1 {
		t.Fatalf("卸载不影响本次清理的记账，实得 %+v", rep)
	}

	// 卸载失败要说清是哪个版本，且本次清理的结果照常带回。
	un.fail = map[string]bool{"php:8.4": true}
	pv3, err := dcPreview(t, svc, false, "container.all")
	if err != nil {
		t.Fatalf("重新预览失败: %v", err)
	}
	rep3, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv3.Token, IDs: dcIDs(pv3.Targets), Uninstall: true})
	if err == nil || !strings.Contains(err.Error(), "没能卸载") {
		t.Fatalf("卸载失败要说清是哪个版本，实得 %v", err)
	}
	if rep3 == nil || rep3.Removed != 1 {
		t.Fatalf("卸载失败不该抹掉本次已完成的清理结果，实得 %+v", rep3)
	}
	if len(un.doneAtCall) != 2 {
		t.Fatalf("第三单勾了开关，应再调一次卸载，实得 %v", un.doneAtCall)
	}
}

// TestCleanExecute_OneFailureDoesNotStopTheRest 是这一层最该说清的一件事（需求 ⑲）：
// 勾了三项、中间一项失败，剩下那项照样删完，界面逐行点名——而不是整单落空。
func TestCleanExecute_OneFailureDoesNotStopTheRest(t *testing.T) {
	inv := engine.DockerInventory{
		Images: []engine.InvImage{{ID: "sha256:busy", Refs: []string{"php:8.4-fpm"}, Size: 50},
			{ID: "sha256:free", Refs: []string{"redis:8"}, Size: 60}},
	}
	eng := &dcEngine{
		info:     engine.DaemonInfo{OK: true, Rootless: true},
		invs:     []engine.DockerInventory{inv, inv},
		failRefs: map[string]bool{"sha256:busy": true},
	}
	svc, _, audit, em := dcSvc(t, eng, &dcUninst{})

	pv, err := dcPreview(t, svc, false, "image.all")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	rep, err := svc.Execute(context.Background(), model.CleanRequest{Token: pv.Token, IDs: dcIDs(pv.Targets)})
	if err != nil {
		t.Fatalf("单项失败不该判死整单: %v", err)
	}
	if rep.Removed != 1 || rep.Failed != 1 || rep.FreedBytes != 60 {
		t.Fatalf("应删成一项、失败一项、只按删成的记账，实得 %+v", rep)
	}
	if len(rep.Items) != 2 || rep.Items[0].OK == rep.Items[1].OK {
		t.Fatalf("逐项结果要一行一条、成与不成各占一行，实得 %+v", rep.Items)
	}
	if !strings.Contains(dcJoined(em.logs), "删除失败 image：") {
		t.Fatalf("失败那一行要带原因进抽屉，实得 %v", em.logs)
	}
	// 逐项删除各广播一次既有 docker:cleanup（事件名一个不增，需求 ㉔）。
	if n := dcCount(em.events, "docker:cleanup"); n != 1 {
		t.Fatalf("只有真删掉的那一项才该广播 removed，实得 %d 次", n)
	}
	if audit.ops[len(audit.ops)-1].Status != "partial" {
		t.Fatalf("有没删掉的项，审计不能写成 ok，实得 %+v", audit.ops[len(audit.ops)-1])
	}
}

// ---- 小助手 ----

func dcNames(ts []model.CleanTarget) string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return strings.Join(out, ", ")
}

func dcIDs(ts []model.CleanTarget) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func dcFind(ts []model.CleanTarget, kind string) *model.CleanTarget {
	for i, t := range ts {
		if t.Kind == kind {
			return &ts[i]
		}
	}
	return nil
}

func dcFindByRef(ts []model.CleanTarget, ref string) *model.CleanTarget {
	for i, t := range ts {
		if t.Name == ref {
			return &ts[i]
		}
	}
	return nil
}

func dcCount(events []string, name string) int {
	n := 0
	for _, e := range events {
		if e == name {
			n++
		}
	}
	return n
}

func dcJoined(lines []string) string { return strings.Join(lines, "\n") }

// dcHasOp 在审计流水里找一类操作（预览与清空各落一行，顺序会随用例动作变，只问「有没有」）。
func dcHasOp(ops []model.Operation, name string) bool {
	for _, o := range ops {
		if o.Op == name {
			return true
		}
	}
	return false
}

// TestCleanHostRowKeysMatchEngine 是两侧的那份对账：服务层按字符串登记了「必须由宿主深扫才有数」
// 的 28 行，引擎侧同一份表是小写常量、包外取不到。两处一旦漂开，症状很难看——
// 界面上某一行的数字明明扫出来了，预览却说「还没读过宿主文件」；或者反过来，
// 一行明明是 Docker 给的数，却被当成宿主行去要授权。这里逐字比一次集合。
func TestCleanHostRowKeysMatchEngine(t *testing.T) {
	eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true, Root: t.TempDir()},
		invs: []engine.DockerInventory{{}}}
	_, _, _, _ = dcSvc(t, eng, &dcUninst{})

	want := map[string]bool{}
	for _, k := range engine.HostRowKeys() {
		want[k] = true
	}
	if len(want) != len(cleanHostRowKeys) {
		t.Fatalf("两侧行数对不上：引擎 %d，服务层 %d", len(want), len(cleanHostRowKeys))
	}
	for k := range want {
		if !cleanHostRowKeys[k] {
			t.Fatalf("引擎会扫这一行，服务层却没登记：%s（这一行会被当成 Docker 侧的行，永不深扫）", k)
		}
	}
	for k := range cleanHostRowKeys {
		if !want[k] {
			t.Fatalf("服务层多登记了这一行，引擎并不去读：%s", k)
		}
	}
}

// TestCleanSwarmConfigRowCarriesBothKinds 锁住「配置 / 密钥」这一行的兑现：
// 界面这句话既数配置也数密钥，动手时两类都真得交给删除那一层。
//
// 密文与配置刻意同名：同名不同类在界面上必须分得开，删除时也不能因为「名字撞了」而被当成同一个对象。
func TestCleanSwarmConfigRowCarriesBothKinds(t *testing.T) {
	inv := engine.DockerInventory{
		Configs: []engine.InvNamed{{ID: "cfg1", Name: "db-password"}},
		Secrets: []engine.InvNamed{{ID: "sec1", Name: "db-password"}},
	}
	eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true},
		invs: []engine.DockerInventory{inv, inv}}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	report, err := svc.Scan(context.Background(), false)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	row := dcRow(report.Rows, "swarm.config")
	if row.Status != model.RowOK || row.Count != 2 {
		t.Fatalf("这一行叫「配置 / 密钥」，配置与密文各一份就该是 2，实得 %+v", row)
	}

	pv, err := dcPreview(t, svc, false, "swarm.config")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if len(pv.Targets) != 2 {
		t.Fatalf("两类对象都要进清单，实得 %+v", pv.Targets)
	}
	cfg, sec := dcFind(pv.Targets, engine.DelSwarmConfig), dcFind(pv.Targets, engine.DelSwarmSecret)
	if cfg == nil || sec == nil {
		t.Fatalf("清单里两类各要有一颗，实得 %+v", pv.Targets)
	}
	if cfg.ID != "swarm_config|cfg1" || sec.ID != "swarm_secret|sec1" {
		t.Fatalf("同名不同类要靠「类型+引用」分开，实得 %q / %q", cfg.ID, sec.ID)
	}
	if !strings.Contains(cfg.Name, "配置") || !strings.Contains(sec.Name, "密钥") {
		t.Fatalf("界面上要看得出删的是哪一类（密钥删了就没了、不落盘），实得 %q / %q", cfg.Name, sec.Name)
	}

	rep, err := svc.Execute(context.Background(), model.CleanRequest{
		Token: pv.Token, IDs: dcIDs(pv.Targets)})
	if err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	// 这一条同时证「动手前核对现场」那张表里有密文：漏了一类，好端端存在的密文会被报成
	// 「已经不在了，跳过」——既没删，界面还说删过了。
	if rep.Removed != 2 || rep.Skipped != 0 || rep.Failed != 0 {
		t.Fatalf("两份都该真删掉，实得 %+v", rep)
	}
	if !dcHasRef(eng.dockerOps, engine.DelSwarmSecret, "sec1") {
		t.Fatalf("密文要真的交给删除那一层，实得 %+v", eng.dockerOps)
	}
	if !dcHasRef(eng.dockerOps, engine.DelSwarmConfig, "cfg1") {
		t.Fatalf("配置照旧要删，实得 %+v", eng.dockerOps)
	}
}

// TestCleanSwarmConfigRowFailsWhenSecretsUnread 是「不知道」不等于「没有」在这一行的落法：
// 密文那一类没读到，整行给「读不到」，不得只拿配置那一份数字显示成 1 项、预览也只交一半单子。
func TestCleanSwarmConfigRowFailsWhenSecretsUnread(t *testing.T) {
	inv := engine.DockerInventory{
		Configs:  []engine.InvNamed{{ID: "cfg1", Name: "c"}},
		Failures: map[string]string{engine.CatSwarmSecrets: "daemon 说读不到"},
	}
	eng := &dcEngine{info: engine.DaemonInfo{OK: true, Rootless: true},
		invs: []engine.DockerInventory{inv, inv}}
	svc, _, _, _ := dcSvc(t, eng, &dcUninst{})

	report, err := svc.Scan(context.Background(), false)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	row := dcRow(report.Rows, "swarm.config")
	if row.Status != model.RowUnavailable {
		t.Fatalf("密文没读到就不能把这一行画成已数清，实得 %+v", row)
	}
	if _, err := svc.Preview(context.Background(), []string{"swarm.config"}); err == nil {
		t.Fatal("读不到的那一半不许拿一份残缺清单去删")
	}
}

func dcRow(rows []model.CleanRow, key string) model.CleanRow {
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	return model.CleanRow{}
}

func dcHasRef(ops []engine.DeleteOp, kind, ref string) bool {
	for _, o := range ops {
		if o.Kind == kind && o.Ref == ref {
			return true
		}
	}
	return false
}
