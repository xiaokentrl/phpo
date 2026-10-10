// 宿主侧删除（clean_hostdel.go）的用例：锁「什么情况下不许动手」与「动手时该敲哪几个参数」。
//
// 这一层是全面板唯一真去改这台机器的地方——删目录、拆网卡、把人移出 docker 组，
// 门一松就是删到别的东西上，所以判定必须有用例钉住，而不是靠读代码放心。
//
// 两条取材规矩：
//   - 凡是要**落命令**的都换成假件（runDeleteCmd / runLocal / pkexecPath），
//     真跑一次 `ip link delete` 或 `gpasswd -d` 会改掉用户这台机器上正在跑的东西。
//   - 凡是要在**盘上验证结果**的（回收站留底、目录真的没了）一律用 t.TempDir() 造出来的目录，
//     不碰 /var/lib/docker。
//
// 只在 Linux 宿主上才成立的几条（要碰 /sys、/var/run/netns、/etc/group 这些写死的真实路径）
// 用 hdOnlyLinux 挡掉——macOS / Windows 上 Docker 在虚拟机里，这些行本来就不给删除按钮（需求 ⑮）。
package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hdOnlyLinux 说明这一条为什么只在 Linux 上跑。
func hdOnlyLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("这一条要碰 Linux 宿主上写死的真实路径，只在 Linux 上跑")
	}
}

// hdCmdCall 记下「本来要敲出去的那一次命令」。
type hdCmdCall struct {
	elevated bool
	argv     []string
}

// hdStub 是 runDeleteCmd 的假件：记参数、按 err 决定成不成。
type hdStub struct {
	calls []hdCmdCall
	err   error
}

func (s *hdStub) run(ctx context.Context, elevated bool, name string, args ...string) (string, error) {
	s.calls = append(s.calls, hdCmdCall{elevated: elevated, argv: append([]string{name}, args...)})
	if s.err != nil {
		return "", s.err
	}
	return "", nil
}

// hdArgvs 把记下来的参数拼成好读的一串，失败信息直接给这一串。
func (s *hdStub) hdArgvs() string {
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		mark := "本机"
		if c.elevated {
			mark = "提权"
		}
		out = append(out, mark+" "+strings.Join(c.argv, " "))
	}
	return strings.Join(out, " | ")
}

func (s *hdStub) hdCalled() bool { return len(s.calls) > 0 }

// hdStubDeleteCmd 换掉删除侧唯一的命令出口，用例结束即还原。
func hdStubDeleteCmd(t *testing.T, s *hdStub) {
	t.Helper()
	orig := runDeleteCmd
	runDeleteCmd = s.run
	t.Cleanup(func() { runDeleteCmd = orig })
}

// hdStubRunLocal 换掉「在本机跑一条命令」那一步，用来看最终 argv。
func hdStubRunLocal(t *testing.T, got *[]string, err error) {
	t.Helper()
	orig := runLocal
	runLocal = func(ctx context.Context, argv []string) (string, error) {
		*got = append(*got, strings.Join(argv, " "))
		if err != nil {
			return "", err
		}
		return "", nil
	}
	t.Cleanup(func() { runLocal = orig })
}

// hdStubPkexec 决定「这台机器有没有 pkexec」，以及有的时候它在哪。
func hdStubPkexec(t *testing.T, path string, err error) {
	t.Helper()
	orig := pkexecPath
	pkexecPath = func() (string, error) { return path, err }
	t.Cleanup(func() { pkexecPath = orig })
}

// hdVol 在 <root>/volumes 下造一个带内容的目录，模拟 Docker 卷的数据目录。
func hdVol(t *testing.T, root, name string) string {
	t.Helper()
	p := filepath.Join(root, "volumes", name)
	if err := os.MkdirAll(filepath.Join(p, "_data"), 0o777); err != nil {
		t.Fatalf("造卷目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p, "_data", "a.txt"), []byte("留在盘上的证据"), 0o777); err != nil {
		t.Fatalf("写卷内容失败: %v", err)
	}
	return p
}

// hdFirstUnder 找出「这台机器上确实还在」的一个对象名，用来演幂等之外的另一档。
// 读不到或目录为空即跳过：猜一个名字等于让用例去断言一件它没看见的事。
func hdFirstUnder(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("这台机器上读不到 %s，这一条跳过: %v", dir, err)
	}
	for _, e := range entries {
		if nameSafe(e.Name()) == nil {
			return e.Name()
		}
	}
	t.Skipf("%s 里没有条目，演不出「这个东西确实还在」那一档", dir)
	return ""
}

