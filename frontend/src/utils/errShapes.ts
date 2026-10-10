// errShapes：后端中文错误句的「形状表」——把认得出的句子（连同参数）从一句中文原文里剥出来。
//
// 这是干什么的：后端到前端的错误只剩一句字符串（error 直出、任务框架日志 reason 参数里的错误原文、
// 历史回放的失败原因），这句话里塞不下消息码。这里放一张「中文形状 → 消息码」对照表：
// 认得出就交回调用方（backendMsg 的整行渲染、msgText 对参数值的现译）按当前语言重说；
// 认不出（新句子没登记、或那本来就是路径／容器输出这类数据）返回 null，调用方照直显示原文，
// 绝不空着、绝不显示成键名。
//
// 表里每一项是 [中文形状, 消息码]。形状里的 {名字} 是参数占位，与语言包里同名 {名字} 一一对应；
// zh-CN 一侧的文案必须与这里逐字相同——它就是回退显示的那句原文，也是任务账本存的那份。
const SIGNALS: ReadonlyArray<readonly [string, string]> = [
  // —— 校验错误码（pkg/errs 的 28 条；码表冻结，这里只是把同一句话挂上记号）
  ['已有任务运行中，请等待完成后再操作', 'err.taskBusy'],
  ['请先完成 phpo 工作目录初始化', 'err.homeNotReady'],
  ['版本号不合法（禁止路径分隔符与 ..）', 'err.versionInvalid'],
  ['该版本已安装', 'err.versionDup'],
  ['Nginx 为单例服务，已安装', 'err.nginxSingle'],
  ['端口格式不正确（1–65535）', 'err.portInvalid'],
  ['端口已被占用', 'err.portInUse'],
  ['端口 {from} 已被占用，自动顺延至 {to}', 'err.portAdvance'],
  ['域名格式不正确', 'err.domainInvalid'],
  ['域名已存在', 'err.domainExists'],
  ['站点目录不能为空', 'err.rootEmpty'],
  ['站点目录必须位于 WWW_ROOT 内', 'err.rootEscaped'],
  ['站点目录不在 WWW_ROOT 内（允许，但请确认挂载与访问路径正确）', 'err.rootOutsideWww'],
  ['该目录已被其他站点使用', 'err.rootDuplicated'],
  ['路径不合法（不允许 .. 或空段）', 'err.pathTraversal'],
  ['目标服务/版本未安装', 'err.notInstalled'],
  ['服务正在运行，请先停止', 'err.isRunning'],
  ['服务未运行', 'err.notRunning'],
  ['同一操作已在任务队列中：{label}', 'err.taskQueued'],
  ['存在依赖该服务的站点，无法继续', 'err.hasDependents'],
  ['备份归档不存在', 'err.backupMissing'],
  ['离线缓存条目不存在', 'err.offlineMissing'],
  ['服务不存在', 'err.svcMissing'],
  ['站点不存在', 'err.siteMissing'],
  ['扩展名不合法', 'err.extInvalid'],
  ['配置内容不能为空', 'err.configEmpty'],
  ['请先安装 Nginx', 'err.nginxNeeded'],
  ['请先安装一个 PHP 版本', 'err.phpNeeded'],
  ['待导入的文件不存在', 'err.fileMissing'],

  // —— 上面那些码拼上具体对象后的整句（preflight 与站点校验那几路都走这个形状）
  ['域名已存在: {domain}', 'err.domainExistsWith'],
  ['站点不存在: {domain}', 'err.siteMissingWith'],
  ['目标服务/版本未安装: PHP {version}', 'err.notInstalledPhp'],
  ['目标服务/版本未安装: {what}', 'err.notInstalledWith'],
  ['备份归档不存在: {name}', 'err.backupMissingWith'],
  ['该目录已被其他站点使用: {path}', 'err.rootDuplicatedWith'],
  ['端口已被占用: {port} ({owner})', 'err.portInUseDetail'],

  // —— 镜像源地址（校验只问「合不合形」，可达性不拦）
  ['镜像源地址不正确: 不能为空', 'err.registryHostEmpty'],
  ['镜像源地址不正确: {value}（不能含空白字符）', 'err.registryHostSpace'],
  ['镜像源地址不正确: {value}（只填主机名或「主机:端口」，不要带路径）', 'err.registryHostPath'],
  ['镜像源地址不正确: {value}', 'err.registryHostBad'],

  // —— 缓存导入
  ['缓存文件类型只能是 image / apk / pecl，得 {field}', 'err.cacheFieldType'],
  ['{field} 扩展只能导入到 php 缓存', 'err.extImportPhpOnly'],
  ['服务尚未初始化', 'err.appNotReady'],
  ['nginx 未安装，无法校验或重载 vhost', 'err.nginxAbsentNoVhost'],
  ['文件夹不存在或打不开：{path}', 'err.folderNotOpenable'],
  ['已重启过一次，工作目录仍未就绪：请检查 config.yaml 的根目录配置与目录权限', 'err.homeNotReadyAfterRestart'],

  // —— 站点端口那一档（降级那句必须说清真因）
  [
    '端口 {port} 绑不上：rootless 模式的容器引擎不能绑定 1024 以下的端口（不是别的程序占了它）。站点配置已落盘，只是这个端口暂不发布。要放开任选其一：给引擎允许特权端口 sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80（写进 /etc/sysctl.d/ 下次开机仍然有效），或把站点端口改成 1024 以上。改好后任意一次站点保存、或重启容器引擎，这个端口就自动补上发布。',
    'err.portRootlessBlocked',
  ],
  ['端口 {port} 已被 {owner} 占用：站点配置已落盘，只是这个端口暂不发布（腾出后自动补齐）', 'err.portBlocked'],
  ['补齐 {n} 个站点后重载 nginx 失败', 'err.reloadAfterHeal'],
  ['部分站点 vhost 补齐失败: {why}', 'err.vhostHealPartial'],
  ['已补齐 {domain}，但部分站点 vhost 补齐失败: {why}', 'err.vhostHealPartialOne'],
  ['站点状态缺失: {domain}', 'err.siteStateMissing'],
  ['vhost 改动须经运行中的 Nginx 校验后才能落盘，请先启动 Nginx 再重试', 'err.nginxNotServingDetail'],
  ['服务未运行: Nginx（vhost 改动须经运行中的 Nginx 校验后才能落盘，请先启动 Nginx 再重试）', 'err.nginxNotServingErr'],

  // —— 服务生命周期
  ['服务未安装，无法重建：{kind}/{version}', 'err.rebuildNotInstalled'],
  ['未注册的服务种类: {kind}', 'err.unregisteredKind'],
  ['未知服务种类: {kind}', 'err.unknownKind'],
  ['校验失败：容器 {name} 不存在', 'err.verifyNoContainer'],
  ['校准回写运行态失败 {kind}/{version}', 'err.calibrateWriteback'],
  ['运行态校验失败：{name} 期望 running={want}，实际 {got}', 'err.stateVerify'],
  ['{what}：端口 {port} 已被本机其它进程占用，Nginx 未重建（在跑的容器与站点均未改动）', 'err.nginxPortBusyHost'],
  ['{what}：端口 {port} 已被 {owner} 占用，{kind}/{version} 未重建（在跑的容器与数据均未改动）', 'err.servicePortBusy'],

  // —— PHP 扩展
  ['扩展名不合法: {name}', 'err.extInvalidName'],
  ['扩展 {name} 安装失败，本次扩展集未应用', 'err.extInstallFailed'],
  ['扩展 {name} 停用失败，本次扩展集未应用', 'err.extDisableFailed'],
  ['扩展 {name} 安装失败（缺系统开发包 {pkgs}），本次扩展集未应用', 'err.extInstallFailedDeps'],
  ['扩展 {name} 停用失败（缺系统开发包 {pkgs}），本次扩展集未应用', 'err.extDisableFailedDeps'],
  ['phpo 不代装系统包：先在正在运行的这个 php 容器里装上上面这些包（Debian 基座 apt-get install -y、Alpine 基座 apk add），再点一次「应用并重建」——装进容器的那一份会随扩展镜像一起固化，下次不用再装。', 'err.extSysPkgGuide'],

  // —— 容器引擎调用
  ['容器内命令失败（退出码 {code}）', 'err.execExit'],
  ['读取 exec 输出失败', 'err.execReadOutput'],
  ['docker commit 失败', 'err.commitFailed'],
  ['docker exec 创建失败', 'err.execCreate'],
  ['docker exec attach 失败', 'err.execAttach'],
  ['docker exec inspect 失败', 'err.execInspect'],
  ['尚未准备好 Docker 客户端', 'err.noDockerClient'],
  ['列出 Docker 容器失败', 'err.containerListFailed'],
  ['网络 {name} 未配置网关', 'err.networkNoGateway'],
  ['版本不能为空', 'err.versionEmpty'],
  ['资源名称冲突：目标名被外部资源占用', 'err.nameConflict'],
  ['拉取 {ref} 失败（{why}）', 'err.pullWithSource'],
  ['服务端错误 HTTP {code}', 'err.mirrorServerError'],
  ['HTTP {code}（该地址可能不是 Docker 镜像源）', 'err.mirrorHttpStatus'],

  // —— 镜像导入导出（Podman 的兼容层只吃本地 unix 端点，那一档给人话 + 手工命令）
  ['未找到 podman CLI；请手动执行 podman load -i {tar}', 'err.podmanNoLoadCLI'],
  ['未找到 podman CLI，无法导出镜像缓存', 'err.podmanNoSaveCLI'],
  ['podman 的镜像 load 仅支持本地 unix 端点（当前端点：{host}）；请手动执行 podman load -i {tar}', 'err.podmanLoadRemote'],
  ['podman 的镜像 save 仅支持本地 unix 端点（当前端点：{host}）', 'err.podmanSaveRemote'],
  ['podman load 失败: {err}：{out}', 'err.podmanLoadFailed'],
  ['podman save 失败: {err}：{out}', 'err.podmanSaveFailed'],
  ['创建导出文件失败', 'err.saveCreateFile'],
  ['打开镜像 tar 失败', 'err.loadOpenTar'],
  ['读取 pull 进度失败', 'err.pullReadProgress'],
  ['docker load 读取失败', 'err.loadRead'],
  ['docker load 失败', 'err.loadFailed'],
  ['docker pull 失败', 'err.pullFailed'],
  ['docker rmi 失败', 'err.rmiFailed'],
  ['docker save 失败', 'err.saveFailed'],
  ['docker save 写入失败', 'err.saveWrite'],
  ['docker tag 失败', 'err.tagFailed'],

  // —— 宿主 ⇄ 容器那条字节通道
  ['从容器取回文件失败', 'err.copyFromFailed'],
  ['读取待拷贝文件失败', 'err.copyReadHost'],
  ['读取容器 tar 失败', 'err.copyReadTar'],
  ['拷贝进容器失败', 'err.copyToFailed'],
  ['容器内建暂存目录失败', 'err.copyMkdirStaging'],
  ['写入取回文件失败', 'err.copyWriteHost'],

  // —— 回收站
  ['创建回收站失败', 'err.trashMkdir'],
  ['从回收站恢复失败 {from} → {to}', 'err.trashRestore'],
  ['回收站根目录未配置', 'err.trashRootMissing'],
  ['清空回收站条目失败 {path}', 'err.trashPurge'],
  ['提权移入回收站失败 {from} → {to}', 'err.trashElevMove'],
  ['移入回收站失败 {from} → {to}', 'err.trashMove'],
  ['回收站条目不存在: {id}', 'err.trashItemMissing'],

  // —— Docker 全量清理（三档：点就删 / 要授权 / 只说明不给按钮）
  ['不认识要删的 Docker 对象类型: {type}', 'err.cleanUnknownType'],
  ['未知资源类型: {type}', 'err.cleanUnknownResType'],
  ['清空构建缓存失败', 'err.cleanPruneBuild'],
  ['删除 swarm 服务失败', 'err.cleanSwarmService'],
  ['删除 swarm 密文失败', 'err.cleanSwarmSecret'],
  ['删除 swarm 配置失败', 'err.cleanSwarmConfig'],
  ['docker plugin rm 失败', 'err.cleanPluginRm'],
  ['当前平台够不着 Docker 的宿主目录', 'err.hostfsUnsupported'],
  ['本机没有 find 命令，无法提权读取', 'err.hostfsNoFind'],
  ['提权读取失败: {err}', 'err.hostfsElevFail'],
  ['系统无 pkexec（polkit），无法自动提权读取', 'err.hostfsNoPkexec'],
  ['路径含空字节', 'err.delNulPath'],
  ['路径是空的', 'err.delEmptyPath'],
  ['名字是空的', 'err.delEmptyName'],
  ['把用户 {user} 移出 docker 组失败', 'err.delGroupRemove'],
  ['本机没有 {cmd} 命令，无法提权删除', 'err.delNoElevCmd'],
  ['本机没有 {cmd} 命令：{err}', 'err.delNoCmd'],
  ['不认识要删的宿主对象类型', 'err.delUnknownKind'],
  ['读 {path} 失败，无法确认用户 {user} 在不在 docker 组里', 'err.delGroupRead'],
  ['回收站还没准备好，这一项不能删（直接删等于绕过 7 天保留）', 'err.delTrashNotReady'],
  ['拒绝删除系统关键目录 {dir}', 'err.delCriticalRoot'],
  ['路径 {p} 不是绝对路径', 'err.delNotAbsolute'],
  ['路径 {p} 不在这一行扫描过的目录之下，或就是那个目录本身，拒绝删除', 'err.delOutsideScan'],
  ['路径 {p} 未归一（可能含 .. 或多余分隔符）', 'err.delNotClean'],
  ['名字 {n} 含空字节', 'err.delNameNul'],
  ['名字 {n} 含路径分隔符，不是一个对象名', 'err.delNameSep'],
  ['名字 {n} 以 - 开头，会被命令当成选项，拒绝执行', 'err.delNameDash'],
  ['删不掉 {path}（通常说明这个 cgroup 里还有进程在跑）', 'err.delCgroupBusy'],
  ['删除网络接口 {name} 失败（多半它还挂在某个容器上）', 'err.delVethFail'],
  ['删除网络命名空间 {name} 失败', 'err.delNetnsFail'],
  ['提权删除 {ref} 失败', 'err.delElevFail'],
  ['删除 {ref} 失败', 'err.delGenericFail'],
  ['系统无 pkexec（polkit），无法自动提权删除', 'err.delNoPkexec'],
  ['{row} 的条目 {p} 不在 {dir} 之下，不是一个接口名', 'err.delEntryNotUnder'],
  ['{row} 的条目 {p} 不是绝对路径', 'err.delEntryNotAbs'],
  ['{row} 的条目名不安全', 'err.delEntryNameUnsafe'],
  ['{row} 的条目不安全', 'err.delEntryUnsafe'],
  ['{row} 这一行的条目 {p} 只是个名字，落不出唯一路径（这一行有 {n} 个候选目录）', 'err.delRowAmbiguous'],
  ['{row} 这一行的条目名 {n} 不是一个安全的文件/目录名', 'err.delRowNameUnsafe'],
  ['{row} 这一项不是宿主上的文件，进不了回收站，只能直接删', 'err.delTrashNa'],

  // —— 清理面板的拒绝，都要说清为什么拒、下一步点哪里
  ['你勾的这几行里有危险项（容器的网卡、网络命名空间、CNI 留下的网卡与配置、docker 组的成员）——删了正在跑的容器会断网，Docker 也可能起不来。确认知道这个后果再清空', 'err.cleanDangerUnconfirmed'],
  ['一项都没勾，先在这份清单里勾出要删哪些', 'err.cleanNothingPicked'],
  ['这张预览已经用过了（一次凭据只能清空一次），请重新预览', 'err.cleanTicketUsed'],
  ['这次预览里没有「{id}」这一项，清空不做（对象可能已经变了，请重新预览）', 'err.cleanTicketMiss'],
  ['这份预览放了超过 {n} 分钟，Docker 上的东西这几分钟里可能已经变了——请重新预览再清空', 'err.cleanTicketExpired'],
  ['资源已清理，但这几个版本没能卸载：{list}', 'err.cleanUninstallPartial'],
  ['一行都没勾，先勾出要清掉哪几行再预览', 'err.previewNoRows'],
  ['这次没能读到 Docker 的清单，先重新扫描一次再预览', 'err.previewNoInventory'],
  ['这几行里没有可以去删的对象（多半是还没点「详细扫描」，或这一行的占用本来就来自别处）', 'err.previewNoObjects'],
  ['清单里没有「{row}」这一行，这次预览不做（行名对不上等于拿一份没人扫过的单子去删东西）', 'err.previewUnknownRow'],
  ['「{row}」这一行 phpo 没有安全的删法，只给说明不给删除（界面不该给出这颗按钮）', 'err.previewInfoOnlyRow'],

  // —— 备份与恢复
  ['归档缺少数据库快照 db/phpo.db，已中止恢复（未清空容器、未落盘）', 'err.restoreMissingSnapshot'],
  ['转储结果为空', 'err.dumpEmpty'],
  ['打开归档快照失败', 'err.restoreOpenSnapshot'],
  ['非法备份文件名: {name}', 'err.backupNameIllegal'],
  ['{path} 不存在', 'err.pathMissing'],

  // —— 服务配置与目录准备
  ['未知配置文件名: {name}', 'err.unknownConfigFile'],
  ['创建目录失败 {kind}/{version}', 'err.workdirMkdir'],
  ['写入配置失败 {name}', 'err.workdirWriteConf'],
  ['准备挂载目录失败 {path}', 'err.workdirMountDir'],
  ['备份原配置失败 {name}', 'err.confBackupFail'],
  ['创建配置目录失败 {dir}', 'err.confMkdirFail'],

  // —— 运行态存储
  ['生成 SQLite 快照失败', 'err.snapshotFailed'],
  ['回滚 {name} 失败', 'err.migrateRollback'],
  ['迁移文件名不合规 {name}', 'err.migrateFileName'],
  ['迁移 {name} 缺少 -- down 段', 'err.migrateNoDown'],
  ['迁移 {name} 失败', 'err.migrateFail'],
  ['找不到版本 {v} 的迁移文件', 'err.migrateMissing'],
  ['创建数据目录失败', 'err.storeMkdir'],
  ['打开 SQLite 失败', 'err.storeOpen'],

  // —— 任务队列
  ['任务内不得嵌套提交任务', 'err.nestedTask'],
  ['同一操作已在任务队列中', 'err.duplicateQueued'],

  // —— 下载与模板渲染
  ['下载失败 {url}: HTTP {code}', 'err.downloadHttpStatus'],
  ['渲染 {kind}/{name} 失败: {err}', 'err.templateRender'],

  // —— 应用升级（SHA256 + Ed25519 双校验；发布源不可达不影响使用）
  ['updater: 发布清单返回 {code}', 'err.updateManifestStatus'],
  ['updater: 解析发布清单失败', 'err.updateManifestParse'],
  ['updater: 下载返回 {code}', 'err.updateDownloadStatus'],
  ['清单既无 assets 也无 url', 'err.manifestNoAssets'],
  ['清单内无本机平台（{os}）的安装包', 'err.manifestNoPlatform'],
  ['清单缺 version 字段', 'err.manifestNoVersion'],
  ['未配置清单地址', 'err.manifestNoURL'],
  ['updater: 所有发布源均不可用（{why}）', 'err.updateSourcesDown'],
  ['updater: 未配置任何发布源', 'err.updateNoSources'],
  ['updater: 未配置发布源，升级检查不可用', 'err.updateCheckUnavailable'],
  ['{err}; 回滚: {rb}', 'err.updateFailedRollback'],
  ['updater: 签名校验失败', 'err.updateSignFail'],
  ['updater: 未配置签名公钥', 'err.updateNoPubkey'],
  ['updater: Ed25519 公钥长度错误', 'err.updatePubkeyLen'],
  ['updater: SHA256 校验失败', 'err.updateShaFail'],

  // —— Nginx 与 hosts
  ['nginx reload 失败', 'err.nginxReloadFailed'],
  ['nginx -t 失败', 'err.nginxTFailed'],
  ['落盘 vhost 失败', 'err.vhostWriteFail'],
  ['nginx 配置校验未通过，未写入 {file}', 'err.vhostRejectWrite'],
  ['nginx 配置校验未通过，已回滚 {file}', 'err.vhostRejectRollback'],
  ['读取 hosts 失败', 'err.hostsReadFail'],
  ['osascript 提权写入失败', 'err.hostsMacFail'],
  ['polkit 提权写入失败', 'err.hostsPolkitFail'],
  ['UAC 提权写入失败', 'err.hostsUacFail'],
  ['系统无 pkexec（polkit），无法自动提权', 'err.hostsNoPkexec'],

  // —— 归档打包
  ['归档前缀不能为空', 'err.tarPrefixEmpty'],
  ['创建归档目录失败', 'err.tarMkdir'],
  ['创建归档文件失败', 'err.tarCreate'],
  ['打开归档失败', 'err.tarOpen'],
  ['读取 {path} 失败', 'err.tarReadEntry'],
  ['读取 tar 头失败', 'err.tarReadHeader'],
  ['解压 gzip 失败', 'err.tarGunzip'],
  ['拒绝绝对路径条目: {p}', 'err.tarAbsEntry'],
  ['拒绝路径穿越条目: {p}', 'err.tarTraversalEntry'],
  ['收尾 gzip 失败', 'err.tarGzipClose'],
  ['收尾 tar 失败', 'err.tarClose'],

  // —— 容器引擎健康（doctor 与环境门直接取这两句）
  ['当前用户无权访问容器引擎。（{err}）', 'err.engineNoPermission'],
  ['未检测到容器引擎（Docker 或 Podman）。（{err}）', 'err.engineNotInstalled'],
  ['容器引擎未运行。（{err}）', 'err.engineNotRunning'],
  ['无法连接容器引擎：{err}', 'err.engineConnect'],
  ['容器引擎版本较旧（{ver} < 20.10），部分功能可能不可用。', 'err.engineOld'],
  [
    '未检测到容器引擎，请任选其一安装：Docker（sudo apt install docker.io，或按 docs.docker.com 安装 docker-ce）或 Podman（sudo apt install podman，装后执行 systemctl --user enable --now podman.socket）。',
    'err.hintInstallLinux',
  ],
  ['未检测到容器引擎，请安装 Docker Desktop（{url}）或 Podman（podman.io）。', 'err.hintInstallDesktop'],
  ['请启动容器引擎：Docker（sudo systemctl start docker）或 Podman（systemctl --user start podman.socket）。', 'err.hintStartLinux'],
  ['请启动 Docker Desktop 或 Podman machine（podman machine start）。', 'err.hintStartDesktop'],
  [
    '当前用户无权访问容器引擎 socket。Docker：请把用户加入 docker 组（sudo usermod -aG docker $USER 后重新登录）；Podman：请启用用户级 socket（systemctl --user enable --now podman.socket）。',
    'err.hintPermLinux',
  ],
  ['当前用户无权访问容器引擎。请重新启动 Docker Desktop / Podman machine；仍不行时以管理员身份运行本应用。', 'err.hintPermDesktop'],
  [
    '请确认容器引擎已就绪（Docker：sudo systemctl start docker；Podman：systemctl --user start podman.socket）且端点可达后重试。',
    'err.hintConnectLinux',
  ],
  ['请确认 Docker Desktop 或 Podman machine 已启动后重试。', 'err.hintConnectDesktop'],
  ['是否继续？', 'err.hintAskContinue'],
]

