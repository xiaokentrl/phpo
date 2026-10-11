package engine

// 本文件是宿主侧采集器的用例。它一条 Docker 都不碰：
// 全部用 t.TempDir() 造目录，把该 stub 的两颗注入点（提权读取、外部命令）换掉，
// 因此 `go test` 期间不会弹 polkit 授权框，也不会真的去跑 iptables-save。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"phpo/internal/model"
)

// ---------- 用例助手（每一个都必须被调到，否则 CI 的 unused 检查会判红） ----------

// cmpStrings 逐项比对两组路径，要求同序同个数；一致时交空串，否则交一句人话差异。
func cmpStrings(got, want []string) string {
	if len(got) != len(want) {
		return fmt.Sprintf("个数不同：应为 %v，实得 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Sprintf("第 %d 项应为 %q，实得 %q", i+1, want[i], got[i])
		}
	}
	return ""
}

// hsListenUnix 在指定路径上开一只真的 Unix 套接字，用完关掉并删掉。
//
// 为什么不能用普通文件冒充：删除那道门判的是模式位（fs.ModeSocket），造个空文件它认不出来，
// 用例就会「绿在一个根本没被测到的东西上」。
func hsListenUnix(t *testing.T, p string) {
	t.Helper()
	ln, err := net.Listen("unix", p)
	if err != nil {
		t.Fatalf("造套接字失败：%v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(p)
	})
}

func hsEntry(src, path, rel string, isDir bool, size int64) hostEntry {
	return hostEntry{src: src, path: path, rel: rel, isDir: isDir, size: size}
}

// hsMustRow 取某一行的结果；这一行没出现就直接判失败——
// 「行整个消失」和「行显示 0」是两件不同的事，前者是缺陷（defect 9）。
func hsMustRow(t *testing.T, rows []HostRow, key string) HostRow {
	t.Helper()
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("结果里没有 %s 这一行", key)
	return HostRow{}
}

func hsHasPath(t *testing.T, r HostRow, want string) {
	t.Helper()
	for _, p := range r.Paths {
		if p == want {
			return
		}
	}
	t.Fatalf("%s 的路径清单里没有 %v（实得 %v）", r.Key, want, r.Paths)
}

// hsStubProbes 换掉两颗注入点，测试结束自动还原。
func hsStubProbes(t *testing.T, find func(context.Context, []string, int) (string, error),
	probe func(context.Context, string) (string, error)) {
	t.Helper()
	oldFind, oldProbe := elevateFind, probeCommand
	elevateFind, probeCommand = find, probe
	t.Cleanup(func() { elevateFind, probeCommand = oldFind, oldProbe })
}

func hsRefuse(context.Context, []string, int) (string, error) {
	return "", errors.New("用例不授权")
}

func hsNoCommand(context.Context, string) (string, error) { return "", exec.ErrNotFound }

func hsFindWith(out string) func(context.Context, []string, int) (string, error) {
	return func(context.Context, []string, int) (string, error) { return out, nil }
}

// ---------- 进度：报满 hostStagesTotal 格，且不重复 ----------

func TestHostProgressCoversEveryStage(t *testing.T) {
	hsStubProbes(t, hsRefuse, hsNoCommand)
	info := DaemonInfo{OK: true, Root: t.TempDir()}

	var seen []string
	onStage := StageFn(func(stage string) { seen = append(seen, stage) })
	scanHostSources(context.Background(), newHostScan(), info, nil, onStage)

	if len(seen) != hostStagesTotal {
		t.Fatalf("报了 %d 格进度，hostStagesTotal 是 %d", len(seen), hostStagesTotal)
	}
	uniq := map[string]bool{}
	for _, s := range seen {
		if uniq[s] {
			t.Fatalf("进度格 %q 报了两遍，界面那个分母就对不上了", s)
		}
		uniq[s] = true
	}
	if seen[len(seen)-1] != StageHostElevate {
		t.Fatalf("最后一格必须是提权读取，实得 %q", seen[len(seen)-1])
	}

	plan := PlanHostScan(info)
	if plan.Stages != hostStagesTotal {
		t.Fatalf("PlanHostScan 的 Stages=%d，与 hostStagesTotal=%d 不符", plan.Stages, hostStagesTotal)
	}
	if len(plan.Sources) != hostStagesTotal {
		t.Fatalf("PlanHostScan 的 Sources=%d 项，与 Stages=%d 不符", len(plan.Sources), hostStagesTotal)
	}
	for i, s := range plan.Sources {
		if s != seen[i] {
			t.Fatalf("第 %d 格计划是 %q，实报 %q", i, s, seen[i])
		}
	}
	if plan.Supported != (runtime.GOOS == "linux") {
		t.Fatalf("Supported=%v 与当前平台 GOOS=%s 不符", plan.Supported, runtime.GOOS)
	}
}

// 跳过的源也要报一格，否则前端的「已报到 / 总数」永远差几格。
func TestHostSkippedSourcesStillReport(t *testing.T) {
	hsStubProbes(t, hsRefuse, hsNoCommand)
	// info.OK=false：rootDep 的源全部跳过，但一格都不能少报。
	info := DaemonInfo{OK: false, Reason: "拿不到 Docker 数据目录"}

	var seen []string
	scanHostSources(context.Background(), newHostScan(), info, nil, StageFn(func(s string) {
		seen = append(seen, s)
	}))
	if len(seen) != hostStagesTotal {
		t.Fatalf("跳过时仍应报满 %d 格，实得 %d", hostStagesTotal, len(seen))
	}
}

// ---------- 容器目录：一类条目只落一行 ----------

func TestHostEntryContainers(t *testing.T) {
	h := newHostScan()
	root := "/docker"

	// 目录形态的 checkpoint：count 要涨，bytes 必须是 0（目录本身不算占用）。
	h.classify(hsEntry("containers", root+"/c1/checkpoint", "c1/checkpoint", true, 0))
	// 同容器第二个 checkpoint 不重复计数。
	h.classify(hsEntry("containers", root+"/c1/checkpoint-2", "c1/checkpoint-2", true, 0))

	// 滚动后的日志与在写的日志是两行，不能混。
	h.classify(hsEntry("containers", root+"/abc/abc-json.log.1.gz", "abc/abc-json.log.1.gz", false, 11))
	h.classify(hsEntry("containers", root+"/abc/abc-json.log", "abc/abc-json.log", false, 7))
	// 容器元数据：同容器多文件只计一次，路径记容器目录。
	h.classify(hsEntry("containers", root+"/zzz/config.v2.json", "zzz/config.v2.json", false, 3))
	h.classify(hsEntry("containers", root+"/zzz/hostconfig.json", "zzz/hostconfig.json", false, 4))
	// 容器根下的一层条目（还没进容器目录）不属任何行。
	h.classify(hsEntry("containers", root+"/uuid", "uuid", true, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: root})

	cp := hsMustRow(t, rows, rowContainerCheckpoint)
	if cp.Count != 1 {
		t.Fatalf("checkpoint 计 %d 个，应为 1（同一容器只算一次）", cp.Count)
	}
	if cp.Bytes != 0 {
		t.Fatalf("checkpoint 体积 %d，目录不该计入占用", cp.Bytes)
	}
	hsHasPath(t, cp, root+"/c1/checkpoint")

	if got := hsMustRow(t, rows, rowLogRotate); got.Count != 1 || got.Bytes != 11 {
		t.Fatalf("滚动日志行 count/bytes = %d/%d，应为 1/11", got.Count, got.Bytes)
	}
	if got := hsMustRow(t, rows, rowLogContainer); got.Count != 1 || got.Bytes != 7 {
		t.Fatalf("在写日志行 count/bytes = %d/%d，应为 1/7", got.Count, got.Bytes)
	}

	meta := hsMustRow(t, rows, rowContainerMeta)
	if meta.Count != 1 {
		t.Fatalf("容器元数据计 %d 个，同一容器两个文件应只算 1 个", meta.Count)
	}
	hsHasPath(t, meta, root+"/zzz")
}

// ---------- 数据卷：有 _data 与裸卷分开、裸卷路径只记名字 ----------

func TestHostEntryVolumesAndFinish(t *testing.T) {
	h := newHostScan()
	root := "/docker"

	// 卷目录下的第一层条目（rel 只有一段）不建桶。
	h.classify(hsEntry("volumes", root+"/volumes/metadata.db", "metadata.db", false, 9))
	// 带 _data 的卷：体积只累加 _data 之下的文件。
	h.classify(hsEntry("volumes", root+"/volumes/vol1/_data", "vol1/_data", true, 0))
	h.classify(hsEntry("volumes", root+"/volumes/vol1/_data/x.txt", "vol1/_data/x.txt", false, 7))
	h.classify(hsEntry("volumes", root+"/volumes/vol1/opt.json", "vol1/opt.json", false, 100))
	// 裸卷（驱动卷）：目录里没有 _data，体积算其余文件。
	h.classify(hsEntry("volumes", root+"/volumes/vol2/opt.json", "vol2/opt.json", false, 3))

	rows := h.finalise(DaemonInfo{OK: true, Root: root})

	data := hsMustRow(t, rows, rowVolumeData)
	if data.Count != 1 || data.Bytes != 7 {
		t.Fatalf("带数据卷 count/bytes = %d/%d，应为 1/7（卷目录的 100 不算数据）", data.Count, data.Bytes)
	}
	hsHasPath(t, data, root+"/volumes/vol1/_data")

	driver := hsMustRow(t, rows, rowVolumeDriver)
	if driver.Count != 1 || driver.Bytes != 3 {
		t.Fatalf("驱动卷 count/bytes = %d/%d，应为 1/3", driver.Count, driver.Bytes)
	}
	hsHasPath(t, driver, "vol2")
	if _, ok := h.acc["metadata.db"]; ok {
		t.Fatalf("卷根下的散落文件不该建桶")
	}
}

