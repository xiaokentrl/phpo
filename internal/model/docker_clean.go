// Docker 全量资源清理：总览页那张 60 行清单的静态定义 · 三档删除能力 · 前后端之间的载荷形状
package model

import "time"

// Tier 说的是「这一行的东西要怎么才能删掉」，界面上有没有那颗「彻底清空」按钮由它决定。
type Tier int

const (
	// TierSDK：Docker 自己能删，点按钮就执行。
	TierSDK Tier = 1
	// TierElev：东西在宿主磁盘上，要管理员授权才动得了；按钮照给，删之前先弹授权。
	TierElev Tier = 2
	// TierInfo：phpo 没有一条安全的删法（防火墙规则、docker0 网桥、Docker 自己的配置文件…）。
	// 这一行只说清「这里有什么、为什么不在这里删、想彻底清该动哪里」，不给按钮。
	TierInfo Tier = 3
)

// RowStatus 是这一行扫描结果的口径。取不到真实数字时绝不报 0，那等于把「不知道」说成「没有」。
type RowStatus string

const (
	RowOK           RowStatus = "ok"            // 数字是当场从 Docker／宿主取到的
	RowNoPerm       RowStatus = "no_perm"       // 无权读取，需要管理员授权，该行给重试入口
	RowNotSupported RowStatus = "not_supported" // 这个平台上够不着这个位置（Docker 跑在虚拟机里）
	RowUnavailable  RowStatus = "unavailable"   // Docker 这次没答上来（未运行／接口报错）
)

// RiskLevel 只用来给整行上色和写徽标，不参与任何判定：
// 能不能被「全选」、要不要单独确认，只由 DangerousCleanRowKeys 那 4 行决定。
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// CleanRowMeta 是一行的固定属性：叫什么、在哪一组、能不能删、是不是 phpo 自己建的东西。
type CleanRowMeta struct {
	Key       string    `json:"key"`
	Group     string    `json:"group"`
	Tier      Tier      `json:"tier"`
	Risk      RiskLevel `json:"risk"`
	IsPhpo    bool      `json:"isPhpo"`
	LinuxOnly bool      `json:"linuxOnly"`
}

// AllCleanGroups 与 AllCleanRows 的先后顺序就是界面上分组和逐行的显示顺序。
var AllCleanGroups = []string{
	"container", "image", "volume", "network", "build", "log",
	"plugin", "swarm", "compose", "system", "other",
}

