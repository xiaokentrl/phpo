// Docker 全量资源清理 · 宿主侧采集器：把 Docker 落在**这台机器磁盘上**、但它自己那个接口答不出来的那一片数清楚。
//
// 为什么要单独问磁盘而不全信 Docker 接口：容器检查点、cgroup 目录、宿主上的日志文件、
// `/var/lib/docker` 里那一大堆目录、防火墙规则、docker 用户组——这些不是「Docker 资源」，
// Docker 的清单接口从来不说它们。总览页那 60 行要给出真实数字，就只能自己走一遍磁盘。
//
// 这一层只读不删。删是用户在界面上勾出来的决定，由服务层执行；本文件只回答「在哪里、有多少、读得到吗」。
//
// 读不到就说读不到，绝不给 0：一次权限不足不能让那一行显示「0 项 · 0 B」——那是把「不知道」说成「没有」，
// 用户照着这个数字决定「那就不用清」，才是真出事（需求 ㉘）。所以每行都带一个状态：
// 数到了（ok）／要授权才读得动（no_perm）／这台机器上够不着这个位置（not_supported）／这次没答上来（unavailable）。
package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"phpo/internal/model"
)

// ErrHostNotSupported 说「这台机器的宿主够不着 Docker 的那些目录」。
//
// 为什么是「带着结果一起返回」而不是只返回一个错误：macOS / Windows 上 Docker 跑在虚拟机里，
// 那 60 行里的宿主项照样得显示出来（写清「够不着」以及为什么），只是不能给删除按钮。
// 服务层拿到这个错也不能因此把整片留空——结果里已经是 not_supported 的行，直接用。
var ErrHostNotSupported = errors.New("当前平台够不着 Docker 的宿主目录")

// hostOS 是「这台机器是什么系统」的**可注入**版本：判「这一行的落点在不在 Linux 上」一律问它，不问 runtime.GOOS。
//
// 为什么要多这一层：CI 只有 Linux runner，非 Linux 那一条分支（容器引擎跑在虚拟机里、宿主够不着那些目录）
// 平时一行都跑不到，改坏了没人拦得住。做成包级变量后，测试能在同一台 Linux 上假装自己是 macOS，
// 把那条分支的**形状**锁死——每一行都要出一行、每一行都要带得出一句原因。
var hostOS = runtime.GOOS

// Docker 在 Linux 上写死的几个位置。
// 说清一件事：这些路径**不是 phpo 的产出物**，也不跟着 PHPO_HOME 走，所以不适用总纲 §0.1.1 的 `./` 记法——
// 它们是 Docker 自己的固定落点，写死是准确的，不是偷懒。
const (
	dirContainerd         = "/var/lib/containerd"
	dirEtcDocker          = "/etc/docker"
	dirEtcContainers      = "/etc/containers" // podman / containers-common 的配置目录
	dirCniConf            = "/etc/cni/net.d"
	dirNetns              = "/var/run/netns"
	dirSysCgroup          = "/sys/fs/cgroup"
	dirSysNet             = "/sys/class/net"
	dirVarLog             = "/var/log"
	dirJournal            = "/var/log/journal"
	dirRunJournal         = "/run/log/journal"
	dirEtcGroup           = "/etc/group"
	dirVarCacheContainers = "/var/cache/containers" // rootful 安装下 containers 引擎的系统级缓存目录
	dotDocker             = ".docker"               // 用户主目录下的 Docker 配置目录
)

// 宿主侧进度阶段文案（人话）。服务层用既有 docker:cleanup 事件逐行广播，界面进度条 = 已报到 / 总数，
// 不新增事件名，也不把这次扫描塞进任务队列（扫描期间照常建站、启停服务）。
const (
	StageHostContainers  = "正在统计：容器目录里的配置、日志与检查点"
	StageHostVolumes     = "正在统计：数据卷在磁盘上的目录"
	StageHostPlugins     = "正在统计：插件的配置与数据目录"
	StageHostTmp         = "正在统计：Docker 的临时目录"
	StageHostBuilder     = "正在统计：构建器与运行时留下的目录"
	StageHostTrust       = "正在统计：内容信任与 Swarm 状态目录"
	StageHostUserConfig  = "正在统计：Docker 在你自己账户下的配置目录"
	StageHostContainerd  = "正在统计：containerd 的数据目录"
	StageHostEtcDocker   = "正在统计：Docker 的配置文件目录"
	StageHostCgroup      = "正在统计：容器的资源限制目录"
	StageHostNetDevices  = "正在统计：网络接口（网桥、veth、CNI 网卡）"
	StageHostNetns       = "正在统计：网络命名空间文件"
	StageHostCniConf     = "正在统计：CNI 配置目录"
	StageHostDaemonLog   = "正在统计：Docker 自己的日志文件"
	StageHostJournal     = "正在统计：systemd 日志里属于 Docker 的那一段"
	StageHostDiskRoot    = "正在统计：Docker 数据目录的总占用（这一项跨整个目录，最慢）"
	StageHostIptables    = "正在统计：防火墙里 Docker 写下的规则"
	StageHostGroup       = "正在统计：docker 用户组的成员"
	StageHostEngineCache = "正在统计：容器引擎放在宿主上的结果缓存"
	StageHostElevate     = "正在读取需要管理员授权的目录"
	hostStageCountOffset = 1 // 上面那 19 段之外还要报「提权读取」这一段，进度总数才不致于对不上
)

// 每行清单位于宿主上的哪一类落点。行名（key）与 model.AllCleanRows 那份静态表一字不差。
const (
	rowContainerCheckpoint = "container.checkpoint"
	rowContainerMeta       = "container.meta"
	rowContainerCgroup     = "container.cgroup"
	rowVolumeData          = "volume.data"
	rowVolumeDriver        = "volume.driver"
	rowNetworkBridge       = "network.bridge"
	rowNetworkVeth         = "network.veth"
	rowNetworkNetns        = "network.netns"
	rowNetworkCni          = "network.cni"
	rowNetworkIptables     = "network.iptables"
	rowLogContainer        = "log.container"
	rowLogDaemon           = "log.daemon"
	rowLogJournald         = "log.journald"
	rowLogRotate           = "log.rotate"
	rowLogBuild            = "log.build"
	rowPluginConfig        = "plugin.config"
	rowPluginData          = "plugin.data"
	rowSystemContainerd    = "system.containerd"
	rowSystemTmp           = "system.tmp"
	rowSystemBuilder       = "system.builder"
	rowSystemTrust         = "system.trust"
	rowSystemConfig        = "system.config"
	rowSystemUser          = "system.user"
	rowSystemGroup         = "system.group"
	rowSystemRoot          = "system.root"
	rowOtherEvents         = "other.events"
	rowOtherCache          = "other.cache"
	rowImageSign           = "image.sign"
)

// 界面那三句动态文案。行名与「这一行是什么」的说明在前端 locales（需求 ⑱），
// 这里只放**当场才知道**的状态原因——不然就等于把界面文案搬进后端第二份。
const (
	MsgNoPerm  = "无权读取（需要管理员授权）"
	MsgPartial = "部分目录读不动，数字可能偏小（要完整结果请点该行的重试）"
	MsgVMRoot  = "Docker 的数据目录不在这台机器上（跑在虚拟机里），够不着"
	// MsgNoRoot 得把两种可能都说出来：这一句在 Linux 上也会用到，而 Linux 上引擎同样可能跑在
	// 虚拟机里（Docker Desktop / colima / lima），那时那份数据落在虚拟机的磁盘文件里，宿主这边
	// 确实没有这个目录——只说「这台机器上没有这个目录」会把第二种可能说成「这里本来就是空的」。
	MsgNoRoot = "这台机器上没有这个目录：要么本机确实没建过，要么容器引擎跑在虚拟机里，那份数据在虚拟机的磁盘里，宿主这边够不着"
	MsgCancel = "这次扫描被中断，这一项没数完"

	// 界面这一行的说明：数不到东西时交出去的那句话（需求 ㉘ 不得编造数字）。
	// 前三行要的落点在 Docker 这儿从来不存在，只给人话原因、不给数字；
	// other.cache 不同——它真能数到 Podman 落在宿主上的那一份结果缓存，数到了就报数字，
	// 数不到（引擎是 Docker，或这台机器上没有那几个目录）才报下面这句。
	MsgLogBuild    = "构建日志就放在 buildkit 目录里，那份占用已经算进「构建器与运行时目录」这一行，这里不再重复计一遍"
	MsgOtherEvents = "Docker 的事件只在内存里留一小段时间，默认不落盘成文件，所以这里没有可数的东西"
	MsgOtherCache  = "这一行数的是容器引擎放在宿主上的结果缓存，只有 Podman 有这一份（~/.local/share/containers/cache、~/.cache/containers）；Docker 不落这么一份，引擎是 Docker 或这台机器上没有那两个目录时就数不到东西"
	MsgImageSign   = "镜像签名跟着层文件一起走，Docker 没有单独存放签名的目录；要清掉签名只能删镜像本身"
)