// hdErr 取错误文本，供「原因里必须含这句话」的判定。
func hdErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestHostNameSafe_RejectsOptionLookalikes 锁住进命令行之前的那道名字门。
//
// 为什么要这么严：`ip` 和 `gpasswd` 没有 `--` 这颗分隔符，一个以 - 开头的名字会被它们当成
// 自己的选项——那不是「删错一样东西」，而是「多做了别的事」。用户在界面上勾出来的名字
// 走到这里必须已经不可能翻出一个解释器。
func TestHostNameSafe_RejectsOptionLookalikes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "名字是空的"},
		{"-f", "以 - 开头"},
		{"a/b", "含路径分隔符"},
		{`a\b`, "含路径分隔符"},
		{"a\x00b", "含空字节"},
	}
	for _, c := range cases {
		err := nameSafe(c.in)
		if err == nil {
			t.Fatalf("nameSafe(%q) 应拒绝，却放过了", c.in)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("nameSafe(%q) 的原因应含 %q，实得 %q", c.in, c.want, err)
		}
	}
	if err := nameSafe("phpo-network"); err != nil {
		t.Fatalf("正常名字不该被拦，用户会看到删不掉: %v", err)
	}
}

// TestHostCheckPath_ShapeRefusals 锁住形状那一半：空、空字节、相对、没归一，四样都不许删。
//
// 「没归一」这条看着苛刻（把 /a/b/../c 化简一下不行吗），但化简等于替用户猜：
// 扫描层报出来的路径本来就是归一过的，到这里还带 .. 就说明中间有人拼过字符串。
func TestHostCheckPath_ShapeRefusals(t *testing.T) {
	hdOnlyLinux(t)
	dir := t.TempDir()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空路径", "", "路径是空的"},
		{"含空字节", dir + "\x00x", "路径含空字节"},
		{"相对路径", "volumes/abc", "不是绝对路径"},
		{"未归一", dir + "//abc", "未归一"},
		{"藏了 ..", filepath.Dir(dir) + "/phpo/../abc", "未归一"},
	}
	for _, c := range cases {
		err := checkHostPath(c.in, []string{dir})
		if err == nil {
			t.Fatalf("%s：checkHostPath(%q) 应拒绝", c.name, c.in)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s：原因应含 %q，实得 %q", c.name, c.want, err)
		}
	}
}

// TestHostCheckPath_CatastrophicRootsRefused 把系统关键目录一圈逐项跑一遍。
//
// 判定故意只用**完全相等**：给这批根目录任何一个当候选根，都不许整份删掉。
// 用户在界面上勾「容器目录」这一行时勾到的应该是其中一个容器，不是整个 /var。
func TestHostCheckPath_CatastrophicRootsRefused(t *testing.T) {
	hdOnlyLinux(t)
	for _, p := range []string{"/", "/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib32",
		"/lib64", "/libx32", "/media", "/mnt", "/opt", "/proc", "/root", "/run", "/sbin",
		"/srv", "/sys", "/tmp", "/usr", "/var"} {
		if !catastrophicRoots[p] {
			t.Fatalf("%s 本该在「无论如何不许整份删掉」那份名单里", p)
		}
		// 候选根给成 / 意味着「它下面的一切都在扫描范围内」——仍然必须被灾难根这一道挡住。
		err := checkHostPath(p, []string{"/"})
		if err == nil || !strings.Contains(err.Error(), "拒绝删除系统关键目录") {
			t.Fatalf("系统关键目录 %s 没被挡住，实得 %q", p, hdErr(err))
		}
	}
}

// TestHostCheckPath_UnderRootYesRootNo 是这道门最要紧的一格：允许根目录**之下**的东西，
// 拒绝根目录**本身**，也拒绝跑到别处去。
//
// 反例场景：network.cni 这一行的条目就在 /etc 底下，而 /etc 是灾难根——
// 按前缀判等于把这一行的删除按钮永远锁死（需求 ⑫ 明确给了它按钮）。
func TestHostCheckPath_UnderRootYesRootNo(t *testing.T) {
	hdOnlyLinux(t)

	if err := checkHostPath(filepath.Join(dirCniConf, "00-phpo.conflist"), []string{dirSysNet, dirCniConf}); err != nil {
		t.Fatalf("CNI 配置该允许（它在 /etc 底下但不是 /etc 本身），实得 %q", err)
	}

	root := t.TempDir()
	volRoot := filepath.Join(root, "volumes")
	if err := os.MkdirAll(volRoot, 0o777); err != nil {
		t.Fatalf("建卷根目录失败: %v", err)
	}
	if err := checkHostPath(volRoot, []string{volRoot}); err == nil ||
		!strings.Contains(err.Error(), "或就是那个目录本身") {
		t.Fatalf("整份删掉卷根目录必须被拒，实得 %q", hdErr(checkHostPath(volRoot, []string{volRoot})))
	}
	if err := checkHostPath(filepath.Join(root, "containers", "abc"), []string{volRoot}); err == nil ||
		!strings.Contains(err.Error(), "不在这一行扫描过的目录之下") {
		t.Fatalf("这一行的条目跑到别的目录去了，必须被拒")
	}
}

