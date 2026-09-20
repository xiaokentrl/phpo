# 更新日志

> 遵循 Keep a Changelog 精神，按里程碑（M0–M7）记录 phpo 的演进。版本号策略见 [版本策略](./版本策略.md)（此处指**应用自身**版本，比较用 `pkg/version/semver`）。当前应用版本 `0.1.24`。冲突以 AGENTS.md 为准。
>
> **应用版本单一真实来源**：`wails.json` 的 `info.productVersion`。`scripts/bump-version.sh [patch|minor|major]`（默认 patch）改写它并同步 `build/linux/nfpm.yaml` 的 `version`（deb/rpm 包内版本）；运行时基准由 Taskfile 经 `-ldflags "-X phpo/internal/app.Version=$(bash scripts/version.sh)"` 注入 `internal/app/di.go`。因此每次 `task release:local` 出包都会让 patch +1，使安装包可区分、可覆盖升级。

## [未发布 / M7 收尾]

- **新建站点取消 nginx 门禁（只告警 + 降级），并在装/启 nginx 后自动补齐；弹窗回显 vhost 宿主路径；建站成功即刻刷新列表**：上一条把 nginx 立为建站唯一阻断项，结果是没装 nginx 就一个站点都建不了——与「最小限制：能警告的不要阻止」相悖。现**建站不设任何服务门禁**：nginx 未装 / 未运行、PHP 未装一律只出降级告警，站点照常落库、建站点目录、写 hosts，仅跳过 vhost 落盘与端口发布。告警文案前后端逐字对齐（后端 `rules_site.go#nginxPendingWarn` / 前端 `usePreflight.ts` 同名函数，未装说「安装 Nginx 后自动补齐」、未运行说「启动 Nginx 后自动补齐」）。
  - **降级必须能自愈**，否则「取消门禁」只是把站点建成死档：就绪判据从「PHP 已装 + 端口空闲」扩为三元 `siteServeReady`（**nginx 已装且在运行** + 所选 PHP 已装 + 所选端口未被占用），`serveReady` 与 `ReconcileServe` 共用同一份快照。nginx 走 `docker exec` 跑 `nginx -t` / `reload`，只装不运行同样写不了 vhost，故判据取 Running 而非 Installed。
  - **事件驱动补齐**：`AppService` 新增 `SiteHealer` 注入点（DI 接 `*SiteService.ReconcileServe`），`Install` / `Start` 仅在 `kind=nginx` 时追加「补齐站点 vhost」一步——逐站点重判就绪、补写缺失的 conf、reload、按发布集重发布端口、广播；其余服务不触发。补齐失败只记任务日志、不判整个安装任务失败（nginx 已装好这件事是真的，站点仍可经下次写操作再补），并把失败域名汇总进日志，不静默。`ReconcileServe` **不走 task.Manager**：它由 nginx 安装任务内联调用，而任务管理器是单飞的，再 `Run` 一次只会拿到 `ErrBusy`。
  - **弹窗回显真实宿主路径**：`SiteAddModal` 在「站点目录」的宿主路径行下方追加一行 vhost 绝对路径 `{NGINX_SITES_ROOT}/{域名}.conf`（与 `vhost.Manager#Path` 同口径，`NGINX_SITES_ROOT` 已随 `FlatEnv()` 出口展开为真路径），域名未填时以 `<domain>` 占位。新增 `siteAdd.vhostPath` 中英 2 键。
  - **建站成功立刻同步**：`submit()` 由「发完不管」改为 `await addSite(...)` → `await syncState()`，新站点当场出现在列表，不再依赖 `state:changed` 的事件时序；失败仍走 `toast` 上抛。
  - 新增/改写单测 `TestSiteAddWithoutNginxWarn`、`TestSiteAddNginxNotRunningWarn`（替换原 `TestSiteNeedNginx`）、`TestSiteAddNeedsNoService`（原 `TestSiteAddNeedsOnlyNginx`）、`TestSiteService_Add_DegradesWithoutNginx`、`TestSiteService_ReconcileServe_HealsAfterNginxReady`、`TestAppService_NginxReadyHealsSites`；`readyWorld()` / `newFakeSiteStore()` 补 nginx 已装且运行的前置。文档同步：`最小限制原则.md` 门禁行、`端口策略.md` 落地链路与自愈两条、`用户手册.md` 建站章节。
