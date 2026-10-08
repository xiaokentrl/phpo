// 高频日志行的消息码对照（v2.9.16 多语言 Phase 3）：
// 这是干什么的——抽屉里每一行日志仍然由后端发中文原文（§5.16.3 冻结的措辞、任务账本里的等效日志原文都不动），
// 这一张表只做一件事：认出这一行说的是哪种消息，把句子里那几段动态内容（域名、路径、字节数、错误原因）
// 作为参数一起随 task:log 带出去，界面因此能按用户选的语言说同一句话。
// 什么情况下看不见效果：表里没登记的行——容器内命令逐行打出来的输出、以数据开头的句子——认不出消息码，
// 界面就照直显示后端原文。缺记号绝不等于少一行日志。
// 怎么补：新加一行日志时把它的中文句子登记进 logPatterns（句中夹数据）或 logLines（中文在句首），
// 并在 frontend/src/locales/{zh-CN,en-US}.ts 各补一条 log.<码>；第 1 项门禁核对两侧键集合相等，
// 用例 logmap_test.go 锁死「表里的中文确实还在源码里出现」。
package task

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// logPattern 一句中文消息的形状：正则（必须整句锚定）+ 消息码 + 捕获组对应的参数名。
type logPattern struct {
	re   *regexp.Regexp
	code string
	keys []string
	// zh 是这句消息的中文模板（占位写成 {参数名}）。语言包里的 log.<码> 中文文案与用例断言都以它为准，
	// 免得「表里一句、语言包一句」迟早对不上。
	zh string
}