// TestHostResolvePath_BareNameNeedsExactlyOneRoot 说的是「猜根目录等于删到别处去」。
//
// 采集层往外给的东西形状不齐：有的行给绝对路径，有的行只给一个裸名字（卷名、网卡名）。
// 裸名字只在这一行恰好只有一个候选目录时才落得出唯一路径；两个就宁缺毋滥——
// 界面上这一项会失败并说明「有 2 个候选目录」，而不是挑一个赌一把。
func TestHostResolvePath_BareNameNeedsExactlyOneRoot(t *testing.T) {
	root := t.TempDir()
	volRoot := filepath.Join(root, "volumes")

	got, err := resolveHostPath(rowVolumeData, "abc", []string{volRoot})
	if err != nil {
		t.Fatalf("唯一候选目录时该落得出路径: %v", err)
	}
	if got != filepath.Join(volRoot, "abc") {
		t.Fatalf("落出的路径应是 %s，实得 %s", filepath.Join(volRoot, "abc"), got)
	}

	// 绝对路径原样透传：采集层已经给全了，这里不再拼第二次。
	abs := filepath.Join(volRoot, "abc")
	if got, err = resolveHostPath(rowVolumeData, abs, []string{dirCniConf}); err != nil || got != abs {
		t.Fatalf("绝对路径应原样透传，实得 %s / %v", got, err)
	}

	for _, c := range []struct {
		roots []string
		want  string
	}{
		// system.group / network.iptables 这类行问的是命令不是磁盘，候选目录数为 0。
		{nil, "0 个候选目录"},
		{[]string{dirSysNet, dirCniConf}, "2 个候选目录"},
		// 只有空串等于「这台机器报不出数据目录」，同样不能猜——它数上是 1 个候选，却落不出路径。
		{[]string{""}, "落不出唯一路径"},
	} {
		_, err := resolveHostPath(rowVolumeData, "abc", c.roots)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("候选目录 %v 时应报 %q，实得 %q", c.roots, c.want, hdErr(err))
		}
	}

	for _, bad := range []string{"..", "a/b", `a\b`, "a\x00b"} {
		if _, err := resolveHostPath(rowVolumeData, bad, []string{volRoot}); err == nil ||
			!strings.Contains(err.Error(), "不是一个安全的文件/目录名") {
			t.Fatalf("条目名 %q 该被拒（它不是名字，是要往外走一层）", bad)
		}
	}
}

// TestHostRowRootsCountsPerRow 锁住「每行落在哪些目录」这张表本身。
//
// 它是删除侧唯一的落点来源：分类、路径门、幂等判定全按它判。
// 采集层改了落点而这里没跟着改，就会出现「扫得到却删不掉」或者更糟的「删到隔壁目录」。
// 两处共用同一份 hostSources，所以这里也是那张表的对账用例。
func TestHostRowRootsCountsPerRow(t *testing.T) {
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true, Rootless: true}

	single := []string{rowVolumeData, rowVolumeDriver, rowContainerCheckpoint, rowContainerMeta,
		rowLogContainer, rowLogRotate, rowSystemTmp, rowPluginConfig, rowPluginData,
		rowContainerCgroup, rowNetworkBridge, rowNetworkVeth, rowNetworkNetns,
		rowLogDaemon, rowSystemContainerd}
	for _, row := range single {
		if got := hostRowRoots(row, info); len(got) != 1 {
			t.Fatalf("%s 该只有 1 个候选目录，实得 %d 个：%v", row, len(got), got)
		}
	}
	// 两个候选目录的行：日志同时看 /var/log 与 journal，CNI 既可能是网卡也可能是配置，
	// system.config 双引擎各一套配置目录（/etc/docker 与 /etc/containers）。
	for _, row := range []string{rowNetworkCni, rowLogJournald, rowSystemBuilder, rowSystemRoot, rowSystemConfig} {
		if got := hostRowRoots(row, info); len(got) != 2 {
			t.Fatalf("%s 该有 2 个候选目录，实得 %d 个：%v", row, len(got), got)
		}
	}
	// 走命令不走磁盘的两行：候选目录为 0，因此它们的名字不可能被拼成路径删掉。
	for _, row := range []string{rowNetworkIptables, rowSystemGroup} {
		if got := hostRowRoots(row, info); len(got) != 0 {
			t.Fatalf("%s 不该有任何候选目录（它问的是命令），实得 %v", row, got)
		}
	}

	// 唯一出口的落点必须真的挂在数据目录下面，不能写死成 ~/phpo 那一套。
	if got := hostRowRoots(rowVolumeData, info); len(got) != 1 || got[0] != filepath.Join(root, "volumes") {
		t.Fatalf("volume.data 的根应是 %s，实得 %v", filepath.Join(root, "volumes"), hostRowRoots(rowVolumeData, info))
	}

	// 守护进程报不出数据目录时：挂它下面的行降到 0 个候选，写死的常量路径不受影响。
	empty := map[string]int{
		rowVolumeData: 0, rowSystemTmp: 0, rowPluginConfig: 0, rowContainerCheckpoint: 0,
		rowSystemBuilder: 1, rowSystemRoot: 1, rowNetworkCni: 2, rowLogJournald: 2,
		rowNetworkNetns: 1, rowContainerCgroup: 1, rowSystemGroup: 0,
	}
	for row, want := range empty {
		if got := hostRowRoots(row, DaemonInfo{}); len(got) != want {
			t.Fatalf("无数据目录时 %s 该有 %d 个候选目录，实得 %d 个：%v", row, want, len(got), got)
		}
	}
}