var AllCleanRows = []CleanRowMeta{
	// 容器
	{"container.all", "container", TierSDK, RiskHigh, true, false},
	{"container.running", "container", TierSDK, RiskHigh, true, false},
	{"container.stopped", "container", TierSDK, RiskLow, true, false},
	{"container.paused", "container", TierSDK, RiskMedium, true, false},
	{"container.orphan", "container", TierSDK, RiskLow, true, false},
	{"container.checkpoint", "container", TierElev, RiskMedium, true, true},
	{"container.layer", "container", TierSDK, RiskHigh, true, false},
	{"container.meta", "container", TierElev, RiskHigh, true, true},
	{"container.cgroup", "container", TierElev, RiskHigh, true, true},

	// 镜像
	{"image.all", "image", TierSDK, RiskHigh, true, false},
	{"image.dangling", "image", TierSDK, RiskLow, true, false},
	{"image.unused", "image", TierSDK, RiskMedium, true, false},
	{"image.layer", "image", TierSDK, RiskHigh, true, false},
	{"image.meta", "image", TierSDK, RiskHigh, true, false},
	{"image.sign", "image", TierInfo, RiskMedium, true, false},
	{"image.multiarch", "image", TierSDK, RiskMedium, true, false},

	// 卷
	{"volume.all", "volume", TierSDK, RiskHigh, true, false},
	{"volume.unused", "volume", TierSDK, RiskMedium, true, false},
	{"volume.data", "volume", TierElev, RiskHigh, true, true},
	{"volume.driver", "volume", TierElev, RiskHigh, true, true},
	// 绑定挂载的目录是用户自己选的任意位置：Docker 认不出「哪个空目录其实是我的项目」，
	// phpo 也没有一条能把「残留」和「还在用」分开的删法，所以这一行只数不给按钮（档位 ③）。
	{"volume.bind", "volume", TierInfo, RiskHigh, true, true},

	// 网络
	{"network.all", "network", TierSDK, RiskHigh, true, false},
	{"network.unused", "network", TierSDK, RiskMedium, true, false},
	{"network.bridge", "network", TierInfo, RiskHigh, true, true},
	{"network.veth", "network", TierElev, RiskHigh, true, true},
	{"network.netns", "network", TierElev, RiskHigh, true, true},
	{"network.iptables", "network", TierInfo, RiskHigh, true, true},
	{"network.cni", "network", TierElev, RiskMedium, true, true},

	// 构建缓存
	{"build.cache", "build", TierSDK, RiskLow, false, false},
	{"build.buildkit", "build", TierSDK, RiskLow, false, false},
	// buildx 的构建器实例不在 Docker 的资源清单里（它是 buildx 自己的配置），Docker 侧数不出对象，
	// 也没有「只删构建器」的接口，所以这一行只说清去哪儿清（档位 ③）。
	{"build.buildx", "build", TierInfo, RiskMedium, false, false},
	{"build.context", "build", TierSDK, RiskMedium, false, false},

	// 日志
	{"log.container", "log", TierElev, RiskLow, false, false},
	{"log.daemon", "log", TierElev, RiskMedium, false, true},
	{"log.journald", "log", TierElev, RiskMedium, false, true},
	{"log.rotate", "log", TierElev, RiskLow, false, false},
	// 构建日志就躺在 buildkit 目录里，占用已经算进 system.builder 那一行；这里没有第二份可单独删的东西
	// （引擎那一行给的是这句话本身，不是 0），故不给按钮。
	{"log.build", "log", TierInfo, RiskLow, false, false},

	// 插件
	{"plugin.all", "plugin", TierSDK, RiskHigh, false, false},
	{"plugin.config", "plugin", TierElev, RiskHigh, false, true},
	{"plugin.data", "plugin", TierElev, RiskHigh, false, true},

	// Swarm
	{"swarm.service", "swarm", TierSDK, RiskHigh, true, false},
	{"swarm.config", "swarm", TierSDK, RiskHigh, true, false},
	{"swarm.node", "swarm", TierInfo, RiskHigh, true, false},
	{"swarm.stack", "swarm", TierSDK, RiskHigh, true, false},
	{"swarm.volume", "swarm", TierSDK, RiskHigh, true, false},
	{"swarm.leave", "swarm", TierInfo, RiskHigh, true, false},

	// Compose
	{"compose.project", "compose", TierSDK, RiskHigh, true, false},
	{"compose.config", "compose", TierInfo, RiskMedium, true, false},

	// 系统 / 运行时
	{"system.containerd", "system", TierElev, RiskHigh, false, true},
	{"system.tmp", "system", TierElev, RiskHigh, false, true},
	{"system.builder", "system", TierElev, RiskHigh, false, true},
	{"system.trust", "system", TierElev, RiskHigh, false, true},
	{"system.config", "system", TierInfo, RiskHigh, false, true},
	{"system.user", "system", TierInfo, RiskMedium, false, false},
	{"system.group", "system", TierElev, RiskHigh, false, true},
	{"system.root", "system", TierInfo, RiskCritical, false, true},

	// 其他
	// 事件流不落盘（引擎那一行直接说清这件事），~/.docker 里的东西也已分别算进别的行——
	// 这两行都没有第二份可以单独删掉的对象，所以只给一句人话、不给按钮。
	{"other.events", "other", TierInfo, RiskLow, false, true},
	{"other.scout", "other", TierInfo, RiskLow, false, false},
	{"other.desktop", "other", TierInfo, RiskMedium, false, false},
	{"other.cache", "other", TierInfo, RiskLow, false, false},
}

// DangerousCleanRowKeys 是唯一一份「危险项」名单：删了会让 Docker 起不来或让容器集体断网。
// 这 4 行永远进不了「全选」，每次勾它都要单独弹框说清后果并点「确定要选上」。
var DangerousCleanRowKeys = []string{
	"network.veth", "network.netns", "network.cni", "system.group",
}

// AllCleanRows 供对账测试（60 行 · 11 组）
func AllCleanRowMetas() []CleanRowMeta { return AllCleanRows }

// CleanRowMetaOf 按行名取固定属性；名单外的名字（比如手抖写错的 key）返回 false。
func CleanRowMetaOf(key string) (CleanRowMeta, bool) {
	for _, m := range AllCleanRows {
		if m.Key == key {
			return m, true
		}
	}
	return CleanRowMeta{}, false
}

// IsDangerousCleanRow 判定一行是否危险项——只有这份名单参与判定，risk 徽标不参与。
func IsDangerousCleanRow(key string) bool {
	for _, k := range DangerousCleanRowKeys {
		if k == key {
			return true
		}
	}
	return false
}

