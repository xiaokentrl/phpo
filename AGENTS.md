# phpo 项目总纲（MASTER PLAN）

> **文档类型**：最高项目总纲
> **文档版本**：v2.9.3
> **生效状态**：FROZEN（冻结，禁止未走评审流程修改）
> **效力等级**：★★★ 最高（本项目所有其他文档、代码、注释、测试必须与本文件一致）
> **适用范围**：全体开发者 · CI/CD 流水线 · AI Agent
> **唯一真实来源**：单文件 HTML 原型（前端唯一界面来源.txt）
> **技术栈约束**：Wails ≥ 3 + Go ≥ 1.27 + Vue 3.5+ + TypeScript 5.x
> **目标产物**：Windows / macOS / Linux 三平台桌面应用（**仅 GUI，不提供 CLI**）
> **核心原则**：以真实开发者工作流为标准；最小限制；用户是程序员；**离线优先**
> **端口策略**：站点端口默认 80，用户可指定任意端口；**新建站点端口被占用时不顺延**——仅弹框告警并把站点降级（vhost 暂不落盘、端口暂不发布），站点照常创建；改已有站点的端口时占用才顺延 1–65535 首个可用（不报错、无窗口上限）；服务端口占用仍报错
> **应用升级**：支持版本检查和自动升级
> **配置存储**：单一 `config.yaml`（YAML）落在各平台 XDG 用户配置目录内的 `phpo` 子目录，承载工作根目录 + 每服务版本的明文密码/宿主端口；SQLite 仅存运行态，且**延迟建库**——两根工作目录写入 `config.yaml` 前不创建用户数据目录；`dirReady` 不落库，由快照按「两根已持久化 + 目录实际存在」派生；不再使用 `~/phpo/.env` 或 `~/.phpo/config.json`
> **密码策略**：明文，默认 `123456`，可修改，可为空，长度不校验，UI 可查看
> **最小限制原则**：除 8 条硬红线外，所有限制放开或降级为警告
> **Docker 清洁原则**：所有操作幂等、原子、可回滚、可清理
> **离线缓存原则**：装任何镜像/扩展必先查缓存 · 命中零网络 · 未命中临时下载编译 · 成功后提升到缓存 · 无论成败均清空临时目录 · 断网重装靠缓存 · 内网开发靠缓存
> **编码准则**：**编码前思考 · 简洁优先 · 精准修改 · 目标驱动执行**（v2.7 新增，见 §3.4）

---

## 0. 文档使用规约

### 0.1 效力声明

本文件为 **phpo 项目的最高项目总纲**。当本文件与其他任何文档、注释、口述、历史草案冲突时，**一律以本文件为准**。

### 0.2 Agent 行为约束

1. **禁止修改本文件**：除非用户在当次会话中显式要求「更新总纲」。任何修改必须同步递增 `文档版本`。
2. **禁止臆测数字**：本文件中的每项数字均已逐行核对原型源码。Agent 不得凭印象改写。
3. **禁止技术栈漂移**：不得以「Tauri 更轻」「Node 迁移更容易」等理由替换技术栈。
4. **禁止引入 CLI**：本项目仅提供 GUI 桌面应用。
5. **禁止引入密码加密**：密码明文存储，默认 `123456`，允许为空，长度不校验。
6. **禁止校验密码格式**：不允许校验密码长度、复杂度、非空。
7. **禁止限制服务版本**：各服务版本开放给用户自由输入。
8. **禁止限制版本号字符集**：仅做路径安全校验。
9. **禁止 PHP 切换模糊匹配**：必须精确字符串 `php-{version}-fpm:9000`。
10. **禁止状态不同步**：后端是唯一权威；前端只订阅事件、不做乐观更新。
11. **禁止旁路写操作**：所有写操作走「preflight → task → applyStateChange」三段式。
12. **禁止跨层引用**：`internal/` 内部严格单向依赖。
13. **禁止手改自动生成目录**：`frontend/bindings/` 由 `wails3 generate bindings` 生成。
14. **禁止跳过 preflight**：UI 层校验仅作即时反馈，最终裁决在 `internal/preflight/`。
15. **禁止对用户操作设置非必要限制**：除非违反 8 条硬红线，否则一律放行。
16. **禁止把「警告」当「阻止」**：能警告的不要阻止；能放行的不要拦。
17. **禁止把端口硬编码为 80**：站点端口走「默认 80 + 用户可指定任意端口」逻辑；**新建站点端口被占用只弹框告警 + 站点降级，不得擅改用户所填端口**；改已有站点的端口才走「顺延 1–65535 首个可用（不报错）」。
18. **禁止跳过应用升级校验**：升级包必须验证 SHA256 + 签名。
19. **禁止对 Docker 留下脏状态**：任何操作前必须先清理同名/冲突资源；任何操作失败必须回滚。
20. **禁止破坏用户数据**：卸载默认保留 volume；删除 volume 必须二次确认；回收站机制。
21. **禁止绕过离线缓存**：
    - **安装任何 Docker 镜像（php/mysql/pgsql/redis/nginx 等）时，必须先查 `~/phpo/offline/{kind}/{version}/` 目录**；命中则 `docker load`（零网络）；未命中才 `docker pull`。
    - **安装任何 PHP 扩展（apk/pecl）时，必须先查 `~/phpo/offline/php/{version}/{apk|pecl}/` 目录**；命中则直接使用（零网络）；未命中才网络下载到临时目录。
    - **临时目录路径固定为 `~/phpo/{kind}/{version}/ext/`**（如 `~/phpo/php/8.4/ext/`）。
    - **未命中时下载到临时目录 → 编译/加载 → 成功后立刻把文件从临时目录提升到缓存目录 → 无论成功失败，必须清空临时目录**。
    - **编译失败 → 直接清空临时目录 → 报错，不进缓存**。
    - **任务取消 → 直接清空临时目录**。
    - **应用启动时 → 扫描并清空残留临时目录**。
    - **清空临时目录的目的是防止污染下次使用**。
    - **不允许静默跳过离线缓存检查**。
    - **不允许编译成功后不清空临时目录**。
    - **不允许编译失败后保留临时目录**。
22. **发现冲突时的处理**：立即停止执行，向用户报告冲突点，等待裁决。
23. **遵守 §3.4 编码准则**（v2.7 新增）：编码前思考 · 简洁优先 · 精准修改 · 目标驱动执行。

### 0.3 数字权威表（Agent 引用禁止出错）

| 项 | 权威值 | 来源 |
|----|-------|------|
| preflight action 数 | **17** | `internal/preflight/preflight.go` 的 `Run` switch-case 与 `AllActions`（`ActionCount`） |
| NEEDS_HOME 动作数 | **15** | 同文件 `needsHome` map（`NeedsHomeCount`）；对账测试 `TestActionAndNeedsHomeCounts` |
| §5.6 事件名数 | **17** | 事件协议表（冻结，不新增） |
| 任务队列载体 | **`Snapshot.Tasks`（`TaskBoard`）** | 随 `state:changed` 推送，不新增事件名 |
| 任务账本日志保留 | **尾部 500 行** | `internal/task/ledger.go`（`maxLedgerLines`）；写回 `operations` 表（迁移 0008 加 `task_id/label/logs`） |
| SQLite 迁移数 | **8** | `internal/store/migrate/0001–0008.sql` |
| VHosts 方法数 | **9** | `VHosts` 对象方法 |
| 路由数 / 渲染函数数 | **10 / 6** | `NAV` / `VIEWS` |
| SVC_META 服务数 | **5** | `SVC_META` |
| MOUNTS 挂载表数 | **5** | `MOUNTS` |
| REWRITE_PRESETS 数 | **9** | `REWRITE_PRESETS` |
| DEFAULT_CONFIGS | **5 服务 7 文件** | `DEFAULT_CONFIGS` |
| THEMES 数 | **6** | `THEMES` |
| CMD_ITEMS 数 | **20** | `CMD_ITEMS` |
| MESSAGES 键数 | **约 300+** | `MESSAGES['zh-CN']` |
| UI_SCALE 档位数 | **7** | `UI_SCALE.snap` |
| 日志行类型 | **5** | `cmd / meta / ok / dim / err` |
| 任务状态 | **4** | `running / success / failed / cancelled` |
| 原型文件规模 | **约 2500 行** | 单文件统计 |
| 默认密码 | **`123456`** | 本项目策略 |
| 密码长度限制 | **无** | 用户可任意长度（含空） |
| 版本号格式限制 | **仅路径安全** | 不限制字符集 |
| 可用端口范围 | **1–65535** | 用户可指定任意端口 |
| 硬红线数量 | **8** | 见 §3.3 |
| PHP 切换上游格式 | **`php-{version}-fpm:9000`** | 保留版本号原样 |
| 默认站点端口 | **80** | 本项目策略 |
| 新建站点端口冲突 | **不顺延：告警 + 站点降级** | 保留用户所填端口，站点照常创建 |
| 改站点端口顺延范围 | **1–65535 首个可用** | 占用不报错、无窗口上限 |
| 服务端口冲突 | **报错 portInUse** | 服务端口不自动顺延 |
| 升级检查频率 | **启动时 + 每 24 小时** | 本项目策略 |
| 资源命名前缀 | **`phpo-`** | 见 §5.13.2 |
| 网络名 | **`phpo-network`** | 见 §5.13.2 |
| 卷名前缀 | **`phpo-{kind}-{version}-`** | 见 §5.13.2 |
| 回收站保留期 | **7 天** | 见 §5.13.7 |
| 操作审计日志 | **`<用户数据目录>/logs/operations.log`** | 见 §5.13.10 · §4.2 |
| 幂等操作数 | **8** | 见 §5.13.4 |
| 清理模式数 | **3** | 保守 / 标准 / 激进 |
| 离线缓存根目录 | **`~/phpo/offline/`** | 见 §5.14.2 |
| 镜像缓存路径 | **`~/phpo/offline/{kind}/{version}/image.tar`** | 见 §5.14.2 |
| apk 缓存路径 | **`~/phpo/offline/php/{version}/apk/`** | 见 §5.14.2 |
| pecl 缓存路径 | **`~/phpo/offline/php/{version}/pecl/`** | 见 §5.14.2 |
| 临时目录路径 | **`~/phpo/{kind}/{version}/ext/`** | 见 §5.14.2 |
| 缓存清单文件 | **`manifest.json`** | 每个版本一份 |
| 缓存命中优先级 | **离线缓存 > 网络** | 见 §5.14.3 |
| 临时目录生命周期 | **单任务，任务结束必清空** | 见 §5.14.4 |
| 提升时机 | **编译/加载成功后立即提升** | 见 §5.14.4 |
| 失败清理 | **立即清空临时目录** | 见 §5.14.4 |
| 缓存校验方式 | **SHA256** | 见 §5.14.5 |
| 缓存条目数上限 | **无限制** | 用户可手动清理 |

### 0.4 真实场景画像

**画像 A：外包/接单开发者（占比约 50%）**

- 机器：Windows 10/11 + Docker Desktop（WSL2）
- 同时维护 3–8 个客户项目，PHP 版本从 7.4 到 8.4 不等
- 痛点：切项目要切 PHP 版本、客户数据库配置不同、交付要打包环境
- 关键场景：反复安装/卸载 PHP 版本；内网或弱网环境

**画像 B：小团队技术负责人（占比约 30%）**

- 机器：macOS（M 系列芯片）+ Docker Desktop
- 管理 2–3 台开发机，需要新同事当天跑起环境
- 痛点：新同事装机 2–3 天、排查「跑不起来」、团队环境不一致
- 关键场景：新同事首次装机；团队内网环境

**画像 C：独立开发者/学习者（占比约 20%）**

- 机器：Windows 或 macOS，配置一般
- 学习 Laravel / ThinkPHP / WordPress
- 痛点：教程版本不一致、Docker 命令太难、出错不知哪里错
- 关键场景：反复试验；网络不稳定