- **新建站点端口占用不再擅自改用户所填端口：只告警 + 站点降级（AGENTS.md 授权变更至 v2.9.2）**：`site-add` 此前对占用端口执行「顺延首个可用」（`AutoAdvance`），静默把用户填的 80 改成 81，与「用户是程序员、所见即所填」相悖。现分三档：**新建站点**端口占用 → 保留原端口、只出告警，站点照常创建但**降级**（跳过 vhost 落盘与端口发布——端口已被占用，发布会让 nginx 绑不上；硬红线 2 同时要求上游 PHP 容器存在，否则 `nginx -t` 必失败）；**改已有站点**端口 → 仍顺延 1–65535 首个可用（不报错）；**服务端口**占用 → 仍报 `portInUse`。降级判据与 PHP 缺失降级共用一条路径：`SiteService.serveReady`（所选 PHP 已装 **且** 所选端口未被占用）与 preflight 的 `conflictKeepWarn` 同源取自 `store.CollectUsedPorts`；腾出端口后 `SwitchPHP` / `SetPort` / `SetRewrite` 任一次即经 `writeVHost` 自愈补写。`pkg/port` 新增 `Options.KeepOnConflict` 与 `Result.Occupied`，`internal/preflight` 的 `validatePort` 布尔参改为 `portConflict` 三值枚举；前后端告警文案逐字对齐（`usePreflight.ts` 新增 `portDegradeWarn`）；建站弹窗不再回填 `adjusted.port`，改为 `DangerConfirm` 弹框提示（`siteAdd.port.hint` 中英 2 键同步改口径）；doctor「80 端口可用」一项的告警文案同步改为降级说明。新增/改写单测 `TestValidateKeepOnConflict`、`TestSiteAddOccupiedPortDegrades`、`TestSiteAddFreePortNoWarn`、`TestSiteAddBadPortStillBlocked`（替换原 `TestSiteAddAdvancePort`）、`TestSiteService_Add_DegradesOnPortConflict`、`TestSiteService_AddPortConflictSkipsPublish`、`TestSiteService_DegradedSitePortNeverPublished`。同时封掉发布集漏洞：降级站点库里仍记着端口，此前 `RepublishNginx` 的端口集取自全量站点，任一次重发布都会把降级站点的端口（可能正是 mysql/pgsql/redis 已占用的端口）带给 nginx 容器去抢绑，绑不上即全站瘫痪。现 `SiteService.publishPorts` 只计入 **vhost 已落盘**的站点端口，并剔除数据服务占用端口；数据服务端口表从 `store.CollectUsedPorts` 抽出 `store.CollectServicePorts` 作为单一来源，两处共用。
- **新建站点：工作根路径不再以 `~` 出口（修「无法找到 /…/build/bin/~/www」）**：`ConfigStore.FlatEnv()`（快照 `env` 的唯一合成点）此前直接取原始持久值，`~` 前缀原样进前端；`SiteAddModal` 把它当绝对路径交给原生目录选择器，被按当前工作目录解析成 `build/bin/~/www`。同样问题在 `App.HomeDefaults()`（向导回显）也存在。现统一在**出口处展开**：`FlatEnv()` 经 `ExpandEnvHomes(DerivePaths(...))` 合成全部派生路径，`HomeDefaults()` 返回 `ExpandEnvHomes(container.Env)` 的 PHPO_HOME / WWW_ROOT；`config.yaml` 内仍原样存 `~`（跨机可迁移不变）。展开只发生在出口，落盘语义与 `RootsPersisted/RootsReady` 判据不受影响。新增单测 `TestFlatEnv_RootsAlwaysExpanded`（含默认根与全部派生子键）、`TestHomeDefaults_RootsExpanded`。
- **新建站点只以 nginx 为服务门禁，PHP 缺失降级不阻断（最小限制原则）**（本条的「nginx 未装仍是唯一阻断项」已被最上一条取代：现 nginx 缺席也只告警）：`site-add` 此前要求「已装 PHP」且「所选 PHP 版本必须已安装」，两项均为阻断错误，导致只有 nginx 时无法建站。现改为：nginx 未装仍是唯一阻断项；PHP 未选/未装只出告警，站点照常落库、建站点目录、写 hosts，**仅跳过 vhost 落盘**——上游 `php-{version}-fpm:9000` 的容器不存在时 DNS 解析不了，`nginx -t` 必失败（硬红线 2），写出即回滚，站点根本建不成。降级可自愈：安装 PHP 后切换版本走 `writeVHost`，vhost 自动补写。同一判定前后端对齐（`rules_site.go#siteAdd` / `usePreflight.ts` 的 `site-add`），建站弹窗在无 PHP 时显示「未安装 PHP」占位与说明（新增 `siteAdd.phpVersion.none/hint` 中英 2 键）。新增单测 `TestSiteAddNeedsOnlyNginx`、`TestSiteAddPhpNotInstalledWarn`、`TestSiteService_Add_DegradesWithoutPhp`。
- **「已设置」弹框与主界面同步归位，刷新失败即重启兜底**：`useStateSync.ts` 新增公共同步入口 `syncState()`（取一次权威快照走同一 `applySnapshot`，返回是否落地；启动快照、侧栏「同步状态」`resync`、装机向导三处复用，删除各自的 `getState + applySnapshot` 复制）。`HomeSetupWizard` 在两条路径上都把它作为**第一件事**：挂载读到 `CONFIGURED=true` 时、以及 `HomeEnsure` 成功后立即 `syncState()`，弹框与主界面同一时刻归位（此前只等 `state:changed` 时序，界面仍停在未就绪态、每次写操作被弹回向导）。同步后 `homeReady` 仍为 false → 说明本会话的对象图已按旧根展开（`di.go` 九处按值持有 `env`），刷新无法收敛，升级为请后端 `App.Restart()`：置 `restartPending` → `Quit()` → `ServiceShutdown` 释放资源后 `relaunchSelf()` 以子进程按 `config.yaml` 重建，子进程带 `PHPO_RESTARTED=1` 标记，第二代再请求重启即拒绝（防重启循环）。弹框内以 `busy` 明示「正在同步主界面 / 正在重启应用」（新增 `wiz.syncing` / `wiz.restarting` 中英 2 键），同步期间「完成」禁用。新增单测 `app_test.go`：带标记拒绝重启且不误置 `restartPending`、宿主未注入返回 `errNotReady`。
- **装机向导先决检测「工作目录已设置」，禁止重复创建**：`HomeEnsure` 原跳过判据是「请求路径 ≡ 已持久化路径」，只要向导输入与已设目录不同就会**再建一套工作目录并覆盖 `config.yaml`**。现改为后端统一判据 `WizardService.Configured()`（两根「已持久化 + 目录实际存在」，与快照 `dirReady` 同源 `ConfigStore.RootsReady()`）：已设置一律只广播权威快照，不建任何目录、不改写配置。向导挂载即经 `HomeDefaults()` 新增的 `CONFIGURED` 键检测（`map[string]string` 加键，无需重生成 bindings），已设置则只回显当前两根 +「完成」，三步流程、「验证」「确认并创建」全部不出现（新增 `wiz.already.*` 中英 2 键）。顺带封掉同类旁路：doctor 的「PHPO_HOME / WWW_ROOT 可写」两项原走 `dirWritable`（会 `MkdirAll`），改用只读 `dirProbeWritable`，目录不存在只报错不建。`WizardConfig` 接口随之收为 `SetRoots + RootsReady`（`Roots()` 与 `alreadyReady`/`dirExists` 变为死码，删除）。新增单测：`TestHomeEnsure_RefusesSecondWorkingDir`（已设置时传不同路径 → 新目录不出现、`SetRoots` 零调用、仍广播）、`TestDirProbeWritable_CreatesNothing`。
- **装机向导「验证」改为纯只读预检（目录/文件只在「确认并创建」后产生）**：原 `HomeVerify` 走 `ensureTree → dirWritable`，而 `dirWritable` 会 `os.MkdirAll` 并写入 `.phpo-writetest` 探测文件——点「验证」时工作目录子树与文件已被真创建，「确认并创建」形同走过场。现拆为两条路径：`HomeVerify` 只 `os.Stat` 判存在 + 按属主权限位判可写（不存在但最近已存在祖先可写 → 报「○ …（确认后将创建）」），**零 `MkdirAll`、零探测文件**；`ensureTree`（`MkdirAll` + 试写）只由 `HomeEnsure` 调用。UI 文案随之改实：第三步「验证并创建」→「只读验证」、按钮 `dir.verify` →「验证」（中英各 2 键）。新增单测锁死该不变量：验证后临时目录仍为空、根目录不存在、祖先不可写时报错且不落盘，确认后才建出子树。
- **首启零落盘：`dirReady` 改派生 + 运行态库延迟创建（AGENTS.md 授权变更至 v2.9.1）**：装机向导完成前不再在用户数据目录留下任何文件——`store.New` 只记路径，首次访问才「建父目录 → 建库 → 迁移」，门禁取自 `ConfigStore.RootsPersisted()`（两根是否已写入 `config.yaml`）；未就绪时读写统一返回 `ErrHomeNotSet`（文案复用 `errs.HomeNotReady`）。SQLite `dir_ready` 表下线（迁移 `0007_drop_dir_ready.sql`，原为每次启动被覆盖的派生缓存，删除不丢信息），快照 `dirReady` 改由 `ConfigStore.RootsReady()` 按「已持久化 + 目录实际存在」逐根派生：目录被删即回落 `false` 重新拦截写操作，而已装状态照常可读、不回退；`wizard_service.RefreshDirReady/markReady/SetDirReady` 移除，启动校准 `Calibrate` 加两根门禁。
- **装机向导收口交互（禁止目录设置与后续写操作串联）**：`useModals` 的 `openInstallModal`/`openSiteAddModal` 在两根未就绪时只弹装机向导，不再携带完成后接续安装/建站的回调（`onReady` 参数移除）；`HomeSetupWizard` 在 `HomeEnsure` 成功后就地展示「工作目录设置成功」并回显 PHPO_HOME / WWW_ROOT 终值，用户点「完成」才关闭窗口返回主界面（新增 3 个 `wiz.done.*` i18n 键，`dir.confirm` 文案由「确认并继续」改为「确认并创建」以消除接续语义）。
- **配置存储统一为单一 YAML（AGENTS.md 授权变更至 v2.9.0）**：全部 `env` 读写迁入 `config.yaml`（落 XDG 用户配置目录 `os.UserConfigDir()/phpo/config.yaml`，Linux `~/.config/phpo`），路径/密码/端口集中一处，消除旧 `~/phpo/.env` + `~/.phpo/config.json` 双份散落与失同步；`internal/config/env.go`、`internal/store/password.go`、SQLite `env` 表移除，新增 `internal/config/configstore.go` 门面；SQLite 退居纯运行态；快照 `env` 由 `ConfigStore.FlatEnv()` 合成，前端 `app.env.*` 键名契约不变；备份恢复改经 `ConfigStore.Reload()` 热重载；clean switch，不迁移旧数据。`.env.example` → `config.example.yaml`。
- **文档定稿（T704）**：`docs/` 全中文专项文档 + 用户手册（本目录 30 篇）。
- **真机冒烟（T705，待办）**：三平台安装/卸载/升级/回滚端到端，交由用户/CI 环境执行。
- **版本基准推进至 `0.1.12`**：`0.1.7`–`0.1.12` 是 T705 Linux 冒烟期间反复 `task release:local` 出包（`bump-version.sh` 每次 patch +1，用于验证 dpkg 覆盖升级与降级回滚）累积所得，**不含额外功能**，保留不回退；正式对外发布仍从 `release.yml` 手填版本号并经 `scripts/sign-release.sh` 签名 + `checksums.txt`。