// 一次宿主扫描报出的进度段数（也是界面进度条的分母）。
// 这个数字必须与 ScanHost 实际报出去的次数完全相等，否则进度条永远差一两格。
const hostStagesTotal = 19 + hostStageCountOffset

// hostPathCap 限制一行往外带多少个具体路径。
// 真机上 `/var/lib/docker/volumes` 动辄上千个目录，全铺给确认弹框等于让界面卡住；
// 前 hostPathCap 个足够用户看清「要动的是哪一片」，其余只进数字不进清单。
const hostPathCap = 200

// 容器可写层挂载点。走磁盘时**绝不进这一层**：它是一个活着的联合挂载，
// 进去数一遍等于把镜像层的大小再算一次，「数据卷占用」「容器目录占用」就会一起虚高。
const dirMerged = "merged"

// 认接口名字用得上这两条：CNI 类网卡（minikube / kind / calico 留下的）与滚动过的容器日志文件名。
var (
	cniIfaceRe = regexp.MustCompile(`^(cni|nerdctl|cali|flannel|kind)`)
	// 滚动过的日志一定带序号（1699999999-1.json.log.gz），序号必须在正则里钉死：
	// 写成 `-json\.log(\.\d+)?` 会把当前那份 00e3....json.log 也认成滚动件，
	// 于是「在写的这份有多大」被算进「历史积压」，两行数字一起错。
	logRotateRe  = regexp.MustCompile(`-json\.log\.\d+(\.gz)?$`)
	logPlainRe   = regexp.MustCompile(`-json\.log$`)
	checkpointRe = regexp.MustCompile(`^checkpoint`)
)

// 宿主扫描的每行台账状态。0 值即「一切正常」。
const (
	srcStateOK        = 0
	srcStateDenied    = 1 // 有目录读不动，已经试过授权或还没试过
	srcStateAbsent    = 2 // 根目录压根不在盘上（虚拟机里的路径、这台机器没装这个组件）
	srcStateNoRootDep = 3 // Docker 这次没答出数据目录在哪，凡是挂在它下面的项都无从数起
	srcStateCancelled = 4
)

// DaemonInfo 是 Docker 接口对自己那台宿主的一句话总结：数据目录在哪、用的是哪种 cgroup、是不是 rootless。
//
// OK 为假时 Reason 必须给得出人话原因——那决定了下面十几行是显示数字还是显示「取不到」，不能含糊。
type DaemonInfo struct {
	Root         string // DockerRootDir，容器/卷/插件/临时目录都在它下面
	CgroupDriver string // cgroupfs / systemd：决定去 /sys/fs/cgroup 的哪一支找容器目录
	Rootless     bool   // rootless 模式：整片目录都在用户自己家里，不需要提权
	OK           bool
	Reason       string
}

// DockerInfo 问一次 Docker：你的数据目录在哪。
//
// 这一步不猜默认值。Docker 没答上来就返回 OK=false + 原因，让宿主那十几行显示「取不到」，
// 而不是回落到 /var/lib/docker 然后报出一个「0」——那是凭空造出来的数字。
func (c *Client) DockerInfo(ctx context.Context) DaemonInfo {
	inf, err := c.cli.Info(ctx)
	if err != nil {
		return DaemonInfo{OK: false, Reason: fmt.Sprintf("没能问出 Docker 的数据目录在哪: %v", err)}
	}
	root := filepath.Clean(inf.DockerRootDir)
	if root == "." || root == "" {
		return DaemonInfo{OK: false, Reason: "Docker 回的数据目录是一个空值，宿主侧那几行无从数起"}
	}
	rootless := false
	for _, s := range inf.SecurityOptions {
		if strings.Contains(s, "name=rootless") {
			rootless = true
			break
		}
	}
	return DaemonInfo{
		Root:         root,
		CgroupDriver: inf.CgroupDriver,
		Rootless:     rootless,
		OK:           true,
	}
}

// HostRow 是宿主侧对某一行清单的回答：数字、体积、具体落在哪些路径上、以及这次到底读没读到。
//
// Paths 与 Aggregate 只在本层与服务层之间流动，不进前后端载荷（model.CleanRow 没有这两样）。
type HostRow struct {
	Key       string
	Status    model.RowStatus
	Count     int
	Bytes     int64
	HasBytes  bool
	Message   string
	Paths     []string
	Aggregate bool // 真表示「这一行是把别处已经数过的加总起来」，服务层算总计体积时必须跳过它，否则双倍
	ScannedAt time.Time
}

// HostScanResult 是一次完整宿主扫描（点「详细扫描」那颗按钮）的结果。
type HostScanResult struct {
	Rows        []HostRow
	DeepScanned bool // 是否真做过提权读取；没做过就不能对用户说「这是完整清单」
	Elevated    bool
	ScannedAt   time.Time
	Warnings    []string
}

// HostScanPlan 是扫描开始前就能算出来的计划，只为一件事：让界面进度条的分母准确。
//
// Supported=false 时服务层不该把「详细扫描」做成可点，因为点了也不会多读到任何东西。
type HostScanPlan struct {
	Sources   []string // 与 ScanHost 逐段报出去的顺序一致
	Stages    int
	Supported bool
}

// HostPathFact 是「这个路径现在到底是什么情形」的一组事实，供服务层判「算不算遗留残骸」。
//
// 用途很窄但很关键：绑定挂载遗留目录这一行要回答「这个宿主路径还在不在、是不是空的、
// 是不是正被挂载着」，正在用的不能算残留、更不能删。
type HostPathFact struct {
	Path     string
	Exists   bool
	Empty    bool
	IsMount  bool
	Readable bool
	Error    string
}

// hostSource 说清「要数哪一行，就去哪个目录走一趟」。
//
// 这张表是 PlanHostScan 与 ScanHost **共用**的唯一来源：进度段数、要走的目录、每行落在哪个目录，
// 全部只在这里写一遍。分两处写迟早漂移成「进度条说 19 段、实际报出 18 段」。
type hostSource struct {
	id        string
	stage     string
	roots     []string
	maxDepth  int    // 相对根目录的深度上限，0 表示不限（伪文件系统要设上限，不然整片扫完没意义）
	probe     string // 非空表示这一项不走磁盘而是问命令：iptables / group
	rootDep   bool   // 真表示路径挂在 Docker 报回的数据目录下面
	linuxOnly bool
	absentIsZ bool // 目录不在即「真的是 0」（journald 没落盘就是 0），而不是「够不着」
	rows      []string
}