const FRAGMENTS: ReadonlyArray<readonly [string, string]> = [
  ['未指定', 'err.fragUnspecified'],
  ['其他程序', 'err.fragOtherProgram'],
  ['本机其它进程', 'err.fragLocalProcess'],
  ['docker 未安装', 'err.fragDockerMissing'],
  ['docker 未运行', 'err.fragDockerDown'],
  ['docker socket 权限不足', 'err.fragDockerPerm'],
]
const esc = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

interface Compiled {
  re: RegExp
  code: string
  keys: string[]
  literal: number
}

// 形状里的 {名字} 换成一段捕获组，其余字面量原样锚定整行——认不出就什么都不是，不做「差不多就算」。
function compile(zh: string, code: string): Compiled {
  const parts = zh.split(/\{(\w+)\}/)
  const keys: string[] = []
  let src = ''
  let literal = 0
  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 0) {
      src += esc(parts[i])
      literal += parts[i].length
      continue
    }
    keys.push(parts[i])
    src += '([\\s\\S]+?)'
  }
  return { re: new RegExp('^' + src + '$'), code, keys, literal }
}

// 越具体的形状先比：不带参数的整句优先，其次字面量更长的优先。
// 少了这一步，「镜像源地址不正确: {value}」会把「镜像源地址不正确: 不能为空」那一句吞掉。
const TABLE = SIGNALS.map(([zh, code]) => compile(zh, code)).sort(
  (a, b) => a.keys.length - b.keys.length || b.literal - a.literal,
)

export const FRAGMENT_CODES = new Map<string, string>(FRAGMENTS.map(([zh, code]) => [zh, code]))

export const HAN = /[\u4e00-\u9fff]/

export interface ShapeMatch {
  code: string
  params: Record<string, string>
}

// Wails v3 把 Go service 调用的 error 包成 RuntimeError 抛给 JS，String(err) 就是
// 「RuntimeError: <后端原句>」——不剥前缀，锚定整行的形状匹配永远打不中。
// 只在匹配时剥；认不出时调用方返回原行，前缀不丢（它是调用栈信息，不是要隐藏的东西）。
const WAILS_ERR_PREFIX = /^(?:RuntimeError|Error):\s*/

// 整行形状匹配：命中返回码与原始捕获参数（未渲染），不命中返回 null。
export function matchShape(line: string): ShapeMatch | null {
  const stripped = line.replace(WAILS_ERR_PREFIX, '')
  for (const e of TABLE) {
    const m = e.re.exec(line) ?? (stripped !== line ? e.re.exec(stripped) : null)
    if (!m) continue
    const params: Record<string, string> = {}
    e.keys.forEach((k, i) => {
      params[k] = m[i + 1]
    })
    return { code: e.code, params }
  }
  return null
}