// TestHostClassify_KindsAndOverrides 是删除侧唯一的分类门。
//
// 服务层传来的 Kind 一律不作数：外面传个 host_path 就把一张网卡当目录 `rm` 掉，
// 那是删不干净又留下一条看不懂的报错。分类错了用户看到的就是「删失败」，
// 但真正该做的是拆网卡。network.cni 先按网卡认、认不出再按配置走，这个顺序是刻意的。
func TestHostClassify_KindsAndOverrides(t *testing.T) {
	hdOnlyLinux(t)
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true}

	cases := []struct {
		name     string
		op       DeleteOp
		wantKind string
		wantRef  string
	}{
		{"veth 拆成网卡", DeleteOp{Row: rowNetworkVeth, Kind: DelHostPath, Ref: "/sys/class/net/veth42"}, DelHostLink, "veth42"},
		{"命名空间", DeleteOp{Row: rowNetworkNetns, Ref: filepath.Join(dirNetns, "cni-7f3a")}, DelHostNetns, "cni-7f3a"},
		{"cni 先按网卡认", DeleteOp{Row: rowNetworkCni, Ref: "/sys/class/net/calix1"}, DelHostLink, "calix1"},
		{"cni 认不出网卡即按配置走", DeleteOp{Row: rowNetworkCni, Ref: filepath.Join(dirCniConf, "10-calico.conflist")}, DelHostPath, filepath.Join(dirCniConf, "10-calico.conflist")},
		{"cgroup 目录", DeleteOp{Row: rowContainerCgroup, Ref: filepath.Join(dirSysCgroup, "system.slice/docker-abc.scope")}, DelHostCgroup, filepath.Join(dirSysCgroup, "system.slice/docker-abc.scope")},
		{"移出 docker 组", DeleteOp{Row: rowSystemGroup, Kind: DelHostPath, Ref: "webdev"}, DelHostGroup, "webdev"},
		{"卷目录走路径门", DeleteOp{Row: rowVolumeData, Ref: "abc"}, DelHostPath, filepath.Join(root, "volumes", "abc")},
	}
	for _, c := range cases {
		got, err := classifyHostOp(c.op, info)
		if err != nil {
			t.Fatalf("%s：分类不该失败: %v", c.name, err)
		}
		if got.Kind != c.wantKind {
			t.Fatalf("%s：类型应是 %s，实得 %s（传进来的 %s 不作数）", c.name, c.wantKind, got.Kind, c.op.Kind)
		}
		if got.Ref != c.wantRef {
			t.Fatalf("%s：参数应是 %s，实得 %s", c.name, c.wantRef, got.Ref)
		}
	}

	// 三种「服务层传了别的 Kind」都要被现算的结果盖掉。
	for _, op := range []DeleteOp{
		{Row: rowNetworkVeth, Kind: DelHostPath, Ref: "/sys/class/net/veth42"},
		{Row: rowVolumeData, Kind: DelHostLink, Ref: filepath.Join(root, "volumes", "abc")},
		{Row: rowSystemGroup, Kind: DelHostPath, Ref: "webdev"},
	} {
		got, err := classifyHostOp(op, info)
		if err != nil || got.Kind == op.Kind {
			t.Fatalf("外面传的 Kind 必须被盖掉：%+v 实得 %+v / %v", op, got, err)
		}
	}

	// 认不出接口名、又不在自己目录之下的 cni 条目：两头都不挨着，只能拒。
	if _, err := classifyHostOp(DeleteOp{Row: rowNetworkCni, Ref: filepath.Join(dirVarLog, "docker.log")}, info); err == nil {
		t.Fatal("这条既不是网卡也不在 CNI 配置目录下，不该分类成功")
	}

	bad := []struct {
		name string
		op   DeleteOp
		want string
	}{
		{"网卡名以 - 开头", DeleteOp{Row: rowNetworkVeth, Ref: "/sys/class/net/-x"}, "条目名不安全"},
		{"网卡名下还有一层", DeleteOp{Row: rowNetworkVeth, Ref: "/sys/class/net/a/b"}, "不是一个接口名"},
		{"网卡名是相对路径", DeleteOp{Row: rowNetworkVeth, Ref: "veth42"}, "不是绝对路径"},
		{"组名以 - 开头", DeleteOp{Row: rowSystemGroup, Ref: "-d"}, "条目不安全"},
		// 认不出的行也必须过路径门：这一行在扫描表里没有落点，任何路径都不该被它带走。
		{"这一行没有扫过的目录", DeleteOp{Row: rowOtherCache, Ref: filepath.Join(root, "x")}, "不在这一行扫描过的目录之下"},
	}
	for _, c := range bad {
		if _, err := classifyHostOp(c.op, info); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s：应报含 %q 的原因，实得 %q", c.name, c.want, hdErr(err))
		}
	}
}