// hostSources 列出宿主侧全部 19 个统计段。info.Root 为空时 rootDep 那几条的根就是空串，
// 由 walkSource 统一判成「无从数起」，不在这里做分支。
func hostSources(info DaemonInfo) []hostSource {
	root := info.Root
	home, homeErr := os.UserHomeDir()
	var userDir, podmanUserDir string
	var engineCacheRoots []string
	if homeErr == nil && home != "" {
		userDir = filepath.Join(home, dotDocker)
		podmanUserDir = filepath.Join(home, ".config", "containers") // podman rootless 的用户配置（containers.conf 等）
		engineCacheRoots = []string{
			filepath.Join(home, ".local", "share", "containers", "cache"), // podman rootless 的 blob 信息缓存
			filepath.Join(home, ".cache", "containers"),                   // 短名别名表那类用户级缓存
		}
	}
	engineCacheRoots = dedupe(append(engineCacheRoots, dirVarCacheContainers)) // rootful 安装的系统级缓存
	return []hostSource{
		// 每个容器在宿主上的「档案」目录。两个引擎放的地方不一样：Docker 是 <root>/containers/<id>/，
		// Podman 是 <root>/overlay-containers/<id>/userdata/。两个都认才数得全，缺哪个另一个就整行报 0。
		{id: "containers", stage: StageHostContainers,
			roots:   dedupe([]string{joinRoot(root, "containers"), joinRoot(root, "overlay-containers")}),
			rootDep: true, rows: []string{rowContainerCheckpoint, rowContainerMeta, rowLogContainer, rowLogRotate}},

		{id: "volumes", stage: StageHostVolumes, roots: []string{joinRoot(root, "volumes")},
			rootDep: true, rows: []string{rowVolumeData, rowVolumeDriver}},

		// 数据目录总量：整个 Docker 数据目录 + containerd 的目录加在一起。
		// 这一行**刻意与上面逐行的数字重叠**（所以标 Aggregate，服务层算总计时必须排除）。
		// 重叠不合并是因为「哪部分该归哪一行」一旦靠猜就会数错，多走一遍磁盘只多花时间——需求 ㉓ 明确不设时限。
		{id: "disk-root", stage: StageHostDiskRoot, roots: dedupe([]string{root, dirContainerd}),
			rootDep: true, rows: []string{rowSystemRoot}},

		{id: "plugins", stage: StageHostPlugins, roots: []string{joinRoot(root, "plugins")},
			rootDep: true, rows: []string{rowPluginConfig, rowPluginData}},

		// 引擎的临时/运行时目录。三个根按引擎分：Docker rootful 是 <数据目录>/tmp；
		// rootless Podman 把自己此刻正在用的那一片放在 <运行时目录>/libpod/tmp（rootlessport 的
		// 套接字与就绪管道、pause.pid、exits/、persist/）与 <运行时目录>/containers（overlay 与
		// overlay-containers 的挂载点树、netavark 的运行期文件）；这两处以前一个都没认，
		// 于是这一行在 rootless Podman 机器上永远报「这台机器上没有这个目录」。
		// 活对象不删：落在用户运行时目录底下的候选项在删除那侧要过 userRuntimeGuard，
		// 挂载点、套接字、命名管道照常计数，但动不得（见 clean_hostdel.go 的 checkHostPath）。
		{id: "tmp", stage: StageHostTmp,
			roots:   dedupe([]string{joinRoot(root, "tmp"), runUserDir("libpod/tmp"), runUserDir("containers")}),
			rootDep: true, rows: []string{rowSystemTmp}},

		// 构建器与运行时留下的目录。<运行时目录>/docker 是 rootless dockerd 的那一份，
		// 与上面写死的 /run/docker（rootful 那一份）是同一件事的两种落点，缺一个就有一种引擎数不到。
		{id: "builder", stage: StageHostBuilder,
			roots:   dedupe([]string{joinRoot(root, "buildkit"), "/run/docker", runUserDir("docker")}),
			rootDep: true, rows: []string{rowSystemBuilder}},

		{id: "trust", stage: StageHostTrust,
			roots: dedupe([]string{joinRoot(userDir, "trust"), joinRoot(root, "trust"), joinRoot(root, "swarm")}),
			rows:  []string{rowSystemTrust}},

		// 用户自己的 Docker 配置目录：~/.docker 里除了 trust 那一片（上面单独数）都算这一行。
		// 注意它**不是** linuxOnly：macOS / Windows 上这个目录就在宿主上，读得到，不该跟着一起说「够不着」。
		{id: "userconfig", stage: StageHostUserConfig, roots: dedupe([]string{userDir, podmanUserDir}),
			rows: []string{rowSystemUser}},

		// 容器引擎放在宿主上的结果缓存：只有 Podman 有这一份（blob 信息缓存 sqlite、短名别名表），
		// Docker 不落这么独立的一处。三个根都缺席时这一行不消失——由 rowFor 退回那句人话。
		// 只数不删：这一行是第三档（TierInfo），界面不给按钮。
		{id: "engine-cache", stage: StageHostEngineCache, roots: engineCacheRoots,
			rows: []string{rowOtherCache}},

		{id: "containerd", stage: StageHostContainerd, roots: []string{dirContainerd},
			linuxOnly: true, rows: []string{rowSystemContainerd}},

		// /etc/containers 是 podman 的配置目录（containers.conf、storage.conf、netavark 网络配置）：
		// 引擎是 podman 时这一行必须数得到它，否则配置占用永远报「这台机器上没有这个目录」。
		// 两个根都缺席 → 走 walkSource 的 MsgNoRoot，语义不变。
		{id: "etc-docker", stage: StageHostEtcDocker, roots: dedupe([]string{dirEtcDocker, dirEtcContainers}),
			linuxOnly: true, rows: []string{rowSystemConfig}},

		// 容器的资源限制目录。深度设 4 层是实测够用：cgroupfs 在 /sys/fs/cgroup/<子系统>/docker/<id>，
		// systemd 驱动在 /sys/fs/cgroup/system.slice/docker-<id>.scope。伪文件系统的 size 一律 0，
		// 所以这一行只报「多少个」，不报体积（报 0 B 会让人以为它不占地方）。
		{id: "cgroup", stage: StageHostCgroup, roots: []string{dirSysCgroup}, maxDepth: 4,
			linuxOnly: true, rows: []string{rowContainerCgroup}},

		// 网络接口：/sys/class/net 下每个目录是一张网卡。有 bridge 子目录的就是网桥，
		// 名字以 veth 开头的是容器配对网卡，名字像 cni/nerdctl/cali/flannel/kind 的是 CNI 那套留下的。
		{id: "net-devices", stage: StageHostNetDevices, roots: []string{dirSysNet}, maxDepth: 2,
			linuxOnly: true, rows: []string{rowNetworkBridge, rowNetworkVeth, rowNetworkCni}},

		{id: "netns", stage: StageHostNetns,
			roots:     dedupe([]string{dirNetns, runUserNetns()}),
			linuxOnly: true, rows: []string{rowNetworkNetns}},

		// CNI 的配置目录，加上 Podman 那一套等价物：<root>/networks（netavark 的网络定义）。
		// 引擎是 rootless Podman 时 /etc/cni/net.d 常常根本不存在，只认它就等于这一行永远数不到。
		{id: "cni-conf", stage: StageHostCniConf,
			roots:     dedupe([]string{dirCniConf, joinRoot(root, "networks")}),
			linuxOnly: true, rows: []string{rowNetworkCni}},

		{id: "daemon-log", stage: StageHostDaemonLog, roots: []string{dirVarLog}, maxDepth: 1,
			linuxOnly: true, rows: []string{rowLogDaemon}},

		// systemd 日志里属于 Docker 的那一段。整目录不在 = journald 没往磁盘写，那是真 0（absentIsZ），
		// 不是「够不着」；在目录但读不动 = 要授权。这两种必须分开，否则用户看不出区别。
		{id: "journal", stage: StageHostJournal, roots: []string{dirJournal, dirRunJournal},
			linuxOnly: true, absentIsZ: true, rows: []string{rowLogJournald}},

		{id: "iptables", stage: StageHostIptables, probe: "iptables",
			linuxOnly: true, rows: []string{rowNetworkIptables}},

		{id: "group", stage: StageHostGroup, probe: "group",
			linuxOnly: true, rows: []string{rowSystemGroup}},
	}
}

// HostRowKeys 报出宿主侧统计段覆盖的全部行名，供服务层对账（两处表漂开会症状很难看：
// 某行扫出了数却永不深扫，或某行被当成宿主行去白要一次授权）。集合语义，比对不看顺序。
func HostRowKeys() []string {
	var out []string
	for _, s := range hostSources(DaemonInfo{Root: "/var/lib/docker"}) {
		out = append(out, s.rows...)
	}
	for _, r := range reasonRows() { // 那几行不落磁盘、只给人话原因，但它同样是宿主扫描交回来的行
		out = append(out, r.Key)
	}
	return dedupe(out)
}

// PlanHostScan 在动手之前把「会报几段进度、走哪些目录」说出来，供界面画准确的进度条（需求 ㉓/㉔）。
func PlanHostScan(info DaemonInfo) HostScanPlan {
	srcs := hostSources(info)
	plan := HostScanPlan{Stages: len(srcs) + hostStageCountOffset, Supported: hostOS == "linux"}
	for _, s := range srcs {
		plan.Sources = append(plan.Sources, s.stage)
	}
	plan.Sources = append(plan.Sources, StageHostElevate)
	return plan
}

// hostEntry 一条磁盘上的条目。rel 是相对本次根目录的路径，分类只看它。
type hostEntry struct {
	src   string
	path  string
	rel   string
	isDir bool
	size  int64
}

// volBucket 一个卷目录的账：有没有 _data、_data 里多大、除此之外多大。
// 分这两半是因为界面那两行问的正是这个：有数据的卷 / 只有驱动自己写的目录。
type volBucket struct {
	hasData  bool
	dataPath string
	data     int64
	other    int64
}

// ifaceBucket 一张网卡的账。
type ifaceBucket struct {
	isBridge bool
	isVeth   bool
	isCNI    bool
}

// rowAcc 一行清单在扫描过程中的累计量。seen 用来把「文件数」收成「个数」
// （一个容器有十几个文件，界面那一行要的是容器数，不是文件数）。
type rowAcc struct {
	count int
	bytes int64
	paths []string
	seen  map[string]struct{}
	added map[string]struct{} // 同一个路径只加一次体积：提权那一路可能重复报同一条
}

func (a *rowAcc) path(p string) {
	if len(a.paths) >= hostPathCap {
		return
	}
	a.paths = append(a.paths, p)
}

// track 第一次见到这个键才计数。
func (a *rowAcc) track(key string) bool {
	if a.seen == nil {
		a.seen = map[string]struct{}{}
	}
	if _, ok := a.seen[key]; ok {
		return false
	}
	a.seen[key] = struct{}{}
	return true
}