**画像 D：高级用户/黑客型开发者（占比约 10%）**

- 机器：任意平台，通常配置较高
- 知道自己在做什么，不需要软件替他们做决定
- 关键场景：需要能完全控制 Docker 环境；需要能手动清理缓存

**画像 E：自动化/CI 用户（占比约 5%）**

- 需要 phpo 在无人值守下反复操作
- 关键场景：每次运行都要保证环境干净；离线缓存是刚需

**画像 F：内网/弱网/离线环境用户（占比约 5%）**

- 机器：企业内网，无法访问 Docker Hub
- 关键场景：完全依赖离线缓存完成安装；断网重装是核心需求

**真实场景对设计的约束**：

| 真实约束 | 设计决策 |
|---------|---------|
| 反复安装/卸载 | 幂等 + 清理 |
| 内网无法访问 Docker Hub | 离线缓存优先 |
| 断网重装 | 零网络安装 |
| 网络不稳定 | 缓存成功后不重复下载 |
| 磁盘空间有限 | 缓存可清理 |
| 缓存可能损坏 | SHA256 校验 |
| 临时目录污染下次使用 | 无论成败都清空临时目录 |

---

## 1. 项目定位与核心结论

### 1.1 产品定义

**phpo** 是一款面向 PHP 开发者的本地 Docker 化开发环境管理器。

| 维度 | 结论 |
|------|------|
| 对标竞品 | Laravel Herd / Laragon / phpStudy 的「Docker 多版本化」替代 |
| 目标用户 | 见 §0.4 画像 A / B / C / D / E / F |
| 差异化 | 多 PHP × 多 DB 版本并存 + 中文框架预设 + 一键备份迁移 + 最小限制 + Docker 清洁 + 离线缓存 |
| 产品形态 | 跨平台 GUI 桌面应用，不提供 CLI |
| 密码策略 | 明文；默认 `123456`；可修改；可为空；长度不校验；UI 可查看 |
| 版本策略 | 不限制；允许任意字符串；仅做路径安全校验 |
| PHP 切换策略 | vhost 上游精确指向 `php-{version}-fpm:9000` |
| 状态同步 | 后端唯一权威；前端订阅事件；无本地乐观更新 |
| 端口策略 | 站点端口默认 80、用户可指定任意端口；新建站点占用只告警 + 降级（不改用户所填端口、不阻断建站）；改站点端口占用才顺延；服务端口占用报错 |
| 限制策略 | 最小限制；仅 8 条硬红线；警告代替阻止 |
| Docker 清洁策略 | 所有操作幂等、原子、可回滚、可清理 |
| 离线缓存策略 | 装任何镜像/扩展必先查缓存；命中零网络；未命中临时下载编译；成功后提升到缓存；无论成败均清空临时目录 |

### 1.2 原型资产盘点

| 维度 | 结论 |
|------|------|
| 规模 | 单文件 约 2500 行（CSS ~370 / HTML ~70 / JS ~2060） |
| 完成度 | 领域模型与校验层真实可用，执行层与持久层为虚构 |
| 可平移资产 | `derivePaths` / `MOUNTS` / `VHosts` / `preflight` / `REWRITE_PRESETS` / `DEFAULT_CONFIGS` |
| 需重构 | 全局可变 state、全量 innerHTML 重绘、4 条旁路 |
| 离线缓存现状 | 原型中有 `renderOffline()` 视图和 6 条 demo 数据；`buildScript('offline')` 为假日志；本项目将其真实化 |

### 1.3 核心结论

原型不是「演示稿」，而是可执行规格。已完成生产功能定义的 **60–70%**。

### 1.4 关于等效命令展示

原型日志抽屉中的 `.drawer-cmd` 元素会显示「等效命令」。生产版保留此展示，作为辅助信息。但：

- 该命令不可执行。
- UI 中标注「仅展示」徽标。
- 复制时 Toast 提示「本产品不提供 CLI」。

### 1.5 关于密码策略的说明

**核心策略**：明文存储 + 默认 `123456` + 可修改 + 可为空 + 长度不校验 + UI 可查看。

**六项规则**：

| # | 规则 | 说明 |
|---|------|------|
| 1 | 明文存储 | 存于 `config.yaml`（各平台 XDG 用户配置目录内的 `phpo` 子目录）的 `services.{kind}.{version}.password`；经快照 `env` 扁平键（`{KIND}_{VER}_PASSWORD`）回显前端 |
| 2 | 默认 `123456` | 新建服务时自动填入 |
| 3 | 可修改 | UI 提供行内编辑 |
| 4 | 可为空 | 空字符串是合法密码 |
| 5 | 长度不校验 | 1 位、100 位、含特殊字符、含空格均合法 |
| 6 | UI 可查看 | 👁 显示 / 📋 复制 / ↺ 重置 |

**明确禁止**：校验密码长度、复杂度、非空；引入 keyring、Vault、AES、bcrypt、argon2。

### 1.6 关于版本策略的说明

**核心策略**：不限制任何服务的版本号；仅做路径安全校验。

**校验规则（唯一保留）**：非空 / 无 `/` `\` / 无 `..` / 无 `\x00` / 长度 ≤ 128 / 首尾 trim。

### 1.7 关于 PHP 版本切换准确性

**核心约束**：站点切换 PHP 版本时，vhost 的 fastcgi 上游必须唯一且精确地指向目标版本容器。

**版本号 → 容器名映射**：保留版本号原样（含点）。

### 1.8 关于状态同步

**核心原则**：后端唯一权威 + 前端订阅事件。

### 1.9 关于站点端口的说明

**核心策略**：站点端口默认 80；**新建站点**时端口被占用**不顺延**——只在确认前弹框告警并把站点降级（vhost 暂不落盘、端口暂不发布），**站点照常创建、用户所填端口原样保留**；腾出该端口或改一次端口即自愈。**改已有站点**的端口时占用才在 1–65535 内顺延首个可用端口，**不报错、无窗口上限**。用户可指定任意端口。服务端口（安装/改配置）占用仍报 `portInUse` 错误，不自动顺延。

### 1.10 关于应用版本检查和升级

**核心功能**：phpo 应用自身支持版本检查和自动升级。

### 1.11 关于最小限制原则

**核心原则**：除 8 条硬红线外，所有限制放开或降级为警告。

### 1.12 关于 Docker 操作清洁性

**核心原则**：phpo 对 Docker 的任何操作都必须干净，不会影响后续操作。

**六项保证**：幂等性 / 原子性 / 隔离性 / 一致性 / 可清理性 / 可恢复性。

### 1.13 关于离线缓存的说明

**核心原则一句话**：

> **phpo 安装任何 Docker 镜像或 PHP 扩展时，必须先查离线缓存；命中则零网络使用；未命中则网络下载到临时目录，成功后立刻把文件从临时目录提升到缓存目录，然后清空临时目录；编译失败则清空临时目录。断网重装靠缓存，内网开发靠缓存。**

#### 1.13.1 核心行为（三条铁律）

| # | 铁律 | 说明 |
|---|------|------|
| 1 | 装前必查 | 任何镜像/扩展安装前，必须先查离线缓存目录 |
| 2 | 命中零网络 | 缓存命中且 SHA256 校验通过 → 直接使用，不碰网络 |
| 3 | 临时目录必清 | 无论编译成功还是失败，临时目录必须清空 |

#### 1.13.2 以 PHP 8.4 为例的完整流程

**安装 PHP 8.4 镜像**：

```
1. 检查 ~/phpo/offline/php/8.4/image.tar
   ├─ 存在 + SHA256 通过
   │   → docker load -i image.tar（零网络）
   │   → 发射 cache:hit 事件
   │   → 完成
   └─ 不存在 / SHA256 失败
       → docker pull php:8.4-fpm（网络）
       → docker save -o {tmp}/image.tar
       → 校验
       → mv {tmp}/image.tar ~/phpo/offline/php/8.4/image.tar
       → 更新 ~/phpo/offline/php/8.4/manifest.json
       → 清空 {tmp}
       → 发射 cache:miss + cache:promote 事件
       → 完成
