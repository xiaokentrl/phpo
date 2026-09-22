# phpo 项目总纲（MASTER PLAN）

> **文档类型**：最高项目总纲
> **文档版本**：v2.9.10
> **生效状态**：FROZEN（冻结，禁止未走评审流程修改）
> **效力等级**：★★★ 最高（本项目所有其他文档、代码、注释、测试必须与本文件一致）
> **适用范围**：全体开发者 · CI/CD 流水线 · AI Agent
> **唯一真实来源**：单文件 HTML 原型（前端唯一界面来源.txt）
> **技术栈约束**：Wails ≥ 3 + Go ≥ 1.27 + Vue 3.5+ + TypeScript 5.x
> **目标产物**：Windows / macOS / Linux 三平台桌面应用（**仅 GUI，不提供 CLI**）
> **核心原则**：以真实开发者工作流为标准；最小限制；用户是程序员；**离线优先**
> **端口策略**：站点端口默认 80，用户可指定任意端口；**新建站点端口被占用时不顺延**——仅弹框告警并把站点降级（vhost 暂不落盘、端口暂不发布），站点照常创建；改已有站点的端口时占用才顺延 1–65535 首个可用（不报错、无窗口上限）；服务端口占用仍报错
> **应用升级**：支持版本检查和自动升级
> **配置存储**：单一 `config.yaml`（YAML）落在各平台 XDG 用户配置目录内的 `phpo` 子目录，承载工作根目录 + 自定义缓存根/备份根 + 每服务版本的明文密码/宿主端口/数据目录；SQLite 仅存运行态，且**延迟建库**——两根工作目录写入 `config.yaml` 前不创建用户数据目录；`dirReady` 不落库，由快照按「两根已持久化 + 目录实际存在」派生；不再使用 `./.env` 或 `~/.phpo/config.json`
> **可自定义根**（v2.9.8 新增，见 §5.15）：离线缓存根、备份归档根、以及**每个服务版本的数据目录**皆可由用户编辑路径或浏览选定任意文件夹；三处各自**互斥唯一**——自定义一旦设定即完全取代对应默认根（`./offline/` · `./backups/` · `{KIND_ROOT}/{version}/data`），任一时刻只有一条路径生效，不并存、不做二级回退；唯一校验是路径安全（硬红线 3）
> **PHP 扩展**（v2.9.9 新增，见 §5.16）：每个 PHP 版本一份**全量扩展目录**（73 项 · 8 分组），安装弹窗与「管理扩展」弹窗共用同一份；常用 11 项在安装时**默认勾选**，勾选/取消即本次的**目标扩展集**；容器内编译输出**逐行实时**进抽屉日志，某一项失败必须**点名该扩展**并中止本单（不静默、不吞）；**固化镜像不在本机即容器退回基座重建时，待编译集换成完整目标集**（基座不含原启用集，只装增量会让库里「已启用」而容器里没有，§5.16.2）
> **备份归档**（v2.9.9 新增，见 §5.17）：打包时读不动的条目**跳过并逐目录聚合告警**，不判死整包；mysql / pgsql / redis 在暂停服务**之前**先做**逻辑导出**（`mysqldump` / `pg_dumpall` / `redis-cli --rdb`），产物入归档 `dump/` 前缀——冷拷贝缺的那部分由 dump 补回
> **容器日志出口**（v2.9.9 新增，见 §5.18）：服务容器内进程**不得往宿主 bind 挂载目录写日志文件**（容器 uid 对该目录无写权限即 FATAL 崩溃循环，服务永远启不来）；日志一律走 stderr → 由 Docker 收集；启停必须等**稳定 running**，失败报错带容器日志尾部
> **路径记法**：本文件的 **`./` 一律指 PHPO_HOME 根**（即 `config.yaml` 的 `phpo_home`，由装机向导指向任意目录；`~/phpo` 只是默认值，**打包安装后不得假定工作目录在用户主目录**）。`<用户数据目录>` 仍是各平台 XDG 的 `os.UserConfigDir()/phpo`（`config.yaml` / `phpo.db` / `logs` / `trash` / `updates`），与 PHPO_HOME **不同源**；`~/www/` 是 WWW_ROOT 的默认值（同样可改）。同一记法**同等约束 `docs/` 全部文档、任务工单、代码注释与面向用户的文案（前端 locales）**——只有带「默认」字样的默认值/预填值可写字面量。详见 §0.1.1
> **状态同步**：后端唯一权威；前端只订阅事件、不做乐观更新；**一切操作/日志/队列/请求/响应必须实时同步界面 UI 与抽屉日志**——§5.6 的 17 个事件名逐一有前端落地处（见 §5.6.2，无任务归属的事件走抽屉左栏的「系统日志通道」）
> **密码策略**：明文，默认 `123456`，可修改，可为空，长度不校验，UI 可查看
> **最小限制原则**：除 8 条硬红线外，所有限制放开或降级为警告
> **Docker 清洁原则**：所有操作幂等、原子、可回滚、可清理
> **离线缓存原则**：装任何镜像/扩展必先查缓存 · 命中零网络 · 镜像未命中先探本机镜像库（已有即零网络重建缓存）· 否则下载编译 · 成功后提升到缓存 · 无论成败均清空临时目录 · 断网重装靠缓存 · 内网开发靠缓存
> **编码准则**：**编码前思考 · 简洁优先 · 精准修改 · 目标驱动执行**（v2.7 新增，见 §3.4）

---

## 0. 文档使用规约

### 0.1 效力声明

本文件为 **phpo 项目的最高项目总纲**。当本文件与其他任何文档、注释、口述、历史草案冲突时，**一律以本文件为准**。

### 0.1.1 路径记法（v2.9.4 新增；v2.9.5 把同一记法扩到派生文档、代码注释与面向用户的文案）

本文件描述路径时使用三种记法，**不得混用**：

| 记法 | 含义 | 权威来源 | 说明 |
|------|------|---------|------|
| **`./`** | **PHPO_HOME 根**（工作目录） | `config.yaml` 的 `phpo_home`（`internal/config/configstore.go`），装机向导落地 | 相对记法。默认值为 `~/phpo`（`internal/config/config.go` 的 `DefaultHome`），**但打包安装后位置由用户选定**，可能是任意磁盘/任意路径；因此本文件一律写 `./offline/`、`./{kind}/{version}/ext/`，不再写死 `~/phpo/…` |
| **`<用户数据目录>/`** | 应用自身配置与运行态存储 | `os.UserConfigDir()/phpo`（`internal/config/userdata.go`） | Windows `%APPDATA%\phpo\` · macOS `~/Library/Application Support/phpo/` · Linux `~/.config/phpo/`。**与 PHPO_HOME 不同源、不同生命周期** |
| **`~/www/`** | WWW_ROOT 默认值 | `config.yaml` 的 `www_root` | 同样是可改的根，仅给出默认形态 |

**约束**：

- `./` 段下的所有子路径（`offline/`、`{kind}/{version}/ext/`、`php/<ver>/conf`、`nginx/sites/`、`backups/` 等）都随 PHPO_HOME 移动，**不可在代码或文档里拼死 `~/phpo`**。
- **`./offline/` 与 `./backups/` 是这两类根的「默认值」记法，不是它们的实际位置**（v2.9.8）：用户可把缓存根 / 备份根 / 某服务版本的数据目录自定义到任意文件夹（§5.15）。因此描述**运行时行为**时要说「缓存根（默认 `./offline/`）」而非「`./offline/` 目录」；说成后者等于断言用户没改过根。临时目录 `./{kind}/{version}/ext/` **不可自定义**，始终随 PHPO_HOME，不受此约束。
- 描述 `config.yaml`、`phpo.db`、`logs/operations.log`、`trash/`、`updates/` 时**不得**用 `./`——它们在 `<用户数据目录>/`，不受装机向导的 PHPO_HOME 影响。
- 历史遗留字面量 `~/.phpo/config.json` 是已废弃的旧配置路径，与本记法无关，原样保留以便识别。
- 引用具体代码位置时保持仓库相对路径（如 `internal/cache/tempdir.go`），不加 `./` 前缀。
- **本记法的适用范围不止本文件**：`docs/` 全部文档、`任务工单.md`、`实施顺序.md`、代码注释，以及**面向用户的文案**（前端 `frontend/src/locales/`、向导提示、错误信息）一律同守——文案里出现 `~/phpo/…` 这类运行时子路径，等于向用户断言工作目录固定在主目录，与装机向导可选任意位置的事实矛盾。
- **唯一允许写字面量的情形是「默认值 / 预填值」本身**：`config.DefaultHome`、`config.DefaultWWW`、`config.example.yaml`、向导预填，且**在人可读的文案与注释里必须带「默认」字样**（缺「默认」二字的 `~/phpo` 即视为记法违规）。**两类例外不强求**：① 代码里的常量赋值（如 `DefaultHome = "~/phpo"`、前端 `DEFAULT_HOME`）——其语义已由命名承担；② 单测 / mock fixture 里的示例根字符串（如 `internal/config/paths_test.go`、`api/mockEvents.ts`）——它只是构造派生结果的输入，不代表运行时位置。
- **真机验收留痕**（`docs/M6-集成验收记录.md`、`docs/T705-Linux真机冒烟记录.md`）要写当次实际路径时，一律写成 `./…`（本机 PHPO_HOME 取默认值 `~/phpo`）——先给相对记法，再把具体值作为该次实例注明，不得用具体值代替通用记法。

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
    - **安装任何 Docker 镜像（php/mysql/pgsql/redis/nginx 等）时，必须先到缓存根（默认 `./offline/`，用户可自定义，见 §5.15）下查 `{kind}/{version}/` 目录**；命中则 `docker load`（零网络）；未命中**先探本机 Docker 镜像库**：镜像已在本机就直接 `docker save` 提升到缓存（零网络，`cache:miss` 的 `action=local`），本机也没有才 `docker pull`（`action=pull`）。
    - **安装任何 PHP 扩展（apk/pecl）时，必须先查 `./offline/php/{version}/{apk|pecl}/` 目录**；命中则直接使用（零网络）；未命中才网络下载到临时目录。
    - **临时目录路径固定为 `./{kind}/{version}/ext/`**（如 `./php/8.4/ext/`）。
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
24. **禁止「同一类路径两条并存」**（v2.9.8 新增，见 §5.15）：缓存根、备份根、每服务版本的数据目录都是**互斥唯一**——用户自定义即**完全取代**对应默认根（`./offline/` · `./backups/` · `{KIND_ROOT}/{version}/data`）。不得在自定义后仍回读写默认根、不得把默认根当二级回退去探测、不得在数据目录已自定义时再凭空创建默认的那个；清空自定义值即回落默认根，同样是「一条」。**唯一校验是路径安全（硬红线 3）**，不要求绝对路径、不要求已存在、不限字符集。
25. **禁止「事件发了界面却不动」**（v2.9.8 新增，需求 6 收口，见 §5.6.2）：§5.6 的 17 个事件名**每一个都必须有前端落地处**——要么改写 store/快照，要么逐行进任务抽屉日志；新增事件而无落地处即视为破坏本条。
26. **禁止「容器内命令的输出攒成一段」**（v2.9.9 新增，见 §5.16.3）：容器内 exec（扩展编译、逻辑导出）的 stdout / stderr 必须**去帧后逐行**实时进 `task:log`；失败必须**点名**到具体扩展 / 具体服务，不得只报「命令失败」。Docker exec attach 流带 8 字节帧头，直读原始流即把二进制垃圾打进日志。
27. **禁止备份归档被单个读不动的条目判死**（v2.9.9 新增，见 §5.17）：宿主数据目录由容器内 uid 拥有，逐文件读权限不可保证——读不动的条目**跳过 + 逐目录聚合告警**，整包照常产出；数据靠**逻辑导出**（`mysqldump` / `pg_dumpall` / `redis-cli --rdb`）补齐，不得静默缺项。
28. **禁止服务容器往宿主 bind 挂载目录写日志文件**（v2.9.9 新增，见 §5.18）：该目录由宿主用户创建（0755），容器内进程是另一个 uid，建文件即 `Permission denied` → 进程 FATAL → `unless-stopped` 无限重启，服务永远启不来。日志一律走 stderr 由 Docker 收集；启动必须等**稳定 running**，失败报错必须带容器日志尾部。

### 0.3 数字权威表（Agent 引用禁止出错）

| 项 | 权威值 | 来源 |
|----|-------|------|
| preflight action 数 | **19**（原型 17 + 生产新增 `root-set` / `cache-import`） | `internal/preflight/preflight.go` 的 `Run` switch-case 与 `AllActions`（`ActionCount`） |
| NEEDS_HOME 动作数 | **17** | 同文件 `needsHome` map（`NeedsHomeCount`）；对账测试 `TestActionAndNeedsHomeCounts` |
| PF 校验错误码数 | **28** | `pkg/errs/codes.go`（`CodeCount`；原型 27 + 生产新增 `FileMissing`） |
| §5.6 事件名数 | **17** | 事件协议表（冻结，不新增） |
| 事件前端落地覆盖率 | **17/17**（`update:progress` 由升级弹窗进度条承载，不进日志） | `frontend/src/composables/useStateSync.ts` 的 `landEvent` + `eventNote`（§5.6.2） |
| 可自定义根面数 | **3**（缓存根 · 备份根 · 每服务版本数据目录） | `config.yaml` 的 `offline_root` / `backup_root` / `services.{kind}.{ver}.data_dir`（§5.15） |
| 同类路径生效根数 | **1**（自定义与默认**互斥唯一**，不并存、不做二级回退） | `internal/config/paths.go` 的 `applyRoot` / `DataDirFor` 一处判定（§5.15.1 · 决策 23） |
| PHP 扩展目录条目数 | **73**（内置 **49** ／ pecl **24**，分 **8** 组） | `frontend/src/constants/ext.ts` 的 `EXT_CATALOG`；与 `internal/config/extensions.go` 的 `peclExts` 由 `scripts/check-ext-catalog.go` 对账（§5.16） |
| 安装时默认勾选的常用扩展 | **11** | 同文件 `common: true` → `commonExts(version)` |
| 数据服务逻辑导出数 | **3**（mysql ／ pgsql ／ redis） | `internal/service/backup_service.go` 的 `dumpKinds`（§5.17.2） |
| 归档读不动条目的处置 | **跳过 + 按所在目录聚合告警**（每目录至多列 5 项，另起一行报总数） | `pkg/archive/targz.go` 的 `Skip` + `backup_service.go` 的 `logSkips`（§5.17.1） |
| 容器启动就绪等待 | **12s**（轮询 300ms；见 running 后复验 2s） | `internal/engine/container.go` 的 `startWait` / `startInterval` / `startHold`（§5.18.3） |
| CI 门禁 check 脚本数 | **5** | `scripts/check-{i18n-keys,templates,docker-naming,cache-manifest,ext-catalog}.go`（`task check` 与 `ci.yml` 同步登记） |
| 抽屉两栏默认占比 | **70% ／ 30%**（可左右拖拽，夹取区间 40–80%；双击中缝复位） | `frontend/src/components/business/TaskDrawer.vue`（`.drawer-splitter`）+ `constants/layout.ts` 的 `LAYOUT_LIMITS.split` + `--drawer-split`（§5.6.1） |
| 抽屉系统日志上限 | **200 行**（滚动裁尾） | `frontend/src/stores/taskStore.ts` 的 `SYS_MAX` |
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
| 队列排序 | **提交时间倒序（新任务置顶）** | §5.6.1 |
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
| 离线缓存根目录 | **默认 `./offline/`**（可自定义，见 §5.15） | 见 §5.14.2 |
| 镜像缓存路径 | **默认 `./offline/{kind}/{version}/image.tar`** | 见 §5.14.2 |
| apk 缓存路径 | **默认 `./offline/php/{version}/apk/`** | 见 §5.14.2 |
| pecl 缓存路径 | **默认 `./offline/php/{version}/pecl/`** | 见 §5.14.2 |
| 备份根目录 | **默认 `./backups/`**（可自定义，见 §5.15） | 见 §5.15 |
| 服务数据目录 | **默认 `./{kind}/{version}/data`**（每服务版本可自定义，见 §5.15） | `config.yaml` 的 `services.{kind}.{ver}.data_dir` |
| 临时目录路径 | **`./{kind}/{version}/ext/`**（不可自定义） | 见 §5.14.2 |
| 缓存清单文件 | **`manifest.json`** | 每个版本一份 |
| 缓存命中优先级 | **离线缓存 > 本机 Docker 镜像库 > 网络** | 见 §5.14.3 |
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
| 状态同步 | 后端唯一权威；前端订阅事件；无本地乐观更新；**17 个事件名每一个都有前端落地处**（见 §5.6.2） |
| 任务抽屉 | 日志左**默认 70%** ／ 任务队列右**默认 30%**（中缝可左右拖拽、双击复位，夹取 40–80%）；头部左侧标签固定为「服务」二字；新任务永远在最上面；每行显式显示态（等待中／执行中／已完成 + 兜底位）；无任务归属的 `cache:*`／`docker:*`／`update:*` 事件走**系统日志通道**逐行显示，见 §5.6.1 |
| 可自定义根 | **缓存根 / 备份根 / 每服务版本数据目录**三处可自定义；每一类路径**永远只有一个**（自定义与默认互斥，置空即回落默认）；唯一限制是路径安全，见 §5.15 |
| PHP 扩展目录 | **每版本一份全量目录（73 项 · 8 分组）**，安装弹窗与「管理扩展」弹窗共用；常用 **11** 项默认勾选，勾选/取消即目标扩展集；编译输出逐行进抽屉日志，失败点名扩展并中止（见 §5.16） |
| 备份归档 | 读不动的条目**跳过 + 逐目录聚合告警**，不判死整包；mysql／pgsql／redis 暂停**之前**先**逻辑导出**入归档 `dump/`；恢复侧明示「dump 不自动重放」（见 §5.17） |
| 数据服务运行态 | 容器内进程**不往宿主 bind 目录写日志**（pgsql 走 stderr → Docker 收集）；旧装机的坏配置就地自愈；启停等稳定 running，失败带容器日志尾部（见 §5.18） |
| 端口策略 | 站点端口默认 80、用户可指定任意端口；新建站点占用只告警 + 降级（不改用户所填端口、不阻断建站）；改站点端口占用才顺延；服务端口占用报错 |
| 限制策略 | 最小限制；仅 8 条硬红线；警告代替阻止 |
| Docker 清洁策略 | 所有操作幂等、原子、可回滚、可清理 |
| 离线缓存策略 | 装任何镜像/扩展必先查缓存（**在用户选定的缓存根下**，默认 `./offline/`）；命中零网络；镜像未命中先探本机镜像库、已有即零网络重建缓存，否则下载编译；成功后提升到缓存；支持**手工导入任意文件**为缓存条目；无论成败均清空临时目录 |

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

**落地覆盖要求**（v2.9.8 补，需求 6）：状态同步不止于「快照变了界面跟着变」——**每一次操作、每一条日志、每一个队列变化、每一份请求/响应，都必须实时回流到界面 UI 与任务抽屉日志**。§5.6 的 17 个事件名每一个都必须有前端落地处（改写 store/快照，或逐行进抽屉日志），逐事件对照表见 §5.6.2；「事件发了界面却不动」即视为违反硬红线 4（§0.2 规则 25）。

### 1.8.1 关于可自定义根

**核心策略**（v2.9.8 新增，需求 1/2/7/8）：离线缓存根、备份根、以及**每个服务版本的数据目录**三处路径可由用户自定义（编辑输入框 + 目录选择器）。每一类路径**永远只有一个生效值**，**自定义与默认互斥**——写了自定义即用自定义，置空即回落默认；系统不得同时读写两处。唯一限制是**路径安全**（硬红线 3），此外不加绝对路径、存在性、字符集等任何限制。完整条款见 §5.15。

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

> **phpo 安装任何 Docker 镜像或 PHP 扩展时，必须先查离线缓存；命中则零网络使用；镜像未命中则先探本机 Docker 镜像库——镜像已在本机就直接 `docker save` 重建缓存（零网络），本机也没有才联网下载；扩展未命中则联网下载到临时目录；成功后立刻把文件从临时目录提升到缓存目录，然后清空临时目录；编译失败则清空临时目录。断网重装靠缓存，内网开发靠缓存。**

#### 1.13.1 核心行为（三条铁律）

| # | 铁律 | 说明 |
|---|------|------|
| 1 | 装前必查 | 任何镜像/扩展安装前，必须先查离线缓存目录 |
| 2 | 命中零网络 | 缓存命中且 SHA256 校验通过 → 直接使用，不碰网络 |
| 3 | 临时目录必清 | 无论编译成功还是失败，临时目录必须清空 |

**铁律 2 的补充（v2.9.6）**：**「零网络」不止于缓存命中**——缓存丢失但镜像还在本机 Docker 里时（换机、误删 `./offline/`、手工 `docker load` 过），未命中分支必须先 `ImageExists` 探本机镜像库，命中即 `docker save` 就地重建缓存，同样**不碰网络**；探不到才允许联网拉取。这是画像 F（内网/断网）在「缓存已丢失」情形下唯一的自救路径。

#### 1.13.2 以 PHP 8.4 为例的完整流程

**安装 PHP 8.4 镜像**：

```
1. 检查 ./offline/php/8.4/image.tar
   ├─ 存在 + SHA256 通过
   │   → docker load -i image.tar（零网络）
   │   → 发射 cache:hit 事件
   │   → 完成
   └─ 不存在 / SHA256 失败
       → 探本机 Docker 镜像库（ImageExists php:8.4-fpm）
       ├─ 本机已有（零网络，cache:miss 的 action=local）
       │   → docker save -o {tmp}/image.tar
       └─ 本机没有 → docker pull php:8.4-fpm（网络，action=pull）
                    → docker save -o {tmp}/image.tar
       → 校验
       → mv {tmp}/image.tar ./offline/php/8.4/image.tar
       → 更新 ./offline/php/8.4/manifest.json
       → 清空 {tmp}
       → 发射 cache:miss + cache:promote 事件
       → 完成