// add 记一笔体积。同一个 path 重复出现（Phase A 读过、提权又报一遍）只算一次。
func (a *rowAcc) add(e hostEntry) {
	if e.isDir {
		return // 目录项那 4 KB 不是占用，只算文件
	}
	if e.size <= 0 {
		return
	}
	if a.added == nil {
		a.added = map[string]struct{}{}
	}
	if _, ok := a.added[e.path]; ok {
		return
	}
	a.added[e.path] = struct{}{}
	a.bytes += e.size
}

// hostScan 是一次扫描的全部中间状态。
type hostScan struct {
	acc     map[string]*rowAcc
	state   map[string]int // source id → srcState*
	reason  map[string]string
	vols    map[string]*volBucket
	ifaces  map[string]*ifaceBucket
	now     time.Time
	warn    []string
	elevate bool // 真做过一次提权读取
}

func newHostScan() *hostScan {
	return &hostScan{
		acc:    map[string]*rowAcc{},
		state:  map[string]int{},
		reason: map[string]string{},
		vols:   map[string]*volBucket{},
		ifaces: map[string]*ifaceBucket{},
		now:    time.Now(),
	}
}

func (h *hostScan) row(key string) *rowAcc {
	a := h.acc[key]
	if a == nil {
		a = &rowAcc{}
		h.acc[key] = a
	}
	return a
}

// mark 记下这个统计段的结局。已经定论的两种结局不被后来的「正常」覆盖：
//   - 「读不动」→「正常」：半份数字加一句「可能偏小」比一句「齐了」诚实；
//   - 「已取消」→「正常」：走目录时上下文已经取消，后面那段收尾的「正常」是路过写的，
//     覆盖上去就等于把「这次没扫完」说成「扫完了」——界面会拿出一个假的完整数字。
//
// 唯一有权把「读不动」翻成「正常」的是 Phase B 的提权补读，它绕过本函数直接写状态（见 elevateDenied）。
func (h *hostScan) mark(src string, state int, reason string) {
	prev := h.state[src]
	if state == srcStateOK && (prev == srcStateDenied || prev == srcStateCancelled) {
		return
	}
	h.state[src] = state
	if reason != "" {
		h.reason[src] = reason
	}
}

// byteRows 说清哪些行的数字是「磁盘占用」。
// 只有这类行才在没有读到任何东西时报「无权读取」而不是「—」；网卡、cgroup、防火墙规则那几行本来就没有体积。
var byteRows = map[string]bool{
	rowContainerCheckpoint: true, rowContainerMeta: true,
	rowVolumeData: true, rowVolumeDriver: true,
	rowPluginConfig: true, rowPluginData: true,
	rowLogContainer: true, rowLogDaemon: true, rowLogJournald: true, rowLogRotate: true,
	rowSystemContainerd: true, rowSystemTmp: true, rowSystemBuilder: true,
	rowSystemTrust: true, rowSystemConfig: true, rowSystemUser: true, rowSystemRoot: true,
	rowOtherCache: true,
}

// aggregateRows 标出「加总行」：服务层算总占用时必须排除，否则与逐行数字双倍。
var aggregateRows = map[string]bool{rowSystemRoot: true}

// hostRowSources 反过来查「这一行的数字是从哪几个统计段来的」，供单行重试只用重扫那几段。
// 也从 hostSources 一次生成，不在第二处维护。
var hostRowSources = func() map[string][]string {
	m := map[string][]string{}
	for _, s := range hostSources(DaemonInfo{}) {
		for _, r := range s.rows {
			m[r] = append(m[r], s.id)
		}
	}
	return m
}()

// ScanHost 走一遍宿主：先把免授权能读的全读出来，再只对着「读不动」的那几个目录问一次管理员授权。
//
// 两段式的取舍：一上来就弹授权框太打扰（多数机器上 Docker 的数据目录 711 但里面的子目录可读）；
// 完全不提权又会让几行永远显示「无权读取」。所以先自己读、读到拒绝为止，再提权补那一小块。
//
// 非 Linux 平台不再整片早退。以前这里一进门就 return，只交出「Linux 才有落点」的那几行，
// 于是数据目录底下那十几行连「为什么是 —」都没送出去——把「没查」和「查了这里没有」画成同一个样子，
// 规则 41 不收这种形状。现在按段分岔：Linux 才有的段落逐段标够不着（原因照给），
// 宿主上本来就读得到的段落（~/.docker、引擎放在宿主上的结果缓存）照常数，
// 数据目录那几段走磁盘时自然落到「这台机器上没有这个目录」那句原因上。
func (c *Client) ScanHost(ctx context.Context, info DaemonInfo, onStage StageFn) (HostScanResult, error) {
	h := newHostScan()
	scanHostSources(ctx, h, info, nil, onStage)
	res := HostScanResult{
		Rows:        h.finalise(info),
		DeepScanned: h.elevate,
		Elevated:    h.elevate,
		ScannedAt:   h.now,
		Warnings:    h.warn,
	}
	if hostOS != "linux" {
		res.Warnings = append([]string{MsgVMRoot}, res.Warnings...)
		return res, fmt.Errorf("%w: %s", ErrHostNotSupported, MsgVMRoot)
	}
	return res, nil
}

// RescanHostRow 只重扫某一行需要的那几个目录——界面上「无权读取」那一行给的重试入口走这里。
//
// 为什么不整片重扫：用户点的是这一行的重试，弹一次授权框只为补这一行，其余已扫到的数字保持不动。
func (c *Client) RescanHostRow(ctx context.Context, key string, info DaemonInfo) HostRow {
	srcs, ok := hostRowSources[key]
	if !ok {
		return HostRow{Key: key, Status: model.RowUnavailable, Message: "这一行不在宿主采集范围内", ScannedAt: time.Now()}
	}
	want := map[string]bool{}
	for _, id := range srcs {
		want[id] = true
	}
	h := newHostScan()
	scanHostSources(ctx, h, info, want, nil)
	rows := h.finalise(info)
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	if m := reasonRow(key); m.Message != "" {
		return m
	}
	return HostRow{Key: key, Status: model.RowUnavailable, Message: "这一行不在宿主采集范围内", ScannedAt: h.now}
}

// scanHostSources 逐段执行。want 为 nil 表示全部；进度段一律照报（跳过的也报），
// 否则界面那个「已报到 / 总数」会永远差几格。
func scanHostSources(ctx context.Context, h *hostScan, info DaemonInfo, want map[string]bool, onStage StageFn) {
	srcs := hostSources(info)
	var pending []hostSource
	for _, s := range srcs {
		onStage.report(s.stage)
		if want != nil && !want[s.id] {
			continue
		}
		if s.rootDep && !info.OK {
			h.mark(s.id, srcStateNoRootDep, info.Reason)
			continue
		}
		if hostOS != "linux" && s.linuxOnly {
			h.mark(s.id, srcStateAbsent, MsgVMRoot)
			continue
		}
		if err := ctx.Err(); err != nil {
			h.mark(s.id, srcStateCancelled, MsgCancel)
			h.warn = append(h.warn, fmt.Sprintf("%s 中断：%v", s.stage, err))
			continue
		}
		if s.probe != "" {
			h.runProbe(ctx, s)
			continue
		}
		pending = append(pending, s)
		h.walkSource(ctx, s)
	}
	onStage.report(StageHostElevate)
	// 非 Linux 不提权：宿主上读不动的那一小块留给用户自己处理，
	// 这一趟的产出是「每一行都有数、都有原因」，不是「多弹一次授权框」。
	if hostOS == "linux" && (want == nil || len(pending) > 0) {
		h.elevateDenied(ctx, pending)
	}
}

// walkSource 走一趟某个统计段（Phase A：不提权）。
//
// 每个根目录独立判定在不在盘上：全都不在 → absent（够不着）；在但读不动 → denied（要授权）。
// 这两种给界面的话完全不同，混成一种用户就不知道该做什么。
func (h *hostScan) walkSource(ctx context.Context, s hostSource) {
	anyRoot := false
	for _, root := range s.roots {
		if root == "" {
			continue
		}
		if _, err := os.Lstat(root); err != nil {
			continue
		}
		anyRoot = true
		h.walkDir(ctx, s, root)
	}
	if anyRoot {
		h.mark(s.id, srcStateOK, "")
		return
	}
	if s.absentIsZ {
		// 目录不在就是「这一项真的是 0」：journald 没往磁盘写日志，是查得出来的事实，不是没查到。
		h.mark(s.id, srcStateOK, "")
		return
	}
	h.mark(s.id, srcStateAbsent, MsgNoRoot)
}

