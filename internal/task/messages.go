// 任务消息码（v2.9.16 多语言，§5.25 方案 A 渐进迁移）：
// 后端 Label 仍发中文原文作为回退；LabelCode + LabelParams 让前端优先走 i18n 渲染。
// 键名规范：task.<type>，与前端 locales 内的键一一对应。
// 未迁移的 Label 构造点继续用中文原文（前端原样显示，渐进替换）。
package task

// 任务消息码常量（i18n 键，前端 locales 按此键定义文案）
const (
	MsgTaskInstall        = "task.install"
	MsgTaskReinstall      = "task.reinstall"
	MsgTaskStart          = "task.start"
	MsgTaskStop           = "task.stop"
	MsgTaskRemove         = "task.remove"
	MsgTaskBackupCreate   = "task.backupCreate"
	MsgTaskBackupRestore  = "task.backupRestore"
	MsgTaskBackupDelete   = "task.backupDelete"
	MsgTaskSitePort       = "task.sitePort"
	MsgTaskSitePhp        = "task.sitePhp"
	MsgTaskSiteRewrite    = "task.siteRewrite"
	MsgTaskSiteVhost      = "task.siteVhost"
	MsgTaskSiteHosts      = "task.siteHosts"
	MsgTaskSiteRemove     = "task.siteRemove"
	MsgTaskConfigSave     = "task.configSave"
	MsgTaskUpdate         = "task.update"
	MsgTaskExtensionApply = "task.extensionApply"
	MsgTaskWizardInit     = "task.wizardInit"
	MsgTaskOfflineRemove  = "task.offlineRemove"
	MsgTaskOfflineImport  = "task.offlineImport"
	MsgTaskDockerClean    = "task.dockerClean"
	MsgTaskCleanupOrphan  = "task.cleanupOrphan"
	MsgTaskCleanupCache   = "task.cleanupCache"
	MsgTaskTrashRestore   = "task.trashRestore"
	MsgTaskTrashEmpty     = "task.trashEmpty"
)