## [0.1.0] — M7 发布脚手架

- **打包资源（T701）**：三平台 build 资源就位——`build/windows/nsis/installer.nsi`、`build/darwin/{Info.plist,entitlements.plist,icon.icns}`、`build/linux/{nfpm.yaml,phpo.desktop,postinstall.sh,icons/}`、`build/appicon.png`；Taskfile `build/package[:linux|:windows|:darwin]`（wails3 委托 go-task 模型）。
- **签名与校验（T702）**：`scripts/sign-release.sh`（genkey/pubkey/sign，Ed25519 over sha256-hex，对齐 `updater/verifier.go`）、`scripts/gen-checksums.sh`、`scripts/check-cache-manifest.go`（manifest JSON 标签对账）、公钥嵌入；`task check` 首次四脚本全绿。
- **一键出包（T703）**：`.github/workflows/release.yml`（matrix ubuntu/windows/macos + 汇总签名 + `softprops/action-gh-release`），actionlint 通过。

## [M6] — 运维面（T601–T608）

- 扩展离线缓存链路（T601）；备份/恢复/删除（T602）；doctor 15 项诊断（T603）。
- 应用升级闭环（T604）：下载 → SHA256+Ed25519 双校验 → 备份 → 安装 → 失败回滚 + 前端 updaterStore/UpdateModal。
- 清洁三模式 + 回收站 + 审计 JSON Lines（T605）；离线缓存 OfflineView 真数据化（T606）。
- 装机向导 HomeVerify/HomeEnsure + 首启拦截（T607）；M6 集成验收（画像 F 断网全链路 live + 风险回访，T608）。