func (h *hostScan) walkDir(ctx context.Context, s hostSource, root string) {
	root = filepath.Clean(root)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if cErr := ctx.Err(); cErr != nil {
			h.mark(s.id, srcStateCancelled, MsgCancel)
			return cErr
		}
		if err != nil {
			// 单个条目读不动（权限、或目录本身打不开）只记下「要授权」，不能让整段扫描停下。
			if errors.Is(err, fs.ErrPermission) {
				h.mark(s.id, srcStateDenied, MsgNoPerm)
			}
			return nil
		}
		rel, rErr := filepath.Rel(root, p)
		if rErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(rel), "/")
		if s.maxDepth > 0 && depth >= s.maxDepth && d.IsDir() {
			return fs.SkipDir // 到深度上限的目录不再往下走
		}
		if d.IsDir() {
			if d.Name() == dirMerged {
				return fs.SkipDir // 活着的联合挂载，进去等于把镜像层再数一遍
			}
			if s.id == "journal" {
				// journald 的目录树很深且全是二进制段文件，深度设 6 层够覆盖机器 ID 那几层。
				if depth >= 6 {
					return fs.SkipDir
				}
			}
		}
		var size int64
		if !d.IsDir() {
			if fi, iErr := d.Info(); iErr == nil {
				size = fi.Size()
			}
		}
		h.classify(hostEntry{src: s.id, path: p, rel: filepath.ToSlash(rel), isDir: d.IsDir(), size: size})
		return nil
	})
}

// classify 把一条磁盘条目记到它对应的那一行。
//
// 一条铁律：**同一个统计段里，每个条目只落一行**。落两行等于总占用双倍，
// 而总占用是用户决定「要不要清磁盘」的唯一依据。跨段的重复只允许出现在标了 Aggregate 的那一行。
func (h *hostScan) classify(e hostEntry) {
	parts := strings.Split(e.rel, "/")
	name := parts[len(parts)-1]

	switch e.src {
	case "containers":
		h.entryContainers(e, parts)
	case "volumes":
		h.entryVolumes(e, parts)
	case "disk-root":
		a := h.row(rowSystemRoot)
		if e.isDir {
			// 目录只带「这一片在哪」：只记根下面第一层那几个目录名，深一层的路径对用户没意义，
			// 还会把 hostPathCap 的额度提前占满。
			if len(parts) == 1 {
				a.path(e.path)
			}
			return
		}
		a.count++
		a.add(e)
	case "plugins":
		h.entryPlugins(e, parts)
	// 这四行往清单里只收**根下第一层那一片**（tmp/、buildkit/、trust/、containerd 的 content/ 与快照目录）。
	// 两个理由：① 逐层往下的碎片列出来既没法逐个动手（要删的是它整片），又会瞬间占满 hostPathCap，
	// 让界面上「能勾的对象」只剩目录树最前面那一小截；② 服务层删的就是这些顶层条目本身。
	// 去重用 track：提权补读那一路会把同一个顶层条目再报一遍（Phase A 只读到目录名、没读到内容的时候）。
	case "tmp":
		a := h.row(rowSystemTmp)
		a.add(e)
		if !e.isDir {
			a.count++
		}
		if len(parts) == 1 && a.track(e.path) {
			a.path(e.path)
		}
	case "builder":
		a := h.row(rowSystemBuilder)
		a.add(e)
		if !e.isDir {
			a.count++
		}
		if len(parts) == 1 && a.track(e.path) {
			a.path(e.path)
		}
	case "trust":
		a := h.row(rowSystemTrust)
		a.add(e)
		if !e.isDir {
			a.count++
		}
		if len(parts) == 1 && a.track(e.path) {
			a.path(e.path)
		}
	case "userconfig":
		// trust 那一片由 system.trust 那一行负责，这里跳过，别让同一份文件进两行。
		if strings.HasPrefix(e.rel, "trust/") || name == "trust" {
			return
		}
		a := h.row(rowSystemUser)
		a.add(e)
		if !e.isDir {
			a.count++
			a.path(e.path)
		}
	case "engine-cache":
		// 只数不删：这一行是第三档（没有安全删法，界面不给按钮），数的是引擎自己会重建的缓存文件。
		if !e.isDir {
			a := h.row(rowOtherCache)
			a.add(e)
			a.count++
			a.path(e.path)
		}
	case "containerd":
		a := h.row(rowSystemContainerd)
		a.add(e)
		if !e.isDir {
			a.count++
		}
		if len(parts) == 1 && a.track(e.path) {
			a.path(e.path)
		}
	case "etc-docker":
		a := h.row(rowSystemConfig)
		a.add(e)
		if !e.isDir {
			a.count++
			a.path(e.path)
		}
	case "cgroup":
		h.entryCgroup(e)
	case "net-devices":
		h.entryNetDevices(e, parts)
	case "netns":
		a := h.row(rowNetworkNetns)
		if a.track(name) {
			a.count++
			a.path(e.path)
		}
	case "cni-conf":
		a := h.row(rowNetworkCni)
		if !e.isDir {
			a.add(e)
			a.count++
			a.path(e.path)
		}
	case "daemon-log":
		if !e.isDir && strings.HasPrefix(name, "docker") && strings.Contains(name, ".log") {
			a := h.row(rowLogDaemon)
			a.add(e)
			a.count++
			a.path(e.path)
		}
	case "journal":
		if !e.isDir && strings.HasSuffix(name, ".journal") {
			a := h.row(rowLogJournald)
			a.add(e)
			a.count++
			// 清单收的是**一份份 .journal 文件**而不是 <机器 ID> 那层目录：
			// 删目录等于把整机所有进程的 systemd 日志一起删掉，而这一行问的只是 Docker 那一段。
			if a.track(e.path) {
				a.path(e.path)
			}
		}
	}
}

// entryContainers 拆 <数据目录>/containers/<容器 ID>/ 下面那一片。
//
// 同一个容器目录里可能同时有配置、日志、滚动日志、检查点，所以按文件名分派；
// 计数一律按「几个容器」而不是「几个文件」——界面那一行问的是前者。
func (h *hostScan) entryContainers(e hostEntry, parts []string) {
	if len(parts) < 2 {
		return // 容器目录本身不落任何一行
	}
	// Podman 比 Docker 多一层：容器档案放在 overlay-containers/<id>/userdata/ 底下。
	// 剥掉 userdata 这一层，下面这套「按容器目录里的顶层文件分类」的口径两个引擎就能共用一份，
	// 不必为 Podman 另立一张表（两张表迟早对不上，症状是同一个容器被数两次或一次都不数）。
	if parts[1] == "userdata" {
		if len(parts) < 3 {
			return // userdata 目录本身不是任何一行
		}
		parts = append([]string{parts[0]}, parts[2:]...)
	}
	id := parts[0]
	// 检查点目录名以 checkpoint 开头（docker checkpoint 写出来的就是这个名字）。
	// 体积先不管后面怎么计数就先加：检查点本身是个目录，add 会跳过它自己、
	// 只收它下面的文件；若是空检查点目录，这一行就是「有 1 个、0 字节」——那是查出来的事实。
	for i, seg := range parts[1:] {
		if !checkpointRe.MatchString(seg) {
			continue
		}
		a := h.row(rowContainerCheckpoint)
		a.add(e)
		// 计数与清单只认检查点目录那一层（parts[1]），它下面的文件不再各算一次。
		if i == 0 && len(parts) == 2 {
			if a.track(id) {
				a.count++
			}
			a.path(e.path)
		}
		return
	}
	if len(parts) != 2 || e.isDir {
		return
	}
	name := parts[1]
	switch {
	case logRotateRe.MatchString(name):
		a := h.row(rowLogRotate)
		a.add(e)
		a.count++
		a.path(e.path)
	case logPlainRe.MatchString(name):
		a := h.row(rowLogContainer)
		a.add(e)
		a.count++
		a.path(e.path)
	default:
		// 其余顶层文件（config.v2.json、hostconfig.json、hostname、hosts、link、resolved…）
		// 就是这个容器在宿主上的「档案」，即 container.meta 那一行。
		a := h.row(rowContainerMeta)
		if a.track(id) {
			a.count++
			a.path(filepath.Dir(e.path))
		}
		a.add(e)
	}
}

// entryVolumes 把 <数据目录>/volumes/<驱动-卷名>/ 分成「有 _data」与「只有驱动写的目录」两堆。
// 后者在默认 local 驱动下通常是空的——那一行显示 0 是查出来的事实，不是没查到。
func (h *hostScan) entryVolumes(e hostEntry, parts []string) {
	key := parts[0]
	if key == "" {
		return
	}
	// 卷目录本身（volumes/<名字>）与根下那份 metadata.db 都不属于任何一卷的占用，先拦掉；
	// 放在建桶之前，免得给 metadata.db 凭空开一个卷。
	if len(parts) == 1 {
		return
	}
	b := h.vols[key]
	if b == nil {
		b = &volBucket{}
		h.vols[key] = b
	}
	if parts[1] == "_data" {
		if !b.hasData {
			b.hasData = true
			b.dataPath = filepath.Join(filepath.Dir(e.path), "_data")
			if len(parts) == 2 {
				return // _data 目录本身不是占用
			}
		}
		if !e.isDir {
			b.data += e.size
		}
		return
	}
	if !e.isDir {
		b.other += e.size
	}
}