// re 编译表内正则。表是写死的固定句式，编译不过即启动即崩——由用例先行拦截。
func re(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// logPatterns 中文不在句首、或一句里有多段数据的行。整句锚定，不做子串匹配，避免把数据里的中文当成句子。
var logPatterns = []logPattern{
	// 数据服务与容器状态
	{
		re:   re(`^核对现场：本次要动 (\d+) 项，另有 (\d+) 项在预览之后已经不在了$`),
		code: "log.checkTargets",
		keys: []string{"planned", "gone"},
		zh:   "核对现场：本次要动 {planned} 项，另有 {gone} 项在预览之后已经不在了",
	},
	{
		re:   re(`^已删除 (\S+)：(.+?)（腾出 (\d+) 字节）$`),
		code: "log.deletedFreed",
		keys: []string{"kind", "name", "bytes"},
		zh:   "已删除 {kind}：{name}（腾出 {bytes} 字节）",
	},
	{
		re:   re(`^删除失败 (\S+)：(.+) —— (.+)$`),
		code: "log.deleteFailNamed",
		keys: []string{"kind", "name", "reason"},
		zh:   "删除失败 {kind}：{name} —— {reason}",
	},
	{
		re:   re(`^解出 (\d+) 个文件$`),
		code: "log.archiveUnpacked",
		keys: []string{"n"},
		zh:   "解出 {n} 个文件",
	},
	{
		re:   re(`^归档内的 (\d+) 份逻辑转储（dump/）本次未重放进数据库：同机恢复无需重放，异机恢复该数据目录尚未导入$`),
		code: "log.dumpNotReplayed",
		keys: []string{"n"},
		zh:   "归档内的 {n} 份逻辑转储（dump/）本次未重放进数据库：同机恢复无需重放，异机恢复该数据目录尚未导入",
	},
	{
		re:   re(`^(\S+) (\S+) 逻辑导出失败：(.+) —— 本次归档不含该库数据$`),
		code: "log.dumpFail",
		keys: []string{"kind", "version", "reason"},
		zh:   "{kind} {version} 逻辑导出失败：{reason} —— 本次归档不含该库数据",
	},
	{
		re:   re(`^(\S+) (\S+) 未运行，跳过逻辑导出（其数据目录按冷拷贝打包，读不动的部分不在归档内）$`),
		code: "log.dumpSkipNotRunning",
		keys: []string{"kind", "version"},
		zh:   "{kind} {version} 未运行，跳过逻辑导出（其数据目录按冷拷贝打包，读不动的部分不在归档内）",
	},
	{
		re:   re(`^本次备份共跳过 (\d+) 个读不动的条目；数据库内容已由 dump/ 逻辑导出兜住，其余缺失项请自行核对$`),
		code: "log.backupSkipped",
		keys: []string{"n"},
		zh:   "本次备份共跳过 {n} 个读不动的条目；数据库内容已由 dump/ 逻辑导出兜住，其余缺失项请自行核对",
	},
	{
		re:   re(`^释放 (\d+) 字节（(\d+) 条目）$`),
		code: "log.freed",
		keys: []string{"bytes", "n"},
		zh:   "释放 {bytes} 字节（{n} 条目）",
	},
	{
		re:   re(`^无法确认 (\S+) 状态（(.+)），跳过重启；配置将在下次启动生效$`),
		code: "log.stateUnknownSkipRestart",
		keys: []string{"name", "reason"},
		zh:   "无法确认 {name} 状态（{reason}），跳过重启；配置将在下次启动生效",
	},
	{
		re:   re(`^重启 (\S+) 失败（配置已保存，重启成功后生效）：(.+)$`),
		code: "log.restartFail",
		keys: []string{"name", "reason"},
		zh:   "重启 {name} 失败（配置已保存，重启成功后生效）：{reason}",
	},
	{
		re:   re(`^(\S+) 未运行，配置将在下次启动生效$`),
		code: "log.notRunningNextStart",
		keys: []string{"name"},
		zh:   "{name} 未运行，配置将在下次启动生效",
	},
	{
		re:   re(`^(\S+) 已重启，新配置已生效$`),
		code: "log.restartedEffect",
		keys: []string{"name"},
		zh:   "{name} 已重启，新配置已生效",
	},
	{
		re:   re(`^(\S+) 已在运行，无需启动$`),
		code: "log.alreadyRunning",
		keys: []string{"name"},
		zh:   "{name} 已在运行，无需启动",
	},
	{
		re:   re(`^(\S+) 运行状态校验通过$`),
		code: "log.stateVerified",
		keys: []string{"name"},
		zh:   "{name} 运行状态校验通过",
	},
	{
		re:   re(`^启动 (\S+)（切换目标未运行）$`),
		code: "log.startSwitchTarget",
		keys: []string{"name"},
		zh:   "启动 {name}（切换目标未运行）",
	},
	// 扩展链路
	{
		re:   re(`^待启用 (\d+) 项 / 待停用 (\d+) 项$`),
		code: "log.extPending",
		keys: []string{"added", "removed"},
		zh:   "待启用 {added} 项 / 待停用 {removed} 项",
	},
	{
		re:   re(`^扩展 (\S+) 缺编译要用的系统开发包: (.+)$`),
		code: "log.extMissingPkg",
		keys: []string{"name", "pkgs"},
		zh:   "扩展 {name} 缺编译要用的系统开发包: {pkgs}",
	},
	{
		re:   re(`^扩展 (\S+) 安装失败：(.+)$`),
		code: "log.extInstallFail",
		keys: []string{"name", "reason"},
		zh:   "扩展 {name} 安装失败：{reason}",
	},
	{
		re:   re(`^扩展 (\S+) 停用失败：(.+)$`),
		code: "log.extDisableFail",
		keys: []string{"name", "reason"},
		zh:   "扩展 {name} 停用失败：{reason}",
	},
	{
		re:   re(`^命中 apk 构建依赖缓存（零网络）: (\d+) 份 → (\S+)$`),
		code: "log.apkHit",
		keys: []string{"n", "dir"},
		zh:   "命中 apk 构建依赖缓存（零网络）: {n} 份 → {dir}",
	},
	{
		re:   re(`^已缓存扩展包: pecl/(\S+) → (\S+)$`),
		code: "log.extPkgCached",
		keys: []string{"file", "dir"},
		zh:   "已缓存扩展包: pecl/{file} → {dir}",
	},
	{
		re:   re(`^已缓存构建依赖包: apk/(\S+) → (\S+)$`),
		code: "log.apkCached",
		keys: []string{"file", "dir"},
		zh:   "已缓存构建依赖包: apk/{file} → {dir}",
	},
	{
		re:   re(`^暂存目录内无 (\S+)，退回在线编译$`),
		code: "log.extPkgAbsent",
		keys: []string{"ext"},
		zh:   "暂存目录内无 {ext}，退回在线编译",
	},
	{
		re:   re(`^基座包管理器为 (\S+)，构建依赖不产生可离线包文件$`),
		code: "log.pkgManagerNoFile",
		keys: []string{"pm"},
		zh:   "基座包管理器为 {pm}，构建依赖不产生可离线包文件",
	},
	{
		re:   re(`^nginx 容器 (\S+) 运行态未知，跳过重载: (.+)$`),
		code: "log.nginxStateUnknown",
		keys: []string{"name", "reason"},
		zh:   "nginx 容器 {name} 运行态未知，跳过重载: {reason}",
	},
	{
		re:   re(`^nginx 容器 (\S+) 未运行，跳过重载$`),
		code: "log.nginxNotRunningSkip",
		keys: []string{"name"},
		zh:   "nginx 容器 {name} 未运行，跳过重载",
	},
}

// logLines 中文在句首的行：首句之后那一段动态内容整体作为 {rest} 交给界面塞回文案。
// 路径、argv、错误原因这些本来就是语言中立的数据，不拆。
var logLines = map[string]string{
	"确保镜像就绪（缓存优先）: ":         "log.ensureImage",
	"镜像安装失败: ":               "log.imageFail",
	"镜像就绪: ":                 "log.imageReady",
	"站点目录已存在，跳过创建: ":         "log.siteDirExists",
	"已创建站点目录: ":              "log.siteDirCreated",
	"站点已有 index.php，不覆盖: ":   "log.siteIndexExists",
	"已写入 index.php: ":        "log.siteIndexWritten",
	"已写入 vhost: ":            "log.vhostWritten",
	"hosts 写入失败（站点仍创建成功）: ":  "log.hostsWriteFail",
	"hosts 写入失败: ":           "log.hostsAddFail",
	"已添加 hosts: 127.0.0.1 ":  "log.hostsAdded",
	"hosts 已存在，跳过":           "log.hostsExists",
	"hosts 回收失败（站点仍删除成功）: ":  "log.hostsReclaimFail",
	"已回收 hosts: 127.0.0.1 ":  "log.hostsReclaimed",
	"hosts 无该条目，跳过":          "log.hostsAbsent",
	"站点目录不存在，跳过回收: ":         "log.siteDirAbsent",
	"站点目录已移入回收站: ":           "log.siteDirTrashed",
	"已移除 vhost: ":            "log.vhostRemoved",
	"已写入 ":                   "log.fileWritten",
	"已经不在了，跳过: ":             "log.alreadyGone",
	"已删除 ":                   "log.deleted",
	"这次取消，没动它: ":             "log.cancelledUntouched",
	"回收站里没有这个落点，不登记: ":       "log.trashSpotAbsent",
	"回收站登记失败（东西已经在回收站目录里）: ": "log.trashRegisterFail",
	"已放进回收站，7 天内可恢复: ":       "log.trashed",
	"归档顶层: ":                 "log.archiveTops",
	"归档已不在，无需删除: ":           "log.archiveAbsent",
	"已导出到 dump/":             "log.dumpDone",
	"未入档 ":                   "log.skippedEntries",
	"暂停 ":                    "log.pause",
	"重启 ":                    "log.restart",
	"重建 ":                    "log.rebuild",
	"删除失败 ":                  "log.deleteFail",
	"已恢复 ":                   "log.trashRestored",
	"清空到期回收站 ":               "log.trashPurged",
	"nginx 已重载，新配置已生效":       "log.nginxReloaded",
	"重载 nginx 失败（配置已保存，重载成功后生效）：": "log.nginxReloadFail",
	"写扩展清单: ":    "log.extEnvWritten",
	"确保基座镜像就绪: ": "log.prepareBaseImage",
	"固化镜像 ":      "log.extImageAbsent",
	"扩展 ini 清单读取失败，本次逐项照原样停用: ":        "log.extIniFail",
	"基座内建（无 conf.d ini 可删），停用无效，已跳过: ": "log.extBuiltinSkipped",
	"停用扩展 ":        "log.extDisableCmd",
	"已停用扩展: ":      "log.extDisabled",
	"已安装扩展: ":      "log.extInstalled",
	"清空容器暂存目录: ":   "log.extStagingCleared",
	"已固化镜像: ":      "log.extCommitted",
	"以扩展镜像重建容器: ":  "log.extRebuilt",
	"容器已运行于固化镜像: ": "log.extRunning",
	"未能实测启用集（容器内 php -m），本次落库的是请求的目标集，下次打开「管理扩展」即按实测纠正（非实时）": "log.extNotLive",
	"未接入 nginx，跳过重载": "log.nginxAbsent",
	"nginx 未安装，跳过重载": "log.nginxNotInstalled",
	"Nginx 重载失败（扩展已生效，站点若 502 请检查 nginx 配置）: ": "log.nginxReloadErr",
	"Nginx 已重载，上游指向 ":                          "log.nginxUpstream",
	"phpo 不代装系统包：先在正在运行的这个 php 容器里装上上面这些包（Debian 基座 apt-get install -y、Alpine 基座 apk add），再点一次「应用并重建」——装进容器的那一份会随扩展镜像一起固化，下次不用再装。": "log.extSysPkgHint",
	"安装扩展 ": "log.extInstallCmd",
	"扩展包缓存查询失败，本次走网络: ":         "log.extPkgLookupFail",
	"扩展包缓存校验失败，回退网络: ":          "log.extPkgCorrupted",
	"扩展包回填容器失败，本次走网络: ":         "log.extPkgRestoreFail",
	"命中扩展包缓存（零网络）: ":            "log.extPkgHit",
	"取扩展包到容器暂存目录: ":             "log.extPkgFetch",
	"pecl download 失败，退回在线编译: ": "log.extPeclDownloadFail",
	"扩展包取回宿主失败（本次不提升缓存）: ":      "log.extPkgTakeBackFail",
	"扩展包提升失败（不影响本次编译）: ":        "log.extPkgPromoteFail",
	"基座包管理器探测失败，跳过构建依赖预取: ":     "log.pkgManagerProbeFail",
	"apk 缓存查询失败，本次走网络: ":        "log.apkLookupFail",
	"apk 缓存回填失败，本次走网络: ":        "log.apkRestoreFail",
	"预取构建依赖: ":                  "log.apkPrefetch",
	"构建依赖预取失败（不影响扩展编译）: ":       "log.apkPrefetchFail",
	"apk 取回宿主失败（本次不提升缓存）: ":     "log.apkTakeBackFail",
	"apk 提升失败（不影响本次编译）: ":       "log.apkPromoteFail",
	"php 容器未运行，先恢复: ":           "log.phpRestartFirst",
	"导出扩展镜像到临时目录: ":             "log.extImageExport",
	"已提升到离线缓存: ":                "log.promotedToCache",
	"已删除缓存 ":                    "log.cacheEntryRemoved",
	"已导入 ":                      "log.cacheImported",
	"站点端口未发布到 Nginx: ":          "log.sitePortUnpublished",
	"补齐其他降级站点失败: ":              "log.healOtherSitesFail",
	"启动 ":                       "log.start",
}

// prefixes 按首句长度倒序，保证「已写入 vhost: 」先于「已写入 」命中。
var prefixes = sortedPrefixes()

func sortedPrefixes() []string {
	out := make([]string, 0, len(logLines))
	for k := range logLines {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) == len(out[j]) {
			return out[i] < out[j]
		}
		return len(out[i]) > len(out[j])
	})
	return out
}