// ---------- 插件：配置与数据分桶，路径记插件目录 ----------

func TestHostEntryPlugins(t *testing.T) {
	h := newHostScan()
	root := "/p/plugins"

	h.classify(hsEntry("plugins", root+"/id1/config.v2.json", "id1/config.v2.json", false, 5))
	h.classify(hsEntry("plugins", root+"/id1/data/x", "id1/data/x", false, 6))
	// 插件目录本身（rel 一段）不落任何行。
	h.classify(hsEntry("plugins", root+"/id2", "id2", true, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: "/p"})

	cfg := hsMustRow(t, rows, rowPluginConfig)
	if cfg.Count != 1 || cfg.Bytes != 5 {
		t.Fatalf("插件配置 count/bytes = %d/%d，应为 1/5", cfg.Count, cfg.Bytes)
	}
	hsHasPath(t, cfg, filepath.Join(root, "id1"))

	data := hsMustRow(t, rows, rowPluginData)
	if data.Count != 1 {
		t.Fatalf("插件数据计 %d 个，应为 1", data.Count)
	}
	if data.Bytes != 6 {
		t.Fatalf("插件数据字节 %d，应为 6", data.Bytes)
	}
}

// ---------- cgroup：只认 docker 相关的那批 ----------

func TestHostEntryCgroup(t *testing.T) {
	h := newHostScan()

	h.classify(hsEntry("cgroup", "/sys/fs/cgroup/system.slice/docker-abc.scope",
		"system.slice/docker-abc.scope", true, 0))
	h.classify(hsEntry("cgroup", "/sys/fs/cgroup/docker/abc/pids",
		"docker/abc/pids", true, 0))
	h.classify(hsEntry("cgroup", "/sys/fs/cgroup/init.scope", "user.slice/init.scope", true, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: "/docker"})
	cg := hsMustRow(t, rows, rowContainerCgroup)
	if cg.Count != 2 {
		t.Fatalf("cgroup 计 %d 项，应为 2（与 docker 无关的那项不计）", cg.Count)
	}
	// 伪文件系统里 size 恒 0，这一行只报「多少个」。
	if cg.Bytes != 0 {
		t.Fatalf("cgroup 字节 %d，伪文件系统不该报占用", cg.Bytes)
	}
}

// ---------- 网络设备：一块网卡可以同时落 bridge / veth / cni ----------

func TestHostEntryNetDevicesAndFinish(t *testing.T) {
	h := newHostScan()

	h.classify(hsEntry("net-devices", "/sys/class/net/br-xyz/bridge", "br-xyz/bridge", true, 0))
	h.classify(hsEntry("net-devices", "/sys/class/net/veth1a2b/address", "veth1a2b/address", false, 6))
	h.classify(hsEntry("net-devices", "/sys/class/net/cali99/address", "cali99/address", false, 6))
	// 普通网卡与 rel 首段为空的条目都不落行。
	h.classify(hsEntry("net-devices", "/sys/class/net/lo/address", "lo/address", false, 6))
	h.classify(hsEntry("net-devices", "/sys/class/net/", "", false, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: "/docker"})

	br := hsMustRow(t, rows, rowNetworkBridge)
	if br.Count != 1 {
		t.Fatalf("bridge 计 %d 个，应为 1", br.Count)
	}
	hsHasPath(t, br, "br-xyz")

	ve := hsMustRow(t, rows, rowNetworkVeth)
	if ve.Count != 1 {
		t.Fatalf("veth 计 %d 个，应为 1", ve.Count)
	}
	hsHasPath(t, ve, filepath.Join(dirSysNet, "veth1a2b"))

	cni := hsMustRow(t, rows, rowNetworkCni)
	if cni.Count != 1 {
		t.Fatalf("CNI 网卡计 %d 个，应为 1", cni.Count)
	}
	hsHasPath(t, cni, filepath.Join(dirSysNet, "cali99"))
}

// ---------- classify 的几处刻意例外 ----------

func TestHostClassifySpecialCases(t *testing.T) {
	h := newHostScan()
	home := "/home/u"

	// ~/.docker/trust 归 system.trust，不许同时进 system.user。
	h.classify(hsEntry("userconfig", home+"/.docker/trust/tls/fca.pem", "trust/tls/fca.pem", false, 4))
	if a := h.acc[rowSystemUser]; a != nil {
		t.Fatalf("~/.docker/trust 下的文件不该落进 system.user")
	}
	// 正常的用户配置文件要落进 system.user。
	h.classify(hsEntry("userconfig", home+"/.docker/config.json", "config.json", false, 8))
	if a := h.acc[rowSystemUser]; a == nil || a.count != 1 {
		t.Fatalf("system.user 计 %v，应为 1", a)
	}

	// daemon.log 只认 docker*.log。
	h.classify(hsEntry("daemon-log", "/var/log/docker.log", "docker.log", false, 9))
	h.classify(hsEntry("daemon-log", "/var/log/other.log", "other.log", false, 9))
	if a := h.acc[rowLogDaemon]; a == nil || a.count != 1 {
		t.Fatalf("system 守护日志计 %v，应为 1（other.log 不该算）", a)
	}

	// journald 只认 .journal 后缀。
	h.classify(hsEntry("journal", "/var/log/journal/machine/system.journal",
		"machine/system.journal", false, 2))
	h.classify(hsEntry("journal", "/var/log/journal/machine/readme.txt",
		"machine/readme.txt", false, 2))
	if a := h.acc[rowLogJournald]; a == nil || a.count != 1 || a.bytes != 2 {
		t.Fatalf("journald 行 %v，应只算 .journal", a)
	}

	// netns 同名去重、伪条目无体积。
	h.classify(hsEntry("netns", "/var/run/netns/foo", "foo", false, 0))
	h.classify(hsEntry("netns", "/var/run/netns/foo", "foo", false, 0))
	if a := h.acc[rowNetworkNetns]; a == nil || a.count != 1 {
		t.Fatalf("netns 计 %v，同名应只算一个", a)
	}

	// cni-conf 的目录不落行。
	h.classify(hsEntry("cni-conf", "/etc/cni/net.d", "", true, 0))
	if a := h.acc[rowNetworkCni]; a != nil && a.count != 0 {
		t.Fatalf("cni-conf 目录不该计数，实得 %v", a)
	}

	// disk-root：只有根下第一层目录记路径，文件才计数与累加。
	h.classify(hsEntry("disk-root", "/docker/containers", "containers", true, 0))
	h.classify(hsEntry("disk-root", "/docker/containers/deep", "containers/deep", true, 0))
	h.classify(hsEntry("disk-root", "/docker/x.txt", "x.txt", false, 5))
	rows := h.finalise(DaemonInfo{OK: true, Root: "/docker"})
	root := hsMustRow(t, rows, rowSystemRoot)
	if root.Count != 1 || root.Bytes != 5 {
		t.Fatalf("system.root count/bytes = %d/%d，应为 1/5", root.Count, root.Bytes)
	}
	hsHasPath(t, root, "/docker/containers")
	if len(root.Paths) != 1 {
		t.Fatalf("system.root 只该记第一层目录，实得 %v", root.Paths)
	}
	if !root.Aggregate {
		t.Fatalf("system.root 必须标 Aggregate，服务层算总占用时要排除它")
	}
}

// ---------- mark 的两道守卫 ----------

func TestHostMarkGuards(t *testing.T) {
	h := newHostScan()

	h.mark("s1", srcStateDenied, MsgNoPerm)
	h.mark("s1", srcStateOK, "")
	if h.state["s1"] != srcStateDenied {
		t.Fatalf("读不动被普通扫描翻成了正常，defect 7 又回来了")
	}

	h.mark("s2", srcStateCancelled, MsgCancel)
	h.mark("s2", srcStateOK, "")
	if h.state["s2"] != srcStateCancelled {
		t.Fatalf("中断被后续 OK 抹掉了，界面就看不到「这一项没数完」")
	}

	h.mark("s3", srcStateAbsent, MsgNoRoot)
	h.mark("s3", srcStateOK, "")
	if h.state["s3"] != srcStateOK {
		t.Fatalf("目录本来就没有、后来探到了，应当翻成正常")
	}

	// reason 传空串不覆盖已有原因。
	h.mark("s4", srcStateAbsent, "真因")
	h.mark("s4", srcStateAbsent, "")
	if h.reason["s4"] != "真因" {
		t.Fatalf("空 reason 把已有原因清掉了，实得 %q", h.reason["s4"])
	}
}

// ---------- statusOf 的真值表 ----------

