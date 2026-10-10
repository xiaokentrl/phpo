// 步骤名的消息码对照——**兼容层，不是新增步骤的登记处**（总纲 §5.15 原则 3 / 规则 46）。
//
// 这是干什么的——每一次任务都要逐条报「步骤 X」「X 完成」「X 失败」，X 就是步骤名，
// 它是抽屉里出现频率最高的中文。这两张表把**那些构造点还没直接给码的**步骤名认成消息码，
// 界面才能跟着语言换。构造点给了码（`FuncStep{Code: …}`）就先用码，认名字那一套只在码缺席时才跑。
//
// 适用场景（只有这两种）：① 旧日志与任务账本回看；② 尚未迁移到「构造点直接给码」的存量步骤。
//
// 新增一个步骤**不得**登记到这里——正确做法是在构造点写 `Code: defs.StepXxx` 与 `Params`，
// 中文名由定义源生成（`stepNamePatterns` 里那些带数据的形状同理）。
//
// 什么情况下看不见：新加的步骤既没在构造点给码、也没登记进来时，那一行照直显示后端给的中文步骤名，
// 不会少一行日志。
//
// 下线条件（可 grep 验证）：`grep -rn "StepCodeOf(" internal/ --include=*.go` 只剩 stepRef 这一处、
// 且所有 `task.FuncStep` / `steps.*` 构造点都带 `Code:`（用例 `TestAppService_StepConstructionPointsCarryCodes`
// 那类锁死扩到全仓）时，本文件的两张表与 StepCodeOf 一并删除。
package task

// stepNames 固定步骤名 → 消息码。
var stepNames = map[string]string{
	"升级应用到新版本":             "step.updateApply",
	"核对现场":                 "step.checkTargets",
	"把 phpo 自己卷里的数据挪进回收站":  "step.trashPhpoVolumes",
	"删除 Docker 侧的对象":       "step.deleteDockerObjects",
	"删除宿主上的文件与内核对象":        "step.deleteHostObjects",
	"导出 SQLite 快照":         "step.dumpSqlite",
	"打包归档":                 "step.packArchive",
	"解包归档":                 "step.unpackArchive",
	"清空 phpo 容器命名空间":       "step.pruneNamespace",
	"落盘配置/数据/站点/缓存":        "step.landFiles",
	"逻辑重放 SQLite 快照":       "step.replaySqlite",
	"重建已安装容器":              "step.rebuildInstalled",
	"删除归档文件":               "step.deleteArchiveFile",
	"逻辑导出数据服务":             "step.dumpDataServices",
	"暂停数据服务":               "step.pauseDataServices",
	"重启数据服务":               "step.restartDataServices",
	"创建工作目录子树":             "step.createHomeTree",
	"清空到期回收站":              "step.purgeExpired",
	"按模式清理缓存":              "step.cleanCache",
	"移回原位并注销登记":            "step.trashMoveBack",
	"永久删除到期条目":             "step.purgeForever",
	"配置生效说明":               "step.configEffectNote",
	"容器内编译扩展":              "step.compileExtensions",
	"准备镜像":                 "step.prepareImage",
	"准备基座镜像（缓存优先）":         "step.prepareBaseImage",
	"写扩展清单 extensions.env": "step.writeExtEnv",
	"从扩展镜像重建容器":            "step.rebuildFromExtImage",
	"重载 Nginx":             "step.reloadNginx",
	"重载 nginx 使配置生效":       "step.reloadNginxEffect",
	"删除缓存目录":               "step.deleteCacheDir",
	"读取源文件":                "step.readSourceFile",
	"写入缓存":                 "step.writeCache",
	"补齐站点 vhost":           "step.reconcileVhost",
	"落盘工作目录与配置":            "step.landHomeConfigs",
	"写入 hosts":             "step.writeHosts",
	"创建站点目录":               "step.createSiteDir",
	"生成 vhost":             "step.genVhost",
	"写入 vhost":             "step.writeVhost",
	"站点目录入回收站":             "step.trashSiteDir",
	"移除 vhost":             "step.removeVhost",
	"回收 hosts":             "step.reclaimHosts",
	"发布站点端口到 Nginx":        "step.publishSitePorts",
	"写入 index.php":         "step.writeSiteIndex",
	"校验":                   "step.verify",
}

// stepNamePatterns 名字里带数据的步骤名：捕获组就是文案里的 {rest}，zh 是这句中文名的模板。
var stepNamePatterns = []logPattern{
	{
		re:   re(`^重启 (\S+) 使配置生效$`),
		code: "step.restartContainerEffect",
		keys: []string{"rest"},
		zh:   "重启 {rest} 使配置生效",
	},
	{
		re:   re(`^创建并启动 (\S+)$`),
		code: "step.createAndStart",
		keys: []string{"rest"},
		zh:   "创建并启动 {rest}",
	},
	{
		re:   re(`^启动 PHP (\S+)（切换目标）$`),
		code: "step.startPhpTarget",
		keys: []string{"rest"},
		zh:   "启动 PHP {rest}（切换目标）",
	},
	{
		re:   re(`^固化扩展镜像 phpo/php:(\S+)$`),
		code: "step.commitExtImage",
		keys: []string{"rest"},
		zh:   "固化扩展镜像 phpo/php:{rest}",
	},
}

// StepZh 交出每个步骤名的中文模板（消息码 → 中文）；语言包的中文文案由它生成，不再两处各写一份。
func StepZh() map[string]string {
	out := make(map[string]string, len(stepNames)+len(stepNamePatterns))
	for name, code := range stepNames {
		out[code] = name
	}
	for _, p := range stepNamePatterns {
		out[p.code] = p.zh
	}
	return out
}

// StepCodeOf 认一个步骤名的消息码与参数；认不出返回空码（那一行就照直显示中文名）。
func StepCodeOf(name string) (string, map[string]string) {
	if code, ok := stepNames[name]; ok {
		return code, nil
	}
	for _, p := range stepNamePatterns {
		if m := p.re.FindStringSubmatch(name); m != nil {
			return p.code, zipParams(p.keys, m[1:])
		}
	}
	return "", nil
}