// Deletable 说的是界面上给不给这颗按钮：只读说明那一档（TierInfo）永远不给。
func (m CleanRowMeta) Deletable() bool { return m.Tier != TierInfo }

// ---- 前后端之间的载荷 ----

// CleanRow 是面板上的一行：静态属性 + 这次扫到的真实数字。
// 按钮给不给、要不要单独确认，全在这里带上，前端不再自己推第二套判据。
type CleanRow struct {
	Key       string    `json:"key"`
	Group     string    `json:"group"`
	Tier      Tier      `json:"tier"`
	Deletable bool      `json:"deletable"`
	Dangerous bool      `json:"dangerous"`
	IsPhpo    bool      `json:"isPhpo"`
	Risk      RiskLevel `json:"risk"`
	Status    RowStatus `json:"status"`
	Count     int       `json:"count"`
	Bytes     int64     `json:"bytes"`
	HasBytes  bool      `json:"hasBytes"`
	Message   string    `json:"message,omitempty"`
	ScannedAt time.Time `json:"scannedAt"`
}

// NewCleanRow 从静态表起一行，避免调用处漏带档位。
// ScannedAt 默认取当下（⑪ 每行要带自己的取数时间）；宿主深扫那几行可在此之后自行覆写。
func NewCleanRow(key string, status RowStatus, count int, bytes int64, hasBytes bool) CleanRow {
	m, ok := CleanRowMetaOf(key)
	if !ok {
		return CleanRow{Key: key, Status: status, Count: count, Bytes: bytes, HasBytes: hasBytes, ScannedAt: time.Now()}
	}
	return CleanRow{
		Key: m.Key, Group: m.Group, Tier: m.Tier, Deletable: m.Deletable(),
		Dangerous: IsDangerousCleanRow(m.Key), IsPhpo: m.IsPhpo, Risk: m.Risk,
		Status: status, Count: count, Bytes: bytes, HasBytes: hasBytes,
		ScannedAt: time.Now(),
	}
}

// CleanScanReport 是一次扫描的全部结果：60 行 + 总计 + 这一轮有没有做过宿主深扫。
type CleanScanReport struct {
	Rows        []CleanRow `json:"rows"`
	TotalCount  int        `json:"totalCount"`
	TotalBytes  int64      `json:"totalBytes"`
	ScannedAt   time.Time  `json:"scannedAt"`
	DeepScanned bool       `json:"deepScanned"`
	Warnings    []string   `json:"warnings"`
}

// CleanTarget 是确认框里的一项具体对象（一个容器、一个镜像、一个卷…），带一颗开关。
type CleanTarget struct {
	Row       string `json:"row"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	InUse     bool   `json:"inUse"`
	Foreign   bool   `json:"foreign"`
	NeedsRoot bool   `json:"needsRoot"`
}

// CleanInstalled 是这次清理会动到的 phpo 已装版本，供确认框里那颗默认不勾的「同时卸载」。
type CleanInstalled struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

// CleanPreview 是一次预览的凭据。token 是一次性的：过期或用过就得重新预览。
type CleanPreview struct {
	Token           string           `json:"token"`
	ExpiresAt       time.Time        `json:"expiresAt"`
	Rows            []string         `json:"rows"`
	Targets         []CleanTarget    `json:"targets"`
	Warnings        []string         `json:"warnings"`
	ConsentRequired bool             `json:"consentRequired"`
	Installed       []CleanInstalled `json:"installed"`
}

// CleanRequest 是「彻底清空」的入参：一次性凭据 + 选中的对象 ID + 知情同意 + 是否顺手卸载。
type CleanRequest struct {
	Token     string   `json:"token"`
	IDs       []string `json:"ids"`
	Consent   bool     `json:"consent"`
	Uninstall bool     `json:"uninstall"`
}

// CleanExecuteReport 是一次彻底清空的收尾：逐项结果 + 四个计数。
// Skipped 记的是「预览时还在、动手时已经不在」的那些——不判死整单，只逐行说明。
type CleanExecuteReport struct {
	TaskID     string        `json:"taskId"`
	Status     TaskStatus    `json:"status"`
	Items      []CleanedItem `json:"items"`
	Removed    int           `json:"removed"`
	Failed     int           `json:"failed"`
	Skipped    int           `json:"skipped"`
	FreedBytes int64         `json:"freedBytes"`
	Trashed    []string      `json:"trashed"`
}