// TestHostDeleteObjects_BadItemKillsOnlyItself 锁住「彻底清空」的循环语义（需求 ⑲/㉘）。
//
// 勾了 2 项，其中 1 项指的是卷根目录本身（不该删），另外 1 项照常删掉；
// 失败那项在界面上仍然有一行，带自己的原因和原来的名字——
// 而不是整单落空，也不是悄悄少报一行。
func TestHostDeleteObjects_BadItemKillsOnlyItself(t *testing.T) {
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true}
	good := hdVol(t, root, "abc")
	volRoot := filepath.Join(root, "volumes")

	ops := []DeleteOp{
		{Row: rowVolumeData, Ref: "..", Size: 4242}, // 不该往外走一层
		{Row: rowVolumeData, Ref: good, Size: 100},  // 这一颗该删掉
	}
	var seen []string
	res := (&Client{}).DeleteHostObjects(context.Background(), ops, info, nil, func(r DeleteResult) {
		seen = append(seen, r.Row+":"+hdErr(r.Err))
	})

	if len(res) != len(ops) {
		t.Fatalf("回执数应等于指令数，实得 %d", len(res))
	}
	if len(seen) != len(ops) {
		t.Fatalf("每项都要回调一次（界面靠它逐行点名），实得 %d 次", len(seen))
	}
	if res[0].Err == nil {
		t.Fatal("第一条该带着自己的原因失败")
	}
	if !strings.Contains(res[0].Err.Error(), "不是一个安全的文件/目录名") {
		t.Fatalf("第一条的原因要说清为什么不让删，实得 %q", res[0].Err)
	}
	// 分类失败的条目没跑出类型，回执按行名推一个，界面才不会显示「不知道是什么东西删失败了」。
	if res[0].Kind != DelHostPath {
		t.Fatalf("失败条目也应带上类型标签，实得 %s", res[0].Kind)
	}
	if res[0].Ref != ".." {
		t.Fatalf("失败条目应保留用户勾的那条原样名字，实得 %s", res[0].Ref)
	}
	if res[0].Freed != 0 {
		t.Fatalf("失败项 Freed 必须为 0，否则虚报磁盘空间，实得 %d", res[0].Freed)
	}
	if res[1].Err != nil || res[1].Freed != 100 {
		t.Fatalf("第二项应删成并按扫描体积记账，得 Freed=%d Err=%v", res[1].Freed, res[1].Err)
	}
	if _, err := os.Stat(volRoot); err != nil {
		t.Fatalf("卷根目录本身还在（一处都不该动到它），却报 %v", err)
	}
	if _, err := os.Stat(good); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("第二项应真的从盘上没了，实得 %v", err)
	}
}

// TestHostDeleteRef_ArgvShapes 锁住五种删法各自敲出去的那几个参数。
//
// 这一条是「argv 直接传、不经 shell」在删除侧的唯一证据：只要有任何一处改成拼 shell 字符串，
// 用户在界面上勾出来的名字就会被解释器二次理解。参数位上的 `--` 同样要看住——
// 没有它，一个以 - 开头的名字就会被命令当成选项。
func TestHostDeleteRef_ArgvShapes(t *testing.T) {
	hdOnlyLinux(t)
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true}

	// 属主是 root 的卷数据目录：必须提权 `rm -rf --`，且腾出的体积按扫描时数到的那份记。
	s := &hdStub{}
	hdStubDeleteCmd(t, s)
	op := DeleteOp{Row: rowVolumeData, Ref: hdVol(t, root, "needroot"), Size: 1234, NeedsRoot: true}
	if freed, _, err := (&Client{}).deleteHostOne(context.Background(), op, info, nil); err != nil {
		t.Fatalf("提权删除这条不该失败: %v", err)
	} else if freed != 1234 {
		t.Fatalf("删成应按扫描体积记账，实得 %d", freed)
	}
	want := []string{"rm", "-rf", "--", op.Ref}
	if got := s.hdArgvs(); got != "提权 "+strings.Join(want, " ") {
		t.Fatalf("提权删目录的参数应是 %v，实得 %s", want, got)
	}

	// cgroup 目录：不递归删，能删掉说明这个资源组真的空了。
	s2 := &hdStub{}
	hdStubDeleteCmd(t, s2)
	cg := DeleteOp{Row: rowContainerCgroup, Ref: filepath.Join(dirSysCgroup, "system.slice/docker-x.scope"), NeedsRoot: true}
	if _, _, err := (&Client{}).deleteHostOne(context.Background(), cg, info, nil); err != nil {
		t.Fatalf("提权删 cgroup 不该失败: %v", err)
	}
	if got, want2 := s2.hdArgvs(), "提权 rmdir -- "+cg.Ref; got != want2 {
		t.Fatalf("删 cgroup 应是 %s，实得 %s（绝不能递归）", want2, got)
	}

	// 拆网卡要挑一个**这台机器上确实还在**的名字：换一个不存在的名字，deleteHostRef
	// 会当成「已经不在了」幂等成功，那条命令根本不会敲出去，参数也就没地方看。
	link := hdFirstUnder(t, dirSysNet)
	s3 := &hdStub{}
	hdStubDeleteCmd(t, s3)
	if _, _, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowNetworkVeth, Ref: filepath.Join(dirSysNet, link), Size: 7}, info, nil); err != nil {
		t.Fatalf("拆网卡不该失败: %v", err)
	}
	if got, want3 := s3.hdArgvs(), "本机 ip link delete "+link; got != want3 {
		t.Fatalf("拆网卡的参数应是 %s，实得 %s", want3, got)
	}
}