func TestHostStatusOfTruthTable(t *testing.T) {
	// 「无数据根」优先于一切：这一行压根没数过。
	h := newHostScan()
	h.mark("containers", srcStateNoRootDep, "Docker 没跑")
	if st, msg := h.statusOf(rowContainerMeta, hostRowSources[rowContainerMeta], nil); st != model.RowUnavailable || msg != "Docker 没跑" {
		t.Fatalf("够不着数据根时应点名原因，实得 %v/%q", st, msg)
	}

	// 两个来源都读不动 → 「—」；只要有一个够得着就不是「—」。
	h2 := newHostScan()
	h2.mark("net-devices", srcStateAbsent, MsgVMRoot)
	h2.mark("cni-conf", srcStateAbsent, MsgVMRoot)
	if st, msg := h2.statusOf(rowNetworkCni, hostRowSources[rowNetworkCni], nil); st != model.RowNotSupported || msg != MsgVMRoot {
		t.Fatalf("两个来源都够不着要显示「—」，实得 %v/%q", st, msg)
	}
	h3 := newHostScan()
	h3.mark("net-devices", srcStateOK, "")
	h3.mark("cni-conf", srcStateAbsent, MsgVMRoot)
	if st, msg := h3.statusOf(rowNetworkCni, hostRowSources[rowNetworkCni], nil); st != model.RowOK || msg != "" {
		t.Fatalf("查过的 0 就是 0，不该被另一段的缺席污染，实得 %v/%q", st, msg)
	}

	// 记到了东西但有一段读不动 → 照实显示 + 「可能偏小」。
	h4 := newHostScan()
	h4.mark("containers", srcStateDenied, MsgNoPerm)
	hit := &rowAcc{count: 1, bytes: 5}
	if st, msg := h4.statusOf(rowContainerMeta, hostRowSources[rowContainerMeta], hit); st != model.RowOK || msg != MsgPartial {
		t.Fatalf("保住已扫到的那一半才符合 ㉘，实得 %v/%q", st, msg)
	}
	// 一行都没记到 + 读不动 → 无权读取，且**不给 0**。
	if st, msg := h4.statusOf(rowContainerMeta, hostRowSources[rowContainerMeta], nil); st != model.RowNoPerm || msg != MsgNoPerm {
		t.Fatalf("读不动且没数到东西要显示无权读取，实得 %v/%q", st, msg)
	}
	// 中断优先于「读不动」的措辞。
	h5 := newHostScan()
	h5.mark("containers", srcStateCancelled, MsgCancel)
	if st, msg := h5.statusOf(rowContainerMeta, hostRowSources[rowContainerMeta], nil); st != model.RowUnavailable || msg != MsgCancel {
		t.Fatalf("中断要显示没数完，实得 %v/%q", st, msg)
	}
	// 没有任何原因时兜底一句「这个目录没有」。上面那个 h5 已经存了「中断」这条原因，
	// 兜底分支只能用一份干净的状态来验。
	h6 := newHostScan()
	if got := h6.firstReason([]string{"containers"}); got != MsgNoRoot {
		t.Fatalf("firstReason 兜底应为 MsgNoRoot，实得 %q", got)
	}
}

// ---------- finalise 的形状 ----------

func TestHostFinaliseShape(t *testing.T) {
	h := newHostScan()
	h.row(rowVolumeData).count = 1
	h.row(rowContainerCgroup).count = 1
	// 一段都没标记、只落了台账的这份 fixture：已落地的 2 行 + 四行「说不清原因」的固定行 = 6。
	// 「读过了但数出来是 0」的行该不该出场由下面的 TestHostFinaliseEmitsEveryReadRow 管；
	// 这一条锁的是反面——**根本没读过的段落不能凭空造出一行 0**。
	rows := h.finalise(DaemonInfo{OK: true, Root: "/docker"})

	if len(rows) != 6 {
		t.Fatalf("行数应为「已落地的 2 行 + 固定 4 行」= 6，实得 %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Key > rows[i].Key {
			t.Fatalf("行没按 key 排好，%q 在 %q 前面", rows[i-1].Key, rows[i].Key)
		}
	}
	// 四行「说不清原因」的行必须出现，且都是 unavailable。
	for _, key := range []string{rowLogBuild, rowOtherEvents, rowOtherCache, rowImageSign} {
		r := hsMustRow(t, rows, key)
		if r.Status != model.RowUnavailable || r.Message == "" {
			t.Fatalf("%s 应给人话原因，实得 %v/%q", key, r.Status, r.Message)
		}
	}
	if r := hsMustRow(t, rows, rowVolumeData); !r.HasBytes {
		t.Fatalf("volume.data 该报体积，实得 %+v", r)
	}
	if r := hsMustRow(t, rows, rowContainerCgroup); r.HasBytes {
		t.Fatalf("container.cgroup 是伪文件系统，不该报体积")
	}
}

// TestHostFinaliseEmitsEveryReadRow 锁的是规则 41 的另一半：查过了、这里没有，也得让这一行出场。
//
// 改之前的形状是「只有数到东西的行才出现」，于是干净机器上深扫一遍有十九行整行不见了——
// 界面拿到的是「这一行读不到」，而真相是「这一行是 0」。用户分不清「没查」和「查了没有」，
// 就等于把「不知道」画成了「没有」。
func TestHostFinaliseEmitsEveryReadRow(t *testing.T) {
	root := t.TempDir()
	info := DaemonInfo{OK: true, Root: root}
	h := newHostScan()
	// Linux 专属的几段在非 Linux 上会被标成「够不着」，那是另一条口径（需求 ⑮），
	// 这一条只管「读过的行必须出场」，所以把那一类行从断言里摘出去。
	notLinux := map[string]bool{}
	for _, s := range hostSources(info) {
		h.mark(s.id, srcStateOK, "")
		if s.linuxOnly && runtime.GOOS != "linux" {
			for _, r := range s.rows {
				notLinux[r] = true
			}
		}
	}
	rows := h.finalise(info)

	if len(rows) != len(HostRowKeys()) {
		t.Fatalf("全量深扫应出满 %d 行宿主行，实得 %d 行：%v", len(HostRowKeys()), len(rows), hsKeys(rows))
	}
	for _, key := range HostRowKeys() {
		hsMustRow(t, rows, key)
	}
	// 数出来是 0 的行给的是「查过的 0」，不是 unavailable。
	// 四行「说不清数字」的固定行除外——它们交人话原因，本来就不该有数字。
	for _, r := range rows {
		if reasonRow(r.Key).Key != "" || notLinux[r.Key] {
			continue
		}
		if r.Status != model.RowOK || r.Count != 0 {
			t.Fatalf("%s 该是查过的 0，实得 %v/%d（这句原因：%q）", r.Key, r.Status, r.Count, r.Message)
		}
	}
}

// TestHostFinalise_PartialRescanKeepsUnreadRowsOut 是上一条的反面：
// 界面上「重数这一行」只走它自己那几段（want），其余段这一趟没读，不能跟着报 0。
func TestHostFinalise_PartialRescanKeepsUnreadRowsOut(t *testing.T) {
	info := DaemonInfo{OK: true, Root: t.TempDir()}
	h := newHostScan()
	h.mark("volumes", srcStateOK, "")
	rows := h.finalise(info)

	// 该出场的只有 volumes 段那两行，外加固定那四行解释。
	want := []string{rowVolumeData, rowVolumeDriver, rowLogBuild, rowOtherEvents, rowOtherCache, rowImageSign}
	if len(rows) != len(want) {
		t.Fatalf("只重数 volumes 段时该出 %d 行，实得 %d 行：%v", len(want), len(rows), hsKeys(rows))
	}
	for _, key := range want {
		hsMustRow(t, rows, key)
	}
	if r := hsMustRow(t, rows, rowVolumeData); r.Status != model.RowOK || r.Count != 0 {
		t.Fatalf("读过的 volume.data 该是查过的 0，实得 %v/%d", r.Status, r.Count)
	}
	for _, key := range []string{rowContainerMeta, rowNetworkNetns, rowSystemConfig} {
		for _, r := range rows {
			if r.Key == key {
				t.Fatalf("%s 这一趟没读（只读了 volumes 段），不该出现在结果里：%v", key, hsKeys(rows))
			}
		}
	}
}

func hsKeys(rows []HostRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Key)
	}
	return out
}

// ---------- Podman 的落点：多一层的容器档案、用户运行时目录、netavark 网络定义 ----------