// LookupLine 认一行日志的消息码。先看整句锚定的形状表，再退到「中文在句首」的表。
// 返回空码表示这一行不在表内（容器内逐行输出这类数据行），界面照直显示原文。
func LookupLine(text string) (string, map[string]string) {
	for _, p := range logPatterns {
		if m := p.re.FindStringSubmatch(text); m != nil {
			params := zipParams(p.keys, m[1:])
			// 参数里那个 name 多半是步骤名或容器名：认得出步骤名就换成 @消息码 记号，认不出原样留着
			if v, ok := params["name"]; ok {
				params["name"] = stepRef(v)
			}
			// 参数值是步骤名的（框架行里那一段），换成 @消息码 记号，界面先渲染它再塞回整句
			if v, ok := params["name"]; ok {
				params["name"] = stepRef(v)
			}
			return p.code, params
		}
	}
	for _, pre := range prefixes {
		if strings.HasPrefix(text, pre) {
			rest := text[len(pre):]
			if rest == "" {
				return logLines[pre], nil
			}
			return logLines[pre], map[string]string{"rest": rest}
		}
	}
	return "", nil
}

// PatternZh 交出形状行的中文模板（消息码 → 中文模板），语言包的中文文案由它生成，两侧不再各写一份。
func PatternZh() map[string]string {
	out := make(map[string]string, len(logPatterns))
	for _, p := range logPatterns {
		out[p.code] = p.zh
	}
	return out
}