func (h *hostScan) entryPlugins(e hostEntry, parts []string) {
	if len(parts) < 2 {
		return
	}
	id := parts[0]
	kind := rowPluginConfig
	if parts[1] == "data" || parts[1] == "privatedata" || parts[1] == "state" {
		kind = rowPluginData
	}
	a := h.row(kind)
	a.add(e)
	if a.track(id) {
		a.count++
		a.path(filepath.Join(filepath.Dir(filepath.Dir(e.path)), id))
	}
}

// entryCgroup 只认路径里带 docker 那几支：那是容器资源限制目录的命名空间，其余是整机其他进程的。
// 比对用 e.rel（相对 cgroup 挂载点），因为容器那一片在根下就叫 docker/<容器 ID>/...，
// 拿文件名比对等于永远看不见它。伪文件系统 size 一律 0，所以这一行只计数、不报体积。
func (h *hostScan) entryCgroup(e hostEntry) {
	if !strings.Contains(e.rel, "docker") {
		return
	}
	a := h.row(rowContainerCgroup)
	if a.track(e.path) {
		a.count++
		a.path(e.path)
	}
}

func (h *hostScan) entryNetDevices(e hostEntry, parts []string) {
	if len(parts) == 0 || parts[0] == "" {
		return
	}
	name := parts[0]
	b := h.ifaces[name]
	if b == nil {
		b = &ifaceBucket{isVeth: strings.HasPrefix(name, "veth"), isCNI: cniIfaceRe.MatchString(name)}
		h.ifaces[name] = b
	}
	if len(parts) == 2 && parts[1] == "bridge" && e.isDir {
		b.isBridge = true
	}
}

// runProbe 跑两个「问命令而不是走目录」的统计段：防火墙规则与 docker 用户组。
//
// 命令一律以 argv 传入、不经 shell（同 §5.16 的口径），参数里没有用户可控内容。
func (h *hostScan) runProbe(ctx context.Context, s hostSource) {
	switch s.probe {
	case "iptables":
		h.probeIptables(ctx)
	case "group":
		h.probeGroup(ctx)
	}
}

// probeCommand 跑一条只读的探测命令（argv 传入，不经 shell）。
//
// 测试注入点：单测不能真去跑 iptables-save。找不着命令必须把 exec.ErrNotFound 原样透出去——
// 调用方靠这个错误判定「本机没这个工具」，包一层 fmt.Errorf 就没了。
var probeCommand = func(ctx context.Context, name string) (string, error) {
	exe, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, exe).Output()
	if err != nil && len(out) == 0 {
		return "", err
	}
	return string(out), nil
}

func (h *hostScan) probeIptables(ctx context.Context) {
	total := 0
	seenCmd := 0
	var fails []string
	chains := map[string]struct{}{}
	for _, exe := range []string{"iptables-save", "ip6tables-save"} {
		out, err := probeCommand(ctx, exe)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				fails = append(fails, exe+" 本机没有")
				continue
			}
			fails = append(fails, fmt.Sprintf("%s: %v", exe, err))
			continue
		}
		seenCmd++
		total += countDockerRules(out, chains)
	}
	if seenCmd == 0 {
		h.mark("iptables", srcStateAbsent, "本机没有 iptables-save，无法统计防火墙规则（"+strings.Join(fails, "；")+"）")
		return
	}
	a := h.row(rowNetworkIptables)
	a.count = total
	for c := range chains {
		a.path(c)
	}
	h.mark("iptables", srcStateOK, "")
	if len(fails) > 0 {
		h.warn = append(h.warn, "防火墙规则只统计了能跑的那一半："+strings.Join(fails, "；"))
	}
}

// countDockerRules 数出 Docker 写进宿主防火墙的那几条规则。
// 认链名以 DOCKER 开头（DOCKER / DOCKER-USER）或含 KUBE-（Kubernetes 那套留下的）。
func countDockerRules(out string, chains map[string]struct{}) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "-A ") {
			continue
		}
		field := strings.Fields(line)
		if len(field) < 2 {
			continue
		}
		chain := field[1]
		if !strings.HasPrefix(chain, "DOCKER") && !strings.Contains(chain, "KUBE-") {
			continue
		}
		n++
		chains[chain] = struct{}{}
	}
	return n
}

func (h *hostScan) probeGroup(ctx context.Context) {
	_ = ctx
	data, err := os.ReadFile(dirEtcGroup)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			h.mark("group", srcStateDenied, MsgNoPerm)
		} else {
			h.mark("group", srcStateAbsent, fmt.Sprintf("读不到 %s: %v", dirEtcGroup, err))
		}
		return
	}
	h.mark("group", srcStateOK, "")
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "docker:") {
			continue
		}
		field := strings.Split(line, ":")
		if len(field) < 4 {
			return
		}
		for _, u := range strings.Split(field[3], ",") {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			a := h.row(rowSystemGroup)
			if a.track(u) {
				a.count++
				a.path(u)
			}
		}
		return
	}
	// 没有 docker 这一行：这台机器上没有 docker 用户组。这是查出来的事实，报 0 是对的。
	// 这次 h.row 只为把这一行的账本建出来——finalise 按键集收行，不建就整行消失而不是显示 0。
	_ = h.row(rowSystemGroup)
}

// elevateDenied 只对「Phase A 读不动」的统计段提权补读（Phase B）。
//
// 一次「详细扫描」只问一次管理员授权：先把所有读不动那几段的目录并成一份清单，
// 用一次 find 全部读回来，再逐段把这份结果分回去。
// 能这么合是因为 find 本来就接受多个目录，而每一段只认真正落在自己目录下的那些行
// （matchRoot 用它自己的目录做匹配，比它自己深度上限更深的直接丢掉），所以并成一次读不会把数字算串。
//
// 两处刻意的收窄：
//  1. 只对权限失败的目录补读，不对「读得到但是空的」——为空是已经验证过的事实，弹授权框只是白打扰一次。
//  2. 只读：命令是 find 打印条目，不写不改任何文件（需求 ⑰）。
//
// 授权被拒（或本机没有提权工具）时：已经数到的那一半原样保住，读不动的这几段各自留一行原因、
// 照常标「无权读取」，界面可以从某一行的「重试」再问一次（需求 ㉘）。
func (h *hostScan) elevateDenied(ctx context.Context, srcs []hostSource) {
	// 待补读的段，连同它自己那份归一化后的目录——分回去的时候只认这一份。
	type pending struct {
		src   hostSource
		roots []string
	}
	var todo []pending
	merged := make([]string, 0, len(srcs))
	seen := map[string]bool{}
	unlimited := false // 有一段不限深度，合并后就不能设上限，否则那段读不全
	depth := 0
	for _, s := range srcs {
		if h.state[s.id] != srcStateDenied {
			continue
		}
		roots := make([]string, 0, len(s.roots))
		for _, r := range s.roots {
			if r != "" {
				roots = append(roots, filepath.Clean(r))
			}
		}
		if len(roots) == 0 {
			continue
		}
		todo = append(todo, pending{src: s, roots: roots})
		for _, r := range roots {
			if !seen[r] {
				seen[r] = true
				merged = append(merged, r)
			}
		}
		if s.maxDepth == 0 {
			unlimited = true
		} else if s.maxDepth > depth {
			depth = s.maxDepth
		}
	}
	if len(todo) == 0 {
		return
	}
	if unlimited {
		depth = 0
	}
	out, err := elevateFind(ctx, merged, depth)
	if err != nil {
		// 一次授权都没拿到：逐段留一行原因，状态原样停在「无权读取」（需求 ㉘）。
		for _, p := range todo {
			h.warn = append(h.warn, fmt.Sprintf("%s 提权读取失败：%v", p.src.stage, err))
		}
		return
	}
	h.elevate = true
	for _, p := range todo {
		h.classifyElevated(p.src, p.roots, out)
		// 提权读到了就是把这一段读全了：结局翻成「正常」，并把「无权读取」那句原因抹掉。
		// 这里不经 mark —— mark 的守卫正是拦「后来的正常覆盖前面的坏结局」，
		// 而 Phase B 是唯一一个有资格翻这个面的地方（拿不到授权时上面已经整批返回了）。
		h.state[p.src.id] = srcStateOK
		h.reason[p.src.id] = ""
	}
}