// TestHostEntryContainers_PodmanLayer 认的是 rootless Podman 的真实形状：
// 容器档案在 <数据目录>/overlay-containers/<id>/userdata/ 底下，比 Docker 多一层 userdata。
// 这一层不认，Podman 机器上容器档案/日志四行整行数不到。
func TestHostEntryContainers_PodmanLayer(t *testing.T) {
	h := newHostScan()
	root := "/podstore"
	ur := root + "/overlay-containers"

	h.classify(hsEntry("containers", ur+"/abc/userdata/config.json", "abc/userdata/config.json", false, 3))
	h.classify(hsEntry("containers", ur+"/abc/userdata/hostconfig.json", "abc/userdata/hostconfig.json", false, 4))
	// 日志与 Docker 同形状，只是多一层 userdata。
	h.classify(hsEntry("containers", ur+"/abc/userdata/abc-json.log", "abc/userdata/abc-json.log", false, 7))
	h.classify(hsEntry("containers", ur+"/abc/userdata/abc-json.log.1.gz", "abc/userdata/abc-json.log.1.gz", false, 11))
	// userdata 目录本身、容器目录本身都不是任何一行。
	h.classify(hsEntry("containers", ur+"/zzz/userdata", "zzz/userdata", true, 0))
	h.classify(hsEntry("containers", ur+"/zzz", "zzz", true, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: root})

	meta := hsMustRow(t, rows, rowContainerMeta)
	if meta.Count != 1 {
		t.Fatalf("Podman 容器档案计 %d 个，同一容器多个文件应只算 1 个", meta.Count)
	}
	// 删除侧要拿到userdata 那一条目录，路径门只放行这一行登记过的根之下的东西。
	hsHasPath(t, meta, ur+"/abc/userdata")
	if got := hsMustRow(t, rows, rowLogContainer); got.Count != 1 || got.Bytes != 7 {
		t.Fatalf("Podman 在写日志 count/bytes = %d/%d，应为 1/7", got.Count, got.Bytes)
	}
	if got := hsMustRow(t, rows, rowLogRotate); got.Count != 1 || got.Bytes != 11 {
		t.Fatalf("Podman 滚动日志 count/bytes = %d/%d，应为 1/11", got.Count, got.Bytes)
	}
}

// TestHostEntryNetns_UserRuntimeDir 锁的是本机实测到的那一格：
// rootless 引擎把命名空间放在 /run/user/<uid>/netns，而 /var/run/netns 是空的。
// 只认后一个的话这一行永远报 0，而它其实是「有 9 个」。
func TestHostEntryNetns_UserRuntimeDir(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	if got := runUserNetns(); got != filepath.Join(xdg, "netns") {
		t.Fatalf("runUserNetns 应跟 XDG_RUNTIME_DIR 走，实得 %q", got)
	}
	// 拿不到 XDG_RUNTIME_DIR 才按惯例拼 /run/user/<uid>。
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got := runUserNetns(); got != filepath.Join("/run/user", strconv.Itoa(os.Getuid()), "netns") {
		t.Fatalf("缺 XDG_RUNTIME_DIR 时应回落 /run/user/<uid>，实得 %q", got)
	}

	h := newHostScan()
	p := filepath.Join(xdg, "netns", "cni-7f3a")
	h.classify(hsEntry("netns", p, "cni-7f3a", false, 0))
	// 同名不同目录不重复计数（两个根都可能报出同一个名字的场合由 track 收）。
	h.classify(hsEntry("netns", p, "cni-7f3a", false, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: t.TempDir()})
	r := hsMustRow(t, rows, rowNetworkNetns)
	if r.Count != 1 {
		t.Fatalf("命名空间计 %d 个，应为 1", r.Count)
	}
	hsHasPath(t, r, p)
}

// TestHostEntryCniConf_PodmanNetworks 数的是 Podman 那套等价物：<数据目录>/networks 里的网络定义。
// 引擎是 rootless Podman 时 /etc/cni/net.d 常常不存在，只认它就等于这一行永远数不到。
func TestHostEntryCniConf_PodmanNetworks(t *testing.T) {
	root := t.TempDir()
	h := newHostScan()
	p := filepath.Join(root, "networks", "phpo-network.json")
	h.classify(hsEntry("cni-conf", p, "phpo-network.json", false, 611))
	// 目录本身不是配置，不落行。
	h.classify(hsEntry("cni-conf", filepath.Dir(p), "networks", true, 0))

	rows := h.finalise(DaemonInfo{OK: true, Root: root})
	r := hsMustRow(t, rows, rowNetworkCni)
	if r.Count != 1 || r.Bytes != 611 {
		t.Fatalf("netavark 网络定义 count/bytes = %d/%d，应为 1/611", r.Count, r.Bytes)
	}
	hsHasPath(t, r, p)
}

// TestHostSources_IncludesPodmanRuntimeDirs 锁的是 rootless 引擎放在**用户运行时目录**底下的那三处落点。
//
// 本机实测（rootless Podman）：数据目录底下没有 tmp/ 这一层，运行时那一套放在
// <XDG_RUNTIME_DIR>/libpod/tmp（rootlessport 的套接字与就绪管道、pause.pid、exits/、persist/）
// 与 <XDG_RUNTIME_DIR>/containers（overlay 与 overlay-containers 的挂载点树、netavark 的运行期文件）；
// rootless Docker 的对应物是 <XDG_RUNTIME_DIR>/docker，而 builder 那一段以前只写了 rootful 的 /run/docker。
// 三处都缺，这两行在 rootless 机器上就永远报「这台机器上没有这个目录」。
func TestHostSources_IncludesPodmanRuntimeDirs(t *testing.T) {
	root := t.TempDir()
	xdg := filepath.Join(root, "runtime")
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	info := DaemonInfo{OK: true, Root: root, Rootless: true}

	wantTmp := []string{filepath.Join(root, "tmp"), filepath.Join(xdg, "libpod", "tmp"), filepath.Join(xdg, "containers")}
	wantBuilder := []string{filepath.Join(root, "buildkit"), "/run/docker", filepath.Join(xdg, "docker")}
	got := map[string][]string{}
	for _, s := range hostSources(info) {
		got[s.id] = s.roots
	}
	if diff := cmpStrings(got["tmp"], wantTmp); diff != "" {
		t.Fatalf("system.tmp 的根不对：%s", diff)
	}
	if diff := cmpStrings(got["builder"], wantBuilder); diff != "" {
		t.Fatalf("system.builder 的根不对：%s", diff)
	}
	// 这两行各只挂自己那一个统计段，别把邻居的行顺带接管走。
	for _, s := range hostSources(info) {
		switch s.id {
		case "tmp", "builder":
			if len(s.rows) != 1 {
				t.Fatalf("%s 段该只交出一行，实得 %v", s.id, s.rows)
			}
		}
	}
}

// TestHostScanHost_CountsUserRuntimeDirs 走的是真链路：造一个假的运行时目录，让 ScanHost 真的数到它。
//
// 为什么不停在 hostSources 那张表上：表对了一切还得走 walkSource 的「缺席根跳过」与
// rowAcc 的「只收根下第一层」，那两步任一写错，界面上看到的就还是 0。
func TestHostScanHost_CountsUserRuntimeDirs(t *testing.T) {
	store := t.TempDir()
	xdg := filepath.Join(store, "runtime")
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	hsStubProbes(t, hsRefuse, hsNoCommand)

	// 引擎留下的普通残骸：一个顶层文件 + 一个目录里的一个文件（目录本身不计个数，只收它的体积）。
	hsMk(t, xdg, "libpod/tmp/leftover.txt", 6)
	hsMk(t, xdg, "libpod/tmp/rp/inner.txt", 5)
	hsMk(t, xdg, "docker/engine.txt", 3)

	res, err := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: store, Rootless: true}, nil)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	tmp := hsMustRow(t, res.Rows, rowSystemTmp)
	// leftover.txt 与 rp/inner.txt 都在 tmp 那一段里数到；libpod/tmp 与 containers 两个根都挂在 system.tmp 这一行。
	if tmp.Count != 2 || tmp.Bytes != 11 {
		t.Fatalf("system.tmp = %d/%d，应为 2/11", tmp.Count, tmp.Bytes)
	}
	hsHasPath(t, tmp, filepath.Join(xdg, "libpod", "tmp", "leftover.txt"))
	hsHasPath(t, tmp, filepath.Join(xdg, "libpod", "tmp", "rp"))

	bd := hsMustRow(t, res.Rows, rowSystemBuilder)
	hsHasPath(t, bd, filepath.Join(xdg, "docker", "engine.txt"))
	// 只硬断「这次造的那一条在里面」：builder 段另有一根写死的 /run/docker，
	// CI 那台机器上有没有它、里面有几个文件，都不该影响这条用例要锁的东西。
	if bd.Count < 1 || bd.Bytes < 3 {
		t.Fatalf("system.builder 至少该数到本次造的那 1 个文件 3 字节，实得 %d/%d", bd.Count, bd.Bytes)
	}
}