```

**安装 PHP 8.4 的 redis 扩展（pecl）**：

```
1. 检查 ~/phpo/offline/php/8.4/pecl/redis-6.0.2.tgz
   ├─ 存在 + SHA256 通过
   │   → 复制到 ~/phpo/php/8.4/ext/pecl/
   │   → 编译安装
   │   → 清空 ~/phpo/php/8.4/ext/
   │   → 发射 cache:hit 事件
   └─ 不存在 / SHA256 失败
       → 下载到 ~/phpo/php/8.4/ext/pecl/redis-6.0.2.tgz（网络）
       → 编译安装
       ├─ 成功
       │   → mv ~/phpo/php/8.4/ext/pecl/redis-6.0.2.tgz ~/phpo/offline/php/8.4/pecl/
       │   → 更新 ~/phpo/offline/php/8.4/manifest.json
       │   → **清空 ~/phpo/php/8.4/ext/**（防污染下次使用）
       │   → 发射 cache:miss + cache:promote 事件
       └─ 失败
           → **清空 ~/phpo/php/8.4/ext/**（防污染下次使用）
           → 报错
2. 重建镜像 + 重启容器
```

#### 1.13.3 目录结构总览

```
~/phpo/offline/                       # 缓存根目录（持久）
├── php/
│   └── 8.4/
│       ├── image.tar                 # 镜像缓存
│       ├── apk/                      # apk 依赖缓存
│       ├── pecl/                     # pecl 扩展缓存
│       └── manifest.json             # 清单（含 SHA256）
├── mysql/8.4/{image.tar, manifest.json}
├── pgsql/17/{image.tar, manifest.json}
├── redis/8/{image.tar, manifest.json}
└── nginx/alpine/{image.tar, manifest.json}

~/phpo/php/8.4/ext/                   # 临时目录（单任务，结束即空）
├── apk/                              # 编译期间临时存放
└── pecl/
```

#### 1.13.4 明确禁止

- ❌ 安装前不检查离线缓存。
- ❌ 命中缓存后仍走网络下载。
- ❌ 编译成功后不清空临时目录。
- ❌ 编译失败后保留临时目录。
- ❌ 临时目录跨任务持久化。
- ❌ 静默跳过缓存检查。
- ❌ 用过期或损坏的缓存（必须 SHA256 校验）。

---

## 2. 技术选型（冻结）

| 层级 | 选型 | 版本 | 选型理由 |
|------|------|------|---------|
| 桌面框架 | Wails | ≥ 3.0 | 原生托盘 / 多 Service 绑定 / 事件总线 / Taskfile |
| 后端语言 | Golang | ≥ 1.27 | 单二进制分发、`go:embed`、goroutine 任务模型 |
| 前端框架 | Vue 3 + TS + Vite | Vue 3.5+ / TS 5.x / Vite 5.x | 10 屏 + 12 模态的规模 |
| 状态管理 | Pinia | 2.x | 替代原型全局 `state` |
| 状态同步 | Wails Events | — | 事件推送替代全量 `render()` |
| 持久化 | SQLite（pure Go） | `modernc.org/sqlite` | CGO_ENABLED=0 交叉编译；**仅存运行态** |
| 配置存储 | YAML `config.yaml` | `gopkg.in/yaml.v3` | 单一配置权威，落各平台 XDG 用户配置目录内的 `phpo` 子目录 |
| 密码存储 | 明文（`config.yaml`） | — | 默认 `123456`；允许为空；长度不限 |
| 容器引擎 | Docker SDK | `docker/docker/client` | 流式进度 |
| 归档 | 标准库 | `archive/tar + compress/gzip` | 无外部依赖 |
| 版本比较 | `Masterminds/semver` | v3 | SemVer 标准实现 |
| 签名校验 | 标准库 `crypto/ed25519` | — | 升级包签名校验 |
| 构建编排 | Taskfile | 3.x | Wails 3 官方推荐 |
| 节点运行时 | Node.js | ≥ 22 LTS | 前端构建 |
| 缓存校验 | 标准库 `crypto/sha256` | — | 镜像/扩展包完整性校验 |
| Docker 镜像导出 | Docker SDK `ImageSave` | — | 导出 tar 到离线缓存 |
| Docker 镜像导入 | Docker SDK `ImageLoad` | — | 从离线缓存加载 |

### 2.1 明确排除的方案

| 排除方案 | 排除理由 |
|---------|---------|
| Tauri 2 + Node.js | 与用户要求冲突 |
| 零框架（原生 DOM） | 组件规模超出可维护边界 |
| JSON 单文件持久化 | 多字段一致性无法保证 |
| CLI（cobra / flag） | 本产品不提供命令行入口 |
| OS keyring / 密码加密 | 本产品明确使用明文密码 |
| 密码长度 / 复杂度校验 | 用户自由 |
| 版本号字符集白名单 | 仅做路径安全校验 |
| 端口范围限制 | 允许 1–65535 |
| 域名格式严格校验 | 允许任意域名格式 |
| 站点根路径强制在 WWW_ROOT 内 | 允许 WWW_ROOT 外（警告） |
| 前端本地乐观更新 | 必须等后端确认 |
| WebSocket / HTTP 轮询 | Wails Events 已足够 |
| 插件系统 | 不做 |
| 非幂等 Docker 操作 | 所有操作必须幂等 |
| 留下脏资源的操作 | 所有操作必须可清理 |
| 跳过离线缓存直接走网络 | 离线优先是硬红线 |
| 临时目录跨任务持久化 | 防污染 |

---

## 3. 设计原则

### 3.1 工程布局六原则

1. **根目录一眼看懂**：入口文件、配置、中文文档置顶。
2. **内部严格分层**：装配 → 配置 → 模型 → 存储 → 引擎 → 模板 → 校验 → 任务 → 服务，单向依赖。
3. **中文注释全覆盖**：每个文件、目录用一句中文说明。
4. **模板内聚**：配置模板以 `go:embed` 嵌入。
5. **UI 偏好留前端**：主题、语言、布局、缩放用 `localStorage`。
6. **命名见名知义**：文件名与原型函数名对齐。

### 3.2 真实场景驱动的设计原则

**原则 1：默认可跑，不让人选择**

**原则 2：核心操作 3 秒内完成**

**原则 3：错误信息是人话**

**原则 4：不做无场景支撑的抽象**

**原则 5：兼容性是底线**

**原则 6：默认用最直观的约定**

**原则 7：最小限制**

**原则 8：Docker 操作必须干净**

**原则 9：离线优先**

- **装前必查**：安装任何 Docker 镜像或 PHP 扩展时，先查离线缓存。
- **命中零网络**：命中缓存 → 零网络使用。
- **未命中走网络**：未命中 → 下载到临时目录 → 编译/加载。
- **成功后提升**：编译/加载成功 → 提升到缓存。
- **无论成败清临时**：编译成功或失败，临时目录必须清空，**防污染下次使用**。

### 3.3 硬红线（仅 8 条）

| # | 硬红线 | 原因 |
|---|--------|------|
| 1 | PHP 切换精确匹配 `php-{version}-fpm:9000` | 功能准确性刚需 |
| 2 | vhost 写入前必须 `nginx -t` 通过 | 防止 nginx 起不来 |
| 3 | 路径穿越 `..` 防御 | 防止破坏系统 |
| 4 | 状态同步后端唯一权威 | 一致性刚需 |
| 5 | 三段式写操作 | 事务性刚需 |
| 6 | 升级包 SHA256 + 签名校验 | 供应链安全 |
| 7 | Docker 未装则不能启动服务 | 功能依赖 |
| 8 | Docker 操作必须清洁 + 离线优先 + 临时目录必清 | 不污染环境；离线优先；防污染下次使用 |

### 3.4 Agent 编码行为准则（v2.7 新增）

**本节约束 Agent 在编写、修改、重构本项目代码时必须遵守的行为准则。**

#### 3.4.1 编码前思考

1. **明确假设，不确定时询问而非猜测**。
   - 遇到需求不清晰、上下文缺失、边界条件不明时，**先向用户确认**，不要自行假设后编码。
   - 假设必须有依据（本文件条款、原型源码、用户明确指示），不能凭经验推断。

2. **存在歧义时，列出多种解释，不默默选定单一方案**。
   - 例如：「支持自定义端口」可能指「允许用户填任意端口」或「允许用户配置端口范围」——必须先列出两种解释让用户选择。
   - 不允许在歧义存在时默认选择一种解释然后编码。

3. **如果任务有明显更简单的做法，直接指出优化思路**。
   - 例如：用户要求「写一个复杂的日志格式化器」，但项目已有 `log/slog`，应指出「直接用标准库即可」。
   - 不允许为了「显得完整」而实现冗余方案。

4. **发现代码矛盾、逻辑不一致时及时暂停，请求信息澄清**。
   - 例如：本文件说「密码可为空」，但已有代码中 `preflight` 强制密码非空——必须暂停并报告冲突。
   - 不允许「绕过」矛盾默默修复。

#### 3.4.2 简洁优先

1. **用最少的代码解决问题，拒绝冗余实现**。
   - 如果一个功能 10 行能写完，绝不写 100 行。
   - 如果一个函数能复用，绝不复写。

2. **不为一次性需求创建抽象层、复杂架构**。
   - 只有出现一次的逻辑，不需要抽象成接口。
   - 只有出现两次以上的重复，才考虑提取。
   - 不允许「为了未来可能的复用」而提前抽象。

3. **不盲目增加扩展性、可配置性，应对「未来可能用到」的场景**。
   - 例如：不需要为「未来可能支持 MySQL 9.0」提前写版本适配层。
   - 不允许 YAGNI（You Aren't Gonna Need It）之外的过度设计。

4. **若代码可大幅精简，主动重写优化**。
   - 发现 200 行的函数可以简化为 50 行，应主动重写。
   - 精简的前提是不牺牲可读性与正确性。

5. **校验标准：以资深工程师视角判断，代码若过于复杂，立即简化**。
   - 判断标准：一个有 5 年 Go 经验的工程师能否在 30 秒内看懂？
   - 若不能，说明代码过于复杂，需要简化。

#### 3.4.3 精准修改

1. **仅修改与当前任务直接相关的代码内容**。
   - 用户要求「改 A 函数」，就不要顺手改 B 函数的缩进。

2. **不顺手优化相邻代码、注释、排版格式**。
   - 相邻代码的格式问题、注释问题，如非任务相关，仅作文字提醒，不擅自修改。
   - 例如：`// TODO: 优化这里` 这种注释，如果任务不是处理它，就不要动。

3. **不重构原本可以正常运行的代码模块**。
   - 除非任务明确要求重构，否则不动能正常工作的代码。
   - 「能跑就不要动」是铁律。

4. **严格匹配项目现有代码风格，保留原有编码习惯**。
   - 例如：项目用 `snake_case` 字段名，就不要新代码用 `camelCase`。
   - 例如：项目用 `if err != nil { return err }` 风格，就不要新代码用 `if err := ...; err != nil { ... }`。

5. **因本次修改产生的无效导入、废弃变量，可直接删除**。
   - 例如：删除某个函数调用后，其导入的包如果不再使用，直接删除导入。
   - 例如：删除某个字段的使用后，其 struct 字段如果不再使用，直接删除字段。

6. **发现项目中原有的死代码、冗余内容，仅做文字提醒，不擅自删除**。
   - 例如：发现 `internal/engine/old.go` 已经没人调用，告知用户，但不删除。
   - 删除死代码属于「重构」，需要用户明确授权。

#### 3.4.4 目标驱动执行

1. **执行任务前，定义清晰、可落地的成功标准**。
   - 例如：「修复 Bug」应转化为「编写用例复现问题，再调试至用例正常通过」。
   - 例如：「新增校验功能」应转化为「针对异常输入编写测试用例，保证全部通过」。
   - 例如：「代码重构」应转化为「完成重构后，确保原有所有测试用例正常运行」。

2. **将「修复 Bug」转化为：编写用例复现问题，再调试至用例正常通过**。
   - 不允许「看一眼代码就改」——必须先有可复现的失败用例。
   - 修改后用例通过，才算修复完成。

3. **将「新增校验功能」转化为：针对异常输入编写测试用例，保证全部通过**。
   - 不允许只实现校验逻辑而不写测试。
   - 测试用例必须覆盖：正常输入、边界输入、异常输入。

4. **将「代码重构」转化为：完成重构后，确保原有所有测试用例正常运行**。
   - 重构前先跑一遍测试，确保基线通过。
   - 重构后再跑一遍，确保全部通过。
   - 若原有测试缺失，重构前先补测试。

5. **多步骤复杂任务，先输出简短执行计划，同时标注每一步的验证方式**。
   - 例如：

     ```
     任务：实现 PHP 扩展离线缓存
     
     步骤 1：定义缓存目录结构（验证：目录创建成功）
     步骤 2：实现 SHA256 校验（验证：单测覆盖）
     步骤 3：实现缓存查找（验证：单测覆盖）
     步骤 4：实现临时目录管理（验证：单测覆盖）
     步骤 5：实现提升逻辑（验证：集成测试）
     步骤 6：实现清空逻辑（验证：集成测试）
     步骤 7：端到端验收（验证：手动验证缓存命中/未命中）
     ```

   - 不允许「走一步看一步」——必须先规划。
   - 每步完成后必须先验证，再进行下一步。

#### 3.4.5 与 §0.2 的关系

| 章节 | 约束对象 | 约束内容 |
|------|---------|---------|
| §0.2 | Agent 对项目的整体行为 | 项目级约束（禁止 CLI / 禁止密码加密 / 禁止绕过离线缓存 等） |
| §3.4 | Agent 的编码行为 | 编码级约束（思考 / 简洁 / 精准 / 目标驱动） |

**两者是补充关系**：§0.2 说「不要做什么」，§3.4 说「怎么做」。

#### 3.4.6 明确禁止

- ❌ 不确定时猜测而非询问。
- ❌ 歧义时默默选定单一方案。
- ❌ 有明显更简单做法时不指出。
- ❌ 发现代码矛盾时不停下。
- ❌ 冗余实现、过度抽象、为未来可能设计。
- ❌ 顺手修改相邻代码、注释、格式。
- ❌ 重构能正常运行的代码。
- ❌ 擅自删除死代码（应仅提醒）。
- ❌ 无明确成功标准就开工。
- ❌ 无测试用例就声称「修复完成」。
- ❌ 多步骤任务不先规划。

---

## 4. 完整目录结构树

### 4.1 根目录

```
phpo/
├── main.go                          # GUI 入口（embed all:frontend/dist + 注册根 Service）
├── app.go                           # 根 Service：唯一对前端暴露的门面（64 个绑定方法）
├── app_test.go
├── go.mod  go.sum                   # module phpo；go 1.27
├── wails.json                       # Wails 配置
├── Taskfile.yml                     # dev / bindings / frontend:{install,build} / build / test / vet / check / package{,:linux,:windows,:darwin} / version:bump / release:local
├── Makefile
├── config.example.yaml              # 配置样例：两根 + services.{kind}.{version}.{password,port}
├── AGENTS.md                        # 本总纲
├── index.html                       # 原型 SSOT 镜像（由 前端唯一界面来源.txt 逐字派生，禁止手改）
├── 前端唯一界面来源.txt              # 单文件 HTML 原型 = 前端唯一界面来源
├── 任务工单.md  实施顺序.md          # 派生工单清单 / 里程碑排期
├── .gitignore  .editorconfig
├── .golangci.yml  .air.toml
│
├── internal/                        # 私有业务码（严格单向依赖，§3.1）
│   ├── app/                         # 装配层
│   │   ├── assemble.go              # 装配顺序：config → store → engine → cache → vhost → task → service
│   │   ├── di.go
│   │   ├── emitter.go               # §5.6 事件总线封装（17 事件名）
│   │   ├── mock_emitter.go          # 无后端时的演示事件源
│   │   └── lifecycle.go             # 启动/关闭钩子（清残留临时目录 + 校准 + 升级回滚探测）
│   │
│   ├── config/                      # 配置层
│   │   ├── config.go
│   │   ├── configstore.go           # 单一配置权威（YAML config.yaml：根目录 + 密码 + 端口）
│   │   ├── userdata.go              # XDG 用户配置目录解析（config.yaml / phpo.db / logs / trash / updates）
│   │   ├── paths.go                 # derivePaths / hostToContainer
│   │   ├── validate.go              # 路径安全校验（§5.4 唯一保留的校验）
│   │   ├── password.go              # 明文密码，默认 123456
│   │   ├── versions.go              # 版本建议表（内联，仓库无 configs/versions.json）
│   │   ├── extensions.go            # PHP 扩展元数据
│   │   └── offline.go               # 离线缓存 / 临时目录路径常量（§5.14.2）
│   │
│   ├── model/                       # 领域模型（13 文件）
│   │   ├── service.go  site.go  task.go  backup.go  offline.go  snapshot.go
│   │   ├── update.go  operation.go  resource.go  cache_entry.go  dto.go
│   │   └── cleanup.go  doctor.go
│   │
│   ├── store/                       # 存储层（仅运行态；延迟建库）
│   │   ├── store.go  sqlite.go  migrate.go  snapshot.go  sync.go
│   │   ├── port.go                  # CollectUsedPorts / CollectServicePorts（端口占用单一判据）
│   │   ├── operation.go             # 操作审计 + 任务账本读写
│   │   ├── trash.go  offline.go  backup.go
│   │   └── migrate/
│   │       ├── 0001_init.sql
│   │       ├── 0002_add_offline.sql
│   │       ├── 0003_add_update.sql
│   │       ├── 0004_add_operations.sql
│   │       ├── 0005_add_cache_manifest.sql
│   │       ├── 0006_add_site_rewrite.sql
│   │       ├── 0007_drop_dir_ready.sql    # dirReady 改快照派生，表下线
│   │       └── 0008_add_task_ledger.sql   # 任务账本：给 operations 加 task_id / label / logs 三列（不另立表）
│   │
│   ├── engine/                      # Docker 引擎层（17 文件）
│   │   ├── client.go  container.go  image.go  registry.go
│   │   ├── network.go  volume.go  mount.go  inspect.go  exec.go
│   │   ├── calibrate.go  health.go
│   │   └── cleaner.go  orphan.go  idempotent.go  verify.go  trash.go  audit.go
│   │
│   ├── cache/                       # 离线缓存核心（10 文件）
│   │   ├── manager.go  lookup.go  manifest.go
│   │   ├── image_cache.go  extension_cache.go  promote.go
│   │   ├── tempdir.go  verifier.go  # SHA256 校验直接用标准库（无 pkg/hash/）
│   │   └── cleaner.go  stats.go
│   │
│   ├── template/
│   │   ├── embed.go  render.go
│   │   └── templates/               # 按种类分，不按版本分（§5.3）
│   │       ├── php/php.ini.tmpl  php-fpm.conf.tmpl
│   │       ├── nginx/nginx.conf.tmpl
│   │       ├── mysql/my.cnf.tmpl
│   │       ├── pgsql/postgresql.conf.tmpl  pg_hba.conf.tmpl
│   │       ├── redis/redis.conf.tmpl
│   │       └── vhost.conf.tmpl      # DEFAULT_CONFIGS = 5 服务 7 文件 + vhost 模板
│   │
│   ├── vhost/
│   │   ├── manager.go  parse.go  upstream.go  rewrite.go
│   │   ├── sync.go  validate.go  reload.go     # 写盘前 nginx -t（硬红线 2）
│   │   └── hosts/                             # §T402 hosts 三平台提权
│   │       ├── hosts.go  manager.go
│   │       └── elevate_linux.go  elevate_darwin.go  elevate_windows.go
│   │
│   ├── preflight/                   # 唯一裁决层（§0.2-14）
│   │   ├── preflight.go             # Run 的 17-case switch + AllActions / needsHome 集合
│   │   ├── validators.go            # 域名 / 版本 / 路径 / 端口校验
│   │   ├── rules_service.go  rules_site.go
│   │   └── rules_ops.go  rules_cache.go
│   │       # 占用判定内联复用 store.CollectUsedPorts（无独立 portprobe.go）
│   │       # 清理规则在 cleanup_service 侧（无 rules_cleanup.go）
│   │
│   ├── task/                        # 三段式任务引擎（硬红线 5）
│   │   ├── manager.go               # 串行 FIFO 队列 + ErrBusy / ErrQueued + Cancel / CancelQueued
│   │   ├── task.go  step.go  funcstep.go  emitter.go  rollback.go
│   │   ├── ledger.go                # 退出即落账（日志尾部 500 行；落账失败不改任务结果）
│   │   └── steps/
│   │       ├── steps_service.go     # 安装/卸载/启停
│   │       ├── steps_site.go        # 建站/删站/改端口/切 PHP/伪静态/vhost
│   │       ├── steps_config.go      # 配置保存并重载
│   │       └── steps_update.go      # 下载/校验/安装/回滚
│   │           # 扩展、备份、清理、向导的编排在 service 层内联，不另立 steps_*.go
│   │
│   ├── service/                     # 服务层（18 文件）
│   │   ├── registry.go              # SVC_META 五服务元数据
│   │   ├── app_service.go           # GetState（权威快照出口，含 TaskBoard）
│   │   ├── env_service.go           # 密码 / 端口 / 偏好读写（config.yaml）
│   │   ├── lifecycle_service.go  php_service.go  db_service.go
│   │   ├── redis_service.go  nginx_service.go  site_service.go
│   │   ├── extension_service.go  config_service.go
│   │   ├── backup_service.go  offline_service.go  doctor_service.go
│   │   ├── wizard_service.go  workdir.go  cleanup_service.go
│   │   └── updater_service.go
│   │
│   ├── updater/
│   │   ├── checker.go  downloader.go  verifier.go   # SHA256 + Ed25519（硬红线 6）
│   │   ├── update.go  rollback.go  scheduler.go     # 启动时 + 每 24h 检查
│   │   ├── embed.go                                 # go:embed signing
│   │   ├── installer.go  installer_windows.go  installer_darwin.go  installer_linux.go
│   │   └── signing/public.key                       # 公钥随包分发（当前为占位，待持钥者一次性替换）
│   │
│   ├── ui/
│   │   └── window.go  tray.go  menu.go
│   │
│   └── util/
│       └── fs.go
│           # 注：文案 i18n 在前端 locales（无 internal/i18n/）
│
├── pkg/
│   ├── version/compare.go  semver.go
│   ├── port/suggest.go  probe.go  sequence.go       # 顺延 + KeepOnConflict + TCP 实探
│   ├── archive/targz.go
│   ├── disk/disk.go  disk_unix.go  disk_windows.go  # 磁盘余量（doctor 用）
│   ├── dockerutil/naming.go  imageref.go  wait.go   # 容器 inspect 在 engine 侧
│   └── errs/errors.go  codes.go
│       # SHA256 用标准库（无 pkg/hash/）；exec 封装在 internal/engine/exec.go（无 pkg/execx/）
│
├── frontend/                        # 前端工程
│   ├── package.json  pnpm-lock.yaml  pnpm-workspace.yaml
│   ├── vite.config.ts  tsconfig.json  tsconfig.node.json
│   ├── index.html                   # Vite 入口
│   ├── dist/index.html              # 占位产物（tracked，保证仓库可独立编译；构建后需还原）
│   ├── bindings/                    # wails3 生成（gitignore）
│   └── src/
│       ├── main.ts  App.vue  env.d.ts
│       ├── router/index.ts          # 10 命名路由（CleanupView 由 CleanupModal 承载，非独立路由）
│       ├── api/                     # 17 文件
│       │   ├── env.ts  lifecycle.ts  site.ts  extension.ts  config.ts
│       │   ├── backup.ts  offline.ts  cache.ts  doctor.ts  task.ts
│       │   ├── state.ts  updater.ts  cleanup.ts  wizard.ts  docker.ts
│       │   └── events.ts  mockEvents.ts
│       ├── stores/                  # Pinia 8
│       │   ├── appState.ts  taskStore.ts  layoutStore.ts  prefsStore.ts
│       │   └── updaterStore.ts  cleanupStore.ts  cacheStore.ts  modalStore.ts
│       ├── views/                   # 12 .vue = 11 路由视图 + ServiceView（服务页共用基座）
│       │   ├── SitesView.vue  PhpView.vue  MysqlView.vue  PgsqlView.vue
│       │   ├── RedisView.vue  NginxView.vue  BackupView.vue
│       │   ├── OfflineView.vue  SettingsView.vue  OverviewView.vue  CleanupView.vue
│       │   └── ServiceView.vue
│       ├── components/
│       │   ├── common/              # 7：ModalShell / ModalRoot / ToastHost / PasswordField
│       │   │                        #   MountList / PathInfoBar / CacheHitBadge
│       │   └── business/            # 18：SiteAddModal / SiteConfigModal / RewriteModal / InstallModal
│       │                            #   ConfigModal / PhpExtensionsModal / TaskDrawer / CmdPalette
│       │                            #   DangerConfirm / HomeSetupWizard / DockerGate / AppTrayMenu
│       │                            #   ThemePickerModal / UpdateModal / CleanupModal / TrashViewer
│       │                            #   CacheDetailModal / CacheCleanupModal（备份与改端口无独立模态）
│       ├── composables/             # 15：usePreflight / useTask / useStateSync / usePhpSwitch
│       │                            #   usePortSuggest / useCache / useCleanup / useUpdater / useI18n
│       │                            #   useModals / useCmdPalette / useDockerPreflight / useToast
│       │                            #   useZoom / useResize
│       ├── constants/               # 9：service / mounts / rewrite / cmd / themes / layout / home / configs / ext
│       ├── locales/index.ts  zh-CN.ts  en-US.ts     # MESSAGES 落点（原型 → 前端）
│       ├── styles/base.css  styles/themes/           # 6 套主题 + index.css
│       ├── types/index.ts
│       └── utils/format.ts  path.ts  str.ts  vhost.ts  # 最终裁决在 internal/preflight
│
├── build/                           # 三平台打包资源
│   ├── appicon.png
│   ├── windows/icon.ico  wails.exe.manifest  nsis/installer.nsi
│   ├── darwin/icon.icns  Info.plist  entitlements.plist
│   ├── linux/phpo.desktop  nfpm.yaml  postinstall.sh  icons/{128x128,256x256,512x512}/phpo.png
│   └── bin/                         # 打包产物（gitignore）
│       # 公钥不在 build/signing/，改由 internal/updater/signing/public.key go:embed
│
├── scripts/
│   ├── check-i18n-keys.go  check-templates.go  check-docker-naming.go  check-cache-manifest.go
│   ├── sign-release.sh  gen-checksums.sh  verify-signing-guard.sh
│   └── bump-version.sh  version.sh  gen-locales.mjs   # gen-locales 会重写 locales，禁止随意执行
│       # dev / build / bindings / 打包统一走 Taskfile，无 dev.* / bindings.* / build-all.* / pkg.* 脚本
│
├── docs/                            # 全中文文档（32 篇）
│   ├── 项目概述.md  架构设计.md  目录规范.md  领域模型.md  接口契约.md
│   ├── 事件流协议.md  界面规格.md  跨平台差异.md
│   ├── 密码策略.md  版本策略.md  域名策略.md  路径策略.md  端口策略.md
│   ├── PHP切换准确性.md  状态同步.md  应用升级.md
│   ├── 最小限制原则.md  编码行为准则.md
│   ├── Docker操作规范.md  资源清洁机制.md  回收站机制.md
│   ├── 离线缓存机制.md  缓存清单规范.md  临时目录生命周期.md  离线缓存协议.md
│   ├── 任务取消语义.md  备份脱敏规范.md  打包发布.md  用户手册.md
│   ├── CHANGELOG.md
│   └── M6-集成验收记录.md  T705-Linux真机冒烟记录.md   # 里程碑验收留痕
│
├── test/
│   └── integration/                 # 真环境 live 用例（PHPO_LIVE=1 + Docker 可用双重 skip 守护）
│       ├── m3_live_test.go  m4_live_test.go
│       ├── m5_mysql_live_test.go  m5_pgsql_live_test.go
│       ├── m5_redis_live_test.go  m5_wordpress_live_test.go
│       └── m6_offline_live_test.go  t601_extension_live_test.go  t602_backup_live_test.go
│       # 单元测试与包同目录（60 个 *_test.go），fake/mock 内联，无 test/{unit,mocks,fixtures,e2e}
│
├── third_party/licenses/THIRD_PARTY_LICENSES.md
│
└── .github/
    ├── workflows/ci.yml  release.yml  lint.yml
    ├── ISSUE_TEMPLATE/bug_report.md  feature_request.md
    └── PULL_REQUEST_TEMPLATE.md
```

> **尚未落地的规划交付物**（属 M7 发布线，不得当作已存在引用）：根目录 `README.md` / `README_EN.md` /
> `CHANGELOG.md`（现仅 `docs/CHANGELOG.md`）/ `CONTRIBUTING.md` / `LICENSE`；`configs/`（默认配置已内联到
> `internal/config` 与前端 `constants/`）；`assets/`、`data/`；`internal/i18n/`（文案在前端 locales）；
> `pkg/hash`、`pkg/execx`、`pkg/fsutil`；`frontend/public/favicon.svg`；win/mac 代码签名链（EV 证书 /
> Developer ID + 公证）与真机冒烟。

### 4.2 运行时用户数据目录

```
$XDG_CONFIG_HOME/phpo/                # 用户数据目录（os.UserConfigDir()/phpo，见 internal/config/userdata.go）
│                                     # Windows: %APPDATA%\phpo · macOS: ~/Library/Application Support/phpo · Linux: ~/.config/phpo
├── config.yaml                       # 单一配置权威（YAML）：phpo_home + www_root + services.{kind}.{version}.{password,port}；0600
├── phpo.db                           # SQLite，仅存运行态（installed / running / sites / php_extensions / trash / operations（含任务账本 task_id/label/logs） / offline / cache_manifest）；**延迟建库**：装机向导把两根目录写入 config.yaml 后才创建
├── logs/
│   └── operations.log                # 操作审计（JSON Lines，§5.13.10）
├── trash/                            # 回收站（7 天保留，§5.13.7）
└── updates/                          # 升级工作区
    ├── downloads/
    └── backups/

# 注：主题、语言、布局、缩放属 UI 偏好，留前端 localStorage（§3.1 原则 5），用户数据目录内不再有 themes/ 与 locales/；
#     运行日志走标准输出 + 系统日志，未单独落 phpo.log。

~/phpo/                               # PHPO_HOME
├── php/<ver>/{conf,logs}
│   └── ext/                          # 临时目录（编译期间，任务结束即清空）
│       ├── apk/
│       └── pecl/
├── nginx/{conf,logs,sites/}
├── mysql/<ver>/{conf,data,logs,initdb}
├── pgsql/<ver>/{conf,data,logs,initdb}
├── redis/<ver>/{conf,data,logs}
├── backups/backup-*.tar.gz
└── offline/                          # 离线缓存根目录（持久）
    ├── php/
    │   └── 8.4/
    │       ├── image.tar
    │       ├── apk/
    │       ├── pecl/
    │       └── manifest.json
    ├── mysql/8.4/{image.tar, manifest.json}
    ├── pgsql/17/{image.tar, manifest.json}
    ├── redis/8/{image.tar, manifest.json}
    └── nginx/alpine/{image.tar, manifest.json}

# 注：密码/端口/工作根目录统一存于用户数据目录内的 config.yaml（见上），不再有 ~/phpo/.env。

~/www/                                # WWW_ROOT（默认，可改）
├── demo.test/
└── ...

# Docker 资源命名空间
#
# 容器：phpo-{kind}-{version}
# 网络：phpo-network
# 卷：phpo-{kind}-{version}-{purpose}
```

> **首启零落盘**：装机向导把两根目录写入 `config.yaml` 之前，用户数据目录内不得出现任何文件——`config.yaml` 与 `phpo.db` 均在首次写入时才创建（运行态存储延迟打开）。`dirReady` 不落库，由快照按「两根已持久化 + 目录实际存在」逐根派生：目录被删即自动回落 `false` 重新拦截写操作，但库内已装状态照常可读、不回退。

---

## 5. 关键设计说明

### 5.1 无 CLI 目录

**本产品不提供 CLI，因此没有 `cmd/` 目录。**

### 5.2 密码明文存储

**五项规则**：明文存储 + 默认 `123456` + 可修改 + 可为空 + 长度不校验 + UI 可查看。

**存储位置**：`config.yaml` 的 `services.{kind}.{version}.password`（各平台 XDG 用户配置目录内的 `phpo` 子目录）；文件权限 `0600`。不再有 `~/phpo/.env`，SQLite 不再持有 `env` 表与 `dir_ready` 表；前端 `app.env.*` 契约经快照 `env` 扁平键合成保持不变。

### 5.3 templates 目录

**按服务种类分，不按版本分。**

### 5.4 版本策略

**校验规则（唯一保留）**：非空 / 无 `/` `\` / 无 `..` / 无 `\x00` / 长度 ≤ 128 / 首尾 trim。

### 5.5 PHP 版本切换准确性

**版本 → 上游映射**：`UpstreamFor(version) = "php-{version}-fpm:9000"`。

**切换流程**：10 步（Preflight → 读取 → 计算 → 校验 → 写盘 → `nginx -t` → `reload` → 落地 → 返回 → 验证）。

### 5.6 状态同步

**后端唯一权威 + 前端订阅事件**。

**事件协议**：

| 事件 | 载荷 | 时机 |
|------|------|------|
| `state:changed` | `{ snapshot }` | 后端状态变化后 |
| `service:changed` | `{ kind, version, running }` | 单服务状态变化 |
| `task:log` | `{ id, level, text }` | 任务输出一行 |
| `task:progress` | `{ id, step, total }` | 进度变化 |
| `task:done` | `{ id, status, duration }` | 任务结束 |
| `update:available` | `{ version, changelog, size }` | 发现新版本 |
| `update:progress` | `{ stage, percent, speed }` | 升级进度 |
| `update:done` | `{ status, version }` | 升级完成 |
| `docker:cleanup` | `{ stage, resource, action }` | 清理进度 |
| `docker:orphan-found` | `{ resources: []Resource }` | 发现孤儿资源 |
| `docker:state-drift` | `{ expected, actual }` | 状态漂移 |
| `cache:hit` | `{ kind, version, source, size }` | 命中缓存 |
| `cache:miss` | `{ kind, version, action }` | 未命中缓存 |
| `cache:promote` | `{ kind, version, entries }` | 提升到缓存 |
| `cache:corrupted` | `{ kind, version, entry }` | 缓存损坏 |
| `cache:cleanup` | `{ mode, freed_bytes }` | 缓存清理完成 |
| `cache:tempdir-cleared` | `{ path, reason }` | 临时目录已清空 |

**事件名冻结为上表 17 个**；队列与进度不新增事件名，按下述三条承载：

1. **队列详情走快照**：`Snapshot.Tasks`（`model.TaskBoard{ Running *TaskBrief, Pending []TaskBrief }`）随 `state:changed` 实时推送——`Running` 是当前任务（含 `step / total` 进度），`Pending` 是其后的 FIFO 排队项。任务状态仍为 **4** 个，运行中 / 排队中由所在分区表达，不设第 5 态。
2. **实时日志与进度走 `task:*` 事件**：`task:log` 逐行输出（5 种 level），`task:progress` 变更步骤，`task:done` 收尾。`duration` 是 Go `time.Duration`，JSON 序列化为**纳秒**，前端须自行换算单位。
3. **历史与失败原因走账本**：任务退出时由 `internal/task/ledger.go` 把终态连同等效日志写回 `operations` 表（迁移 `0008` 加 `task_id / label / logs` 三列，日志取尾部 500 行）。落账失败**不改变**任务成败判定；失败原因取最后一条 `err` 级 `task:log`，并在 `operations.error` / `operations.logs` 留档。

`task.Manager` 串行执行：嵌套提交返回 `ErrBusy`，重复标签返回 `ErrQueued`，排队项可经 `CancelQueued(id)` 撤回。preflight **不再持有「已有任务在跑」的全局守卫**——并发写操作按 FIFO 排队，不作校验错误。

### 5.7 doctor 环境诊断

| 检查项 | 失败时的建议 |
|-------|-----------|
| Docker 已安装 | 「请下载 Docker Desktop：[链接]」 |
| Docker 正在运行 | 「Docker 未运行。请启动 Docker Desktop。」 |
| Docker 版本 | 「版本较旧（< 20.10），部分功能可能不可用。是否继续？」（警告） |
| 80 端口可用 | 「80 端口已被占用。新建站点会以降级态创建（暂不发布端口、暂不写 vhost），或改用其他端口。」 |
| 磁盘空间 | 「磁盘剩余 < 10GB。容器可能无法启动。」（警告） |
| PHPO_HOME 可写 | 「目录无写权限。请检查权限。」 |
| WWW_ROOT 可写 | 同上 |
| hosts 可写 | 「无法修改 hosts。请以管理员身份运行。」 |
| 网络可访问 Docker Hub | 「无法访问 Docker Hub。请检查网络或配置镜像加速。已缓存的版本仍可安装。」 |
| GitHub Releases 可访问 | 「无法检查更新。不影响使用。」 |
| Docker 资源清洁 | 「发现 N 个孤儿资源，点击清理」 |
| Docker 状态一致 | 「检测到状态漂移，点击校准」 |
| 离线缓存完整性 | 「N 个缓存条目，M 个校验失败，点击查看」 |
| 离线缓存占用 | 「缓存占用 X GB，点击清理」 |
| 临时目录残留 | 「检测到临时目录残留，点击清空」 |

### 5.8 站点端口策略

**新建站点端口冲突：不顺延、不报错、不阻断**——保留用户所填端口，仅弹框告警并把站点降级（vhost 暂不落盘、站点端口暂不发布到 nginx），站点照常创建；腾出端口（卸载冲突服务 / 删除冲突站点）或改一次端口即自愈。

**改已有站点端口**：默认 80，被占用则在 1–65535 内顺延首个可用端口，不报错、无窗口上限；用户可指定任意端口（1–65535）。

**站点端口的占用判定**：以权威快照的逻辑占用表（`store.CollectUsedPorts`：mysql/pgsql/redis 服务端口 + 已有站点端口）为准，preflight 与服务层共用同一判据，不另立标准。

**服务端口**（安装服务 / 改配置）占用时仍报 `portInUse` 错误，由用户自行处理，不自动顺延。

### 5.9 应用版本检查和升级

**启动时 + 每 24 小时 + 用户手动；SHA256 + Ed25519 双校验。**

### 5.10 vhost 指令放开

**放开 `proxy_pass`、`return`、`rewrite`、`location`、`auth_basic`、SSL 证书、`load_module` 等**。

**唯一保留**：vhost 写入前必须 `nginx -t` 通过。

### 5.11 域名策略

**允许 `localhost`、单段名、`.local`、`.dev`、泛域名、多域名、IDN 等**。

### 5.12 路径策略

**允许站点根在 WWW_ROOT 外（警告）**。

### 5.13 Docker 操作清洁机制

（见 v2.4.0 详细内容，本节不变）

#### 5.13.1 六项保证

| # | 保证 | 说明 |
|---|------|------|
| 1 | 幂等性 | 任何操作重复执行结果一致 |
| 2 | 原子性 | 要么全成功，要么全回滚 |
| 3 | 隔离性 | `phpo-` 前缀命名空间 |
| 4 | 一致性 | Docker 实际状态 ≡ SQLite 状态 |
| 5 | 可清理性 | 一键清空 phpo 资源 |
| 6 | 可恢复性 | 失败回到操作前 |

#### 5.13.2 资源命名规范

| 资源 | 命名 |
|------|------|
| 容器 | `phpo-{kind}-{version}` |
| 网络 | `phpo-network` |
| 卷 | `phpo-{kind}-{version}-{purpose}` |
| 网络别名 | `{kind}-{version}`（如 `php-8.4-fpm`） |

#### 5.13.3 原子性与回滚

**Task 三阶段**：Pre-Clean → Execute → Post-Verify。

**Step 接口**：`Name() / Execute() / Rollback() / Cleanup() / Cancelable()`。

#### 5.13.4 幂等操作清单（8 个）

安装 / 卸载 / 启动 / 停止 / 重装 / 创建站点 / 删除站点 / 导入。

#### 5.13.5 孤儿资源扫描

启动时 + 每 24 小时 + 手动。默认保留卷（用户可确认删除）。

#### 5.13.6 清理模式（3 种）

保守 / 标准 / 激进。

#### 5.13.7 回收站机制

`<用户数据目录>/trash/`（Linux `~/.config/phpo/trash/`），7 天保留。

#### 5.13.8 操作前后自检

CI 无人值守支持。

#### 5.13.9 状态校准

启动时 + 每次任务后 + 手动。

#### 5.13.10 操作审计日志

`<用户数据目录>/logs/operations.log`（JSON Lines；Linux `~/.config/phpo/logs/operations.log`，见 `internal/config/userdata.go` 的 `AuditLogPath`）。

#### 5.13.11–12 导入/重装清洁

**导入**：清空目标命名空间 → 恢复数据 → 创建容器。
**重装**：卸载（保留卷）→ 安装（复用卷）。

#### 5.13.13 明确禁止

不检查冲突 / 不回滚 / 留无名资源 / 删用户数据 / 重装清数据 / 导入不清空 / 状态不一致 / 静默失败 / 非幂等 / 无审计。

### 5.14 离线缓存机制

**本节定义 phpo 的离线缓存完整机制。**

> **核心原则一句话**：**装任何镜像/扩展必先查缓存 · 命中零网络 · 未命中临时下载编译 · 成功后提升到缓存 · 无论成败均清空临时目录 · 断网重装靠缓存 · 内网开发靠缓存。**

#### 5.14.1 设计目标

| # | 目标 | 说明 |
|---|------|------|
| 1 | 加速安装 | 已缓存内容无需重复下载 |
| 2 | 支持断网 | 内网/弱网环境可完成安装 |
| 3 | 节省流量 | 团队/个人重复安装零流量 |
| 4 | 提高可靠性 | 网络抖动不中断安装 |
| 5 | 可管理 | 用户可查看、清理、迁移缓存 |
| 6 | 防污染 | 无论成败，临时目录必须清空 |

#### 5.14.2 目录结构

**离线缓存根目录**：`~/phpo/offline/`（**持久**）

**临时目录**：`~/phpo/{kind}/{version}/ext/`（**单任务，任务结束即清空**）

**完整结构**：

```
~/phpo/offline/                       # 缓存根目录（持久）
├── php/
│   └── {version}/
│       ├── image.tar                # Docker 镜像（docker save 产物）
│       ├── apk/                     # apk 依赖包
│       │   ├── {package}-{ver}.apk
│       │   └── ...
│       ├── pecl/                    # pecl 扩展包
│       │   ├── {extension}-{ver}.tgz
│       │   └── ...
│       └── manifest.json            # 缓存清单
├── mysql/
│   └── {version}/
│       ├── image.tar
│       └── manifest.json
├── pgsql/
│   └── {version}/
│       ├── image.tar
│       └── manifest.json
├── redis/
│   └── {version}/
│       ├── image.tar
│       └── manifest.json
└── nginx/
    └── {version}/
        ├── image.tar
        └── manifest.json

~/phpo/{kind}/{version}/ext/           # 临时目录（单任务，结束即空）
├── apk/                              # 编译期间临时存放
│   └── {下载中}.apk
└── pecl/
    └── {下载中}.tgz
```

**manifest.json 结构**：

```json
{
  "schema_version": 1,
  "kind": "php",
  "version": "8.4",
  "created_at": "2026-09-18T12:00:00Z",
  "updated_at": "2026-09-18T14:30:00Z",
  "image": {
    "name": "php:8.4-fpm",
    "digest": "sha256:abc123...",
    "size": 450000000,
    "sha256": "def456...",
    "cached_at": "2026-09-18T12:00:00Z"
  },
  "apk": [
    {
      "name": "libzip-1.10.1.apk",
      "sha256": "aaa111...",
      "size": 123456,
      "cached_at": "2026-09-18T12:30:00Z"
    }
  ],
  "pecl": [
    {
      "name": "redis-6.0.2.tgz",
      "sha256": "bbb222...",
      "size": 234567,
      "cached_at": "2026-09-18T13:00:00Z"
    }
  ]
}
```

#### 5.14.3 缓存命中优先级（核心逻辑）

**唯一优先级：离线缓存 > 网络**。

**安装服务（Docker 镜像）**：

```
1. 检查 ~/phpo/offline/{kind}/{version}/image.tar
   ├─ 存在
   │   ├─ 校验 SHA256（对比 manifest.json 中的值）
   │   │   ├─ 通过 → docker load -i image.tar（零网络）
   │   │   │         → 发射 cache:hit 事件
   │   │   │         → 完成
   │   │   └─ 失败 → 标记缓存损坏
   │   │              → 提示用户
   │   │              → 回退到网络
   │   └─ （损坏则走网络）
   └─ 不存在
       → docker pull {image}（网络）
       → docker save -o {tmp}/image.tar
       → 校验
       → mv {tmp}/image.tar ~/phpo/offline/{kind}/{version}/image.tar
       → 更新 ~/phpo/offline/{kind}/{version}/manifest.json
       → 清空 {tmp}
       → 发射 cache:miss + cache:promote + cache:tempdir-cleared 事件
       → 完成
```

**安装 PHP 扩展（apk/pecl）**：

```
1. 判断扩展类型（apk / pecl）
2. 检查 ~/phpo/offline/php/{version}/{type}/{package}
   ├─ 存在
   │   ├─ 校验 SHA256（对比 manifest.json 中的值）
   │   │   ├─ 通过 → 从缓存复制到临时目录 ~/phpo/php/{version}/ext/{type}/
   │   │   │         → 编译安装
   │   │   │         → **清空临时目录**（防污染下次使用）
   │   │   │         → 发射 cache:hit + cache:tempdir-cleared 事件
   │   │   └─ 失败 → 标记缓存损坏
   │   │              → 回退到网络
   │   └─ （损坏则走网络）
   └─ 不存在
       → 下载到 ~/phpo/php/{version}/ext/{type}/（网络）
       → 编译安装
       │   ├─ 成功
       │   │   → mv ~/phpo/php/{version}/ext/{type}/{package} ~/phpo/offline/php/{version}/{type}/
       │   │   → 更新 ~/phpo/offline/php/{version}/manifest.json
       │   │   → **清空 ~/phpo/php/{version}/ext/**（防污染下次使用）
       │   │   → 发射 cache:miss + cache:promote + cache:tempdir-cleared 事件
       │   └─ 失败
       │       → **清空 ~/phpo/php/{version}/ext/**（防污染下次使用）
       │       → 发射 cache:tempdir-cleared 事件（reason: "compile_failed"）
       │       → 报错
3. 重建镜像 + 重启容器
```

#### 5.14.4 临时目录生命周期（严格）

**临时目录路径**：`~/phpo/{kind}/{version}/ext/`

| 时机 | 行为 |
|------|------|
| 任务开始 | **清空临时目录**（如果存在上次残留） |
| 下载中 | 下载到临时目录 |
| 编译中 | 从临时目录读取 |
| **编译成功** | **文件从临时目录移到离线缓存 → 清空临时目录** |
| **编译失败** | **清空临时目录** |
| **任务取消** | **清空临时目录** |
| **任务结束** | **临时目录必然不存在** |
| **应用启动** | **扫描并清空所有残留临时目录** |

**清空方式**：

```go
func ClearTempDir(kind, version string) error {
    path := filepath.Join(PhpoHome, kind, version, "ext")
    if _, err := os.Stat(path); os.IsNotExist(err) {
        return nil  // 不存在则视为已清空
    }
    if err := os.RemoveAll(path); err != nil {
        return fmt.Errorf("清空临时目录失败: %w", err)
    }
    return nil
}
```

**清空时机（4 个必清）**：

1. **编译成功后**（防止下载残留）
2. **编译失败后**（防止失败产物残留）
3. **任务取消后**（防止中断残留）
4. **应用启动时**（防止崩溃残留）

**发射事件**：每次清空都发射 `cache:tempdir-cleared` 事件。

#### 5.14.5 SHA256 校验

**校验时机**：缓存命中时 / 缓存提升时 / 定期扫描时。

**校验失败处理**：标记损坏 → 发射 `cache:corrupted` → 提示用户 → 回退网络。

#### 5.14.6 缓存清理（3 种模式）

| 模式 | 清理内容 | 适用场景 |
|------|---------|---------|
| 保守 | 只清理损坏的缓存条目 | 日常维护 |
| 标准 | 保守 + 清理 N 天未使用的缓存 | 定期清理 |
| 激进 | 标准 + 清理所有缓存（除正在使用的） | 释放空间 |

#### 5.14.7 缓存统计（UI 展示）

**OfflineView**（离线缓存视图）：展示总占用、条目数、最近验证时间、每项详情。

#### 5.14.8 断网场景支持

网络不可达时，只允许使用已缓存的资源；未缓存的资源拒绝安装并提示。

#### 5.14.9 缓存迁移（可选）

可手动拷贝 `~/phpo/offline/` 或通过备份恢复功能包含离线缓存。

#### 5.14.10 缓存 API（Service 层）

```go
type OfflineService interface {
    ListCacheEntries(ctx context.Context) ([]CacheEntry, error)
    GetCacheEntry(ctx context.Context, kind, version string) (*CacheEntry, error)
    GetCacheStats(ctx context.Context) (*CacheStats, error)
    VerifyCacheEntry(ctx context.Context, kind, version string) (*VerifyResult, error)
    VerifyAllCache(ctx context.Context) (*VerifyAllResult, error)
    CleanupCache(ctx context.Context, mode CleanupMode) (*CleanupResult, error)
    RemoveCacheEntry(ctx context.Context, kind, version string) error
    LookupImage(ctx context.Context, kind, version string) (*ImageCacheResult, error)
    LookupExtension(ctx context.Context, phpVersion, extType, extName string) (*ExtCacheResult, error)
    PromoteImage(ctx context.Context, kind, version, tarPath string) error
    PromoteExtension(ctx context.Context, phpVersion, extType, filePath string) error
    ClearTempDir(ctx context.Context, kind, version, reason string) error
}
```

#### 5.14.11 缓存事件（前端订阅）

| 事件 | 载荷 | 说明 |
|------|------|------|
| `cache:hit` | `{ kind, version, source, size }` | 命中缓存 |
| `cache:miss` | `{ kind, version, action }` | 未命中缓存 |
| `cache:promote` | `{ kind, version, entries }` | 提升到缓存 |
| `cache:corrupted` | `{ kind, version, entry }` | 缓存损坏 |
| `cache:cleanup` | `{ mode, freed_bytes }` | 缓存清理完成 |
| `cache:tempdir-cleared` | `{ path, reason }` | 临时目录已清空 |

#### 5.14.12 明确禁止

- ❌ 安装 Docker 镜像前不检查离线缓存。
- ❌ 安装 PHP 扩展前不检查离线缓存。
- ❌ 命中缓存后仍走网络下载。
- ❌ 缓存命中不校验 SHA256。
- ❌ 缓存损坏后不提示用户。
- ❌ 编译成功后不提升到缓存。
- ❌ **编译成功后不清空临时目录**。
- ❌ **编译失败后保留临时目录**。
- ❌ **任务取消后不清空临时目录**。
- ❌ **应用启动时不扫描临时目录残留**。
- ❌ 临时目录跨任务持久化。
- ❌ 静默跳过缓存检查。
- ❌ 静默清理缓存。
- ❌ 清理正在使用的缓存。

#### 5.14.13 与 Docker 清洁机制的协同

| 场景 | 协同行为 |
|------|---------|
| 安装服务（命中缓存） | 缓存提供 image.tar → `docker load` → 容器创建走清洁流程 |
| 安装服务（未命中缓存） | `docker pull` → `docker save` 到缓存 → 容器创建走清洁流程 |
| 卸载服务 | **不动缓存** |
| 重装服务 | **优先命中缓存**（零网络） |
| 孤儿扫描 | **不扫描缓存** |
| 清理模式「激进」 | **可选清理缓存**（用户确认后） |
| 状态校准 | **不动缓存** |

**关键约束**：缓存不受 Docker 清洁机制影响；缓存与 Docker 资源完全解耦。

---

## 6. 原型 → 生产映射表

| 原型元素 | Go 侧落点 | 前端落点 |
|---------|----------|---------|
| state 全局对象 | `internal/store/ + SQLite` | `stores/appState.ts` |
| persistState / hydrateState | `internal/store/snapshot.go` | — |
| preflight()（17 action） | `internal/preflight/preflight.go` | `composables/usePreflight.ts` |
| NEEDS_HOME（15 action） | `internal/preflight/preflight.go`（`needsHome` map） | — |
| validators | `internal/preflight/validators.go` | `composables/usePreflight.ts`（即时反馈）+ `utils/`（无独立 validate.ts） |
| VHosts（9 方法） | `internal/vhost/manager.go` | `api/site.ts` |
| parseVhost / replaceListen | `internal/vhost/parse.go` | — |
| replacePhpUpstream | `internal/vhost/upstream.go` | — |
| REWRITE_PRESETS（9 种） | `internal/vhost/rewrite.go` | `constants/rewrite.ts` |
| derivePaths | `internal/config/paths.go` | `stores/appState.ts`（快照 `env` 扁平键） |
| hostToContainer | `internal/config/paths.go` | `utils/path.ts` |
| MOUNTS / resolveMounts | `internal/engine/mount.go` | `MountList.vue` |
| DEFAULT_CONFIGS | `internal/template/templates/` | — |
| defaultVhost | `internal/template/templates/vhost.conf.tmpl` | — |
| runTask / playTask | `internal/task/manager.go` | `composables/useTask.ts` |
| buildScript 日志 | `internal/task/emitter.go` | `TaskDrawer.vue` |
| applyStateChange | `internal/task/task.go` + `internal/store/sync.go` | — |
| render() 全量重绘 | — | `composables/useStateSync.ts` |
| cancelTask | `internal/task/manager.go#Cancel / CancelQueued` | `api/task.ts` |
| 任务队列 + 实时进度 | `internal/task/manager.go`（串行 FIFO）+ `model.TaskBoard`（随 `Snapshot.Tasks` 推送） | `taskStore.ts + TaskDrawer.vue` |
| 任务账本（历史 + 失败原因） | `internal/task/ledger.go` + `internal/store/operation.go`（迁移 0008） | `TaskDrawer.vue`（历史分区）+ 设置页审计入口 |
| genPassword | `internal/config/password.go` | — |
| suggestPortFor | `pkg/port/suggest.go` | `utils/format.ts` |
| cmpVer | `pkg/version/compare.go` | `utils/format.ts` |
| MESSAGES | —（后端不出文案，错误码在 `pkg/errs/codes.go`） | `locales/zh-CN.ts / en-US.ts` |
| THEMES | — | `styles/themes/*.css` |
| UI_SCALE / zoom 补偿 | — | `useZoom.ts + useResize.ts` |
| CMD_ITEMS（20 命令） | — | `CmdPalette.vue` |
| openDangerConfirm | — | `DangerConfirm.vue` |
| openHomeSetupWizard | `internal/service/wizard_service.go` | `HomeSetupWizard.vue` |
| tray-sim / tray-menu | `internal/ui/tray.go + menu.go` | `AppTrayMenu.vue` |
| doctor 环境诊断 | `internal/service/doctor_service.go` | `OverviewView.vue` |
| backup / restore | `internal/service/backup_service.go` | `BackupView.vue` |
| offline 离线缓存 | `internal/cache/* + internal/service/offline_service.go` | `OfflineView.vue` + `useCache.ts` + `cacheStore.ts` |
| 4 条旁路 | `internal/task/steps/*.go` | 统一走 TaskDrawer |
| 密码明文存储 | `internal/config/configstore.go`（config.yaml） | `PasswordField.vue` |
| SVC_META.suggested | `internal/config/versions.go`（仓库无 `configs/`） | `constants/service.ts` |
| php-select change | `internal/service/site_service.go#SwitchPHP` | `SitesView.vue` 行内下拉 + `usePhpSwitch.ts` |
| hosts 绑定 | `internal/vhost/hosts/`（三平台提权） | `SitesView.vue + api/site.ts#addHosts` |
| stopped 反向推导 | `internal/engine/calibrate.go` | `useStateSync.ts` |
| 端口默认 80 | `pkg/port/suggest.go` | `SiteAddModal.vue` |
| 端口顺延 | `pkg/port/sequence.go` + `internal/store/port.go` | `usePortSuggest.ts` |
| 任意端口 | `internal/store/port.go` | `SiteAddModal.vue` |
| 任意版本号 | `internal/config/versions.go` | `InstallModal.vue` |
| 任意域名 | `internal/preflight/validators.go` | `SiteAddModal.vue` |
| WWW_ROOT 外路径 | `internal/preflight/rules_site.go` | `SiteAddModal.vue` |
| 应用版本检查 | `internal/updater/checker.go` | `useUpdater.ts + updaterStore.ts` |
| 应用升级 | `internal/updater/* + steps_update.go` | `UpdateModal.vue` |
| Docker 清洁 | `internal/engine/cleaner.go + orphan.go + idempotent.go` | `CleanupView.vue` |
| 孤儿资源扫描 | `internal/engine/orphan.go` | `useCleanup.ts` |
| 幂等操作 | `internal/engine/idempotent.go` | — |
| 操作后验证 | `internal/engine/verify.go` | — |
| 回收站 | `internal/engine/trash.go + internal/store/trash.go` | `TrashViewer.vue` |
| 操作审计 | `internal/engine/audit.go + internal/store/operation.go` | 设置页入口 |
| 状态校准 | `internal/engine/calibrate.go` | doctor 入口 |
| 清理任务 | `internal/service/cleanup_service.go + internal/engine/{cleaner,trash}.go`（无独立 steps_cleanup.go） | `CleanupModal.vue`（承载 `CleanupView.vue`） |
| 端口占用三档 | `pkg/port`（`Options.KeepOnConflict` / `Result.Occupied`）+ `internal/preflight`（`conflictKeepWarn` / `conflictAdvance`）+ `store/port.go` | `SiteAddModal.vue`（告警降级）· `usePortSuggest.ts`（改端口顺延） |
| 镜像缓存命中 | `internal/cache/image_cache.go#LookupImage` | `CacheHitBadge.vue` |
| 扩展缓存命中 | `internal/cache/extension_cache.go#LookupExtension` | `CacheHitBadge.vue` |
| 临时目录 | `internal/cache/tempdir.go` | — |
| 编译成功后提升 | `internal/cache/promote.go` | `cache:promote` 事件 |
| 临时目录清空 | `internal/cache/tempdir.go#ClearTempDir` | `cache:tempdir-cleared` 事件 |
| 缓存清单 | `internal/cache/manifest.go` | `CacheDetailModal.vue` |
| 缓存校验 | `internal/cache/verifier.go` | `OfflineView.vue`「全部校验」按钮 |
| 缓存清理 | `internal/cache/cleaner.go` | `CacheCleanupModal.vue` |
| 缓存统计 | `internal/cache/stats.go` | `OfflineView.vue` |

---

## 7. 关键决策

### 决策 1–21（见 v2.4.0，此处省略）

### 决策 22：离线缓存采用「先查缓存 + 未命中才下载 + 成功才提升 + 无论成败清临时」

**核心决策**：**phpo 安装任何 Docker 镜像或 PHP 扩展时，必须先查离线缓存。**

#### 理由

1. **画像 A**：反复安装 PHP 版本，缓存避免重复下载。
2. **画像 B**：新同事首次装机 → 从团队共享缓存快速安装。
3. **画像 C**：网络不稳定，缓存避免安装中断。
4. **画像 D**：希望控制缓存策略。
5. **画像 E**：CI 环境需要稳定的离线安装。
6. **画像 F**：内网/离线环境 **刚需**。

#### 具体规则（四条）

**规则 1：装前必查**：装任何镜像/扩展前，必须先查离线缓存目录。

**规则 2：命中零网络**：缓存命中 + SHA256 通过 → 直接使用（零网络）。

**规则 3：未命中走临时**：下载到 `~/phpo/{kind}/{version}/ext/` → 编译/加载 → 成功 → 提升到 `~/phpo/offline/{kind}/{version}/`。

**规则 4：无论成败清临时**：编译成功/失败/取消/崩溃后，临时目录必须清空。

#### 明确禁止

- ❌ 安装前不检查离线缓存。
- ❌ 命中缓存后仍走网络下载。
- ❌ 缓存命中不校验 SHA256。
- ❌ 编译成功后不提升到缓存。
- ❌ 编译成功后不清空临时目录。
- ❌ 编译失败后保留临时目录。
- ❌ 任务取消后不清空临时目录。
- ❌ 应用启动时不扫描临时目录残留。
- ❌ 临时目录跨任务持久化。

---

## 8. 跨平台差异矩阵

| 差异维度 | Windows | macOS | Linux |
|---------|---------|-------|-------|
| Docker 运行时 | Docker Desktop（WSL2） | Docker Desktop | 原生 Docker Engine |
| 挂载性能 | ⚠️ VM 转发，慢 | ⚠️ VM 转发，慢 | ✅ 原生 |
| 端口 80 | ✅ 无需特权 | ⚠️ 需 root/setcap | ⚠️ 需 root/CAP |
| hosts 提权 | UAC | osascript admin | polkit |
| 用户数据目录 | `%APPDATA%\phpo\` | `~/Library/Application Support/phpo/` | `~/.config/phpo/` |
| 路径解析 | ⚠️ 无 `~` | ✅ | ✅ |
| 路径大小写 | 不敏感 | 默认不敏感 | 敏感 |
| 系统托盘 | ✅ | ✅ | ⚠️ 需 libappindicator |
| 代码签名 | EV 证书 | Developer ID + 公证 | 无 |
| `config.yaml` 权限 | NTFS ACL | `chmod 600` | `chmod 600` |
| 升级安装方式 | NSIS 静默安装 | .app 替换 | AppImage 替换 |
| 缓存路径 | `%USERPROFILE%\phpo\offline\` | `~/phpo/offline/` | `~/phpo/offline/` |
| 缓存文件权限 | NTFS ACL | `chmod 644` | `chmod 644` |
| 临时目录 | `%USERPROFILE%\phpo\{kind}\{version}\ext\` | `~/phpo/{kind}/{version}/ext/` | `~/phpo/{kind}/{version}/ext/` |

### 8.1–8.7（见 v2.6.0）

---

## 9. 风险清单

| 编号 | 风险 | 缓解措施 |
|-----|------|---------|
| R1–R64 | （见 v2.4.0） | — |
| R65–R82 | （见 v2.6.0） | — |
| **R83** | **Agent 未遵守 §3.4 编码准则导致代码质量下降** | **Code Review 时按 §3.4 逐项检查；违反项必须整改**（v2.7） |
| **R84** | **Agent 猜测需求导致实现偏离** | **遇到不确定时必须询问；不允许默默假设**（v2.7） |
| **R85** | **Agent 过度抽象导致代码复杂** | **简洁优先；YAGNI；Code Review 时识别过度设计**（v2.7） |
| **R86** | **Agent 顺手修改无关代码引入 Bug** | **精准修改；只动任务相关代码；无关问题仅提醒**（v2.7） |
| **R87** | **Agent 声称「完成」但无验证标准** | **目标驱动执行；必须先定义成功标准；必须有测试用例**（v2.7） |

---

## 10. 里程碑

| 阶段 | 内容 | 验收标准 |
|------|------|---------|
| **M0 规格冻结** | 原型评审定稿 | 规格文档签认 |
| **M1 骨架** | Wails 3 骨架 + 前端拆分 + 事件总线 | 原型界面 1:1 运行 |
| **M2 底座** | 纯函数层 + preflight 后端化 + SQLite + 密码策略 + 版本策略 + 状态同步核心 + 端口顺延核心 + 升级检查骨架 + Docker 清洁核心 + 离线缓存核心 | 见下方清单 |
| **M3 服务线（PHP + Nginx）** | 真实容器 + 任务引擎 + 版本拉取 + Calibrate + 幂等 + 镜像缓存优先 | 见下方清单 |
| **M4 站点闭环** | 建站 + PHP 切换 + 状态同步 + 端口顺延 UI + 任意端口 + 任意域名 + WWW_ROOT 外路径 | 见下方清单 |
| **M5 数据服务** | MySQL / PG / Redis 全生命周期 + 空密码支持 + 任意密码长度 + 数据卷保留 + 镜像缓存优先 | 见下方清单 |
| **M6 运维面** | 备份/恢复 + 离线缓存 + doctor + 应用升级 + 扩展缓存优先 + 提升 + 临时目录清空 | 见下方清单 |
| **M7 发布** | 三平台安装包 + 签名 + 公证 + 文档 | `release.yml` 一键出包 |

### 10.1–10.5 验收项（见 v2.6.0）

### 10.6 Agent 编码准则验收（v2.7 新增）

- [ ] 每个 PR 是否遵守 §3.4 编码准则？
- [ ] 遇到不确定时是否询问而非猜测？
- [ ] 是否存在过度抽象？（应为否）
- [ ] 是否只改任务相关代码？（应为是）
- [ ] 是否先定义成功标准？（应为是）
- [ ] 是否先写测试用例？（应为是）

---

## 11. 交付物

### 11.1 三平台安装包

- `phpo-setup-x64.exe`（Windows NSIS）+ `.sig` —— 资源已就位（`build/windows/`），三平台签名链未落地
- `phpo-x64.dmg`（macOS 含公证）+ `.sig` —— 资源已就位（`build/darwin/`），公证未接入
- `phpo-x86_64.AppImage`（Linux）+ `.sig`
- `phpo_x.x.x_amd64.deb / .rpm` —— **已在本机产出并真机验证**（`task release:local` → nfpm，见 `docs/T705-Linux真机冒烟记录.md`）
- `checksums.txt`（`scripts/gen-checksums.sh`）

### 11.2 中文文档

`docs/` 现有 32 篇（含 `CHANGELOG.md` 与两份里程碑留痕：`M6-集成验收记录.md`、`T705-Linux真机冒烟记录.md`）：

- 项目概述 / 架构设计 / 目录规范
- 领域模型 / 接口契约 / 事件流协议
- 界面规格 / 跨平台差异
- 密码策略 / 版本策略 / 域名策略 / 路径策略 / 端口策略
- PHP 切换准确性 / 状态同步 / 应用升级
- 最小限制原则 / **编码行为准则**
- Docker 操作规范 / 资源清洁机制 / 回收站机制
- 离线缓存机制 / 缓存清单规范 / 临时目录生命周期
- 任务取消语义 / 离线缓存协议 / 备份脱敏规范
- 打包发布 / 用户手册

### 11.3 开发者工具

- CI 流水线（`.github/workflows/{ci,lint,release}.yml`）
- i18n 键对齐 / 模板一致性 / 资源命名 / 缓存 manifest 四项门禁（`scripts/check-*.go`，`task check` 与 ci.yml 共用）
- 签名与发布辅助：`scripts/sign-release.sh`、`gen-checksums.sh`、`verify-signing-guard.sh`（公钥一致性反推）、`bump-version.sh`、`version.sh`
- 构建编排：`Taskfile.yml`（dev / bindings / build / test / vet / check / package / release:local）
- 测试：与包同目录的 Go 单测（60 个 `*_test.go`，fake/mock 内联）+ `test/integration/*_live_test.go` 真环境用例（`PHPO_LIVE=1` + Docker 可用双重 skip 守护）

> **不包含**：CLI、cobra、keyring、密码加密、密码长度校验、版本号白名单、端口范围限制、域名格式限制、WWW_ROOT 内强制、WebSocket / HTTP 轮询、插件系统、跳过离线缓存的安装实现、临时目录跨任务持久化。

---

## 12. Agent 自查清单

### 12.1 通用检查（见 v2.4.0）

### 12.2 Docker 清洁检查（见 v2.4.0）

### 12.3 离线缓存检查（见 v2.6.0）

### 12.4 硬红线检查（8 条，见 v2.4.0）

### 12.5 编码行为准则检查（v2.7 新增）

**编码前思考**：

- [ ] 是否明确假设而非猜测？
- [ ] 遇到不确定时是否询问而非默默选定？
- [ ] 存在歧义时是否列出多种解释？
- [ ] 有明显更简单做法时是否指出？
- [ ] 发现代码矛盾时是否及时暂停并报告？

**简洁优先**：

- [ ] 是否用最少代码解决问题？
- [ ] 是否为一次性需求创建了抽象层？（应为否）
- [ ] 是否盲目增加扩展性？（应为否）
- [ ] 代码是否可大幅精简？（应为否）
- [ ] 以资深工程师视角，代码是否过于复杂？（应为否）

**精准修改**：

- [ ] 是否仅修改与任务直接相关的代码？
- [ ] 是否顺手优化了相邻代码？（应为否）
- [ ] 是否重构了能正常运行的代码？（应为否）
- [ ] 是否严格匹配项目现有代码风格？
- [ ] 是否删除了本次修改产生的无效导入/变量？
- [ ] 发现死代码时是否仅提醒而非删除？

**目标驱动执行**：

- [ ] 执行任务前是否定义了清晰的成功标准？
- [ ] 「修复 Bug」是否转化为「先写用例复现再修复」？
- [ ] 「新增校验」是否转化为「写异常输入用例」？
- [ ] 「代码重构」是否转化为「确保原有测试通过」？
- [ ] 多步骤任务是否先输出执行计划并标注验证方式？

### 12.6 插件系统检查

- [ ] 是否在 MVP 阶段引入了任何插件相关代码？（应为否）

---

## 13. 总纲变更流程

本文件为**冻结文档**。任何修改必须：

1. 提交变更提案（Change Proposal）。
2. 至少 2 名核心开发者评审通过。
3. 修改「文档版本」字段。
4. 同步更新受影响的其他文档（`docs/` 下）。
5. 若涉及目录结构，同步更新 `docs/目录规范.md` 与 `scripts/check-*.go`。

**未走上述流程的任何修改均视为无效，Agent 有权拒绝执行。**

---

**phpo 项目总纲 v2.9.3**

> **v2.9.3 变更（仅同步既有实现的落点，不改任何策略条款）**：§4.1 目录树按仓库真实文件重排——删去从未落地的
> `configs/`、`internal/i18n/`、`internal/preflight/{common,portprobe,rules_cleanup}.go`、
> `internal/task/steps/{steps_home,steps_extension,steps_offline,steps_backup,steps_cleanup}.go`、
> `pkg/{hash,execx,fsutil}`、`test/{unit,mocks,fixtures,e2e}`、`assets/`、`data/`、`build/signing/`；
> 补入已落地但未登记的 `internal/{config,store,engine,model,service,updater,vhost}` 新文件、`pkg/disk/`、
> 迁移 `0008`、前端 12 视图 / 17 api / 15 composables / 9 constants。§4.2 用户数据目录改为 `os.UserConfigDir()/phpo`
> 实测口径（去掉从未创建的 `phpo.log` / `themes/` / `locales/`），§5.13.7 与 §5.13.10 的路径同步。
> §0.3 补 4 行权威值（事件名 17、队列载体 `Snapshot.Tasks`、账本尾部 500 行、迁移 8），并把 preflight
> 两项来源指到 `internal/preflight/preflight.go` 的 `Run`/`AllActions`/`needsHome` 与对账测试。
> §5.6 补「队列/进度/历史三段承载」说明（**不新增事件名、不改 17 事件协议**）；§6 映射表的 `NEEDS_HOME`、
> `MESSAGES`、`SVC_META.suggested`、`cancelTask`、`PhpSelect.vue`、`stores/setting.ts`、`utils/validate.ts`、
> `steps_cleanup.go` 等落点改为真实文件；§11 标注 Linux 已出包真机验证、win/mac 签名链未落地。
> 端口策略、密码策略、离线缓存、硬红线 8 条、三段式写操作等**行为条款一字未改**。

- 技术栈：Wails ≥ 3 + Go ≥ 1.27 + Vue 3.5+ + TypeScript
- 目标：Windows / macOS / Linux 三平台桌面应用
- 形态：**仅 GUI，不提供 CLI**
- 配置存储：**单一 `config.yaml`（YAML，各平台 XDG 用户配置目录内 `phpo` 子目录）承载根目录 + 明文密码 + 端口；SQLite 仅存运行态且延迟建库（两根未落地即首启零落盘）；`dirReady` 由快照派生、不落库；不再有 `~/phpo/.env` / `~/.phpo/config.json`**
- 密码：**明文，默认 `123456`，可修改，可为空，长度不校验，UI 可查看**
- 版本：**不限制字符集，仅做路径安全校验**
- 端口：**站点端口默认 80、用户可指定任意端口；新建站点占用不顺延（告警 + 站点降级，站点照常创建）；改已有站点端口占用才顺延 1–65535 首个可用（不报错）；服务端口占用报错**
- 域名：**允许 `localhost`、单段名、任意后缀、多域名、泛域名**
- 路径：**允许 WWW_ROOT 外（警告）**
- vhost：**放开 `proxy_pass`、`ssl_certificate`、`load_module` 等（保留 nginx -t）**
- PHP 切换：**精确对应 `php-{version}-fpm:9000`**
- 状态同步：**后端唯一权威 + 前端订阅事件**
- 升级：**支持版本检查和自动升级（SHA256 + Ed25519 双校验）**
- Docker 清洁：**所有操作幂等、原子、隔离、一致、可清理、可恢复**
- 离线缓存：**装任何镜像/扩展必先查缓存 · 命中零网络 · 未命中临时下载编译 · 成功后提升到缓存 · 无论成败均清空临时目录 · 断网重装靠缓存 · 内网开发靠缓存**
- **编码准则：编码前思考 · 简洁优先 · 精准修改 · 目标驱动执行**
- 插件：**不做**
- **核心原则：最小限制 + 警告代替阻止 + 用户是程序员 + Docker 操作干净 + 离线优先 + 临时目录必清 + 编码准则**
- **硬红线：仅 8 条**
- 状态：FROZEN（冻结）

> 本文件为项目最高技术总纲，所有其他文档必须与之保持一致。