## [M5] — 数据服务（T501–T506）

- MySQL / PostgreSQL / Redis 全生命周期（T501–T503）+ 密码/端口 UI 明文接真（T504）。
- 服务配置保存链路：备份 → 写 → 失败回滚（T505）；跨服务联通冒烟 PHP↔MySQL↔Redis↔站点目录（T506）。
- 增强：站点端口发布到 nginx 容器（建站/改端口/删站重发布端口并集）。

## [M4] — 站点闭环（T401–T407）

- vhost 管理 + hosts 三平台提权（T401–T402）；建站/删站（T403）；端口顺延 UI 闭环（T404）。
- PHP 切换十步精确上游（硬红线 1，T405）；伪静态 + vhost 手改 UI（T406）；真实 Docker 端到端验收（硬红线 1/2，T407）。

## [M3] — 服务线 PHP + Nginx（T301–T309）

- Docker 客户端与可用性前置（T301）；镜像 load/pull/save（T302）；缓存优先安装管道（T303）。
- 容器/网络/卷装配（T304）；幂等三阶段 Pre-Clean/Execute/Post-Verify（T305）；状态校准 + 生命周期服务（T306）。
- 模板引擎 5 服务 7 文件 + vhost（T307）；取消/回滚真化（T308）；M3 集成验收（T309）。

## [M2] — 底座（T200–T213）

- model + config（paths/versions/password）+ 单测（T200–T203）；pkg/port 顺延 + store/SQLite + 迁移（T204–T205）。
- preflight 框架 + 17 action 规则（T206–T209）；task 三段式引擎（T210）；internal/cache 离线缓存核心（T211）；updater checker 骨架（T212–T213）。
- 端口策略更新为「无窗口上限、去耗尽报错」，AGENTS.md 授权变更至 v2.8.0。

## [M1] — 骨架（T101–T112）

- Wails 3 + Go 工程骨架、事件总线、托盘（T101、T110–T111）。
- 前端拆分：Vue3.5+TS5+Vite5+Pinia2、10 路由、i18n、6 主题、缩放、11 视图 1:1、12 模态、命令面板 20、日志抽屉（T102–T109）；M1 集成验收（T112）。

## [M0] — 规格冻结

- AGENTS.md v2.7.0 冻结生效；原型冲突修复 + 残留修补，`index.html` 与 SSOT 同步。

## 说明

- 里程碑工单粒度=单 PR 可验收；详细分解见根目录 `任务工单.md` / `实施顺序.md`。
- 更早的逐提交历史以 `git log` 为权威，本文仅按里程碑聚合，不逐条复述。