// TestHostLiveInside_SocketBlocksDeletion 锁的是甲档那句「照常计数，但删之前要现查」。
//
// rootlessport 目录里躺的是引擎正在监听的套接字：扫描把它算进数字（它确实占着位置），
// 但删除那一步必须拒——只判目录本身等于放行「删这个目录」，实际动的却是里面那只套接字。
func TestHostLiveInside_SocketBlocksDeletion(t *testing.T) {
	hdOnlyLinux(t)
	xdg := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	info := DaemonInfo{OK: true, Root: t.TempDir(), Rootless: true}

	live := filepath.Join(xdg, "libpod", "tmp", "rootlessport1213137364")
	if err := os.MkdirAll(live, 0o777); err != nil {
		t.Fatal(err)
	}
	hsListenUnix(t, filepath.Join(live, ".bp.sock"))
	// 同一层的另一处：里面只有普通文件，那就是该删的残骸。
	dead := filepath.Join(xdg, "libpod", "tmp", "stale")
	hsMk(t, dead, "leftover.txt", 4)

	if why := liveInside(live); why == "" {
		t.Fatalf("里面有套接字的目录不能被当成可删对象")
	} else if !strings.Contains(why, "套接字") {
		t.Fatalf("拒绝的理由要点名套接字，实得 %q", why)
	}
	if why := liveInside(dead); why != "" {
		t.Fatalf("普通残骸不该被拦，实得 %q", why)
	}
	// 门只管用户运行时目录这一片：数据目录底下同样的形状照常放行（那是清理的对象）。
	outside := filepath.Join(info.Root, "tmp", "stale")
	hsMk(t, outside, "a.txt", 1)
	if err := checkHostPath(outside, hostRowRoots(rowSystemTmp, info)); err != nil {
		t.Fatalf("数据目录底下的残骸不该走运行时门：%v", err)
	}
	// 运行时目录里的活对象：分类阶段就拒，一个字节都不动。
	if _, err := classifyHostOp(DeleteOp{Row: rowSystemTmp, Ref: live}, info); err == nil {
		t.Fatal("里面还有套接字的运行时目录不该分类成功")
	}
	if _, err := classifyHostOp(DeleteOp{Row: rowSystemTmp, Ref: dead}, info); err != nil {
		t.Fatalf("普通残骸该过门，实得 %v", err)
	}
	// 已经不在了的那一条不该被拦：删除本来就是幂等的空操作。
	if why := liveInside(filepath.Join(xdg, "libpod", "tmp", "gone")); why != "" {
		t.Fatalf("不存在的路径不该有理由，实得 %q", why)
	}
}

// TestHostLiveInside_PidFileBlocksDeletion 锁的是运行时门的第二类活对象：进程号文件。
//
// 为什么只看套接字不够：真机实测（rootless Podman，9 个容器在跑）显示
// `/run/user/<uid>/containers/overlay-containers/<容器>/userdata/` 里既没有套接字也没有挂载点
// ——容器 rootfs 挂在自己的挂载命名空间里，宿主这一边看不到。那一趟只查套接字，等于把正在跑的
// 容器的运行时目录当成残骸整片删掉。里面唯一通用的活痕迹是 `conmon.pid` / `pidfile`。
func TestHostLiveInside_PidFileBlocksDeletion(t *testing.T) {
	hdOnlyLinux(t)
	xdg := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", xdg)

	hsWrite(t, filepath.Join(xdg, "containers", "overlay-containers", "068a0c", "userdata", "conmon.pid"),
		strconv.Itoa(os.Getpid())+"\n")
	// 已经退出的那个进程号：容器停止后留下的残骸，该照常删。
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("起不了子进程，拿不到已退出的 pid")
	}
	hsWrite(t, filepath.Join(xdg, "containers", "overlay-containers", "3c291a", "userdata", "pidfile"),
		strconv.Itoa(cmd.Process.Pid))
	// 目录本身被勾出去的另一档：单看这一份文件也算活的。
	pause := filepath.Join(xdg, "libpod", "tmp", "pause.pid")
	hsWrite(t, pause, strconv.Itoa(os.Getpid()))
	// 空 pid 文件是没起来的容器留下的痕迹，不是活对象。
	hsWrite(t, filepath.Join(xdg, "libpod", "tmp", "exits", "0123456789ab.pid"), "")
	// 名字不像 pid 文件的普通文件，内容是一串数字也不读——那只是碰巧长得像。
	hsWrite(t, filepath.Join(xdg, "libpod", "tmp", "alive"), strconv.Itoa(os.Getpid()))

	if why := liveInside(filepath.Join(xdg, "containers", "overlay-containers", "068a0c")); why == "" {
		t.Fatal("里面记着活进程号的运行时目录不能被当成可删对象")
	} else if !strings.Contains(why, "进程") {
		t.Fatalf("拒绝的理由要点名进程号，实得 %q", why)
	}
	if why := liveInside(filepath.Join(xdg, "containers", "overlay-containers", "3c291a")); why != "" {
		t.Fatalf("进程已经退出留下的残骸不该被拦，实得 %q", why)
	}
	if why := liveInside(pause); why == "" {
		t.Fatal("本身就是活 pid 文件的那一条不该被放行")
	}
	if why := liveInside(filepath.Join(xdg, "libpod", "tmp", "exits")); why != "" {
		t.Fatalf("空 pid 文件不该被拦，实得 %q", why)
	}
	if why := liveInside(filepath.Join(xdg, "libpod", "tmp", "alive")); why != "" {
		t.Fatalf("不该去读普通文件的内容当探针，实得 %q", why)
	}
}

// TestHostMsgNoRootNamesBothCauses 锁住那句原因的两种可能都在。
//
// 为什么值得单独锁：这一句在 Linux 上也会用到，而 Linux 上引擎同样可能跑在虚拟机里
// （Docker Desktop / colima / lima）。只留「这台机器上没有这个目录」会把第二种可能说成
// 「这里本来就是空的」——用户照着这个决定「那就不用清」，才是真出事（规则 41）。
func TestHostMsgNoRootNamesBothCauses(t *testing.T) {
	for _, want := range []string{"没有这个目录", "虚拟机"} {
		if !strings.Contains(MsgNoRoot, want) {
			t.Fatalf("MsgNoRoot 缺了 %q：%s", want, MsgNoRoot)
		}
	}
	if strings.HasSuffix(MsgNoRoot, "没有这个目录") {
		t.Fatalf("只留「本机没有这一种可能」的短句子不被接受：%s", MsgNoRoot)
	}
}

// ---------- matchRoot ----------

func TestHostMatchRoot(t *testing.T) {
	roots := []string{"/a/b", "/a/b/c"}
	if rel, ok := matchRoot("/a/b/c/d", roots); !ok || rel != "d" {
		t.Fatalf("最长根优先失配，实得 %q/%v", rel, ok)
	}
	if rel, ok := matchRoot("/a/b", roots); !ok || rel != "." {
		t.Fatalf("根本身应返回 \".\"，实得 %q/%v", rel, ok)
	}
	if rel, ok := matchRoot("/a/b/..foo", roots); !ok || rel != "..foo" {
		t.Fatalf("文件名叫 ..foo 不是穿越，实得 %q/%v", rel, ok)
	}
	if _, ok := matchRoot("/a/x", roots); ok {
		t.Fatalf("根外的路径不该匹配")
	}
	if _, ok := matchRoot("/a/b/c/d", []string{"", "/a/b"}); !ok {
		t.Fatalf("空根必须被忽略而不是当成匹配一切")
	}
}

// ---------- classifyElevated：提权产出的三列文本 ----------

func TestHostClassifyElevated(t *testing.T) {
	h := newHostScan()
	s := hostSource{id: "containers", roots: []string{"/d/containers"}, maxDepth: 3}
	roots := []string{"/d/containers"}

	out := strings.Join([]string{
		"l\t0\t/d/containers/link",                    // 符号链接不跟
		"d\t0\t/d/containers/aaa/checkpoint/too/deep", // 深度到顶的目录不进去
		"f\t4\t/d/containers/aaa/config.v2.json",      // 正常条目
		fmt.Sprintf("f\t4\t%s", "/d/containers"),      // rel == "." 跳过
		"short-line",                                  // 列数不足忽略
		"junk",
	}, "\n")

	h.classifyElevated(s, roots, out)
	if a := h.acc[rowContainerMeta]; a == nil || a.count != 1 || a.bytes != 4 {
		t.Fatalf("提权条目采集结果 %v，应只落 1 条 config.v2.json", a)
	}
}

// ---------- 提权：唯一有资格把「读不动」翻面的地方 ----------

func TestHostElevateDeniedRefusalKeepsPartialResult(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下造不出读不动的目录")
	}
	tmp := t.TempDir()
	hsMk(t, tmp, "containers/aaa/config.v2.json", 5)
	hsMk(t, tmp, "containers/locked/config.v2.json", 5)
	lockedDir := filepath.Join(tmp, "containers", "locked")
	hsChmod(t, lockedDir, 0)
	t.Cleanup(func() { hsChmod(t, lockedDir, 0o755) })

	// 与下一条用例同一份现场，唯一区别是这次提权被拒。
	hsStubProbes(t, hsRefuse, hsNoCommand)
	res, err := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: tmp}, StageFn(func(string) {}))
	if err != nil && runtime.GOOS == "linux" {
		t.Fatalf("linux 上扫描不该失败：%v", err)
	}
	if runtime.GOOS != "linux" {
		return
	}
	if res.DeepScanned {
		t.Fatalf("一次授权都没拿到却报「已深度扫描」")
	}
	row := hsMustRow(t, res.Rows, rowContainerMeta)
	if row.Status != model.RowOK || row.Message != MsgPartial {
		t.Fatalf("㉘ 要求保住已扫到的那一半，实得 %v/%q", row.Status, row.Message)
	}
	if row.Count != 1 || row.Bytes != 5 {
		t.Fatalf("已扫到的数字丢了：%d/%d，应为 1/5", row.Count, row.Bytes)
	}
	ok := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "提权读取失败") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("提权被拒要留一行说明，实得 %v", res.Warnings)
	}
}