// TestHostDeleteRef_NetnsArgv 单开一条：/var/run/netns 里没有条目时这台机器演不出
// 「命名空间确实还在」那一档，跳过的只该是这一条，不该连带把上面几条的记录也判成没跑。
func TestHostDeleteRef_NetnsArgv(t *testing.T) {
	hdOnlyLinux(t)
	info := DaemonInfo{Root: t.TempDir(), OK: true}

	ns := hdFirstUnder(t, dirNetns)
	s := &hdStub{}
	hdStubDeleteCmd(t, s)
	if _, _, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowNetworkNetns, Ref: filepath.Join(dirNetns, ns), Size: 3}, info, nil); err != nil {
		t.Fatalf("删网络命名空间不该失败: %v", err)
	}
	if got, want := s.hdArgvs(), "本机 ip netns delete "+ns; got != want {
		t.Fatalf("删命名空间的参数应是 %s，实得 %s", want, got)
	}
}

// TestHostDeleteRef_IdempotentWhenAlreadyGone 说的是「已经不在了即删好了」。
//
// 用户点彻底清空要的只是「这东西现在没有了」。第三方工具先删过一次，这里就不该
// 在界面上留一行失败——但也**不能**顺手敲一条命令去删一个不存在的东西。
// 这一道判定读的是 /sys/class/net 与 /var/run/netns（谁都能读），不花授权。
func TestHostDeleteRef_IdempotentWhenAlreadyGone(t *testing.T) {
	hdOnlyLinux(t)
	info := DaemonInfo{Root: t.TempDir(), OK: true}

	for _, c := range []struct {
		row string
		dir string
	}{
		{rowNetworkVeth, dirSysNet},
		{rowNetworkNetns, dirNetns},
	} {
		s := &hdStub{}
		hdStubDeleteCmd(t, s)
		ref := filepath.Join(c.dir, "phpo-definitely-not-here")
		freed, trash, err := (&Client{}).deleteHostOne(context.Background(),
			DeleteOp{Row: c.row, Ref: ref, Size: 9}, info, nil)
		if err != nil {
			t.Fatalf("%s：已经不在了应算删成: %v", c.row, err)
		}
		if freed != 9 {
			t.Fatalf("%s：幂等成功也应按原体积记账，实得 %d", c.row, freed)
		}
		if trash != "" {
			t.Fatalf("%s：没动过任何东西，不该说有回收站落点（那是假账）", c.row)
		}
		if s.hdCalled() {
			t.Fatalf("%s：已经不在了还去敲命令，等于删一个不存在的东西：%s", c.row, s.hdArgvs())
		}
	}
}

