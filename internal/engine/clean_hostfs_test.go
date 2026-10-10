package engine

// 本文件是宿主侧采集器的用例。它一条 Docker 都不碰：
// 全部用 t.TempDir() 造目录，把该 stub 的两颗注入点（提权读取、外部命令）换掉，
// 因此 `go test` 期间不会弹 polkit 授权框，也不会真的去跑 iptables-save。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"phpo/internal/model"
)

// ---------- 用例助手（每一个都必须被调到，否则 CI 的 unused 检查会判红） ----------

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
	// 没被任何一条目录项落过的行**不出现在结果里**（不是「算出个 0」）；
	// 加上四行「说不清原因」的固定行，这份 fixture 就是 2 + 4。
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

// ---------- 非 linux：Linux-only 行照常出现，但只给「—」 ----------

func TestHostNotSupportedRows(t *testing.T) {
	// 四行「原因行」的话术覆盖 MsgVMRoot：它们在任何平台都说不清。
	if r := notSupportedRow(rowLogBuild, true); r.Message != MsgLogBuild || r.Status != model.RowNotSupported {
		t.Fatalf("log.build 该保留自己的原因，实得 %v/%q", r.Status, r.Message)
	}
	if r := notSupportedRow(rowContainerMeta, true); r.Message != MsgVMRoot {
		t.Fatalf("普通 Linux-only 行该说「跑在虚拟机里」，实得 %q", r.Message)
	}
	// ~/.docker 在 macOS/Windows 上就在宿主，够不着时不给那句 VM 解释。
	if r := notSupportedRow(rowSystemUser, false); r.Message != "" {
		t.Fatalf("非 Linux-only 行不该套用 VM 话术，实得 %q", r.Message)
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