func TestHostElevateDeniedSuccessFlipsTheRow(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下造不出读不动的目录")
	}
	tmp := t.TempDir()
	hsMk(t, tmp, "containers/aaa/config.v2.json", 5)
	hsMk(t, tmp, "containers/locked/config.v2.json", 5)
	lockedDir := filepath.Join(tmp, "containers", "locked")
	hsChmod(t, lockedDir, 0)
	t.Cleanup(func() { hsChmod(t, lockedDir, 0o755) })

	// 提权后「看到」的那一份：上面真实存在、只是此刻读不动的那个文件。
	// 路径必须长成 <容器 ID>/config.v2.json（相对 containers 根两段）——
	// entryContainers 判断「这是不是某个容器的目录」用的就是相对根的两段，多套一层等于它看不见。
	out := fmt.Sprintf("f\t5\t%s\n", filepath.Join(tmp, "containers", "locked", "config.v2.json"))
	hsStubProbes(t, hsFindWith(out), hsNoCommand)
	res, err := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: tmp}, StageFn(func(string) {}))
	if err != nil && runtime.GOOS == "linux" {
		t.Fatalf("linux 上扫描不该失败：%v", err)
	}
	if runtime.GOOS != "linux" {
		return
	}
	if !res.DeepScanned || !res.Elevated {
		t.Fatalf("拿到了提权结果必须报已深度扫描：%+v", res)
	}
	row := hsMustRow(t, res.Rows, rowContainerMeta)
	if row.Count != 2 || row.Bytes != 10 {
		t.Fatalf("提权那一份没并进数字：%d/%d，应为 2/10", row.Count, row.Bytes)
	}
	if row.Message != "" {
		t.Fatalf("已经数全了就不该再提示「可能偏小」，实得 %q", row.Message)
	}
	if row.Status != model.RowOK {
		t.Fatalf("提权成功后状态应回到正常，实得 %v", row.Status)
	}
}

// 需求 ⑰：一次「详细扫描」只弹一扇授权框。
// 这里造两个都读不动的统计段（containers 与 volumes 各一个 0 权限目录），
// 断言整次扫描只发起一次提权读取，而且那一次的目录清单同时带着这两段——
// 分回来的数字仍按每一段自己的目录算，所以合并不会把体积算串。
func TestHostElevateDeniedMergesIntoOneCall(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下造不出读不动的目录")
	}
	tmp := t.TempDir()
	hsMk(t, tmp, "containers/aaa/config.v2.json", 5)
	hsMk(t, tmp, "containers/locked/config.v2.json", 5)
	hsMk(t, tmp, "volumes/vol1/_data/x.bin", 7)
	hsMk(t, tmp, "volumes/locked/_data/x.bin", 7)
	cLocked := filepath.Join(tmp, "containers", "locked")
	vLocked := filepath.Join(tmp, "volumes", "locked")
	hsChmod(t, cLocked, 0)
	hsChmod(t, vLocked, 0)
	t.Cleanup(func() {
		hsChmod(t, cLocked, 0o755)
		hsChmod(t, vLocked, 0o755)
	})

	calls := 0
	var asked []string
	out := fmt.Sprintf("f\t5\t%s\nf\t7\t%s\n",
		filepath.Join(cLocked, "config.v2.json"),
		filepath.Join(vLocked, "_data", "x.bin"))
	hsStubProbes(t, func(_ context.Context, roots []string, _ int) (string, error) {
		calls++
		asked = append(asked, roots...)
		return out, nil
	}, hsNoCommand)

	res, err := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: tmp}, StageFn(func(string) {}))
	if err != nil && runtime.GOOS == "linux" {
		t.Fatalf("linux 上扫描不该失败：%v", err)
	}
	if runtime.GOOS != "linux" {
		return
	}
	if calls != 1 {
		t.Fatalf("两段读不动却弹了 %d 次授权：必须合并成一次", calls)
	}
	want := []string{filepath.Join(tmp, "containers"), filepath.Join(tmp, "volumes")}
	for _, w := range want {
		found := false
		for _, a := range asked {
			if a == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("那一次提权读取的目录清单里少了 %s，实得 %v", w, asked)
		}
	}
	meta := hsMustRow(t, res.Rows, rowContainerMeta)
	if meta.Count != 2 || meta.Bytes != 10 {
		t.Fatalf("containers 那一段没并回提权读到的那份：%d/%d，应为 2/10", meta.Count, meta.Bytes)
	}
	vol := hsMustRow(t, res.Rows, rowVolumeData)
	if vol.Count != 2 || vol.Bytes != 14 {
		t.Fatalf("volumes 那一段没并回提权读到的那份：%d/%d，应为 2/14", vol.Count, vol.Bytes)
	}
	if !res.DeepScanned || !res.Elevated {
		t.Fatalf("拿到了提权结果必须报已深度扫描：%+v", res)
	}
}

// ---------- walkSource 的三个出口 ----------

func TestHostWalkSourceExits(t *testing.T) {
	tmp := t.TempDir()
	hsMk(t, tmp, "volumes/vol1/_data/x.txt", 1)

	h := newHostScan()
	h.walkSource(context.Background(), hostSource{id: "volumes", roots: []string{tmp + "/volumes"}})
	if h.state["volumes"] != srcStateOK {
		t.Fatalf("读得到的目录应记为正常，实得 %d", h.state["volumes"])
	}

	h2 := newHostScan()
	h2.walkSource(context.Background(), hostSource{id: "volumes", roots: []string{tmp + "/nope"}})
	if h2.state["volumes"] != srcStateAbsent || h2.reason["volumes"] != MsgNoRoot {
		t.Fatalf("目录不存在要报「这台机器上没有」，实得 %d/%q", h2.state["volumes"], h2.reason["volumes"])
	}

	h3 := newHostScan()
	h3.walkSource(context.Background(), hostSource{id: "journal", roots: []string{tmp + "/nope"}, absentIsZ: true})
	if h3.state["journal"] != srcStateOK {
		t.Fatalf("整目录不在对该行是真 0，不该算缺失")
	}

	// 所有根都是空串：等同没有根。
	h4 := newHostScan()
	h4.walkSource(context.Background(), hostSource{id: "trust", roots: []string{""}})
	if h4.state["trust"] != srcStateAbsent {
		t.Fatalf("空根不该被当成读到了东西")
	}
}

// ---------- 防火墙规则统计 ----------

func TestHostCountDockerRules(t *testing.T) {
	out := strings.Join([]string{
		"-A DOCKER-USER -j ACCEPT",
		"-A KUBE-SVC-ABC -m comment -j KUBE-SEP-1",
		"-A FORWARD -j DOCKER-USER",
		"-P DOCKER ACCEPT",
		"-A DOCKER",
		"",
	}, "\n")
	chains := map[string]struct{}{}
	if n := countDockerRules(out, chains); n != 3 {
		t.Fatalf("数出 %d 条，应为 3（FORWARD 与 -P 不是 docker 自己的规则）", n)
	}
	if _, ok := chains["DOCKER-USER"]; !ok {
		t.Fatalf("链名没记下来：%v", chains)
	}
	// 同一份规则喂两遍，链要合并去重、条数照加。
	if n := countDockerRules(out, chains); n != 3 {
		t.Fatalf("第二遍应再数 3 条，实得 %d", n)
	}
	if len(chains) != 3 {
		t.Fatalf("链去重失败：%v", chains)
	}
}

func TestHostProbeIptablesBothMissing(t *testing.T) {
	h := newHostScan()
	hsStubProbes(t, hsRefuse, hsNoCommand)
	h.probeIptables(context.Background())
	if h.state["iptables"] != srcStateAbsent {
		t.Fatalf("两个命令都没有时应记为「这台机器上没有」，实得 %d", h.state["iptables"])
	}
	if !strings.HasPrefix(h.reason["iptables"], "本机没有 iptables-save") {
		t.Fatalf("原因要点名 iptables-save，实得 %q", h.reason["iptables"])
	}
	if a := h.acc[rowNetworkIptables]; a != nil {
		t.Fatalf("一个都没跑起来时不该造出一行数字")
	}
}

func TestHostProbeIptablesHalfWorks(t *testing.T) {
	h := newHostScan()
	hsStubProbes(t, hsRefuse, func(ctx context.Context, exe string) (string, error) {
		if exe == "iptables-save" {
			return "-A DOCKER -j ACCEPT\n-A FORWARD -j DOCKER\n", nil
		}
		return "", errors.New("exit status 1")
	})
	h.probeIptables(context.Background())
	row := hsMustRow(t, h.finalise(DaemonInfo{OK: true, Root: "/d"}), rowNetworkIptables)
	if row.Count != 1 {
		t.Fatalf("只统计了能跑的那一半，条数应为 1，实得 %d", row.Count)
	}
	if h.state["iptables"] != srcStateOK {
		t.Fatalf("跑起来一半也算数到了，实得 %d", h.state["iptables"])
	}
	ok := false
	for _, w := range h.warn {
		if strings.Contains(w, "只统计了能跑的那一半") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("缺一半必须说明，实得 %v", h.warn)
	}
}

// ---------- 非 linux：每一行都要出场，每一行都要带得出一句原因 ----------

