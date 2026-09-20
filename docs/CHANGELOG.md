# 更新日志

> 遵循 Keep a Changelog 精神，按里程碑（M0–M7）记录 phpo 的演进。版本号策略见 [版本策略](./版本策略.md)（此处指**应用自身**版本，比较用 `pkg/version/semver`）。当前应用版本 `0.1.19`。冲突以 AGENTS.md 为准。
>
> **应用版本单一真实来源**：`wails.json` 的 `info.productVersion`。`scripts/bump-version.sh [patch|minor|major]`（默认 patch）改写它并同步 `build/linux/nfpm.yaml` 的 `version`（deb/rpm 包内版本）；运行时基准由 Taskfile 经 `-ldflags "-X phpo/internal/app.Version=$(bash scripts/version.sh)"` 注入 `internal/app/di.go`。因此每次 `task release:local` 出包都会让 patch +1，使安装包可区分、可覆盖升级。

## [未发布 / M7 收尾]

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
