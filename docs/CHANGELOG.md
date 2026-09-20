# 更新日志

> 遵循 Keep a Changelog 精神，按里程碑（M0–M7）记录 phpo 的演进。版本号策略见 [版本策略](./版本策略.md)（此处指**应用自身**版本，比较用 `pkg/version/semver`）。当前应用版本 `0.1.0`。冲突以 AGENTS.md 为准。

## [未发布 / M7 收尾]

- **配置存储统一为单一 YAML（AGENTS.md 授权变更至 v2.9.0）**：全部 `env` 读写迁入 `config.yaml`（落 XDG 用户配置目录 `os.UserConfigDir()/phpo/config.yaml`，Linux `~/.config/phpo`），路径/密码/端口集中一处，消除旧 `~/phpo/.env` + `~/.phpo/config.json` 双份散落与失同步；`internal/config/env.go`、`internal/store/password.go`、SQLite `env` 表移除，新增 `internal/config/configstore.go` 门面；SQLite 退居纯运行态；快照 `env` 由 `ConfigStore.FlatEnv()` 合成，前端 `app.env.*` 键名契约不变；备份恢复改经 `ConfigStore.Reload()` 热重载；clean switch，不迁移旧数据。`.env.example` → `config.example.yaml`。
- **文档定稿（T704）**：`docs/` 全中文专项文档 + 用户手册（本目录 30 篇）。
- **真机冒烟（T705，待办）**：三平台安装/卸载/升级/回滚端到端，交由用户/CI 环境执行。

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