// hsAsOS 把「这台机器是什么系统」临时换成别的值。
//
// 为什么要有这颗助手：CI 只有 Linux runner，非 Linux 那条分支（容器引擎在虚拟机里）平时一行都跑不到。
// 换成可注入以后，同一台 Linux 上就能假装自己是 macOS 把那条分支的形状锁死。
func hsAsOS(t *testing.T, goos string) {
	t.Helper()
	prev := hostOS
	hostOS = goos
	t.Cleanup(func() { hostOS = prev })
}

// TestHostScanHost_NonLinuxEveryRowHasReason 锁的是规则 41 在非 Linux 平台上的那一格。
//
// 以前的形状：ScanHost 一进门判非 Linux 就整片早退，只交出「Linux 才有落点」的那几行，
// 数据目录底下那十几行既不出现也没原因，界面拿到的是一个空白的「—」——
// 那等于把「没查」和「查了这里没有」画成同一个样子。
// 现在按段分岔：Linux 才有的段落逐段说「跑在虚拟机里」，数据目录那几段说「本机没有这个目录（或那份数据在虚拟机磁盘里）」，
// 而宿主上本来就有的段落（~/.docker 那一类）照常数。
func TestHostScanHost_NonLinuxEveryRowHasReason(t *testing.T) {
	hsStubProbes(t, hsRefuse, hsNoCommand)
	hsAsOS(t, "darwin")
	xdg := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// 数据目录填一个宿主这边不存在的路径——虚拟机里那份就是这个形状。
	root := filepath.Join(home, "var", "lib", "docker")

	res, err := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: root}, nil)
	if !errors.Is(err, ErrHostNotSupported) {
		t.Fatalf("非 linux 应返回 ErrHostNotSupported，实得 %v", err)
	}
	if len(res.Warnings) == 0 || res.Warnings[0] != MsgVMRoot {
		t.Fatalf("非 linux 要给人话告警，实得 %v", res.Warnings)
	}
	if res.Elevated || res.DeepScanned {
		t.Fatalf("非 linux 不提权，实得 elevated=%v deep=%v", res.Elevated, res.DeepScanned)
	}
	if len(res.Rows) != len(HostRowKeys()) {
		t.Fatalf("非 linux 也要出满 %d 行宿主行，实得 %d 行：%v", len(HostRowKeys()), len(res.Rows), hsKeys(res.Rows))
	}
	// 一条都不许是「空原因」：数不到东西又不是「查出来就是 0」的行，必须带着一句话。
	for _, r := range res.Rows {
		if r.Count == 0 && r.Message == "" && r.Status != model.RowOK {
			t.Fatalf("%s 这一行数不到东西却没给原因（状态 %v）", r.Key, r.Status)
		}
	}
	// Linux 才有的落点：数字给「—」，原因是那句「跑在虚拟机里」。
	for _, key := range []string{rowSystemConfig, rowContainerCgroup, rowNetworkBridge, rowLogDaemon, rowNetworkNetns} {
		r := hsMustRow(t, res.Rows, key)
		if r.Status != model.RowNotSupported || r.Message != MsgVMRoot {
			t.Fatalf("%s 该是「够不着」+ VM 原因，实得 %v/%q", key, r.Status, r.Message)
		}
	}
	// 挂在数据目录底下的行：宿主这边确实没这个目录，给那句两种可能并列的原因（不是空白，也不是「0 项」）。
	// 只挑两个根全在数据目录底下的行来硬断——system.builder 另有写死的 /run/docker，
	// CI 那台机器上有没有它不该影响这条用例要锁的东西。
	for _, key := range []string{rowContainerMeta, rowVolumeData, rowSystemTmp} {
		r := hsMustRow(t, res.Rows, key)
		if r.Message != MsgNoRoot {
			t.Fatalf("%s 该说「这台机器上没有这个目录（或在虚拟机磁盘里）」，实得 %q", key, r.Message)
		}
	}
	// 四行「说不清独立落点」的行保留自己那句人话，不被平台话术覆盖。
	for _, key := range []string{rowLogBuild, rowOtherEvents, rowOtherCache, rowImageSign} {
		if got := hsMustRow(t, res.Rows, key); got.Message != reasonRow(key).Message {
			t.Fatalf("%s 该保留自己的原因，实得 %q", key, got.Message)
		}
	}
	// ~/.docker 在 macOS/Windows 上本来就在宿主：这一趟真去读了，就不套 VM 话术。
	if err := os.MkdirAll(filepath.Join(home, ".docker"), 0o777); err != nil {
		t.Fatal(err)
	}
	hsMk(t, filepath.Join(home, ".docker"), "config.json", 12)
	res2, _ := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: root}, nil)
	if r := hsMustRow(t, res2.Rows, rowSystemUser); r.Message == MsgVMRoot {
		t.Fatalf("宿主上读得到的行不该套「跑在虚拟机里」，实得 %q", r.Message)
	}
	if r := hsMustRow(t, res2.Rows, rowSystemUser); r.Count != 1 || r.Bytes != 12 {
		t.Fatalf("非 linux 也该真读到 ~/.docker 里那份配置，实得 %d/%d", r.Count, r.Bytes)
	}
}

// ---------- 单行重试 ----------

func TestHostRescanUnknownKey(t *testing.T) {
	row := (&Client{}).RescanHostRow(context.Background(), "no.such.row", DaemonInfo{OK: true, Root: t.TempDir()})
	if row.Status != model.RowUnavailable {
		t.Fatalf("未登记的 key 要显式不可用，实得 %v", row.Status)
	}
	if row.Message != "这一行不在宿主采集范围内" {
		t.Fatalf("未登记的 key 要给人话说明，实得 %q", row.Message)
	}
	if row.Key != "no.such.row" {
		t.Fatalf("要把 key 原样带回去给界面定位那一行，实得 %q", row.Key)
	}
}

func TestHostRescanSingleRow(t *testing.T) {
	tmp := t.TempDir()
	hsMk(t, tmp, "volumes/v1/_data/a.txt", 3)
	hsMk(t, tmp, "tmp/build-tmp.txt", 4)

	row := (&Client{}).RescanHostRow(context.Background(), rowVolumeData,
		DaemonInfo{OK: true, Root: tmp})
	if runtime.GOOS != "linux" {
		return
	}
	if row.Key != rowVolumeData {
		t.Fatalf("单行重试要带对 key，实得 %q", row.Key)
	}
	if row.Count != 1 || row.Bytes != 3 {
		t.Fatalf("只重试了这一行却数错：%d/%d，应为 1/3", row.Count, row.Bytes)
	}
	// system.tmp 不在这次范围内，不该被顺带扫出来。
	if r := reasonRow(rowSystemTmp); r.Key != "" {
		t.Fatalf("system.tmp 不是原因行")
	}
}

// ---------- HostPathFacts ----------

func TestHostPathFacts(t *testing.T) {
	tmp := t.TempDir()
	empty := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(empty, 0o777); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(tmp, "gone")

	facts := (&Client{}).HostPathFacts([]string{gone, empty, f})
	if len(facts) != 3 {
		t.Fatalf("进去几条就要回几条，实得 %d", len(facts))
	}
	byPath := map[string]HostPathFact{}
	for _, fa := range facts {
		byPath[fa.Path] = fa
	}
	if g := byPath[gone]; g.Exists || g.Error != "" {
		t.Fatalf("不存在只标不存在、不写错误： %+v", g)
	}
	if e := byPath[empty]; !e.Exists || !e.Readable || !e.Empty {
		t.Fatalf("空目录要能区分「空」与「读不动」： %+v", e)
	}
	if x := byPath[f]; !x.Exists || !x.Readable {
		t.Fatalf("普通文件应可读： %+v", x)
	}
}

// ---------- 小工具 ----------