// classifyElevated 把 find 打印回来的行喂进同一个分类器。
//
// 「合并」在这里就是「替换」：Phase A 对这些根目录一个条目都没记到（全被权限挡住了），
// 所以补进来的不会与已有数字重复。若将来 Phase A 改成记部分结果，这里必须改成去重。
func (h *hostScan) classifyElevated(s hostSource, roots []string, out string) {
	for _, line := range strings.Split(out, "\n") {
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) < 3 {
			continue
		}
		// 符号链接不跟随、也不计体积（容器数据目录里链接不少，计了就是假数字）。
		if cols[0] == "l" {
			continue
		}
		p := cols[2]
		size, _ := strconv.ParseInt(cols[1], 10, 64)
		rel, ok := matchRoot(p, roots)
		if !ok || rel == "." {
			continue
		}
		// 合并那一次读的目录上限是所有段里最宽的一个，所以比这一段自己的上限更深的要在这儿丢掉——
		// 不分文件和目录都丢，才对得上「这一段原来单独用 find -maxdepth 读」的那份数字。
		if s.maxDepth > 0 && strings.Count(rel, "/") >= s.maxDepth {
			continue
		}
		h.classify(hostEntry{src: s.id, path: p, rel: rel, isDir: cols[0] == "d", size: size})
	}
}

// matchRoot 找出这个路径落在哪个根目录下，并给出相对那个根的路径（斜杠形式）。
//
// 一个统计段可能有多个根（如宿主日志同时看 /var/log/docker.log 与 journal 两处），
// 取**最长**的那个匹配：根写得越细，相对路径就越接近分类器想知道的答案。
// 不在任何根下（rel 以 .. 开头）即不匹配。
func matchRoot(p string, roots []string) (string, bool) {
	best, bestRel, ok := "", "", false
	for _, r := range roots {
		rel, err := filepath.Rel(r, filepath.Clean(p))
		if err != nil || rel == "" || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if !ok || len(r) > len(best) {
			best, bestRel, ok = r, filepath.ToSlash(rel), true
		}
	}
	return bestRel, ok
}

// elevateFind 与 pkexecPath 是测试注入点：单测不能真去弹授权框。
var (
	pkexecPath  = func() (string, error) { return exec.LookPath("pkexec") }
	findPath    = func() (string, error) { return exec.LookPath("find") }
	elevateFind = func(ctx context.Context, roots []string, maxDepth int) (string, error) {
		exe, err := pkexecPath()
		if err != nil {
			return "", fmt.Errorf("系统无 pkexec（polkit），无法自动提权读取: %w", err)
		}
		bin, err := findPath()
		if err != nil {
			return "", fmt.Errorf("本机没有 find 命令，无法提权读取: %w", err)
		}
		// argv 传入、不经 shell；find 默认不跟随符号链接，filepath.WalkDir 同样不跟随，两路口径一致。
		args := append([]string{bin}, roots...)
		if maxDepth > 0 {
			args = append(args, "-maxdepth", strconv.Itoa(maxDepth))
		}
		args = append(args, "-name", dirMerged, "-prune", "-o", "-printf", "%y\t%s\t%p\n")
		cmd := exec.CommandContext(ctx, exe, args...)
		// 用 Output 不用 CombinedOutput：这里 stdout **就是数据**，
		// 混进 stderr 的一段告警会把某一行解析成假条目（与提权写入那份 tee 先例不同）。
		out, err := cmd.Output()
		if err != nil {
			msg := err.Error()
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr))
			}
			// 某个根不存在会让 find 整体退出码非 0，但其余根的输出是完整可用的：
			// 只有「既没输出又出错」才当失败，否则就是把读到的东西扔掉。
			if len(out) == 0 {
				return "", fmt.Errorf("提权读取失败: %s", msg)
			}
		}
		return string(out), nil
	}
)

// finalise 把台账收成一行行 HostRow。
//
// 一行可能来自两个统计段（network.cni 同时看网卡与 /etc/cni 配置），状态按最保守的那一个来：
// 有一段读不动且一行里一个条目都没记到 → 无权读取；有一段读不动但记到了 → 照实显示并补一句「可能偏小」。
func (h *hostScan) finalise(info DaemonInfo) []HostRow {
	h.finishVolumes()
	h.finishIfaces()

	// 凡这一趟真的去读过的统计段，它所属的每一行都要出一行——**数出来是 0 也出**。
	// 以前只发「数到东西的行」，于是任何零结果的行整行不出现，界面拿到的是一句没有原因的「读不到」；
	// 那等于把「查过了、这里没有」和「根本没查」画成同一个样子，规则 41 不收这种形状。
	keys := make([]string, 0, len(hostRowSources)+4)
	seen := map[string]bool{}
	addKey := func(k string) {
		if seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	for key := range hostRowSources {
		if h.rowRead(key) {
			addKey(key)
		}
	}
	// 记到东西的行一定出场，即使它的统计段没被标过状态（外部注入台账的调用点就走这一路）。
	for key := range h.acc {
		addKey(key)
	}
	for _, r := range reasonRows() {
		addKey(r.Key)
	}
	sort.Strings(keys)

	rows := make([]HostRow, 0, len(keys))
	for _, key := range keys {
		if r := h.rowFor(key, info); r.Key != "" {
			rows = append(rows, r)
		}
	}
	return rows
}

// rowRead 说清这一行所属的统计段这一趟是不是全都真去读过。
//
// 为什么要问这一句：界面上「重数一行」只走那几段（见 RescanHostRow 的 want），其余段这一趟没读。
// 没读过的行不能报 0——那会把「没查」画成「这里没有」。
func (h *hostScan) rowRead(key string) bool {
	srcs, ok := hostRowSources[key]
	if !ok || len(srcs) == 0 {
		return false
	}
	for _, id := range srcs {
		if _, visited := h.state[id]; !visited {
			return false
		}
	}
	return true
}

// rowFor 收出一行。statusOf 决定这次到底算不算数到了东西。
func (h *hostScan) rowFor(key string, info DaemonInfo) HostRow {
	a := h.acc[key]
	// 「说不清数字」那几行交人话原因，绝不交一个编出来的 0。other.cache 是唯一真能数到东西的一行
	// （Podman 的宿主结果缓存）：数到了就报数字，数不到才退回那句解释。
	if a == nil || a.count == 0 {
		if m := reasonRow(key); m.Key != "" {
			return m
		}
	}
	srcs, ok := hostRowSources[key]
	if !ok {
		return HostRow{}
	}
	status, message := h.statusOf(key, srcs, a)
	row := HostRow{
		Key:       key,
		Status:    status,
		HasBytes:  byteRows[key] && status == model.RowOK,
		Aggregate: aggregateRows[key],
		ScannedAt: h.now,
		Message:   message,
	}
	if a != nil {
		row.Count = a.count
		row.Bytes = a.bytes
		row.Paths = a.paths
	}
	return row
}

// statusOf 收一行最终状态：一行可能来自两个统计段，取最保守的那一个说法。
//
// absent 的初值是 true，每过一段都用「这一段是不是够不着」去与它——
// 只有**每一段**都够不着，这一行才是「本平台够不着」（⑮ 的「—」）；有一段读到了就不是。
func (h *hostScan) statusOf(key string, srcs []string, a *rowAcc) (model.RowStatus, string) {
	denied, absent, noRoot, cancelled, live := false, true, false, false, false
	for _, id := range srcs {
		st := h.state[id]
		switch st {
		case srcStateDenied:
			denied = true
		case srcStateNoRootDep:
			noRoot = true
			denied = false
		case srcStateCancelled:
			cancelled = true
		}
		if st == srcStateOK || st == srcStateDenied {
			live = true
		}
		absent = absent && (st == srcStateAbsent)
	}
	switch {
	case noRoot:
		return model.RowUnavailable, h.firstReason(srcs)
	case a != nil && a.count > 0:
		if denied {
			return model.RowOK, MsgPartial
		}
		if cancelled {
			return model.RowUnavailable, MsgCancel
		}
		return model.RowOK, ""
	case cancelled:
		return model.RowUnavailable, MsgCancel
	case denied:
		return model.RowNoPerm, MsgNoPerm
	case live:
		// 目录读得到、里面确实没有东西：这是查出来的 0。
		return model.RowOK, ""
	case absent:
		return model.RowNotSupported, h.firstReason(srcs)
	default:
		return model.RowUnavailable, h.firstReason(srcs)
	}
}

func (h *hostScan) firstReason(srcs []string) string {
	for _, id := range srcs {
		if r := h.reason[id]; r != "" {
			return r
		}
	}
	return MsgNoRoot
}

func (h *hostScan) finishVolumes() {
	for _, name := range sortedKeys(h.vols) {
		b := h.vols[name]
		if b.hasData {
			a := h.row(rowVolumeData)
			a.count++
			a.bytes += b.data
			a.path(b.dataPath)
			continue
		}
		a := h.row(rowVolumeDriver)
		a.count++
		a.bytes += b.other
		a.path(name)
	}
}

func (h *hostScan) finishIfaces() {
	for _, name := range sortedKeys(h.ifaces) {
		b := h.ifaces[name]
		if b.isBridge {
			a := h.row(rowNetworkBridge)
			a.count++
			a.path(name)
		}
		if b.isVeth {
			a := h.row(rowNetworkVeth)
			a.count++
			a.path(filepath.Join(dirSysNet, name))
		}
		if b.isCNI {
			a := h.row(rowNetworkCni)
			a.count++
			a.path(filepath.Join(dirSysNet, name))
		}
	}
}

// sortedKeys 让输出顺序稳定：同一份磁盘状态两次扫描应给出一模一样的行序，
// 否则界面每次刷新都在重排，用户看不出到底有没有变。
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// reasonRows 是那几行「这一行数不出独立落点」的清单：只给人话原因，绝不给编造的数字。
// 它们同时是**零数兜底**——宿主扫描数到东西时报数字（other.cache 就是这一档：Podman 才有那一份结果缓存），
// 数不到才交这一句；其余三行在任何机器上都数不到，永远交这一句。
func reasonRows() []HostRow {
	return []HostRow{
		{Key: rowLogBuild, Status: model.RowUnavailable, Message: MsgLogBuild, ScannedAt: time.Now()},
		{Key: rowOtherEvents, Status: model.RowUnavailable, Message: MsgOtherEvents, ScannedAt: time.Now()},
		{Key: rowOtherCache, Status: model.RowUnavailable, Message: MsgOtherCache, ScannedAt: time.Now()},
		{Key: rowImageSign, Status: model.RowUnavailable, Message: MsgImageSign, ScannedAt: time.Now()},
	}
}

func reasonRow(key string) HostRow {
	for _, r := range reasonRows() {
		if r.Key == key {
			return r
		}
	}
	return HostRow{}
}

// HostPathFacts 把一组宿主路径的当前情形查回来，供服务层判「这是遗留残骸还是正在用的挂载」。
//
// 只读，且只用 Lstat：不跟随符号链接、不打开文件内容，读不到就把 Readable=false 带回去并说明原因。
func (c *Client) HostPathFacts(paths []string) []HostPathFact {
	mounts := hostMountPoints()
	out := make([]HostPathFact, 0, len(paths))
	for _, p := range paths {
		f := HostPathFact{Path: p}
		fi, err := os.Lstat(p)
		switch {
		case err != nil && errors.Is(err, fs.ErrNotExist):
			out = append(out, f) // Exists=false：这就是「不在了」的事实
			continue
		case err != nil:
			f.Error = err.Error()
			out = append(out, f)
			continue
		}
		f.Exists = true
		f.IsMount = mounts[p]
		if !fi.IsDir() {
			f.Readable = true
			out = append(out, f)
			continue
		}
		entries, rErr := os.ReadDir(p)
		if rErr != nil {
			f.Error = rErr.Error()
			out = append(out, f)
			continue
		}
		f.Readable = true
		f.Empty = len(entries) == 0
		out = append(out, f)
	}
	return out
}

// hostMountPoints 读 /proc/mounts 得到当前所有挂载点。读不到就返回空表——
// 那种情况一律当作「不是挂载点」，宁可不删也别把正在挂载的目录当残骸删掉。
func hostMountPoints() map[string]bool {
	m := map[string]bool{}
	if runtime.GOOS != "linux" {
		return m
	}
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		field := strings.Fields(line)
		if len(field) < 2 {
			continue
		}
		m[unescapeMountPath(field[1])] = true
	}
	return m
}

