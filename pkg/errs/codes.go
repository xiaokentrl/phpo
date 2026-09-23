// PF 校验错误码表：preflight 返回的用户级错误文案（原型 PF 直译 + 生产新增，共 27 条；
// 原型的 lastPhp「至少保留一个 PHP 版本」已随 §1.11 最小限制裁决删除，见 AGENTS.md）
package errs

// 错误码 → 中文文案；占位符 {from} {to} {version} 等由 preflight 填充
const (
	TaskBusy       = "已有任务运行中，请等待完成后再操作"
	HomeNotReady   = "请先完成 phpo 工作目录初始化"
	VersionInvalid = "版本号不合法（禁止路径分隔符与 ..）"
	VersionDup     = "该版本已安装"
	NginxSingle    = "Nginx 为单例服务，已安装"
	PortInvalid    = "端口格式不正确（1–65535）"
	PortInUse      = "端口已被占用"
	PortAdvance    = "端口 {from} 已被占用，自动顺延至 {to}"
	DomainInvalid  = "域名格式不正确"
	DomainExists   = "域名已存在"
	RootEmpty      = "站点目录不能为空"
	RootEscaped    = "站点目录必须位于 WWW_ROOT 内"
	RootOutsideWww = "站点目录不在 WWW_ROOT 内（允许，但请确认挂载与访问路径正确）"
	RootDuplicated = "该目录已被其他站点使用"
	PathTraversal  = "路径不合法（不允许 .. 或空段）"
	NotInstalled   = "目标服务/版本未安装"
	IsRunning      = "服务正在运行，请先停止"
	NotRunning     = "服务未运行"
	HasDependents  = "存在依赖该服务的站点，无法继续"
	BackupMissing  = "备份归档不存在"
	OfflineMissing = "离线缓存条目不存在"
	SvcMissing     = "服务不存在"
	SiteMissing    = "站点不存在"
	ExtInvalid     = "扩展名不合法"
	ConfigEmpty    = "配置内容不能为空"
	NginxNeeded    = "请先安装 Nginx"
	PhpNeeded      = "请先安装一个 PHP 版本"
	FileMissing    = "待导入的文件不存在"
)

// CodeCount 供对账测试：PF 表条目数
const CodeCount = 27

// AllCodes 返回全部错误码文案（测试对账用）
func AllCodes() []string {
	return []string{
		TaskBusy, HomeNotReady, VersionInvalid, VersionDup, NginxSingle,
		PortInvalid, PortInUse, PortAdvance,
		DomainInvalid, DomainExists, RootEmpty, RootEscaped, RootOutsideWww,
		RootDuplicated, PathTraversal, NotInstalled, IsRunning, NotRunning,
		HasDependents, BackupMissing, OfflineMissing, SvcMissing,
		SiteMissing, ExtInvalid, ConfigEmpty, NginxNeeded, PhpNeeded, FileMissing,
	}
}