func TestHostSmallUtils(t *testing.T) {
	if joinRoot("", "x") != "" {
		t.Fatalf("数据目录为空时不能拼出相对路径")
	}
	if joinRoot("/d", "containers") != "/d/containers" {
		t.Fatalf("joinRoot 拼错")
	}

	got := dedupe([]string{"", "a", "a", "", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("dedupe 要去空去重并保序，实得 %v", got)
	}

	if ks := sortedKeys(map[string]int{"b": 1, "a": 2, "c": 3}); len(ks) != 3 || ks[0] != "a" || ks[2] != "c" {
		t.Fatalf("sortedKeys 没升序：%v", ks)
	}

	if unescapeMountPath("/mnt/my\\040dir") != "/mnt/my dir" {
		t.Fatalf("空格没还原")
	}
	if unescapeMountPath("/a\\011b") != "/a\tb" {
		t.Fatalf("制表符没还原")
	}
	if unescapeMountPath("/a\\012b") != "/a\nb" {
		t.Fatalf("换行没还原")
	}
	if unescapeMountPath("/a\\134b") != `/a\b` {
		t.Fatalf("反斜杠没还原")
	}
}

// ---------- 端到端：只断言根在 t.TempDir() 里的那几行 ----------

func TestHostScanHost_TempRoot(t *testing.T) {
	tmp := t.TempDir()
	// 把 rootless 运行时目录钉在一个空位上：这一条用例只管「根在 t.TempDir() 里的那几行」，
	// 不该把这趟真机上 libpod/tmp 里那些正在用的东西一起数进来。
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "runtime-absent"))
	hsMk(t, tmp, "containers/aaa/config.v2.json", 5)
	hsMk(t, tmp, "containers/aaa/aaa-json.log", 7)
	hsMk(t, tmp, "containers/aaa/aaa-json.log.1.gz", 3)
	hsMk(t, tmp, "volumes/v1/_data/a.txt", 4)
	hsMk(t, tmp, "plugins/p1/config.v2.json", 2)
	hsMk(t, tmp, "tmp/leftover.txt", 1)

	hsStubProbes(t, hsRefuse, hsNoCommand)
	var stages int
	res, err := (&Client{}).ScanHost(context.Background(), DaemonInfo{OK: true, Root: tmp},
		StageFn(func(string) { stages++ }))
	if runtime.GOOS != "linux" {
		// 非 linux 走的是「够不着」分支：必须报错并给一句人话，而不是静默给一片 0。
		if !errors.Is(err, ErrHostNotSupported) {
			t.Fatalf("非 linux 应返回 ErrHostNotSupported，实得 %v", err)
		}
		if len(res.Warnings) == 0 || res.Warnings[0] != MsgVMRoot {
			t.Fatalf("非 linux 要给人话告警，实得 %v", res.Warnings)
		}
		return
	}
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if stages != hostStagesTotal {
		t.Fatalf("报了 %d 格，hostStagesTotal 是 %d", stages, hostStagesTotal)
	}
	if res.ScannedAt.IsZero() {
		t.Fatalf("每一行都要带采集时间")
	}

	meta := hsMustRow(t, res.Rows, rowContainerMeta)
	if meta.Count != 1 || meta.Bytes != 5 {
		t.Fatalf("容器元数据 %d/%d，应为 1/5", meta.Count, meta.Bytes)
	}
	if meta.ScannedAt.IsZero() {
		t.Fatalf("行上的采集时间是零值，界面就画不出「多久之前」")
	}
	if time.Since(meta.ScannedAt) > time.Minute {
		t.Fatalf("采集时间不对：%v", meta.ScannedAt)
	}
	if lg := hsMustRow(t, res.Rows, rowLogContainer); lg.Count != 1 || lg.Bytes != 7 {
		t.Fatalf("容器日志 %d/%d，应为 1/7", lg.Count, lg.Bytes)
	}
	if rl := hsMustRow(t, res.Rows, rowLogRotate); rl.Count != 1 || rl.Bytes != 3 {
		t.Fatalf("滚动日志 %d/%d，应为 1/3", rl.Count, rl.Bytes)
	}
	if vd := hsMustRow(t, res.Rows, rowVolumeData); vd.Count != 1 || vd.Bytes != 4 {
		t.Fatalf("卷数据 %d/%d，应为 1/4", vd.Count, vd.Bytes)
	}
	if pc := hsMustRow(t, res.Rows, rowPluginConfig); pc.Count != 1 || pc.Bytes != 2 {
		t.Fatalf("插件配置 %d/%d，应为 1/2", pc.Count, pc.Bytes)
	}
	// system.tmp 的根也在 tmp 下，可以硬断。
	if st := hsMustRow(t, res.Rows, rowSystemTmp); st.Count != 1 || st.Bytes != 1 {
		t.Fatalf("临时目录 %d/%d，应为 1/1", st.Count, st.Bytes)
	}
	root := hsMustRow(t, res.Rows, rowSystemRoot)
	if !root.Aggregate || !root.HasBytes {
		t.Fatalf("system.root 的标记不对：%+v", root)
	}
	hsHasPath(t, root, filepath.Join(tmp, "containers"))
	if root.Count < 6 {
		t.Fatalf("system.root 至少该数到本次造的 6 个文件，实得 %d", root.Count)
	}
}

// ---------- 目录构造助手 ----------

func hsMk(t *testing.T, root, rel string, size int) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o666); err != nil {
		t.Fatal(err)
	}
}

// hsWrite 落一份给定内容的文件（父目录顺带建好）：hsMk 只能写零字节，pid 文件要的是内容。
func hsWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func hsChmod(t *testing.T, p string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

// hostSources 必须把 Podman 的配置目录纳入检索（引擎是 podman 时 /etc/docker 与 ~/.docker 往往不存在，
// 漏了这两根，配置两行就永远报「这台机器上没有这个目录」——真机取证级的漏检）。
func TestHostSources_IncludesPodmanConfigDirs(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	srcs := hostSources(DaemonInfo{Root: "/var/lib/docker"})
	byID := map[string]hostSource{}
	for _, s := range srcs {
		byID[s.id] = s
	}
	etc, ok := byID["etc-docker"]
	if !ok {
		t.Fatal("etc-docker source 缺失")
	}
	wantEtc := map[string]bool{"/etc/docker": false, "/etc/containers": false}
	for _, r := range etc.roots {
		wantEtc[r] = true
	}
	for _, want := range []string{"/etc/docker", "/etc/containers"} {
		if !wantEtc[want] {
			t.Fatalf("etc-docker 应含 %q，got=%v", want, etc.roots)
		}
	}
	user, ok := byID["userconfig"]
	if !ok {
		t.Fatal("userconfig source 缺失")
	}
	wantUser := map[string]bool{"/home/u/.docker": false, "/home/u/.config/containers": false}
	for _, r := range user.roots {
		wantUser[r] = true
	}
	for _, want := range []string{"/home/u/.docker", "/home/u/.config/containers"} {
		if !wantUser[want] {
			t.Fatalf("userconfig 应含 %q，got=%v", want, user.roots)
		}
	}
}

// engine-cache 那一段必须真的把容器引擎放在宿主上的结果缓存纳入检索：
// 引擎是 Podman 时那一份就摆在 ~/.local/share/containers/cache 与 ~/.cache/containers，
// 漏了它，other.cache 这一行就永远只能说「没有这样一份东西」——而那是不实的话。
func TestHostSources_IncludesPodmanResultCache(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	byID := map[string]hostSource{}
	for _, s := range hostSources(DaemonInfo{Root: "/var/lib/docker"}) {
		byID[s.id] = s
	}
	ec, ok := byID["engine-cache"]
	if !ok {
		t.Fatal("engine-cache source 缺失")
	}
	// 只挂 other.cache 这一行：挂到别的行会把别处的占用算第二遍。
	if len(ec.rows) != 1 || ec.rows[0] != rowOtherCache {
		t.Fatalf("engine-cache 应只落 other.cache 一行，实得 %v", ec.rows)
	}
	want := map[string]bool{
		"/home/u/.local/share/containers/cache": false,
		"/home/u/.cache/containers":             false,
		dirVarCacheContainers:                   false,
	}
	if len(ec.roots) != len(want) {
		t.Fatalf("engine-cache 应有 %d 个候选目录，实得 %v", len(want), ec.roots)
	}
	for _, r := range ec.roots {
		if _, ok := want[r]; !ok {
			t.Fatalf("engine-cache 多出一个没登记的落点 %q", r)
		}
		want[r] = true
	}
	for r, seen := range want {
		if !seen {
			t.Fatalf("engine-cache 应含 %q，实得 %v", r, ec.roots)
		}
	}
	// 这一行走用户目录，不是 Linux 专属：macOS 上的宿主目录照样读得到。
	if ec.linuxOnly || ec.rootDep || ec.probe != "" {
		t.Fatalf("engine-cache 不该是 Linux 专属／挂数据目录／走命令：%+v", ec)
	}
}

// 数得到就报数字：other.cache 这一行在 Podman 机器上有真数据。
func TestHostEngineCacheCountsFiles(t *testing.T) {
	root := t.TempDir()
	hsMk(t, root, "blob-info-cache-v1.sqlite", 4096)
	hsMk(t, root, "short-name-aliases.conf.lock", 0) // 0 字节：算一条，但不进体积

	src := hostSource{id: "engine-cache", stage: StageHostEngineCache,
		roots: []string{root}, rows: []string{rowOtherCache}}
	h := newHostScan()
	h.walkSource(context.Background(), src)
	r := hsMustRow(t, h.finalise(DaemonInfo{OK: true, Root: t.TempDir()}), rowOtherCache)

	if r.Status != model.RowOK || r.Count != 2 {
		t.Fatalf("结果缓存行应数到 2 条，实得 %v/%d", r.Status, r.Count)
	}
	if r.Bytes != 4096 || !r.HasBytes {
		t.Fatalf("体积应只算有内容的那一份（4096），实得 %d/HasBytes=%v", r.Bytes, r.HasBytes)
	}
	hsHasPath(t, r, filepath.Join(root, "blob-info-cache-v1.sqlite"))
}

// 数不到时这一行既不消失也不给 0：交那句「只有 Podman 有这一份」的人话（需求 ㉘）。
func TestHostEngineCacheFallsBackToExplanation(t *testing.T) {
	src := hostSource{id: "engine-cache", stage: StageHostEngineCache,
		roots: []string{filepath.Join(t.TempDir(), "no-such-cache")}, rows: []string{rowOtherCache}}
	h := newHostScan()
	h.walkSource(context.Background(), src)
	r := hsMustRow(t, h.finalise(DaemonInfo{OK: true, Root: t.TempDir()}), rowOtherCache)

	if r.Status != model.RowUnavailable || r.Message != MsgOtherCache {
		t.Fatalf("数不到时应给人话原因，实得 %v/%q", r.Status, r.Message)
	}
}