// inUserRuntimeDir 判一个路径是不是落在「当前用户的运行时目录」底下。
//
// 为什么只盯这一片：rootless 容器引擎把**此刻正在用的活东西**放在这里（网络命名空间、
// rootlessport 的套接字与就绪管道、容器 rootfs 的 overlay 挂载点），而数据目录底下那一片
// 本来就是清理的对象——把整盘都按「先证明没在用」来要求，正常机器上会什么都删不掉。
// 目录本身（rel == "."）不算：那是引擎的运行时目录，从来不是某一行能勾出去删的对象。
func inUserRuntimeDir(p string) bool {
	rel, ok := matchRoot(p, []string{userRuntimeDir()})
	return ok && rel != "."
}

// liveNode 认一个套接字或命名管道——两者都是进程间通信的活对象，不是落在磁盘上的内容。
func liveNode(mode fs.FileMode) bool {
	return mode&(fs.ModeSocket|fs.ModeNamedPipe) != 0
}

// liveInside 看一眼 p 自己或它下面有没有引擎正在用的活东西，有就交出一句给用户看的「为什么不能删」。
//
// 为什么要往下看一层：界面上能勾出去的是运行时目录的**顶层条目**（`rootlessport1213137364/` 这样一个目录），
// 而套接字藏在它里面（`.bp.sock`）。只判目录本身等于放行了「删这个目录」，
// 实际动的却是引擎正在监听的那只套接字。
//
// 读不动一律算活的：这一路只在用户运行时目录底下走，证明不了它不是正在用的东西就不能删——
// 界面上多一行「这次没删掉，因为读不动」比把正在跑的引擎踢掉便宜得多。
func liveInside(p string) string {
	mounts := hostMountPoints()
	if mounts[p] {
		return fmt.Sprintf("%s 是当前生效的挂载点", p)
	}
	for m := range mounts {
		if strings.HasPrefix(m, p+"/") {
			return fmt.Sprintf("%s 底下还挂载着 %s", p, m)
		}
	}
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// 本来就不在盘上：删除那一步是幂等的空操作，没有活对象会因此受伤。
		return ""
	case err != nil:
		return fmt.Sprintf("%s 读不动（%v），不能确认它不是引擎正在用的东西", p, err)
	case liveNode(fi.Mode()):
		return fmt.Sprintf("%s 是引擎正在用的套接字或管道", p)
	}
	if !fi.IsDir() {
		if pid := livePidIn(p); pid > 0 {
			return fmt.Sprintf("%s 记着进程 %d，它还在跑——这是引擎此刻在用的运行时状态", p, pid)
		}
		return ""
	}
	reason := ""
	_ = filepath.WalkDir(p, func(q string, d fs.DirEntry, wErr error) error {
		if reason != "" {
			return fs.SkipAll
		}
		if wErr != nil {
			if errors.Is(wErr, fs.ErrNotExist) {
				return nil // 走的这一趟里它刚消失：没有东西可删，也不算正在用
			}
			reason = fmt.Sprintf("%s 里面有一项读不动（%s：%v），不能确认它不是引擎正在用的东西", p, q, wErr)
			return fs.SkipAll
		}
		if mounts[q] {
			reason = fmt.Sprintf("%s 里面挂着 %s", p, q)
			return fs.SkipAll
		}
		if liveNode(d.Type()) {
			reason = fmt.Sprintf("%s 里面有引擎正在用的套接字或管道（%s）", p, q)
			return fs.SkipAll
		}
		if pid := livePidIn(q); pid > 0 {
			reason = fmt.Sprintf("%s 里面有引擎正在用的运行时状态（%s 记着进程 %d，它还在跑）", p, q, pid)
			return fs.SkipAll
		}
		return nil
	})
	return reason
}

// unescapeMountPath 还原 /proc/mounts 里的八进制转义：路径里有空格、制表符、换行或反斜杠时，
// 内核写成 \040 \011 \012 \134。不还原就等于拿一个假路径去比对挂载点，正在挂载的目录会被误判成残骸。
func unescapeMountPath(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func joinRoot(root, sub string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(root, sub)
}

// userRuntimeDir 是当前用户的运行时目录：systemd 给的 XDG_RUNTIME_DIR，拿不到才按惯例拼 /run/user/<uid>。
//
// rootless 容器引擎把自己**此刻正在用**的那一片放在这里，而不是放在数据目录底下——
// 网络命名空间、rootlessport 的套接字与就绪管道、overlay 的挂载点、rootless dockerd 的运行时目录。
// 本机实测（rootless Podman）：数据目录里没有这些，/run/user/1000 底下有 netns、libpod/tmp、containers 三处。
func userRuntimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Clean(d)
	}
	return filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
}

// runUserDir 是用户运行时目录底下的一个子目录。
func runUserDir(sub string) string {
	return filepath.Join(userRuntimeDir(), sub)
}

// runUserNetns 是 rootless 容器引擎放网络命名空间的那一个目录。
//
// 为什么不能只认 /var/run/netns：那是 rootful 的位置，rootless 引擎（含 rootless Podman）
// 把自己的那一份放在当前用户的运行时目录底下。本机实测：/var/run/netns 是空的，
// 而 /run/user/<uid>/netns 里有 9 个文件——只认前一个就等于这一行永远报 0。
func runUserNetns() string {
	return runUserDir("netns")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