// TestHostDeleteRef_FailureSaysWhatToFix 锁住三种失败的措辞（需求 ㉘）。
//
// 用户看得见的那句话必须指向真正的原因：删不掉的 cgroup 几乎总是「里面还有进程在跑」，
// 那是保护不是故障；网卡删不掉多半还挂在某个容器上。把这两种说成「删除失败」，
// 用户就会以为是自己操作错了。
func TestHostDeleteRef_FailureSaysWhatToFix(t *testing.T) {
	hdOnlyLinux(t)
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true}

	s := &hdStub{err: errors.New("exit status 1")}
	hdStubDeleteCmd(t, s)
	_, _, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowContainerCgroup, Ref: filepath.Join(dirSysCgroup, "docker/abc"), NeedsRoot: true}, info, nil)
	if err == nil || !strings.Contains(err.Error(), "这个 cgroup 里还有进程在跑") {
		t.Fatalf("删不掉的 cgroup 要说清那是保护，实得 %q", hdErr(err))
	}

	link := hdFirstUnder(t, dirSysNet)
	s2 := &hdStub{err: errors.New("Device or resource busy")}
	hdStubDeleteCmd(t, s2)
	_, _, err = (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowNetworkVeth, Ref: filepath.Join(dirSysNet, link)}, info, nil)
	if err == nil || !strings.Contains(err.Error(), "多半它还挂在某个容器上") {
		t.Fatalf("拆不掉的网卡要指向「还挂在容器上」，实得 %q", hdErr(err))
	}

	// 不在这个组里：已经是用户要的样子，不调命令、不弹授权框。
	s3 := &hdStub{}
	hdStubDeleteCmd(t, s3)
	freed, _, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowSystemGroup, Ref: "phpo-definitely-not-in-docker-group"}, info, nil)
	if err != nil {
		t.Fatalf("不在组里应算成功（读 /etc/group 就够了）: %v", err)
	}
	if s3.hdCalled() {
		t.Fatalf("不在 docker 组里还去调 gpasswd，等于在用户眼前白弹一次授权框：%s", s3.hdArgvs())
	}
	if freed != 0 {
		t.Fatalf("移出组不腾出磁盘，Freed 应为 0，实得 %d", freed)
	}
}

// TestHostDeleteRef_UnknownKind 是兜底位：认不出要删的对象类型即报错，不猜一种删法。
func TestHostDeleteRef_UnknownKind(t *testing.T) {
	_, err := deleteHostRef(context.Background(), DeleteOp{Kind: "host_whatever", Ref: "/tmp/x"})
	if err == nil || !strings.Contains(err.Error(), "不认识要删的宿主对象类型") {
		t.Fatalf("认不出的类型要如实报，不该当成某一种去删，实得 %q", hdErr(err))
	}
}

// TestHostDeleteOne_TrashRules 锁住「先进回收站」这一条的两半（需求 ④/⑳）。
//
// 该留 7 天的东西不能直接删干净：回收站没准备好时宁可这一项不删——
// 「删了但没留底」会把那句「7 天内可恢复」变成假话。另一头，一张网卡、一个 cgroup
// 没有内容可留，硬塞进回收站只是造一个假的恢复入口。
func TestHostDeleteOne_TrashRules(t *testing.T) {
	hdOnlyLinux(t)
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true}

	// 不是文件的东西进不了回收站——这一条在敲任何命令之前就该拦住。
	s := &hdStub{}
	hdStubDeleteCmd(t, s)
	_, _, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowNetworkVeth, Ref: filepath.Join(dirSysNet, "veth9"), TrashIt: true}, info, nil)
	if err == nil || !strings.Contains(err.Error(), "进不了回收站，只能直接删") {
		t.Fatalf("网卡该明说进不了回收站，实得 %q", hdErr(err))
	}
	if s.hdCalled() {
		t.Fatalf("既然拒了就不该动过任何东西：%s", s.hdArgvs())
	}

	// 回收站缺席：宁可这一项不删。
	src := hdVol(t, root, "keepme")
	_, _, err = (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowVolumeData, Ref: src, TrashIt: true}, info, nil)
	if err == nil || !strings.Contains(err.Error(), "回收站还没准备好") {
		t.Fatalf("回收站没准备好时要拦住，实得 %q", hdErr(err))
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("拦住之后盘上那份不该消失: %v", err)
	}

	// 正常那一路：挪进回收站、不腾出磁盘（东西还在盘上），落点交回服务层登记 7 天。
	tr := NewTrash(t.TempDir())
	freed, trashPath, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowVolumeData, Ref: src, Size: 4096, TrashIt: true}, info, tr)
	if err != nil {
		t.Fatalf("移入回收站不该失败: %v", err)
	}
	if freed != 0 {
		t.Fatalf("进回收站不腾出磁盘，Freed 应为 0，实得 %d", freed)
	}
	if trashPath == "" || filepath.Dir(trashPath) != tr.Root || filepath.Base(trashPath) != "keepme" {
		t.Fatalf("回收站落点应在 %s 下且留着原来的名字，实得 %s", tr.Root, trashPath)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("挪走后原位置该空出来，实得 %v", err)
	}
	body, err := os.ReadFile(filepath.Join(trashPath, "_data", "a.txt"))
	if err != nil || string(body) != "留在盘上的证据" {
		t.Fatalf("内容要原样留在回收站里（7 天内可恢复），实得 %q / %v", body, err)
	}
}