```

**安装 PHP 8.4 的 redis 扩展（pecl）**：

```
1. 检查 ./offline/php/8.4/pecl/redis-6.0.2.tgz
   ├─ 存在 + SHA256 通过
   │   → 复制到 ./php/8.4/ext/pecl/
   │   → 编译安装
   │   → 清空 ./php/8.4/ext/
   │   → 发射 cache:hit 事件
   └─ 不存在 / SHA256 失败
       → 下载到 ./php/8.4/ext/pecl/redis-6.0.2.tgz（网络）
       → 编译安装
       ├─ 成功
       │   → mv ./php/8.4/ext/pecl/redis-6.0.2.tgz ./offline/php/8.4/pecl/
       │   → 更新 ./offline/php/8.4/manifest.json
       │   → **清空 ./php/8.4/ext/**（防污染下次使用）
       │   → 发射 cache:miss + cache:promote 事件
       └─ 失败
           → **清空 ./php/8.4/ext/**（防污染下次使用）
           → 报错
2. 重建镜像 + 重启容器
```

#### 1.13.3 目录结构总览

```
./offline/                       # 缓存根目录（持久）
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

./php/8.4/ext/                   # 临时目录（单任务，结束即空）
├── apk/                              # 编译期间临时存放
└── pecl/
```

#### 1.13.4 明确禁止

- ❌ 安装前不检查离线缓存。
- ❌ 命中缓存后仍走网络下载。
- ❌ 镜像未命中即拨网络（本机已有该镜像时必须零网络 `docker save` 重建缓存）。
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
- **未命中先探本机再联网**：镜像未命中先探本机 Docker 镜像库（已有即零网络重建缓存）；探不到才下载到临时目录 → 编译/加载。
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
├── app.go                           # 根 Service：唯一对前端暴露的门面（68 个绑定方法）
├── app_test.go
├── go.mod  go.sum                   # module phpo；go 1.27
├── wails.json                       # Wails 配置
├── Taskfile.yml                     # dev / bindings / frontend:{install,build} / build / test / vet / check / package{,:linux,:windows,:darwin} / version:bump / release:local
├── Makefile
├── config.example.yaml              # 配置样例：两根 + 可自定义根（offline_root/backup_root）+ services.{kind}.{version}.{password,port,data_dir}
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
│   │   ├── configstore.go           # 单一配置权威（YAML config.yaml：两根 + 可自定义根 + 密码 + 端口 + 每版本数据目录）
│   │   ├── userdata.go              # XDG 用户配置目录解析（config.yaml / phpo.db / logs / trash / updates）
│   │   ├── paths.go                 # derivePaths / ApplyRootOverrides / ApplyDataDirs / hostToContainer
│   │   ├── validate.go              # 路径安全校验（§5.4 唯一保留的校验；ValidateRootPath 供可自定义根复用）
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
│   │   ├── image_cache.go  extension_cache.go  promote.go   # promote.go：提升=搬走 / 手工导入=复制（placeFile）
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
│   │   ├── preflight.go             # Run 的 19-case switch + AllActions / needsHome 集合
│   │   ├── validators.go            # 域名 / 版本 / 路径 / 端口校验
│   │   ├── rules_service.go  rules_site.go
│   │   ├── rules_ops.go  rules_cache.go
│   │   └── rules_root.go            # root-set：自定义缓存根 / 备份根 / 每服务版本数据目录（§5.15，仅路径安全）
│   │       # 占用判定内联复用 store.CollectUsedPorts（无独立 portprobe.go）
│   │       # 清理规则在 cleanup_service 侧（无 rules_cleanup.go）
│   │       # cache-import 规则在 rules_cache.go（手工导入任意文件为缓存条目）
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
│       │   ├── common/              # 9：ModalShell / ModalRoot / ToastHost / PasswordField
│       │   │                        #   MountList / PathInfoBar / CacheHitBadge / EditablePathBar
│       │   │                        #   ExtPicker
│       │   └── business/            # 19：SiteAddModal / SiteConfigModal / RewriteModal / InstallModal
│       │                            #   ConfigModal / PhpExtensionsModal / TaskDrawer / CmdPalette
│       │                            #   DangerConfirm / HomeSetupWizard / DockerGate / AppTrayMenu
│       │                            #   ThemePickerModal / UpdateModal / CleanupModal / TrashViewer
│       │                            #   CacheDetailModal / CacheCleanupModal / CacheImportModal（备份与改端口无独立模态）
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
│   ├── check-ext-catalog.go                      # 前端扩展目录与后端 peclExts 分类对账（第 5 项门禁）
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
│       ├── m6_offline_live_test.go  t601_extension_live_test.go  t602_backup_live_test.go
│       └── g4_pgsql_heal_live_test.go        # 旧配置裸启动必失败（带日志取证）→ 经 Start 自愈后就绪
│       # 单元测试与包同目录（78 个 *_test.go），fake/mock 内联，无 test/{unit,mocks,fixtures,e2e}
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
<用户数据目录>/                         # = os.UserConfigDir()/phpo（见 internal/config/userdata.go）；不受装机向导影响
│                                       # Windows: %APPDATA%\phpo · macOS: ~/Library/Application Support/phpo · Linux: ~/.config/phpo
├── config.yaml                       # 单一配置权威（YAML）：phpo_home + www_root + offline_root? + backup_root? + services.{kind}.{version}.{password,port,data_dir?}；0600
├── phpo.db                           # SQLite，仅存运行态（installed / running / sites / php_extensions / trash / operations（含任务账本 task_id/label/logs） / offline / cache_manifest）；**延迟建库**：装机向导把两根目录写入 config.yaml 后才创建
├── logs/
│   └── operations.log                # 操作审计（JSON Lines，§5.13.10）
├── trash/                            # 回收站（7 天保留，§5.13.7）
└── updates/                          # 升级工作区
    ├── downloads/
    └── backups/

# 注：主题、语言、布局、缩放属 UI 偏好，留前端 localStorage（§3.1 原则 5），用户数据目录内不再有 themes/ 与 locales/；
#     运行日志走标准输出 + 系统日志，未单独落 phpo.log。

./                                      # PHPO_HOME（= config.yaml 的 phpo_home；装机向导可指向任意目录，`~/phpo` 仅默认值）
├── php/<ver>/{conf,logs}
│   └── ext/                          # 临时目录（编译期间，任务结束即清空）
│       ├── apk/
│       └── pecl/
├── nginx/{conf,logs,sites/}
├── mysql/<ver>/{conf,data,logs,initdb}
├── pgsql/<ver>/{conf,data,logs,initdb}
├── redis/<ver>/{conf,data,logs}
├── backups/backup-*.tar.gz           # 备份根（默认位置；config.yaml 的 backup_root 可改到任意目录，见 §5.15）
└── offline/                          # 离线缓存根目录（持久；默认位置，config.yaml 的 offline_root 可改，见 §5.15）
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

# 注：密码/端口/工作根目录统一存于用户数据目录内的 config.yaml（见上），不再有 ./.env。

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

**存储位置**：`config.yaml` 的 `services.{kind}.{version}.password`（各平台 XDG 用户配置目录内的 `phpo` 子目录）；文件权限 `0600`。不再有 `./.env`，SQLite 不再持有 `env` 表与 `dir_ready` 表；前端 `app.env.*` 契约经快照 `env` 扁平键合成保持不变。

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
| `update:done` | `{ status, version }`；启动期回滚探测失败时为 `{ status:"failed", error }` | 升级完成 |
| `docker:cleanup` | `{ stage, resource, action }` | 清理进度 |
| `docker:orphan-found` | `{ resources: []Resource }` | 发现孤儿资源 |
| `docker:state-drift` | `{ expected, actual }`；启动校准取不到比对值时退化为 `{ error }` | 状态漂移 |
| `cache:hit` | `{ kind, version, source, size }` | 命中缓存 |
| `cache:miss` | `{ kind, version, action }`（`action`：镜像 `local`=用本机已有镜像重建缓存（零网络）/ `pull`=联网拉取；扩展 `download`） | 未命中缓存 |
| `cache:promote` | `{ kind, version, entries }` | 提升到缓存 |
| `cache:corrupted` | `{ kind, version, entry }` | 缓存损坏 |
| `cache:cleanup` | `{ mode, freed_bytes }` | 缓存清理完成 |
| `cache:tempdir-cleared` | `{ path, reason }` | 临时目录已清空 |

**事件名冻结为上表 17 个**；队列与进度不新增事件名，按下述四条承载：

1. **队列详情走快照**：`Snapshot.Tasks`（`model.TaskBoard{ Running *TaskBrief, Pending []TaskBrief }`）随 `state:changed` 实时推送——`Running` 是当前任务（含 `step / total` 进度），`Pending` 是其后的 FIFO 排队项。任务状态仍为 **4** 个，运行中 / 排队中由所在分区表达，不设第 5 态（前端抽屉的「等待中 / 执行中 / 已完成」是这一分区与终态的**派生显示态**，不是第 5 个状态，见 §5.6.1）。
2. **实时日志与进度走 `task:*` 事件**：`task:log` 逐行输出（5 种 level），`task:progress` 变更步骤，`task:done` 收尾。`duration` 是 Go `time.Duration`，JSON 序列化为**纳秒**，前端须自行换算单位。
3. **历史与失败原因走账本**：任务退出时由 `internal/task/ledger.go` 把终态连同等效日志写回 `operations` 表（迁移 `0008` 加 `task_id / label / logs` 三列，日志取尾部 500 行）。落账失败**不改变**任务成败判定；失败原因取最后一条 `err` 级 `task:log`，并在 `operations.error` / `operations.logs` 留档。
4. **无任务归属的协议事件走「系统日志通道」**（v2.9.8 新增，需求 3/6）：`cache:*`、`docker:*`、`update:available`、`update:done` 这一类**由后端某处直接发射、不必然属于某个任务**的事件（doctor 离线校验、手动清理缓存、启动校准、启动回滚探测…），前端仍要**逐行**落到抽屉左栏：有运行中任务时归属该任务（串行队列 ⇒ 归属唯一），无运行任务时落入 `taskStore.sysLines` 这条独立的滚动流水（上限 **200 行**，超出裁尾）。落地入口是 `taskStore.eventLine(level, text)` 一处，`useStateSync.eventNote` 是唯一调用方。
   - **不得**为这类事件凭空创建任务记录、任务状态或第 5 个显示态（硬红线 4）；系统日志通道不是任务账本，不落 `operations` 表。
   - `update:progress` **不进日志**——它是连续量，会淹没流水，已由升级弹窗进度条实时承载。
   - 抽屉左栏在「无选中任务」时显示这条通道（空态文案 `task.sysEmpty`）；一旦选中某任务即回到该任务的日志。
   - **`docker:state-drift` 额外触发一次 `syncState()`**：漂移即「Docker 实际状态 ≢ 库里状态」，界面必须跟着校准结果回流，不能只打一行日志。

`task.Manager` 串行执行：嵌套提交返回 `ErrBusy`，重复标签返回 `ErrQueued`，排队项可经 `CancelQueued(id)` 撤回。preflight **不再持有「已有任务在跑」的全局守卫**——并发写操作按 FIFO 排队，不作校验错误。

#### 5.6.1 任务抽屉：日志默认 70% ／ 队列默认 30%（v2.9.7 新增；v2.9.8 补可拖拽、头部口径、系统日志通道）

抽屉（`frontend/src/components/business/TaskDrawer.vue`）展开后是**左右两栏**：

| 区域 | 默认占比 | 内容 |
|------|------|------|
| 左栏 `.drawer-main`（内含 `.drawer-log`） | **70%** | 当前选中任务的逐行日志（`task:log`）、步骤进度、失败原因；**无选中任务时显示系统日志通道**（见下） |
| 中缝 `.drawer-splitter` | 7px（负 margin 借位，不改变占比） | 左右拖拽调宽：`pointerdown` 起拖、按 `drawer-body` 宽度换算百分比 |
| 右栏 `.drawer-queue` | 余下 **30%** | 任务队列列表（竖排、可滚动）：状态点 · 标签 · `step/total` · **显示态文字** · 撤回按钮 |

**占比是默认值，不是固定值**（需求 4）：拖拽区间 **40–80%**（`LAYOUT_LIMITS.split`），**双击中缝复位到 70%**。选定值写 `localStorage`（键 `phpo-drawer-split`，`layoutStore.setSplit` 负责夹取），并以 CSS 变量 `--drawer-split` 下发；抽屉高度仍可沿用既有 `.drawer-resizer` 拖拽（双击复位 160px）。**不得**把拖拽结果当后端状态落库——它是 UI 偏好（§3.1 原则 5）。

**头部（`.drawer-head`）左侧标签固定显示「服务」二字**（需求 5）：该标签取 `t('nav.services')`，**不得**替换成任务名、状态文案或任何其他内容；同一 `.drawer-left` 内的等效命令展示、进度/状态/按钮区照旧（本条只约束那个标签的文字）。

**排序：新任务永远在最上面。** 列表按**提交（入队）时间倒序**排列，最新一条置顶；正在执行的任务不因「开始执行」而移动，行位只随提交先后决定。

**每一行必须给出明确的 UI 显示态。** 显示态是「队列所在分区 + `task:done` 终态」的**派生结果**，映射表如下，**不是**第 5 个任务状态（§0.3 的 4 态与 §5.6 的 17 事件名一字不动）：

| 显示态 | 文案（zh ／ en） | 判据（唯一来源） |
|--------|-----------------|------------------|
| 等待中 | 等待中 ／ Waiting | 在 `Snapshot.Tasks.Pending` 分区内（尚未取得执行权） |
| 执行中 | 执行中 ／ Running | 是 `Snapshot.Tasks.Running` 的 `id`（认后端权威 ID，不认本地记录的 `status` 字段） |
| 已完成 | 已完成 ／ Done | 终态 `success` |
| 失败 | 失败 ／ Failed | 终态 `failed` |
| 已取消 | 已取消 ／ Cancelled | 终态 `cancelled` |
| 状态未知 | 状态未知 ／ Unknown | **预留兜底位**：映射未覆盖的任何后续新增态一律落此，**不得**留空白、**不得**当作已完成 |

**扩展约束**：新增显示态只允许改前端这一张映射表（`taskStore.ts` 的单一 `displayOf` 出口）；**不得**把新态写进后端 `model.TaskStatus`（冻结为 4 个）。兜底位存在即是「提前预留」——后端将来多一种终态，界面最坏显示「状态未知」而不是消失或误报。

**即时同步**：任务一经入队即出现在列表，不等它取得执行权。后端 `SetQueueWatcher`（`internal/app/di.go`）在**入队 / 移交 / 撤回**时重发权威快照，前端 `syncBoard` 为 `Running` **和每一个 `Pending` 项**都建立/更新记录（排队项无日志，左栏给空态提示）。撤回成功后该行随下一次快照消失；若被撤回项正被选中，选中项在同一次快照里回落到运行中任务，**不留「状态未知」的幽灵详情**。全程**不做本地乐观插入、不推断终态**（硬红线 4）。

#### 5.6.2 全局实时同步落地表（v2.9.8 新增，需求 6）

「实时同步」在本项目是**可核对的清单**，不是口号。§5.6 的 17 个事件名逐个给出唯一落地处；新增事件而无落地处即违反 §0.2 规则 25。前端唯一分发点是 `useStateSync.ts`：先 `landEvent`（改 store / 快照），再 `eventNote`（进抽屉日志）。

| 事件名 | 落地到 store / 快照 | 进抽屉日志 | 备注 |
|--------|--------------------|-----------|------|
| `state:changed` | `appState.applySnapshot` + `taskStore.syncBoard` | — | 权威快照；日志已由 `task:*` 逐行给出，不重复 |
| `service:changed` | `appState` 服务运行态 | — | 随快照回流；行内状态点即时更新 |
| `task:log` | `taskStore` 该任务日志 | ✅ 该任务左栏逐行 | 5 种 level |
| `task:progress` | `taskStore` `step/total` | — | 进度条 + `step/total` chip 即时显示 |
| `task:done` | `taskStore` 终态 | ✅ 收尾行 | 决定「已完成／失败／已取消」显示态 |
| `update:available` | `updaterStore` | ✅ `meta` 行 | 版本 + 体积（`humanSize` 换算） |
| `update:progress` | `updaterStore` 进度 | ❌ **不进日志** | 连续量会淹没流水；由升级弹窗进度条承载 |
| `update:done` | `updaterStore` | ✅ `ok`／`err`／`dim` 行 | 按 `status` 选 level；`error` 或缺席的 `version` 作为 detail 兜底 |
| `docker:cleanup` | — | ✅ `dim` 行 | `stage · resource · action` |
| `docker:orphan-found` | — | ✅ `meta` 行 | 只落**计数**；不触发 rescan、不回填 `cleanupStore`（载荷是 `unknown[]`，无法无损映射成 `OrphanReport`） |
| `docker:state-drift` | ✅ **额外 `syncState()`** | ✅ `meta` 行 | 比对值缺席时退化为 `error` 文本 |
| `cache:hit` | `cacheStore`（经 `useCache` 重拉） | ✅ | 命中零网络的证据 |
| `cache:miss` | 同上 | ✅ | `action=local` 显示「本机重建」，`pull`/`download` 显示「走网络」 |
| `cache:promote` | 同上 | ✅ | 手工导入（§5.14.9a）同样发此事件 |
| `cache:corrupted` | 同上 | ✅ | SHA256 校验失败回退网络 |
| `cache:cleanup` | 同上 | ✅ | 三模式清理结果 + 释放体积 |
| `cache:tempdir-cleared` | — | ✅ | 路径 + 原因 |

**除事件外的实时回流要求**（同一条需求的其余部分）：

| 对象 | 实时同步要求 |
|------|-------------|
| 写操作（请求/响应） | 所有 `api/*` 写调用必须 `await` 后端并在其后触发 `syncState()`；UI 的回显只随 `state:changed` 快照落地，**不做本地乐观更新**（硬红线 4） |
| 非快照数据 | 缓存条目、审计列表这类不进快照的数据，写后由对应 store 主动重拉（`useCache.pull` / `cacheStore`），不得停留在旧值 |
| 队列 | 入队 / 移交 / 撤回三处由 `SetQueueWatcher` 重发快照，前端不为排队项造占位态 |
| 目录选择/根路径变更 | `root-set` 落 `config.yaml` → 装配层 `Rebind` → `GetState` → `state:changed`；界面路径条回显来自快照 `env`，**不回填本地输入框**（§5.15） |
| 失败路径 | 任务失败同样要回流：终态 + 失败原因行 + 账本，不得静默（§5.13.9 每任务后校准） |


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

> **核心原则一句话**：**装任何镜像/扩展必先查缓存 · 命中零网络 · 镜像未命中先探本机镜像库（已有即零网络重建缓存）· 否则下载编译 · 成功后提升到缓存 · 无论成败均清空临时目录 · 断网重装靠缓存 · 内网开发靠缓存。**

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

**离线缓存根目录**：**默认 `./offline/`**（**持久**）；用户可自定义到任意路径，见 §5.15。**本节与下文的 `./offline/` 一律读作「当前生效的缓存根」**——`config.yaml` 的 `offline_root` 一旦写入，下列所有子路径都改挂到该自定义根之下，`./offline/` 即不再参与读写（互斥唯一，不会两处并存）。

**临时目录**：`./{kind}/{version}/ext/`（**单任务，任务结束即清空**；**不可自定义**——它是编译中间产物而非缓存，见 §5.15 末条）。

**完整结构**：

```
./offline/                       # 缓存根目录（持久）
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

./{kind}/{version}/ext/           # 临时目录（单任务，结束即空）
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

**唯一优先级：离线缓存 > 本机 Docker 镜像库 > 网络**（第二级为 v2.9.6 补入：缓存被误删/换机后，只要镜像还在本机 Docker 里，就能**零网络**把缓存重建出来；探不到才允许联网）。

**安装服务（Docker 镜像）**：

```
1. 检查 ./offline/{kind}/{version}/image.tar
   ├─ 存在
   │   ├─ 校验 SHA256（对比 manifest.json 中的值）
   │   │   ├─ 通过 → docker load -i image.tar（零网络）
   │   │   │         → 发射 cache:hit 事件
   │   │   │         → 完成
   │   │   └─ 失败 → 标记缓存损坏
   │   │              → 提示用户（发射 cache:corrupted 事件）
   │   │              → 落入下面的未命中分支
   │   └─ （损坏同上，回退到未命中分支）
   └─ 不存在
       → 探本机 Docker 镜像库（ImageExists {image}；探针报错必须上抛，不得静默当作「本机没有」）
       ├─ 本机已有 → 零网络重建缓存（发射 cache:miss，action=local）
       │   → docker save -o {tmp}/image.tar
       └─ 本机没有 → docker pull {image}（网络，发射 cache:miss，action=pull）
                    → docker save -o {tmp}/image.tar
       → 校验
       → mv {tmp}/image.tar ./offline/{kind}/{version}/image.tar
       → 更新 ./offline/{kind}/{version}/manifest.json
       → 清空 {tmp}
       → 发射 cache:promote + cache:tempdir-cleared 事件
       → 完成
```

> **两条未命中路径共用后续步骤**：无论镜像来自本机还是网络，都走「save → 校验 → 提升 → 清临时」，提升后下次装机即命中缓存（零网络）。**扩展（apk/pecl）不适用第二级**——`.so` 编译产物随镜像 commit 走，本机没有独立文件可探，仍是「未命中 → 联网下载 → 编译 → 提升」。

**安装 PHP 扩展（apk/pecl）**：

```
1. 判断扩展类型（apk / pecl）
2. 检查 ./offline/php/{version}/{type}/{package}
   ├─ 存在
   │   ├─ 校验 SHA256（对比 manifest.json 中的值）
   │   │   ├─ 通过 → 从缓存复制到临时目录 ./php/{version}/ext/{type}/
   │   │   │         → 编译安装
   │   │   │         → **清空临时目录**（防污染下次使用）
   │   │   │         → 发射 cache:hit + cache:tempdir-cleared 事件
   │   │   └─ 失败 → 标记缓存损坏
   │   │              → 回退到网络
   │   └─ （损坏则走网络）
   └─ 不存在
       → 下载到 ./php/{version}/ext/{type}/（网络）
       → 编译安装
       │   ├─ 成功
       │   │   → mv ./php/{version}/ext/{type}/{package} ./offline/php/{version}/{type}/
       │   │   → 更新 ./offline/php/{version}/manifest.json
       │   │   → **清空 ./php/{version}/ext/**（防污染下次使用）
       │   │   → 发射 cache:miss + cache:promote + cache:tempdir-cleared 事件
       │   └─ 失败
       │       → **清空 ./php/{version}/ext/**（防污染下次使用）
       │       → 发射 cache:tempdir-cleared 事件（reason: "compile_failed"）
       │       → 报错
3. 重建镜像 + 重启容器
```

#### 5.14.4 临时目录生命周期（严格）

**临时目录路径**：`./{kind}/{version}/ext/`

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

**校验失败处理**：标记损坏 → 发射 `cache:corrupted` → 提示用户 → 落入未命中分支（先探本机镜像库，再决定是否联网）。

#### 5.14.6 缓存清理（3 种模式）

| 模式 | 清理内容 | 适用场景 |
|------|---------|---------|
| 保守 | 只清理损坏的缓存条目 | 日常维护 |
| 标准 | 保守 + 清理 N 天未使用的缓存 | 定期清理 |
| 激进 | 标准 + 清理所有缓存（除正在使用的） | 释放空间 |

#### 5.14.7 缓存统计（UI 展示）

**OfflineView**（离线缓存视图）：展示总占用、条目数、最近验证时间、每项详情。

#### 5.14.8 断网场景支持

网络不可达时，只允许使用已缓存的资源；未缓存且本机 Docker 也没有的镜像、未缓存的扩展，拒绝安装并提示。镜像的例外自救：缓存丢失但本机 Docker 已有该镜像时，`docker save` 即可零网络重建缓存并继续安装（§5.14.3 第二优先级），这是断网机器唯一的恢复路径。

#### 5.14.9 缓存迁移（可选）

三条路径，任一即可，且都**只作用于当前生效的缓存根**（§5.14.2）：

1. **换根**：在离线缓存页把缓存根改成新目录（§5.15）——不改文件位置即指向既有缓存目录，零拷贝。
2. **手拷**：直接把旧缓存根整体拷到新目录（清单、`image.tar`、`apk/`、`pecl/` 结构不变即可被识别）。
3. **手工导入**：把单个 `image.tar` / `.apk` / `.tgz` 文件导入为缓存条目（见下 §5.14.9a），适合团队里只拿到一个包文件的场合。

或通过备份恢复功能包含离线缓存。

#### 5.14.9a 手工导入缓存条目（v2.9.8 新增，需求 1）

> 入口：离线缓存页「导入文件」→ `CacheImportModal.vue` 选服务种类 / 版本 / 文件类型（image／apk／pecl）+ 文件选择器 → `App.OfflineImportFile`。

| 环节 | 口径 |
|------|------|
| 裁决 | 新 preflight action **`cache-import`**（`rules_cache.go`）：`kind` 必须是五种服务之一、`version` 过路径安全校验（§5.4）、`field` ∈ {`image`,`apk`,`pecl`}，且 **apk/pecl 只允许导入到 `php`**。失败即报错，不静默丢弃 |
| 写链路 | 走三段式（硬红线 5）：preflight → task → applyStateChange。任务类型 `cache-import`，两步「读取源文件 / 写入缓存」；源文件缺失或选了目录 → `errs.FileMissing` |
| 落盘 | `internal/cache/promote.go` 的 `ImportImage` / `ImportExtension` → `placeFile(src, dst, keepSrc=true)`：**导入是复制，原件保留**（与自动提升的「搬走」相区分）；目标路径由 `OfflineImageTar` / 扩展缓存路径派生，**即当前生效缓存根** |
| 清单 | 与自动提升同一条路：算 SHA256 → 写/更新 `manifest.json`，故导入后的条目下次安装即按 §5.14.3 命中（零网络），并受 SHA256 校验约束 |
| 事件 | 复用既有 `cache:promote`（`emitPromote`），**不新增事件名**；前端 `useCache` 订阅后重拉列表，抽屉日志同步落一行 |
| 边界 | 导入**只写缓存**，不 `docker load`、不建容器、不改任何服务状态——是否使用仍由后续安装决定（§5.14.13「缓存与 Docker 资源完全解耦」） |

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
    ImportEntry(ctx context.Context, kind, version, extType, srcPath string) error // v2.9.8：手工导入（§5.14.9a）
}
```

#### 5.14.11 缓存事件（前端订阅）

| 事件 | 载荷 | 说明 |
|------|------|------|
| `cache:hit` | `{ kind, version, source, size }` | 命中缓存 |
| `cache:miss` | `{ kind, version, action }`（`action`：镜像 `local`=用本机已有镜像重建缓存（零网络）/ `pull`=联网拉取；扩展 `download`） | 未命中缓存 |
| `cache:promote` | `{ kind, version, entries }` | 提升到缓存 |
| `cache:corrupted` | `{ kind, version, entry }` | 缓存损坏 |
| `cache:cleanup` | `{ mode, freed_bytes }` | 缓存清理完成 |
| `cache:tempdir-cleared` | `{ path, reason }` | 临时目录已清空 |

#### 5.14.12 明确禁止

- ❌ 安装 Docker 镜像前不检查离线缓存。
- ❌ 缓存未命中、镜像其实还在本机，却直接拨网络拉取（必须先探本机镜像库，能零网络重建就不联网）。
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
- ❌ 把缓存路径写死成 `./offline/`（必须经当前生效缓存根派生，用户可自定义，见 §5.15）。
- ❌ 用户已自定义缓存根，安装链路却仍去读默认的 `./offline/`（或反之）——同一类路径**只允许一个生效根**（§0.2 规则 24）。
- ❌ 手工导入时移动/删除用户的源文件（导入是**复制**，原件保留；只有自动提升才搬走临时目录里的文件）。

#### 5.14.13 与 Docker 清洁机制的协同

| 场景 | 协同行为 |
|------|---------|
| 安装服务（命中缓存） | 缓存提供 image.tar → `docker load` → 容器创建走清洁流程 |
| 安装服务（未命中缓存） | 先探本机镜像库：已有 → `docker save` 到缓存（零网络）；没有 → `docker pull` → `docker save` 到缓存 → 容器创建走清洁流程 |
| 卸载服务 | **不动缓存** |
| 重装服务 | **优先命中缓存**（零网络） |
| 孤儿扫描 | **不扫描缓存** |
| 清理模式「激进」 | **可选清理缓存**（用户确认后） |
| 状态校准 | **不动缓存** |

**关键约束**：缓存不受 Docker 清洁机制影响；缓存与 Docker 资源完全解耦。

### 5.15 可自定义根（缓存根 / 备份根 / 每服务版本数据目录）（v2.9.8 新增，需求 1/2/7/8）

**一句话**：**三处路径用户可自定义；每一类永远只有一个生效值——自定义与默认互斥；唯一限制是路径安全。**

#### 5.15.1 三个可自定义根

| 面 | `config.yaml` 键 | 默认值（未自定义时） | 快照 `env` 回显键 | UI 入口 |
|----|-----------------|--------------------|------------------|---------|
| 离线缓存根 | `offline_root` | 默认 `./offline/`（即 `{phpo_home}/offline`） | `OFFLINE_ROOT` | 离线缓存页顶部路径条（需求 1） |
| 备份根 | `backup_root` | 默认 `./backups/`（即 `{phpo_home}/backups`） | `BACKUP_ROOT` | 备份页顶部路径条（需求 8） |
| 每服务版本数据目录 | `services.{kind}.{version}.data_dir` | 默认 `./{kind}/{version}/data` | `{KIND}_{VER}_DATA_DIR` | 服务卡片 + 安装弹窗（需求 7） |

**互斥唯一（核心口径）**：**自定义一旦设定即完全取代对应默认根**（`./offline/` · `./backups/` · `{KIND_ROOT}/{version}/data`）——任一时刻只有一条路径生效，不并存、不做二级回退。判定发生在 `internal/config/paths.go` 的 `applyRoot` / `DataDirFor`——**非空即用自定义、默认派生值同时作废；置空（或删键）即回落默认**。因此不存在「两个根并存、按顺序找」的实现：全链路只有 `Env.OfflineRoot` / `Env.BackupRoot` / `Env.DataDirFor(kind,ver)` **一个出口**，任何缓存/备份读写与容器挂载都必须经它取路径。**不得**新增第二处判定，**不得**在两处之间复制或回退（§0.2 规则 24）。

**数据目录的自定义是「换位置」而非「多一层」**：`workdir.prepareService` 在该版本 `HasCustomDataDir` 为真时**跳过**默认 `{KIND_ROOT}/{ver}/data` 的创建，容器挂载点直接指向自定义目录（`ResolveMounts` 中 `sub=="data"` 行走 `DataDirFor`）——默认目录不会因此凭空出现。

#### 5.15.2 唯一的限制：路径安全（硬红线 3）

`config.ValidateRootPath`（`internal/config/validate.go`）是**全部**校验：`NormPath` 归一 → 拒绝 `..`（含 Windows 反斜杠分段）→ 拒绝 `\x00`。**空串合法，语义即「清除自定义、回落默认」**。不要求绝对路径、不要求目录已存在、不限制字符集、不校验长度——**除此之外不得加任何限制**（§0.2 规则 15/16，最小限制原则）。

#### 5.15.3 写链路与生效时机

三段式照旧（硬红线 5）：**`root-set` preflight → 落 `config.yaml` → 装配层 `Rebind` → `GetState` → `state:changed`**。

1. **入口**：`App.SetOfflineRoot` / `App.SetBackupRoot` / `App.SetServiceDataDir`，共用内部 `setCustomRoot(field, kind, version, value)`；`field` ∈ {`offline_root`, `backup_root`, `data_dir`}。
2. **裁决**：新增 preflight action **`root-set`**（`internal/preflight/rules_root.go`）。其中 `data_dir` 额外要求 `kind`/`version` 与安装同判据（路径安全）；缓存根/备份根是全局根，不涉及 kind/version。
3. **队列门禁**：`Rebind` 会整图换掉配置派生的路径基准，**中途换图会把在跑任务的门面抽掉**，因此 `setCustomRoot` 在 `a.Running()` 为真时直接返回 `errs.TaskBusy`——**改根不得与运行中任务并发**（§5.13.1 原子性）。
4. **生效**：`internal/app` 的 `Rebind(ctx)` 以 `rootsKey(cfg)`（两根 + `OfflineRoot` + `BackupRoot` + 排序后的 `DataDirs`）为指纹做**幂等换图**；指纹未变即不重建。改根后缓存枚举、清理、提升、导入、容器挂载**立即**跟着新根，无需重启应用。
5. **回显**：UI 路径条显示的当前根取自快照 `env`（`EditablePathBar.vue`），**不做本地乐观回填**（硬红线 4）；保存按钮的 disabled 判据是「输入 ≠ 快照回显值」，失败即输入框自动回落到快照值。
6. **数据目录改了且服务正在运行**：preflight 给**警告**（「正在运行，数据目录要重建容器后才生效」），不阻止保存——卷里的数据要重建容器才切过去。

#### 5.15.4 目录选择器与「读写任意路径」

「打开到任意文件夹」用 Wails 原生对话框（`Dialogs.OpenFile({ CanChooseDirectories: true, CanChooseFiles: false, Directory })`），选到的路径**不做必须在 `PHPO_HOME` / `WWW_ROOT` 内的限制**（§5.12 同口径，最多加警告）。手工导入缓存条目同理：文件选择器可选任意文件，唯一判据是 §5.14.9a 的 `cache-import` 规则。

#### 5.15.5 不可自定义的路径（明确排除）

| 路径 | 为何不放开了自定义 |
|------|------------------|
| `phpo_home` / `www_root` | 已在装机向导里由用户选定，等价于自定义 |
| `<用户数据目录>/`（`config.yaml` / `phpo.db` / `logs` / `trash` / `updates`） | 由 OS 的 XDG 规则决定（§0.1.1、§4.2）；放开会让「找配置文件」变成用户负担，且与打包/升级/回滚定位强绑定 |
| 临时目录 `./{kind}/{version}/ext/` | 它是**编译中间产物**而非缓存，路径固定才能保证「任务结束必清空 + 不跨任务持久化」（§5.14.4）；自定义根只影响缓存**结果**的落点，不影响这里 |
| 站点根目录 | 已是每站点独立字段（需求外），不属本条三个面 |

#### 5.15.6 明确禁止

- ❌ 同一类路径出现两个生效根（自定义与默认并用、按顺序回退查找）。
- ❌ 在 `ValidateRootPath` 之外对可自定义根追加校验（绝对路径、存在性、字符集、是否在 `PHPO_HOME` 内、是否可写…).
- ❌ 把「警告」当「阻止」——运行中改数据目录只能警告，不得拒绝保存。
- ❌ 队列非空时改根（必须 `errs.TaskBusy` 挡住，不得绕过 `Rebind` 直接改内存字段）。
- ❌ 前端本地乐观更新路径条文字（回显只认快照 `env`）。
- ❌ 手工导入时删除或移动用户的源文件。
- ❌ 把缓存/备份路径写死为字面量 `~/phpo/offline`、`~/phpo/backups`（违反 §0.1.1 记法，且直接绕过自定义）。

### 5.16 PHP 扩展目录 · 选择器 · 全程实时日志（v2.9.9 新增，需求 ①/②/③）

**一句话**：**每个 PHP 版本一份全量扩展目录，两处入口共用同一份数据；勾选/取消即「目标扩展集」；从编译到重建的每一步都要逐行进抽屉日志，某一项失败必须点名该扩展。**

#### 5.16.1 唯一目录：前端一处清单，后端分类由门禁对账

| 项 | 口径 | 落点 |
|----|------|------|
| 目录规模 | **73** 项（内置 **49** ／ pecl **24**），分 **8** 组：`basic` / `db` / `cache` / `media` / `net` / `async` / `framework` / `debug` | `frontend/src/constants/ext.ts` 的 `EXT_CATALOG`（前端唯一清单） |
| 版本可见性 | `minVer` 按 `major.minor` 过滤（`ffi` ≥ 7.4、`sodium` ≥ 7.2）；版本号非数字（用户自定义标签）时**不做限制**，交后端路径安全裁决 | `extMatchesVersion` / `catalogFor(version)` |
| 默认勾选 | 常用 **11** 项（bcmath · curl · exif · mbstring · opcache · pcntl · mysqli · pdo_mysql · apcu · igbinary · redis） | `common: true` → `commonExts(version)` |
| 安装方式分类 | `builtin` → `docker-php-ext-install <name>`；`pecl` → `pecl install <name>` 再 `docker-php-ext-enable <name>`；命令以 **argv** 传入容器 exec，**不经 shell**（防注入） | `internal/config/extensions.go` 的 `ClassifyExt` / `ExtInstallCmds` |
| 对账门禁 | 前端 `tool: 'pecl'` 项与后端 `peclExts` 必须**逐项相等**——分类错了就是「用内置命令装第三方扩展」，必然编译失败 | `scripts/check-ext-catalog.go`（第 5 项门禁，登记在 `task check` 与 `ci.yml`） |

**目录外仍可手工添加**：用户手输的扩展名只要过扩展名格式校验（`config.ValidateExt`）即入列并选中——最小限制原则（§0.2 规则 15），目录只是「常用可见」的便利集，**不是白名单**。

#### 5.16.2 两个入口，一套语义

| 入口 | 位置 | 默认勾选 | 提交后 |
|------|------|---------|--------|
| 安装期选扩展 | `InstallModal.vue`（仅 `kind === 'php'` 显示扩展区，弹窗转 `lg`） | `commonExts(version)`；**一项都不选即只装基座镜像** | 安装任务之后**串第二个任务**跑 `applyExtensions`（扩展是「容器内编译 → 固化镜像 → 重建」另一条链路） |
| 管理扩展 | `PhpExtensionsModal.vue`（PHP 列表页「管理扩展」） | 本版本**当前已应用的扩展**（= 安装时勾选的 + 之后改过的） | `preflight('extensions')` → `applyExtensions` → `syncState()`；扩展列表写后**拉权威快照归位**，不做本地乐观更新 |

**目标扩展集语义**：弹窗提交的始终是**完整集合**（不是增量），后端 `diffExts(prev, next)` 自算 `added` / `removed`；两者皆空即**不产生任务、不重建容器**（幂等，§5.13.4）。`removed` 不等于删数据——只是不再启用。

**基座退回支：待编译集换成完整目标集**（v2.9.10）：`prevRef` 按本机实际态现取——库里启用过扩展但固化镜像 `phpo/php:{ver}` **不在本机**（手工删镜像、换机没带过来）时，容器只能退回基座 `php:{ver}-fpm` 重建，而**基座不含任何 `prev` 扩展**。此时若仍只编译 `diffExts` 的 `added`，`prev` 里那几项既没被装进容器、又会在任务结尾被原样写进 `php_extensions` 并随快照广播，界面从此显示「已启用」而 `php -m` 里没有——违反 §5.13.1 一致性（Docker 实际状态 ≡ 库里状态）。故该分支把待编译集换成**完整目标集**（`added = 去重排序后的 enabled`），且 `removed` **置空**（基座本就不含那几项，无需再删 ini）。口径变了必须**写进日志**：一行 `dim`「固化镜像 {ref} 不在本机，容器上无原扩展集，目标扩展集全量重编译」，不得静默（§5.16.3）。用例 `TestExtension_Apply_BaseFallbackRecompilesFullSet` 锁死命令序列 + 落库集合 + 该日志行。

**失败口径**：任一扩展编译失败 → 整个目标扩展集**不应用**（`Apply` 报错、`step.Rollback()` 用未变动的原镜像重建干净容器、撤回本次固化镜像、清空临时目录），弹窗**不关闭**并把短消息原样 toast 给用户（`submitWrite` 直出后端 error 原文），用户改一项重试即可。

#### 5.16.3 执行期日志契约（这条是需求 ③ 的硬约束）

六步全链路的日志**一行都不能少**，且**实时**：

| 步骤 | 必须出现的日志 |
|------|--------------|
| 写扩展清单 `extensions.env` | `cmd` 行：路径 → 目标扩展集全文 |
| 准备基座镜像（缓存优先） | `cmd` 行 + `cache:hit`／`cache:miss` 的既有徽标链路 |
| 容器内编译扩展 | `meta` 行「待启用 N 项 / 待停用 N 项」→ 基座退回支另起一行 `dim`（§5.16.2）→ 每条命令一行 `cmd`（含 argv 全文）→ **命令的 stdout/stderr 逐行**（stdout 为 `meta`、stderr 为 `dim`）→ 每项成功一行 `ok` |
| 固化镜像 `phpo/php:{version}` | `docker commit →` 与「已固化镜像」`cmd` 行；导出到离线缓存、提升成功各一行 |
| 从扩展镜像重建容器 | `cmd` 行「以扩展镜像重建容器 ← ref」+ `ok` 行「容器已运行于固化镜像」 |
| 重载 Nginx | `nginx -s reload` + `ok` 行；未接入 nginx 时 `dim` 行「跳过重载」，**不得静默** |

**去帧是硬要求**：Docker exec attach 流每帧带 **8 字节二进制帧头**，直读原始流即把垃圾打进日志。唯一出口是 `engine.ExecStream(ctx, name, argv, stdout, stderr io.Writer)`（内部 `stdcopy.StdCopy` 去帧并分流 stdout/stderr）；写日志侧用逐行 writer 适配 `task.StepLog`，**无换行的超长进度条按 4 KiB 强制断行**——攒成整串等于让用户盯着一段时长未知的「执行中」。同一出口供备份逻辑导出复用（转储写文件、stderr 只留作报错原因）。

**失败点名**：编译／停用失败时先落一行 `err`「扩展 {name} {安装|停用}失败：{原因}」，再把错误消息收紧成「**扩展 {name} {安装|停用}失败，本次扩展集未应用**」——短、可读、含扩展名，供 toast 直出（§3.2 原则 3「错误信息是人话」）。

#### 5.16.4 停用扩展 = 删 ini（真机取证）

官方 `php` 镜像**没有** `docker-php-ext-disable`。`docker-php-ext-enable` 的全部效果就是往 `/usr/local/etc/php/conf.d/` 写 `docker-php-ext-<name>.ini`，故停用即：

```
rm -f /usr/local/etc/php/conf.d/docker-php-ext-<name>.ini
```

真机取证（干净 `php:8.4-fpm`）：`conf.d/` 默认只有 `docker-fpm.ini`、`docker-php-ext-opcache.ini`、`docker-php-ext-sodium.ini`；`rm -f docker-php-ext-sodium.ini` 后 `php -m` 里 sodium 由有到无，且对不存在的 ini `rm -f` 退出码 0（幂等）。`.so` 留在镜像里不加载即视为停用，不做镜像层瘦身（那是 commit 新镜像的事）。

#### 5.16.5 明确禁止

- ❌ 安装弹窗只列三五个扩展、其余要用户手输（需求 ① 的原始缺陷）；也**不得**在两个弹窗里各维护一份裁剪清单。
- ❌ 管理扩展弹窗的默认勾选不是「本版本当前已应用的扩展」。
- ❌ 前端本地乐观改写扩展列表（回显只认快照，硬红线 4）。
- ❌ 编译期把 exec 输出攒成一段、或把 stderr 打进 stdout 混作一行（§5.16.3）。
- ❌ 失败消息只说「编译失败」不点名扩展、不说明「本次扩展集未应用」。
- ❌ 调用镜像里不存在的 `docker-php-ext-disable`。
- ❌ 用 shell 字符串拼接扩展名进容器执行（必须 argv；扩展名过 `ValidateExt` 格式校验）。
- ❌ 目标扩展集无变化仍重建容器；编译失败不清临时目录或不回滚。
- ❌ 容器退回基座重建时仍只编译 `added`（等于把未装的扩展写进权威库），或对该口径静默不写日志行（§5.16.2）。

### 5.17 备份归档：读不动即跳过并告警 + 数据服务逻辑导出（v2.9.9 新增，需求 ④）

**一句话**：**归档不被单个读不动的文件判死；缺的那份数据靠暂停前的逻辑导出补回；缺了什么必须写进日志。**

#### 5.17.1 读不动的条目：跳过 + 按目录聚合告警

**根因**：`./{kind}/{ver}/data`（或自定义数据目录）与 `./{kind}/{ver}/logs` 里的文件由**容器内 uid** 创建，宿主用户读不动——真机取证（同一台机、宿主登录用户既非 root 也不在那些组里，四个路径 `head -c 4` 全部 `Permission denied`）：

| 路径 | 实际属主与权限 | 创建者 |
|------|--------------|--------|
| `./php/8.0/logs/{access,slow}.log` | `-rw------- root root` | php-fpm 主进程 uid 0 |
| `./mysql/8.0/data/ibdata1` | `-rw-r----- {容器 uid} {容器组}` | mysqld |
| `./pgsql/17/data/` | `drwx------ {容器 uid}` | postgres |
| `./redis/8/data/dump.rdb` | `-rw------- root root` | redis-server |

旧实现让整包打包失败（真机报错：`打包归档 失败: open …/php/8.0/logs/access.log: permission denied`）。

**新口径**（`pkg/archive/targz.go`）：

| 环节 | 行为 |
|------|------|
| 单条目读不动（权限不足、打包期间消失） | **跳过该条目**并记 `Skip{Path, Reason}`，整包继续；`Create` 的返回签名是 `([]string, []Skip, error)` |
| 目录级读不动 | 同样跳过并记一条，不递归 |
| 真正致命的错误（建目录、建归档文件、写盘失败） | 照旧上抛，任务失败 |
| 界面回显 | `logSkips` 按**所在目录聚合**（每目录至多列 5 项 + 一行总数）进抽屉日志——真实数据目录动辄上百个不可读文件，一行一条会淹掉日志，但**缺了什么必须看得见** |

#### 5.17.2 逻辑导出（dump 先行，冷拷贝兜底）

`dumpKinds` = **mysql / pgsql / redis** 三种数据服务。备份任务里排在**暂停服务之前**（服务停了就 dump 不动），产物落归档内 `dump/` 前缀：

| 服务 | 容器内命令 | 归档产物 | 要点 |
|------|-----------|---------|------|
| mysql | `mysqldump -u root [--password={明文}] --single-transaction --routines --triggers --events --all-databases` | `dump/mysql-{ver}.sql` | 空密码即原生无密码（§1.5，不校验长度）；口令作 **argv 元素**传，不拼命令行文本 |
| pgsql | `pg_dumpall -U postgres` | `dump/pgsql-{ver}.sql` | 走 `pg_hba` 的 `local trust`，无需口令 |
| redis | `sh -c`：`redis-cli [-a {明文}] --rdb /tmp/phpo-backup.rdb` → `cat` 流回 → 删暂存 | `dump/redis-{ver}.rdb` | `--rdb` 会 `ftruncate` 目标文件，吐 `/dev/stdout` 即 `Invalid argument`（真机取证），只能容器内暂存后流回；暂存文件无论成败都删 |

**容错口径**：

- 单个服务 dump 失败 → 一行 `err` 点名「{kind} {version} 逻辑导出失败：{原因} —— 本次归档不含该库数据」，**继续**备份其余服务，不判死整包。
- 已安装但**未运行**的数据服务 → 一行 `dim`「未运行，跳过逻辑导出（其数据目录按冷拷贝打包，读不动的部分不在归档内）」，不阻止备份。
- dump 产物只在真要导出时才建目录（空目录不会进归档，即不会凭空多出 `dump/` 顶层）；半途而废的转储文件**必须删除**（比没有更危险）；转储为空亦删并报错。
- **stderr 不进转储文件**（否则帧头/告警会把 SQL 污染成不可重放的文本），只留作报错原因。

#### 5.17.3 恢复侧口径

恢复只做冷拷贝回放，**不自动重放 dump**；恢复日志必须给一行明示「归档内的 `dump/` 未自动重放，需要请按库手工导入」，不得静默让用户以为数据已完整回来（§5.13.11 口径不变：导入前清空目标命名空间）。备份脱敏口径见 `docs/备份脱敏规范.md`——明文密码随 `config.yaml` 入档是本项目的既定策略（§1.5），dump 产物含用户数据但不含额外口令（`mysqldump` 的 `--password` 只是连接参数，不落 SQL）。

#### 5.17.4 明确禁止

- ❌ 单个文件读不动即判死整包（需求 ④ 的原始缺陷）。
- ❌ 跳过后不告警、或把上百条 skip 一行条铺进日志（要按目录聚合 + 总数）。
- ❌ 在暂停服务**之后**才 dump。
- ❌ 把 dump 口令拼成 shell 命令行文本（特殊字符需转义、且会被二次展开）。
- ❌ 恢复侧静默吞掉「dump 未重放」这件事。

### 5.18 数据服务运行态：日志出口 · 坏配置自愈 · 启停就绪（v2.9.9 新增，需求 ③）

**一句话**：**容器内进程不得往宿主 bind 挂载目录写日志；旧装机的坏配置要在「启用」时也修；启动必须等到稳定 running，失败要带容器日志尾部。**

#### 5.18.1 根因：容器内写宿主 bind 目录 = 崩溃循环

`pgsql` 的默认 `postgresql.conf` 原写 `logging_collector = on` + `log_directory = '/var/log/postgresql'`，而 `/var/log/postgresql` 是宿主 `./pgsql/{ver}/logs` **bind 挂进去**的目录：该目录由宿主用户创建（0755）、容器内 `postgres` 是另一个 uid，**建文件即 `Permission denied`** → `postgres` FATAL 退出 → `restart=unless-stopped` 无限重启。表现为反复启停后无法启用：`运行态校验失败：phpo-pgsql-17 期望 running=true，实际 false`。

> **真机 A/B 取证**（同一台机、同一 `postgres:17` 镜像、同样把宿主 logs 目录 bind 到 `/var/log/postgresql`）：拿旧版 `postgresql.conf` 起容器 → `FATAL: could not open log file "/var/log/postgresql/…": Permission denied`，永远到不了 `ready to accept connections`；换成 §5.18.1 两行 → `database system is ready to accept connections`、0 次 FATAL、宿主 logs 目录保持为空（日志不再落盘）。崩溃循环中的真机容器实测 `ExitCode=1`、`RestartCount` 持续累加（首次取证 40，收口复现时同一容器已 128）。

**修法**（模板 `internal/template/templates/pgsql/postgresql.conf.tmpl`）：

```
log_destination = 'stderr'
logging_collector = off
```

日志随容器标准输出由 Docker 收集（`docker logs phpo-pgsql-{version}`）。这是**唯一一处**对冻结原型 `DEFAULT_CONFIGS` 的生产偏离，已在 `scripts/check-templates.go` 的 golden 里同步并注明原因（冻结原型 `前端唯一界面来源.txt` 与 `index.html` 不改；demo-only 的 `frontend/src/constants/configs.ts` 是 `!hasBackend()` 回落文本，仍留旧写法）。

#### 5.18.2 旧装机就地自愈

`prepareService` 只在装/重建时执行，且对已存在的配置**一律保留不覆盖**——旧装机的坏 `postgresql.conf` 永远等不到被换掉。故「启用」路径上也跑一次 `healPgLogging`（`internal/service/workdir.go`）：

| 情形 | 行为 |
|------|------|
| 文件仍是旧默认那三行（按原文精确匹配） | 就地把该段替换成 §5.18.1 两行，`ok` 行说明已修复 |
| 含 `logging_collector = on` 但已非默认写法 | **不改写**（用户自己的配置），只 `dim` 行提示「请自行核对该项」 |
| 已是新写法 / 非 pgsql / 读不到文件 | 直接返回 |

**写入必须原地截断**：`postgresql.conf` 是单文件 bind，写临时文件再 `rename` 会换 inode，容器读到的仍是旧那份。

#### 5.18.3 启停就绪与失败取证

| 环节 | 口径 |
|------|------|
| `StartContainer` | 按当前 `ContainerStatus` 分流：已 `running` 不动 → `restarting`／其他一律 `ContainerRestart`／`ContainerStart` |
| `waitRunning` | 轮询到 running 后再静默 `startHold = 2s` **复验**（躲开「起来即崩」窗口），超时 `startWait = 12s`、间隔 `startInterval = 300ms`；这三个量是 `var`，供单测收紧到毫秒级 |
| 失败消息 | `startFailureMsg`：容器名 + 状态 + 退出码 + **容器日志尾部 5 行**（`LogTail`，同样去帧）——人话且自带证据，不用再去翻 Docker |
| `StopContainer` | 只对 `needsStop`（`running`／`restarting`）执行，其余幂等跳过 |

**容器 `State.Running` ≠ 服务进程就绪**（entrypoint 有前置脚本）；运行态校验失败的报错要能指向真正原因，因此 §5.13.9 校准之后仍失败的启停必须走上表的取证路径。

#### 5.18.4 明确禁止

- ❌ 任何服务模板让容器内进程往宿主 bind 目录写日志文件（不止 pgsql）。
- ❌ 只在装/重建时修配置，「启用」路径不修。
- ❌ 用「写临时文件 + rename」改单文件 bind 挂载里的配置文件。
- ❌ 启动只看一次 `State.Running` 即判成功，或超时前不复验。
- ❌ 启动失败只报「期望 running=true，实际 false」而不带退出码与容器日志尾部。

---

## 6. 原型 → 生产映射表

| 原型元素 | Go 侧落点 | 前端落点 |
|---------|----------|---------|
| state 全局对象 | `internal/store/ + SQLite` | `stores/appState.ts` |
| persistState / hydrateState | `internal/store/snapshot.go` | — |
| preflight()（原型 17 action；生产 19） | `internal/preflight/preflight.go` | `composables/usePreflight.ts` |
| NEEDS_HOME（原型 15；生产 17） | `internal/preflight/preflight.go`（`needsHome` map） | — |
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
| 任务队列 UI 显示态（等待中／执行中／已完成 + 兜底位） | `model.TaskBoard` 所在分区 + `task:done` 终态派生（**不新增第 5 态**，§5.6.1） | `taskStore.ts#displayOf` + `TaskDrawer.vue` 右栏（30%） |
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
| 可自定义缓存根/备份根（v2.9.8，需求 1/8） | `internal/config/{paths,configstore,validate}.go` + `internal/preflight/rules_root.go` + `app.go#setCustomRoot` + `internal/app`（`Rebind`/`rootsKey`） | `components/common/EditablePathBar.vue` + `OfflineView.vue` + `BackupView.vue` + `api/env.ts` |
| 每服务版本自定义数据目录（v2.9.8，需求 7） | `config.yaml` 的 `services.{kind}.{ver}.data_dir` + `Env.DataDirFor`/`HasCustomDataDir` + `config/paths.go#ResolveMounts` + `service/workdir.go` | `ServiceView.vue` + `InstallModal.vue` + `constants/mounts.ts#dataDirKey` |
| 手工导入缓存条目（v2.9.8，需求 1） | `internal/preflight/rules_cache.go#cacheImport` + `service/offline_service.go#ImportEntry` + `cache/promote.go#{ImportImage,ImportExtension,placeFile}` | `components/business/CacheImportModal.vue` + `api/offline.ts#importEntry` + `useCache.ts#doImport` |
| 无任务归属事件的系统日志通道（v2.9.8，需求 3/6） | —（纯前端落地；事件源见 §5.6.2） | `stores/taskStore.ts#eventLine` + `sysLines` + `TaskDrawer.vue` 左栏 |
| 抽屉两栏可拖拽（v2.9.8，需求 4） | — | `TaskDrawer.vue#.drawer-splitter` + `layoutStore.setSplit` + `--drawer-split` |
| PHP 扩展全量目录 + 选择器（v2.9.9，需求 ①/②） | `internal/config/extensions.go`（`peclExts` / `ExtInstallCmds`）+ `scripts/check-ext-catalog.go` 对账 | `constants/ext.ts`（唯一清单）+ `components/common/ExtPicker.vue` + `InstallModal.vue` + `PhpExtensionsModal.vue` |
| 扩展执行期实时日志 + 失败点名（v2.9.9，需求 ③） | `engine/exec.go#ExecStream`（`stdcopy` 去帧双 writer）+ `extension_service.go`（`extLogWriter` / `extFailed` / `extDisableArgs`） | 抽屉左栏 `task:log` 逐行 + `submitWrite` toast 原样显示后端短消息 |
| 备份跳过告警 + 逻辑导出（v2.9.9，需求 ④） | `pkg/archive/targz.go`（`Skip`，`Create` 返回 `([]string, []Skip, error)`）+ `backup_service.go`（`dumpKinds` / `dumpStep` / `dumpOne` / `dumpCmd` / `logSkips`） | `BackupView.vue` 无独立模态；结果与缺项进抽屉日志 |
| 数据服务运行态（v2.9.9，需求 ③） | `template/templates/pgsql/postgresql.conf.tmpl`（stderr）+ `workdir.go#healPgLogging` + `engine/container.go`（`waitRunning` / `startFailureMsg` / `needsStop`）+ `engine/inspect.go`（`ContainerStatus` / `LogTail`） | —（报错文本经服务卡片状态点与 toast 回流） |

---

## 7. 关键决策

### 决策 1–21（见 v2.4.0，此处省略）

### 决策 22：离线缓存采用「先查缓存 + 未命中先探本机镜像库再下载 + 成功才提升 + 无论成败清临时」

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

**规则 3：未命中先探本机、再走临时**：镜像未命中先 `ImageExists` 探本机 Docker 镜像库——已有即 `docker save` 到 `./{kind}/{version}/ext/`（零网络重建缓存，`cache:miss` 的 `action=local`）；本机没有才 `docker pull` 后 `docker save`（`action=pull`）。扩展未命中则联网下载到 `./{kind}/{version}/ext/`（`action=download`，编译产物无法从本机探得，不适用第一级）。两条路径之后同样：编译/加载 → 成功 → 提升到 `./offline/{kind}/{version}/`。

**规则 4：无论成败清临时**：编译成功/失败/取消/崩溃后，临时目录必须清空。

#### 明确禁止

- ❌ 安装前不检查离线缓存。
- ❌ 命中缓存后仍走网络下载。
- ❌ 镜像未命中不探本机镜像库就拨网络（本机已有该镜像却去联网拉取，等于放弃画像 F 的零网络自救）。
- ❌ 缓存命中不校验 SHA256。
- ❌ 编译成功后不提升到缓存。
- ❌ 编译成功后不清空临时目录。
- ❌ 编译失败后保留临时目录。
- ❌ 任务取消后不清空临时目录。
- ❌ 应用启动时不扫描临时目录残留。
- ❌ 临时目录跨任务持久化。

### 决策 23：可自定义根采用「单一生效根 + 路径唯一出口 + 改根即换图」（v2.9.8，需求 1/2/7/8）

**核心决策**：缓存根、备份根、每服务版本数据目录三处开放自定义，**每一类路径任何时刻只有一个生效值**；全链路只经 `config.Env` 的一个出口取路径。

#### 理由

1. **画像 A/D**：外包机器磁盘零散，缓存和数据库数据要能放到某块大盘；高级用户要能完全控制环境（画像 D）。
2. **画像 F**：内网/离线时，团队共享的缓存往往挂在网络盘或另一块盘——不能只认 `./offline/`。
3. **互斥而非合并**：若允许「自定义根 + 默认根」两处并存查找，则缓存清理、SHA256 校验、损坏回退都会出现「哪一份为准」的二义性，且用户无法确认自己清掉的是不是正在用的那一份。单一生效根把这个二义性从设计上取消。
4. **换图而非热改字段**：路径基准散落在缓存管理器、装配层、服务层多处；只改内存字段会留下半新半旧的图。`Rebind` + `rootsKey` 指纹保证一次改根 = 一次一致的重建。

#### 具体规则（四条）

1. **单一出口**：`Env.OfflineRoot` / `Env.BackupRoot` / `Env.DataDirFor(kind,ver)`；默认派生值在自定义非空时即被丢弃。
2. **唯一校验**：`config.ValidateRootPath`（路径安全）。空串合法 = 清除自定义。
3. **改根走三段式**且**必须队列空闲**（`errs.TaskBusy`）；成功后 `Rebind` → `GetState` → `state:changed`。
4. **回显只认快照**；运行时服务改数据目录需重建容器才生效，preflight 给警告不阻止。

#### 明确禁止

见 §5.15.6。

### 决策 24：全局实时同步采用「17 事件名逐一落地 + 无任务归属者进系统日志通道」（v2.9.8，需求 3/6）

**核心决策**：把「实时同步」从原则降级为**可核对的清单**——§5.6.2 逐个事件写明落地处；不属于任何任务的 `cache:*` / `docker:*` / `update:*` 事件不再停在各自的视图面板里，而是逐行进入任务抽屉左栏的**系统日志通道**（有运行任务时归属该任务）。

#### 理由

1. 原设计里 `OfflineView` 自带一块 `.off-events` 小面板，`docker:*` 只在清理视图闪现，升级事件只在弹窗里——**同一事实三处各说一半**，用户在一个地方看不到完整时间线。现统一到抽屉（唯一日志出口），删除视图内的重复面板。
2. 抽屉的日志模型是「按任务组织的」，而缓存/Docker/升级事件的产生者未必是任务（doctor 校验、启动校准）。**不为此造第 5 个任务状态**（后端 4 态冻结、17 事件名冻结）——`sysLines` 只是前端一条 200 行滚动流水，不是任务记录、不落账本。
3. `docker:state-drift` 光打日志不够：漂移意味着「库里状态 ≢ Docker 实际」，故额外 `syncState()` 让界面跟着校准结果回流。

#### 明确禁止

- ❌ 新增事件名来承载「无任务日志」（17 个冻结）。
- ❌ 为系统日志通道凭空造任务记录或终态（硬红线 4）。
- ❌ 把 `update:progress` 铺进日志流水（连续量淹没日志；进度条已承载）。
- ❌ 在视图里再立一份事件小面板（唯一出口是抽屉）。

### 决策 25：PHP 扩展采用「每版本一份全量目录 + 目标扩展集 + 实时逐行日志 + 失败点名」（v2.9.9，需求 ①/②/③）

**核心决策**：扩展不再是「让用户手输几个名字」的输入框，而是**一处前端全量目录（73 项 · 8 分组）驱动两处入口**；提交的是**完整目标扩展集**；执行期容器输出**去帧逐行**实时进抽屉日志，失败**点名**到扩展并中止本单。

#### 理由

1. **画像 A/C**：外包与学习者不知道该装哪些扩展（Laravel 要 `mbstring/bcmath/curl/openssl`，WordPress 要 `mysqli/gd`）。目录 + 常用默认勾选把「查教程装环境」变成「看一眼就勾对」。
2. **画像 D**：高级用户要能装目录外的扩展——目录是便利集不是白名单，格式过校验即可添加（最小限制原则）。
3. **两处清单必然漂移**：安装弹窗与管理弹窗各维护一份裁剪清单，迟早一份有 `swoole` 一份没有。单一 `EXT_CATALOG` + 与后端 `peclExts` 的门禁对账（`check-ext-catalog.go`）把「分类错了必编译失败」变成 CI 事实。
4. **编译是有时长的黑盒**：`docker-php-ext-install` / `pecl install` 一跑几十秒。日志攒成一段等于没有日志；失败不点名扩展，用户无从知道该取消勾选哪一项。

#### 具体规则（四条）

1. **单一清单**：`frontend/src/constants/ext.ts`；`catalogFor(version)` 管可见性、`commonExts(version)` 管安装期默认勾选（11 项）。
2. **完整集合语义**：后端 `diffExts` 自算增删，无变化不建任务；停用 = 删 `conf.d` 里的 ini（§5.16.4）。
3. **实时逐行日志**：唯一 exec 出口 `engine.ExecStream`（`stdcopy` 去帧 + stdout/stderr 双 writer + 4 KiB 无换行兜底）。
4. **失败即中止并点名**：`err` 行 + 短错误消息（含扩展名与「本次扩展集未应用」）→ toast 原样显示；容器回滚到原镜像、临时目录清空。

#### 明确禁止

见 §5.16.5。

### 决策 26：备份归档采用「跳过并聚合告警 + 暂停前逻辑导出」双保险（v2.9.9，需求 ④）

**核心决策**：归档**不因单个读不动的条目判死**，缺项写进日志；数据靠 `mysqldump` / `pg_dumpall` / `redis-cli --rdb` 在**暂停服务之前**导出，与冷拷贝并存。

#### 理由

1. **权限现实**：宿主数据目录里的文件由容器内 uid 创建（`0700`/`0600`），宿主侧逐文件可读从来不是可靠前提——真机即因一个 `logs/access.log` 读不动而整包失败。
2. **冷拷贝对运行中的库本就不完备**：热拷贝 `ibdata1` / WAL / AOF 未必自洽；逻辑导出才是「备份 → 换机 → 恢复数据」这条主路的可靠形态。
3. **不假装完整**：跳过必须告警、未运行必须说明、dump 失败必须点名——用户要能判断这份归档值不值得留（可恢复性 §5.13.1）。
4. **不做静默重放**：恢复侧不自动灌 dump（会覆盖用户既有库，风险不可逆），只明示「dump 未重放」，由用户手工导入。

#### 具体规则（四条）

1. `archive.Create` 返回 `([]string, []Skip, error)`：读不动即跳过并记账，致命错误照旧上抛。
2. `logSkips` 按目录聚合（每目录至多 5 项 + 总数一行）进抽屉日志。
3. dump 步骤排在暂停之前，产物入 `dump/` 前缀，口令走 argv，stderr 不入转储文件。
4. 单库 dump 失败只 `err` 点名并继续；未运行的服务 `dim` 说明后跳过。

#### 明确禁止

见 §5.17.4。

### 决策 27：数据服务运行态采用「日志走 stderr + 启用时就地自愈 + 启动复验取证」（v2.9.9，需求 ③）

**核心决策**：模板层把 pgsql 日志改成 `stderr` + `logging_collector = off`；「启用」路径对旧装机的坏配置**就地截断修复**；`StartContainer` 必须等到**稳定 running**（running 后复验 2s），失败消息带退出码与容器日志尾部 5 行。

#### 理由

1. **bind 挂载目录写日志是设计缺陷**：宿主建目录（0755）、容器内另一 uid 建文件必 `Permission denied`，`postgres` FATAL + `unless-stopped` = 无限重启，表现为「反复启停后无法启用」——这正是需求 ③ 的现象。
2. **只改模板修不了旧装机**：`prepareService` 不覆盖已存在的配置，旧 `postgresql.conf` 永远留着那三行；卸载重装不是可接受的修法（画像 A 会因此丢数据）。
3. **`State.Running` ≠ 服务就绪**：只看一次的就绪判定会把「起来即崩」报成成功；复验 + 日志尾部把「为什么起不来」直接放进报错文本，省掉一轮排查（§3.2 原则 3）。
4. 这是**唯一一处**对冻结原型 `DEFAULT_CONFIGS` 的生产偏离，已在 `scripts/check-templates.go` 的 golden 注明；原型 SSOT 不动。

#### 明确禁止

见 §5.18.4。

---

## 8. 跨平台差异矩阵

| 差异维度 | Windows | macOS | Linux |
|---------|---------|-------|-------|
| Docker 运行时 | Docker Desktop（WSL2） | Docker Desktop | 原生 Docker Engine |
| 挂载性能 | ⚠️ VM 转发，慢 | ⚠️ VM 转发，慢 | ✅ 原生 |
| 端口 80 | ✅ 无需特权 | ⚠️ 需 root/setcap | ⚠️ 需 root/CAP |
| hosts 提权 | UAC | osascript admin | polkit |
| 用户数据目录 | `%APPDATA%\phpo\` | `~/Library/Application Support/phpo/` | `~/.config/phpo/` |
| 路径解析 | ⚠️ 无 shell `~`（本项目由 `config.ExpandHome` 用 `os.UserHomeDir()` 在出口展开，三平台一致） | ✅ 同左 | ✅ 同左 |
| 路径大小写 | 不敏感 | 默认不敏感 | 敏感 |
| 系统托盘 | ✅ | ✅ | ⚠️ 需 libappindicator |
| 代码签名 | EV 证书 | Developer ID + 公证 | 无 |
| `config.yaml` 权限 | NTFS ACL | `chmod 600` | `chmod 600` |
| 升级安装方式 | NSIS 静默安装 | .app 替换 | AppImage 替换 |
| 缓存路径 | `./offline/` | `./offline/` | `./offline/` |
| 缓存文件权限 | NTFS ACL | `chmod 644` | `chmod 644` |
| 临时目录 | `./{kind}/{version}/ext/` | `./{kind}/{version}/ext/` | `./{kind}/{version}/ext/` |

> **缓存路径与临时目录跨平台同记法**：两者都从 PHPO_HOME（`config.yaml` 的 `phpo_home`）派生，位置由装机向导决定，**不随平台写死**。各平台的默认候选：Windows `%USERPROFILE%\phpo\`、macOS/Linux `~/phpo/`（仅默认值，非约束）；`~` 的展开由 `internal/config` 在出口统一处理（Windows 无 shell `~`，见上「路径解析」行）。

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
| **R88** | **改根后新旧路径并存导致缓存/数据二义** | **单一生效根 + 唯一出口（§5.15.1）；`rootsKey` + `Rebind` 幂等换图；互斥唯一由 `applyRoot`/`DataDirFor` 一处决定**（v2.9.8） |
| **R89** | **运行中改根抽走在跑任务的门面** | **`setCustomRoot` 以 `a.Running()` 挡住并返回 `errs.TaskBusy`；`internal/app/rebind_test.go` 覆盖队列门禁**（v2.9.8） |
| **R90** | **手工导入误删用户源文件** | **`placeFile(keepSrc=true)` 即复制不搬走；`internal/cache/import_test.go` 断言导入后源文件仍在**（v2.9.8） |
| **R91** | **事件发了界面却不动（「实时同步」沦为口号）** | **§5.6.2 逐事件落地对照表 + §0.2 规则 25；`update:progress` 是唯一显式豁免项；`docker:state-drift` 额外触发 `syncState()`**（v2.9.8） |
| **R92** | **系统日志通道被高频事件撑爆内存** | **`SYS_MAX = 200` 滚动裁尾；连续量（`update:progress`）不进日志**（v2.9.8） |
| **R93** | **扩展目录与后端安装方式分类漂移（用内置命令装 pecl 扩展必编译失败）** | **第 5 项门禁 `scripts/check-ext-catalog.go` 逐项对账 `EXT_CATALOG` ↔ `peclExts`（登记在 `task check` 与 `ci.yml`）**（v2.9.9） |
| **R94** | **容器 exec 输出带 8 字节帧头或被攒成一段，抽屉日志不可读、失败不点名** | **唯一出口 `engine.ExecStream`（`stdcopy` 去帧 + stdout/stderr 双 writer + 4 KiB 无换行强断）；`extension_service_test.go` 三条用例 + `test/integration/t601_extension_live_test.go` 真机取证**（v2.9.9） |
| **R95** | **单个读不动的文件判死整包备份** | **`archive.Create` 跳过并记 `Skip`，`logSkips` 按目录聚合告警（§5.17.1）**（v2.9.9） |
| **R96** | **冷拷贝缺库内数据，恢复后用户以为数据完整** | **暂停服务前先逻辑导出入 `dump/`；恢复侧明示「dump 未重放」（§5.17.2 / §5.17.3）**（v2.9.9） |
| **R97** | **容器内往宿主 bind 挂载目录写日志 → FATAL 崩溃循环，且旧装机的坏配置永不更新** | **pgsql 日志改走 stderr（模板 + `check-templates.go` golden 注明唯一生产偏离）；「启用」路径 `healPgLogging` 原地截断修复；`waitRunning` 复验 + 失败消息带退出码与容器日志尾部；`test/integration/g4_pgsql_heal_live_test.go` 真机两头取证（旧配置裸启动必失败 → 经 `Start` 自愈后就绪）（§5.18）**（v2.9.9） |

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
- i18n 键对齐 / 模板一致性 / 资源命名 / 缓存 manifest / 扩展目录分类**五项**门禁（`scripts/check-*.go`，`task check` 与 ci.yml 共用）
- 签名与发布辅助：`scripts/sign-release.sh`、`gen-checksums.sh`、`verify-signing-guard.sh`（公钥一致性反推）、`bump-version.sh`、`version.sh`
- 构建编排：`Taskfile.yml`（dev / bindings / build / test / vet / check / package / release:local）
- 测试：与包同目录的 Go 单测（78 个 `*_test.go`，fake/mock 内联）+ `test/integration/*_live_test.go` **10** 个真环境用例（`PHPO_LIVE=1` + Docker 可用双重 skip 守护）

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

### 12.7 可自定义根检查（v2.9.8 新增，§5.15）

- [ ] 缓存根 / 备份根 / 每服务版本数据目录三处是否都支持「编辑路径 + 打开到任意文件夹 + 读写」？
- [ ] 是否**只有一条生效路径**？（自定义非空即完全取代默认根；无二级回退、无并存）
- [ ] 取路径是否统一经 `config.Env` 的单一出口（`OfflineRoot` / `BackupRoot` / `DataDirFor`），而不是在别处再拼一次默认值？
- [ ] 除路径安全外是否**没有**追加任何校验（绝对路径、存在性、字符集、是否在 `PHPO_HOME` 内）？
- [ ] 改根是否走三段式（`root-set` preflight → 落 `config.yaml` → `Rebind` → `state:changed`）并在队列非空时返回 `errs.TaskBusy`？
- [ ] 数据目录是否**每服务版本一份**（而非整个 kind 共用）？自定义时默认的 `{KIND_ROOT}/{ver}/data` 是否不再凭空创建？
- [ ] 路径条回显是否只认快照 `env`（不做本地乐观回填）？运行中改数据目录是否只**警告**？
- [ ] 手工导入是否**复制**而不移走用户源文件、并写入 `manifest.json` + 发 `cache:promote`？
- [ ] 文案/注释里的 `./offline/`、`./backups/`、`{kind}/{ver}/data` 是否带「默认」字样（§0.1.1）？

### 12.8 全局实时同步检查（v2.9.8 新增，§5.6.2）

- [ ] §5.6 的 17 个事件名是否**每一个**都能在 §5.6.2 表里找到落地处？（新增事件而无落地处即违反 §0.2 规则 25）
- [ ] `cache:*` / `docker:*` / `update:available` / `update:done` 是否逐行进抽屉左栏（有运行任务归该任务，无任务进系统日志通道）？
- [ ] 系统日志通道是否**只**是前端流水——没有凭空造任务记录、任务状态或第 5 个显示态？（硬红线 4）
- [ ] `update:progress` 是否**没有**进日志流水（由进度条承载）？
- [ ] `docker:state-drift` 是否额外触发一次 `syncState()` 让校准结果回流界面？
- [ ] 离线缓存页是否**没有**再放一份 `.off-events` 小面板（唯一日志出口是抽屉）？
- [ ] 抽屉两栏是否默认 70%／30% 且**可左右拖拽**（40–80%、双击中缝复位、写 `localStorage`）？头部左侧标签是否固定为「服务」二字？
- [ ] 所有写操作是否 `await` 后端 + 触发快照回流？非快照数据（缓存列表、审计）写后是否主动重拉？

### 12.9 PHP 扩展目录 · 备份容错 · 数据服务运行态检查（v2.9.9 新增，§5.16–§5.18）

**扩展（§5.16）**：

- [ ] 两处入口（安装弹窗 / 管理扩展弹窗）是否共用 `constants/ext.ts` 这一份全量目录？有没有另立裁剪清单？
- [ ] 安装期是否默认勾选 `commonExts(version)`（11 项）？管理弹窗默认勾选是否等于**本版本当前已应用的扩展**？
- [ ] 提交的是**完整目标扩展集**吗（无变化不建任务、不重建容器）？
- [ ] 容器内命令的输出是否**去帧后逐行实时**进 `task:log`（stdout `meta` / stderr `dim`，超长无换行 4 KiB 强断）？
- [ ] 失败是否一行 `err` 点名扩展 + 短消息「本次扩展集未应用」并让弹窗**不关**？
- [ ] 停用是否走 `rm -f conf.d/docker-php-ext-<name>.ini`（不是镜像里不存在的 `docker-php-ext-disable`）？
- [ ] 命令是否以 argv 传入容器（不经 shell）？`check-ext-catalog.go` 门禁是否绿？
- [ ] 固化镜像不在本机、容器退回基座重建时，待编译集是否换成**完整目标集**（不是只装 `added`）、并给一行 `dim` 说明？（§5.16.2 / v2.9.10）

**备份（§5.17）**：

- [ ] 读不动的条目是否跳过 + 按目录聚合告警（每目录至多 5 项 + 总数一行），而不是判死整包或静默缺项？
- [ ] 逻辑导出是否排在**暂停服务之前**？未运行的服务是否 `dim` 说明后跳过？
- [ ] 单库 dump 失败是否点名并继续？半途/空转储文件是否删除？
- [ ] 口令是否作 argv 元素传（不拼命令行文本）？stderr 是否**不入**转储文件？
- [ ] 恢复侧是否明示「`dump/` 未自动重放」？

**数据服务运行态（§5.18）**：

- [ ] 服务模板里是否还有让容器内进程往宿主 bind 目录写日志文件的配置项（`logging_collector` 一类）？
- [ ] 「启用」路径是否也修旧装机的坏配置（`healPgLogging`）？是否用**原地截断**写（不换 inode）？
- [ ] 启动是否等到稳定 running（running 后复验）？失败消息是否带状态 + 退出码 + 容器日志尾部？
- [ ] `check-templates.go` 的 golden 是否仍注明「pgsql 日志段是唯一生产偏离」？原型 SSOT 是否未改？

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

**phpo 项目总纲 v2.9.10**

> **v2.9.10 变更（§5.16.2 补「基座退回支：待编译集换成完整目标集」，收口一处权威态与容器实际态不一致的缺陷）**：
> v2.9.9 把扩展目录、执行期日志与失败点名写成冻结条款，但漏掉了**恢复路径的编译集合**这一格：库里已启用过扩展、而
> 固化镜像 `phpo/php:{ver}` 不在本机时（手工删镜像、换机没带过来），容器只能退回基座 `php:{ver}-fpm` 重建——基座
> **不含任何** `prev` 扩展，而编译步骤仍只装 `diffExts` 算出的 `added`，于是 `prev` 里那几项既没被编译、又会在任务
> 结尾被原样写进 `php_extensions` 并随快照广播，界面从此显示「已启用」而 `php -m` 里没有。这违反 §5.13.1 一致性
> （Docker 实际状态 ≡ 库里状态），并把 `任务工单.md` T601 已登记的那条「勾掉再勾回任一扩展」轻量恢复限制放大成
> 静默的数据不一致。现冻结为一条正向条款：**该分支的待编译集换成完整目标集、`removed` 置空（基座本就不含那几项，
> 无需再删 ini）、并给一行 `dim` 说明口径已变**（§5.16.3 日志契约同步补这一行、§5.16.5 补一条禁止项、§12.9 补一项
> 自查）。落地：`internal/service/extension_service.go` 的 `rebuiltFromBase` 分支 + `uniqueSorted` 助手；用例
> `TestExtension_Apply_BaseFallbackRecompilesFullSet` 锁死命令序列 / 落库集合 / 该日志行。假件前提一并校正：
> `ImageExists` 原先只认**本次** commit，导致「固化镜像已在本机」的正常路径无法构造，现加 `hasImages` 供用例显式
> 声明（`TestExtension_Apply_DisableRemovesIni` 据此走回正常支，原有断言一字未削）。同源同步：`任务工单.md` T601
> 段新增 2026-09-23 修正条、`docs/CHANGELOG.md` v2.9.9 段新增「缺陷审计追加」子条。验真：`gofmt -l .` 无输出、
> `go vet ./...`、`go build ./...`、`go test ./... -count=1` 全绿，五项门禁全过（i18n 各 608／模板 golden 空 diff／
> 命名／清单／扩展 73 项分类对账）。**真宿主 GUI 点击级验收仍未做**（本轮只补后端语义与文档）。
> **明确未改**：8 条硬红线原文、三段式写操作、17 个事件名（未新增）、后端任务状态 4 个、preflight **19** action 与
> NEEDS_HOME **17**、`pkg/errs` **28** 码、§0.3 全部数字（扩展目录仍 73／常用仍 11／门禁仍 5 项）、密码／版本／
> 域名／端口策略、离线缓存三条铁律与临时目录四必清、§5.15 三根互斥唯一、§5.16.1 目录与 §5.16.4 停用=删 ini 口径、
> 冻结的原型 SSOT（`前端唯一界面来源.txt` 与 `index.html`）。
>
> **v2.9.9 变更（新增 §5.16 PHP 扩展目录与实时日志、§5.17 备份归档容错与逻辑导出、§5.18 数据服务运行态，把 Request G 的四条收口为冻结条款）**：
> 本版本处理的是四处真实缺陷：① 安装 PHP 时扩展区只有零星几项、要用户手输，且「管理扩展」弹窗的清单被裁剪掉，
> 用户看不见本版本可用扩展的全貌；② 扩展编译期容器内输出既不实时也不可读（Docker exec attach 流带 **8 字节帧头**），
> 失败只报「编译失败」不点名是哪一项；③ pgsql 反复启停后无法启用——默认 `postgresql.conf` 让容器内进程往**宿主 bind
> 挂载的日志目录**写文件（宿主 0755、容器内另一 uid），`Permission denied` → FATAL → `unless-stopped` 无限重启，且
> `prepareService` 不覆盖已存在的配置，旧装机永远修不到；④ 备份因单个读不动的文件（真机是 `logs/access.log`）整包失败。
> 现新增 **§5.16**（唯一全量目录 73 项 · 8 分组 · 常用 11 项默认勾选、目录外可手工添加、**目标扩展集**语义、六步全链路
> 逐行日志、失败点名扩展并中止、停用 = 删 `conf.d` ini 的真机取证）、**§5.17**（读不动即跳过 + 按目录聚合告警、暂停前
> `mysqldump` / `pg_dumpall` / `redis-cli --rdb` 逻辑导出入 `dump/`、恢复侧明示未重放）、**§5.18**（pgsql 日志走 stderr、
> 启用路径 `healPgLogging` 原地截断自愈、`waitRunning` 复验 + 失败带退出码与容器日志尾部）三节条款，并补 §7 **决策 25 / 26 / 27**、
> §9 风险 **R93–R97**、§12.9 自查清单。关键口径：**扩展目录是便利集不是白名单**（过 `ValidateExt` 格式即可添加，最小限制原则）；
> **提交的永远是完整集合**（后端自算增删，无变化不建任务、不重建）；**容器内命令的输出一行都不能少、失败必须点名**
> （§0.2 新增规则 **26**）；**归档不被单个读不动的条目判死，但缺了什么必须写进日志**（规则 **27**）；
> **容器内不得往宿主 bind 目录写日志文件**（规则 **28**）。唯一出口 `engine.ExecStream`（`stdcopy` 去帧 + stdout/stderr 双 writer）
> 取代 `ExecInContainer`，扩展编译与备份转储共用。同步落点：头部三条新条款、§0.3 权威表补 **同类路径生效根数 1（把 v2.9.8 的互斥唯一口径显式写进权威表）／扩展目录 73／常用 11／逻辑导出 3／
> 归档跳过处置／启动就绪 12s／门禁脚本 5** 七行、§1.1 结论表三行、§4.1 目录树（`ExtPicker.vue` 使 common 组件 8→9、
> `scripts/check-ext-catalog.go`）、§6 映射表四行、§11.3 门禁四项→五项。**pgsql 日志段是对冻结原型 `DEFAULT_CONFIGS` 的
> 唯一一处生产偏离**，已在 `scripts/check-templates.go` 的 golden 注明原因；原型 SSOT（`前端唯一界面来源.txt` 与 `index.html`）
> 与 demo-only 的 `frontend/src/constants/configs.ts` 未改。
> **同源补全（真机取证之后）**：§4.1 与 §11.3 登记 `test/integration/g4_pgsql_heal_live_test.go`——旧版 `postgresql.conf` 裸
> `StartContainer` 必超时失败且报错自带容器日志尾部（`could not open log file`），同一份坏配置经 `AppService.Start` 即自愈并等到就绪，
> 把 §5.18.2 的因果在真机上两头锁死（R97 缓解列同步）；`*_test.go` 计数按真实仓库校正为 **78**（与包同目录）+ `test/integration/` **10**
> 个 live 用例（原 77 系既有漂移，非本轮引入）。
> **明确未改**：8 条硬红线原文、三段式写操作、17 个事件名（未新增）、
> 后端任务状态 4 个、preflight **19** action 与 NEEDS_HOME **17**、`pkg/errs` **28** 码、密码／版本／域名／端口策略、
> 离线缓存三条铁律与临时目录四必清、§5.15 三根互斥唯一条款、抽屉 70%／30% 与系统日志通道 200 行上限。
>
> **v2.9.8 变更（三处「可自定义根」+ 手工导入缓存 + 十七事件全落地 + 抽屉占比改默认值，把 Request F 的八条一次性收口为冻结条款）**：
> 本版本处理的不是文案，而是四类真实缺口：① 缓存根/备份根/每服务版本数据目录此前只有后端能配、界面上改不了，
> 且「自定义」与「默认」两条路径可能同时被读到，行为二义；② 离线缓存页的 `off-events` 只是本地列表，与 §5.6
> 的 `cache:*`／`docker:*` 事件协议脱节——用户在那里看不到真实事件，抽屉里也没有任何地方承接「无运行任务时」的事件；
> ③ 抽屉 70%／30% 被 v2.9.7 写成「固定占比」，日志长行无处扩展；④ 抽屉头部左侧标签跟着路由名变字，任务运行时
> 看不出这是全局抽屉。现新增 **§5.15 可自定义根**（三根表 + 互斥唯一判据 + 六步写链路 + 目录选择器 + 不可自定义
> 清单）、**§5.14.9a 手工导入缓存条目**（复制保留原件、写 manifest、复用 `cache:promote`、**不 `docker load`**）、
> **§5.6.2 全局实时同步落地表**（17 事件 × 落地 store／是否进日志逐行核对 + 事件之外的五类回流要求）、
> **§7 决策 23**（单一生效根 + 唯一出口 + 改根即 `Rebind` 换图）与 **决策 24**（事件逐一落地 + 系统日志通道）。
> 关键口径：**同一类路径永远只有一个生效值**——自定义与默认互斥，出口是 `config` 层的 `DerivePaths` →
> `ApplyRootOverrides` → `ApplyDataDirs`，任何层不得自行拼 `~/phpo/offline`；**「未自定义」必须读作空值回落**，
> 不能读作「两条都试一遍」。**抽屉占比改为「默认 70%／30%」**（可拖拽、40–80 夹取、双击复位、写 `localStorage`、
> 属 UI 偏好不落库）——本条**覆盖** v2.9.7 变更段中「两栏固定占比」的表述；抽屉头部左侧标签**固定显示「服务」二字**
> （取 `t('nav.services')`，不随路由变）。系统日志通道的合规性显式写明：它**不是任务记录、不落账本、不造第 5 态、
> 不新增事件名**（硬红线 4），`update:progress` 是唯一不进日志的显式豁免。§0.3 权威数字同步为
> **preflight 19 action／NEEDS_HOME 17／errs 码 28／事件落地覆盖率 17／17／可自定义根面数 3／系统日志上限 200**，
> 并删除该表内重复的「任务抽屉左右占比」行；§4.1 目录树按真实仓库修正为 **68 个绑定方法**（以生成的
> `frontend/bindings/phpo/app.ts` 为准）、补 `rules_root.go`、`EditablePathBar.vue`、`CacheImportModal.vue`；
> §0.2 补规则 **24**（禁止同类路径两条并存）与 **25**（17 事件名每个都要有落地处），且**故意不重编号**——
> 文档内已按号引用既有规则。同步落点：`config.example.yaml` 补 `offline_root`／`backup_root`／`data_dir` 三键；
> `docs/{CHANGELOG,界面规格,事件流协议,状态同步,离线缓存机制,路径策略,接口契约,目录规范,用户手册,备份脱敏规范}.md`、
> `任务工单.md`、`实施顺序.md`。**明确未改**：任务状态仍 **4** 个、事件名仍 **17** 个且未新增、8 条硬红线原文、
> 三段式写操作、端口／密码／版本／域名策略、临时目录「四必清」与 SHA256 校验、§5.14 离线缓存三条铁律的方向、
> `DefaultHome`／`DefaultWWW` 等已标注「默认」的字面量。
>
> **v2.9.7 变更（新增 §5.6.1「任务抽屉：日志 70% ／ 队列 30%」，把抽屉的布局／排序／显示态写成冻结条款）**：
> 此前 §5.6 只规定「队列详情走快照」，抽屉长什么样没有条款，于是实现把任务队列铺成日志上方的一横条 chip，既看不清
> 每条任务的态，也无法在提交后立刻确认「它进队列了没有」。现补三条硬约束：① 展开后**左栏日志 70% ／ 右栏任务队列 30%**
> 两栏固定占比；② **新任务永远在最上面**（按提交／入队时间倒序，正在执行的任务不因开始执行而下移）；③ 每一行必须给出
> **明确的 UI 显示态**——等待中 ／ 执行中 ／ 已完成 三态为主，另列 失败 ／ 已取消，并**预留 `unknown` 兜底位**：
> 映射未覆盖的任何后续新增态一律落兜底，不得留空白、不得当作已完成。
> 关键口径：**显示态是派生，不是第 5 个状态**——§0.3「任务状态 4」与 §5.6 的 17 事件名一字未动，新增显示态只允许改
> 前端 `taskStore.ts` 的单一 `displayOf` 映射，不得往 `model.TaskStatus` 加值。「即时同步」走既有链路：后端
> `SetQueueWatcher` 在入队／移交／撤回时重发权威快照，前端为 `Running` **和每一个 `Pending` 项**建占位行；被撤回的行
> 随下一次快照消失，若它正被选中则在同一快照里把选中回落到运行中任务（不留「状态未知」的幽灵详情），全程不做
> 本地乐观插入、不推断终态（硬红线 4）。同步落点：§0.3 补两行（占比／排序）、§5.6 承载说明补注派生口径、§6 映射表补
> 一行、§1.1 结论表与底部摘要各补一条、`docs/界面规格.md`（两栏结构与行内元素）、`docs/状态同步.md`、`docs/事件流协议.md`、
> `docs/CHANGELOG.md`、`任务工单.md`（Q5/Q6 追加修正条 + i18n 键数 568→570）。**明确未改**：4 个任务状态、17 个事件名、
> 三段式写操作、8 条硬红线、端口／密码／版本／离线缓存策略、§5.6.1 之外的任何行为条款。
>
> **v2.9.6 变更（§5.14.3 补入「未命中先探本机 Docker 镜像库」这一级，与已落地实现对齐；离线缓存铁律方向未变）**：
> 原条款把镜像未命中一律写成「→ `docker pull`（网络）」，把画像 F（内网/断网）在**缓存已丢失**这一情形下堵死了——
> `./offline/{kind}/{version}/image.tar` 被误删、换机或手工 `docker load` 过时，镜像明明还在本机 Docker 里，却因拉不到
> 网络镜像而装不了、缓存也重建不出来。现把优先级从「离线缓存 > 网络」改为 **「离线缓存 > 本机 Docker 镜像库 > 网络」**：
> 未命中先 `ImageExists` 探本机镜像库，命中即 `docker save` 零网络重建缓存（`cache:miss` 的 `action=local`），探不到才
> `docker pull`（`action=pull`）；探针报错必须上抛，不得静默当作「本机没有」（§5.14.12 禁止静默）。同步落点：头部「离线缓存
> 原则」、§1.1 结论表、§1.13 一句话、§1.13.1 铁律 2 补充、§1.13.2 流程图、§1.13.4 与 §5.14.12 禁止项、§0.2 规则 21、
> §0.3「缓存命中优先级」行、§3.2 原则 9、§5.6 与 §5.14.11 的 `cache:miss.action` 取值、§5.14.5 损坏处理、§5.14.8 断网、
> §5.14.13 协同表、决策 22（标题 + 规则 3 + 禁止项）。派生文档同步（§13 第 4 步）：`docs/{离线缓存机制,离线缓存协议,事件流协议,缓存清单规范,用户手册,M6-集成验收记录}.md`、`任务工单.md`（T211/T303/T606b）、`实施顺序.md`（离线优先铁律、3.2 行）；前端 `CacheHitBadge` 按 `action` 分档显示「本机重建 / 走网络」，不得把零网络重建报成联网下载。**明确未改**：三条铁律
> 与「装前必查 / 命中零网络 / 临时目录必清」的方向、§0.3 除「缓存命中优先级」一行外的全部数字、17 个事件名（只补注载荷取值，
> 未新增事件）、§5.14.4 临时目录四必清、SHA256 校验、硬红线 8 条原文。**扩展（apk/pecl）不适用本级**——`.so` 编译产物随镜像
> commit 走，本机无独立文件可探，仍是未命中即联网下载（`action=download`）。
>
> **v2.9.5 变更（把 v2.9.4 的路径记法补全到「派生文档 + 代码注释 + 面向用户的文案」，不改任何行为条款与 §0.3 数字）**：
> v2.9.4 只清了本文件内的 `~/phpo/…`，把「默认值字面量、真机验收留痕、前端 locales 文案」列为刻意保留——这正是
> 用户仍能读到「按模式清理 `~/phpo/offline` 缓存」这类**把默认值当运行时位置断言**的文案的来源。现补齐三处口径：
> §0.1.1 新增三条约束（① 记法适用范围扩到 `docs/`、`任务工单.md`、`实施顺序.md`、代码注释与前端 `locales/` 文案；
> ② 只有「默认值 / 预填值」本身可写字面量，且**必须带「默认」字样**，缺「默认」即视为违规；
> ③ 真机验收留痕要写当次实际路径时，先给 `./…` 相对记法，再把具体值作为该次实例注明）。
> 同步落地：前端文案 `cleanup.cache.subtitle`、`wiz.s1.hint`、`wiz.s2.hint`（zh-CN / en-US 各 3 键，键集不变）改以
> 「工作目录」称呼 PHPO_HOME 并给出相对子路径；`docs/M6-集成验收记录.md`、`docs/T705-Linux真机冒烟记录.md` 的
> `~/phpo/…` 改 `./…` 并注明本机取默认根；`docs/路径策略.md`、`docs/用户手册.md` 的派生子路径补 `./` 前缀；
> `任务工单.md` 的 T201 双根规则改记法、T203 要点的旧 `.env` 改记为 PHPO_HOME 根下的 `./.env`。**默认值字面量仍保留**：`config.DefaultHome`
> / `DefaultWWW`、`config.example.yaml`、`frontend/src/utils/path.ts` 的 `DEFAULT_HOME`/`DEFAULT_WWW`、mock fixture，
> 以及已废弃的 `~/.phpo/config.json`；冻结的原型 SSOT（`前端唯一界面来源.txt` 与派生 `index.html`）未改。
>
> **v2.9.4 变更（仅统一路径记法，不改任何行为条款与权威数字）**：新增 §0.1.1「路径记法」并补头部「路径记法」条目——
> 本文件的 **`./` 一律指 PHPO_HOME 根**（`config.yaml` 的 `phpo_home`，装机向导可指向任意目录），此前写死的
> `~/phpo/…` 字面量全部改为相对记法（共 47 处，覆盖 §0.2 规则 21、§0.3 权威表、§1.13.2/1.13.3、§4.2、§5.2、
> §5.14.2/5.14.3/5.14.4/5.14.9、决策 22、§8 跨平台矩阵）。理由：本文件描述的是**打包安装后**的运行时目录，
> PHPO_HOME 由用户在装机向导选定（默认值才是 `~/phpo`），把默认值当约束写死会导致代码/文档误假定位置。
> §8 的「缓存路径」「临时目录」三平台列统一为 `./offline/` 与 `./{kind}/{version}/ext/`（同源派生、跨平台同记法），
> 并在表下注明各平台默认候选仅为默认值；§8「路径解析」行订正为事实：Windows 无 shell `~`，但本项目由
> `config.ExpandHome`（`os.UserHomeDir()`）在出口统一展开，三平台一致。§4.2 用户数据目录段首改为 `<用户数据目录>/`
> 记法，与 PHPO_HOME 的 `./` 明确区分（两者不同源、不同生命周期）。字面量 `~/.phpo/config.json`（已废弃旧配置）
> 与 `~/www/`（WWW_ROOT 默认值）原样保留。**离线缓存铁律、临时目录路径规则、硬红线 8 条、端口/密码/版本策略、
> §0.3 全部数字一字未改。**
>
> 派生文档同步（§13 第 4 步，同一记法口径）：`docs/目录规范.md`（§7 树根 + `<用户数据目录>` 记法与记法注）、
> `docs/离线缓存机制.md`、`docs/缓存清单规范.md`、`docs/版本策略.md`、`docs/临时目录生命周期.md`、
> `docs/备份脱敏规范.md`、`docs/跨平台差异.md`、`docs/领域模型.md`、`docs/用户手册.md`、`任务工单.md`（T303 验收行）
> 中把 PHPO_HOME 写死为 `~/phpo/…` 的运行时路径统一改为 `./…`；`internal/cache/tempdir.go`、`internal/model/backup.go`、
> `internal/config/offline.go` 的同类注释同步。**明确不改**：已标注「默认」的默认值/预填值字面量（`config.example.yaml`、
> `docs/路径策略.md`、`docs/接口契约.md`、`docs/用户手册.md` 向导条目、`config.DefaultHome`）、OS 决定的用户数据目录绝对路径、
> 真机验收留痕（`docs/M6-集成验收记录.md`、`docs/T705-Linux真机冒烟记录.md`）、历史事实（`docs/CHANGELOG.md`）、
> 冻结的原型 SSOT（`前端唯一界面来源.txt` 与其派生 `index.html`）、测试 fixture 里的示例根。

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
- 配置存储：**单一 `config.yaml`（YAML，各平台 XDG 用户配置目录内 `phpo` 子目录）承载两根 + 可自定义根（`offline_root`/`backup_root`）+ 明文密码 + 端口 + 每服务版本 `data_dir`；SQLite 仅存运行态且延迟建库（两根未落地即首启零落盘）；`dirReady` 由快照派生、不落库；不再有 `./.env` / `~/.phpo/config.json`**
- 可自定义根（v2.9.8）：**缓存根（默认 `./offline/`）· 备份根（默认 `./backups/`）· 每服务版本数据目录（默认 `./{kind}/{version}/data`）三处支持编辑路径 + 打开到任意文件夹 + 读写；每一类互斥唯一、永远只有一条生效路径（自定义即完全取代默认）；唯一限制是路径安全；改根走 `root-set` preflight → 落库 → `Rebind` → `state:changed`，队列非空即 `TaskBusy`；见 §5.15**
- 路径记法：**`./` = PHPO_HOME 根（`config.yaml` 的 `phpo_home`，装机向导可指向任意目录，`~/phpo` 仅默认值）；`<用户数据目录>/` = `os.UserConfigDir()/phpo`（两者不同源）；该记法同等约束 `docs/`、任务工单、代码注释与前端 locales 文案，只有带「默认」字样的默认值/预填值可写字面量；见 §0.1.1**
- 密码：**明文，默认 `123456`，可修改，可为空，长度不校验，UI 可查看**
- 版本：**不限制字符集，仅做路径安全校验**
- 端口：**站点端口默认 80、用户可指定任意端口；新建站点占用不顺延（告警 + 站点降级，站点照常创建）；改已有站点端口占用才顺延 1–65535 首个可用（不报错）；服务端口占用报错**
- 域名：**允许 `localhost`、单段名、任意后缀、多域名、泛域名**
- 路径：**允许 WWW_ROOT 外（警告）**
- vhost：**放开 `proxy_pass`、`ssl_certificate`、`load_module` 等（保留 nginx -t）**
- PHP 切换：**精确对应 `php-{version}-fpm:9000`**
- 状态同步：**后端唯一权威 + 前端订阅事件 + 17 事件名逐一实时落地（§5.6.2）；无任务归属的 `cache:*`/`docker:*`/`update:*` 逐行进抽屉「系统日志通道」（`update:progress` 除外）**
- 可自定义根（v2.9.8）：**缓存根（默认 `./offline/`）／备份根（默认 `./backups/`）／每服务版本数据目录（默认 `./{kind}/{version}/data`）三处可编辑路径 + 打开任意文件夹 + 读写；每一类互斥唯一（自定义即完全取代默认，不做二级回退），唯一限制是路径安全；改根走三段式并需队列空闲，`Rebind` 后即生效；缓存条目可手工导入（复制、不搬走源文件）**
- PHP 扩展（v2.9.9）：**每版本一份全量目录（73 项 · 8 分组，常用 11 项安装时默认勾选，目录外可手工添加）· 安装弹窗与「管理扩展」弹窗共用同一份 · 提交完整目标扩展集（无变化不重建）· 容器内编译输出去帧逐行实时进抽屉 · 失败点名扩展并中止本单 · 停用 = 删 `conf.d` ini（见 §5.16）**
- 备份归档（v2.9.9）：**读不动的条目跳过 + 按目录聚合告警，不判死整包 · mysql／pgsql／redis 在暂停服务前逻辑导出（`mysqldump`／`pg_dumpall`／`redis-cli --rdb`）入归档 `dump/` · 恢复侧明示 dump 未自动重放（见 §5.17）**
- 数据服务运行态（v2.9.9）：**容器内进程不往宿主 bind 挂载目录写日志文件（pgsql 日志走 stderr 由 Docker 收集）· 旧装机的坏配置在「启用」时就地截断自愈 · 启动等稳定 running，失败带退出码与容器日志尾部（见 §5.18）**
- 任务抽屉：**日志左默认 70% ／ 队列右默认 30%（中缝可左右拖拽 40–80%、双击复位；头部左侧标签固定为「服务」二字） · 新任务永远在最上面（提交时间倒序） · 每行显式显示态（等待中／执行中／已完成，另留 unknown 兜底位；系派生，后端任务状态仍为 4 个）**
- 升级：**支持版本检查和自动升级（SHA256 + Ed25519 双校验）**
- Docker 清洁：**所有操作幂等、原子、隔离、一致、可清理、可恢复**
- 离线缓存：**装任何镜像/扩展必先查缓存（在用户选定的缓存根下，默认 `./offline/`）· 命中零网络 · 镜像未命中先探本机镜像库（已有即零网络重建缓存）· 否则下载编译 · 成功后提升到缓存 · 无论成败均清空临时目录 · 支持手工导入任意包文件为缓存条目 · 断网重装靠缓存 · 内网开发靠缓存**
- **编码准则：编码前思考 · 简洁优先 · 精准修改 · 目标驱动执行**
- 插件：**不做**
- **核心原则：最小限制 + 警告代替阻止 + 用户是程序员 + Docker 操作干净 + 离线优先 + 临时目录必清 + 编码准则**
- **硬红线：仅 8 条**
- 状态：FROZEN（冻结）

> 本文件为项目最高技术总纲，所有其他文档必须与之保持一致。