// LineZh 交出「中文在句首」那些行的中文模板（消息码 → 首句 + {rest}）。
func LineZh() map[string]string {
	out := make(map[string]string, len(logLines))
	for pre, code := range logLines {
		if endsWithData(pre) {
			out[code] = pre + "{rest}"
		} else {
			out[code] = pre
		}
	}
	return out
}

// endsWithData 首句以空格、冒号或斜杠结尾 → 后面跟的是数据（路径、域名、文件名）。
func endsWithData(pre string) bool {
	if pre == "" {
		return false
	}
	switch pre[len(pre)-1] {
	case ' ', ':', '/':
		return true
	}
	return strings.HasSuffix(pre, "：")
}

// CoversTemplate 判断「一行的字面开头」是否已被某条整句形状覆盖：
// 表里登记的是渲染后的整句（如 ^无法确认 {name} 状态（{reason}）…$），而源码里那句字面只有开头这截。
// 用例 TestEveryChineseLogLineIsMapped 用它，免得把已经登记的行误报成漏登记。
func CoversTemplate(lit string) bool {
	for _, p := range logPatterns {
		lead := p.zh
		if i := strings.IndexByte(lead, '{'); i >= 0 {
			lead = lead[:i]
		}
		if lead == "" {
			continue
		}
		if strings.HasPrefix(lead, lit) || strings.HasPrefix(lit, lead) {
			return true
		}
	}
	return false
}