// TestHostDeleteOne_TrashNeedsRoot 说的是属主是 root 的那份要提权挪。
//
// 这里要盯住 argv 的形状：`mv -- 源 目标`，两个路径都在 `--` 之后。
// 用 `mv -n` 反而不行——撞名时它静默退出码 0 却不搬，那等于「以为收走了，其实还在原处」。
func TestHostDeleteOne_TrashNeedsRoot(t *testing.T) {
	root := t.TempDir()
	info := DaemonInfo{Root: root, OK: true}
	src := hdVol(t, root, "owned-by-root")
	tr := NewTrash(t.TempDir())

	s := &hdStub{}
	hdStubDeleteCmd(t, s)
	freed, trashPath, err := (&Client{}).deleteHostOne(context.Background(),
		DeleteOp{Row: rowVolumeData, Ref: src, Size: 2048, NeedsRoot: true, TrashIt: true}, info, tr)
	if err != nil {
		t.Fatalf("提权移入回收站不该失败: %v", err)
	}
	if freed != 0 || trashPath == "" {
		t.Fatalf("提权进回收站同样不腾出磁盘、但要给出落点，实得 Freed=%d 落点=%s", freed, trashPath)
	}
	if len(s.calls) != 1 {
		t.Fatalf("只该敲一次命令，实得 %d 次：%s", len(s.calls), s.hdArgvs())
	}
	c := s.calls[0]
	if !c.elevated {
		t.Fatal("属主是 root 的那份必须走授权，不然宿主用户自己的 rename 对它无效")
	}
	if strings.Join(c.argv, " ") != "mv -- "+src+" "+trashPath {
		t.Fatalf("提权挪动的参数应是 mv -- 源 目标，实得 %v", c.argv)
	}
	if filepath.Dir(trashPath) != tr.Root {
		t.Fatalf("落点应在回收站根下，实得 %s", trashPath)
	}
}

// TestRunDeleteCmd_ElevationGate 锁住删除侧唯一的命令出口自己。
//
// 三种情形用户在界面上都会看到一句不同的话：这台机器没有 polkit（只能自己开终端）、
// 本机没有那个命令（不该假装提权成功）、都齐了才真的授权。
// 提权那一路还要盯住「命令名要转成绝对路径再交给 pkexec」——pkexec 只认绝对路径，
// 传个 "rm" 过去它会直接拒绝，界面就变成「不知道为什么删不掉」。
func TestRunDeleteCmd_ElevationGate(t *testing.T) {
	// 不提权：命令名原样排在参数位最前面，不经 pkexec。
	var got []string
	hdStubRunLocal(t, &got, nil)
	if _, err := runDeleteCmd(context.Background(), false, "ip", "link", "delete", "lo"); err != nil {
		t.Fatalf("不提权这条不该失败: %v", err)
	}
	if len(got) != 1 || got[0] != "ip link delete lo" {
		t.Fatalf("不提权时应把命令名原样交给本机，实得 %v", got)
	}

	// 没有 pkexec：要说清是这台机器缺 polkit，不是删除逻辑错了。
	hdStubPkexec(t, "", errors.New("not found"))
	_, err := runDeleteCmd(context.Background(), true, "rm", "-rf", "--", "/tmp/x")
	if err == nil || !strings.Contains(err.Error(), "系统无 pkexec（polkit），无法自动提权删除") {
		t.Fatalf("缺 pkexec 要如实报，实得 %q", hdErr(err))
	}

	// 有 pkexec 但本机没那个命令：不能在授权之后才发现。
	hdStubPkexec(t, "/usr/bin/pkexec", nil)
	_, err = runDeleteCmd(context.Background(), true, "phpo-no-such-delete-cmd", "--", "/tmp/x")
	if err == nil || !strings.Contains(err.Error(), "本机没有 phpo-no-such-delete-cmd 命令，无法提权删除") {
		t.Fatalf("本机没有该命令时要点名命令名，实得 %q", hdErr(err))
	}

	// 都齐了：argv 是 [pkexec 的绝对路径, 命令的绝对路径, 参数…]。
	sh, lookErr := exec.LookPath("sh")
	if lookErr != nil {
		t.Skip("这台机器上没有 sh，演不出成功那一路的参数")
	}
	var got2 []string
	hdStubRunLocal(t, &got2, nil)
	if _, err := runDeleteCmd(context.Background(), true, "sh", "-c", "true"); err != nil {
		t.Fatalf("提权这条不该失败: %v", err)
	}
	want := "/usr/bin/pkexec " + sh + " -c true"
	if len(got2) != 1 || got2[0] != want {
		t.Fatalf("提权参数应是 %q，实得 %v", want, got2)
	}
}

// TestRunLocal_MissingCommand 说清「本机没有这条命令」时用户看得见的那句话。
//
// 这一句会原样进抽屉日志与失败回执：Linux 上 Docker 在、iproute2 却不在的机器真存在，
// 只报「删除失败」用户不知道该装什么。
func TestRunLocal_MissingCommand(t *testing.T) {
	_, err := runLocal(context.Background(), []string{"phpo-no-such-command-for-sure", "--version"})
	if err == nil || !strings.Contains(err.Error(), "本机没有 phpo-no-such-command-for-sure 命令") {
		t.Fatalf("缺命令要点名是哪一条，实得 %q", hdErr(err))
	}
}