// FrameworkCodes 是任务框架行的消息码（每一次任务都要打的那几句：▶ 标签、步骤 X、X 完成、X 失败、
// ⏹ 已取消、预清理失败、核验失败、状态落地失败）。它们由 task/manager.go 现拼，不走上面的表，
// 但同样要有中英两份文案——用例 TestLocaleKeysCoverAllCodes 一起核对。
func FrameworkCodes() []string {
	return []string{
		"log.taskStart", "log.step", "log.stepDone", "log.stepFail",
		"log.stepCancelled", "log.preCleanFail", "log.verifyFail", "log.applyFail",
	}
}

// LogCodes 交出表内全部消息码，供用例与门禁核对语言包是否两侧都补齐。
func LogCodes() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(logLines)+len(logPatterns)+len(stepNames)+4)
	for _, p := range logPatterns {
		if !seen[p.code] {
			seen[p.code] = true
			out = append(out, p.code)
		}
	}
	for _, c := range logLines {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range stepNames {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, p := range stepNamePatterns {
		if !seen[p.code] {
			seen[p.code] = true
			out = append(out, p.code)
		}
	}
	sort.Strings(out)
	return out
}

func zipParams(keys, vals []string) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]string, len(keys))
	for i, k := range keys {
		if i < len(vals) {
			out[k] = vals[i]
		}
	}
	return out
}

// stepRef 把步骤名换成 @消息码记号（§5.6 的 task:log 载荷约定：参数值以 @ 开头即一个消息码，
// 界面先把它渲染出来再塞回整句）；带参数时写成 @码?k=v&k2=v2。
// 认不出的步骤名原样返回——新加一步忘了登记码，最坏是那一行不跟着换语言，不是少一行日志。
func stepRef(name string) string {
	code, params := StepCodeOf(name)
	if code == "" {
		return name
	}
	// 记号形如 @码[?k=v…]｜中文原文。原文带在记号里，语言包万一缺这条键时界面还能显示中文，
	// 不会渲染成空串或键名。
	if len(params) == 0 {
		return "@" + code + "|" + name
	}
	return "@" + code + "?rest=" + url.QueryEscape(params["rest"]) + "|" + name
}

// labelRef 把任务标签换成 @消息码?k=v&k2=v2 记号；标签没有消息码（还没接上 Phase 2 的那几类任务）时原样返回。
func labelRef(t *Task) string {
	if t.LabelCode == "" {
		return label(t)
	}
	if len(t.LabelParams) == 0 {
		return "@" + t.LabelCode + "|" + label(t)
	}
	keys := make([]string, 0, len(t.LabelParams))
	for k := range t.LabelParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	q := make([]string, 0, len(keys))
	for _, k := range keys {
		q = append(q, k+"="+url.QueryEscape(t.LabelParams[k]))
	}
	return "@" + t.LabelCode + "?" + strings.Join(q, "&") + "|" + label(t)
}
