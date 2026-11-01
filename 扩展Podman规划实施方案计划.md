# 扩展 Podman 规划实施方案计划

> **文档类型**：规划方案（不是已完成功能的说明）
> **写于**：2026-10-05
> **适用版本**：phpo 总纲 v2.9.15（FROZEN）
> **本轮动了什么**：**只写这一份文档，一行代码都没改**。下面所有「落在哪个文件」都是在说「将来实现时要改哪里」，不是在说「已经改好了」。
> **需求原文**（用户）：在现有基于 Docker 的 phpo 上增加对 Podman 的支持，要求 ① 绝不在原有 Docker 功能上做破坏性改动，只做增加；② 装好 phpo 后要能检测用户机器上用的是 Podman 还是 Docker，有一个就用它，两个都没有就提示用户去装（Docker 那边早就有检测了，Podman 这一半需要规划）；③ 说清这个方案怎么和当前项目合理地拼在一起。

---

## 0. 一句话结论

**能做，而且可以做到「Docker 用户完全感觉不到这次改动」**——因为整个项目碰 Docker SDK 的地方只有 13 个文件、构造点只有 1 处（`internal/app/di.go` 里的 `engine.New()`），把「用哪种方言说话」收进 `internal/engine` 这一层，上面所有层（service / task / vhost / preflight / store / cache）一个字都不用动。

**真正难的那一件事不是替换 API，是 DNS**：站点的 vhost 配置靠 Docker 自带的内嵌 DNS（`127.0.0.11`）在每次请求时把 `php-8.4-fpm` 这种名字解析成容器 IP；Podman 用的是另一个东西（aardvark-dns），地址不一样、能力也不完全一样。这条直接压在硬红线 1（上游必须精确指向 `php-{version}-fpm:9000`）和硬红线 2（vhost 写入前必须 `nginx -t` 通过）上，**必须先在真机上取证，再决定动不动模板**——不能凭印象改。

下面分五段说：A 边界（为什么不破坏）、B 检测与仲裁怎么做、C 风险清单（按难度分档）、D 总纲要走什么流程、E 分五阶段实施与每阶段怎么验收。最后列四件必须用户拍板的事。

---

## A. 边界：为什么「只做增加」这件事可以证明，而不是承诺

### A.1 这是干什么的

先摸清楚「Docker 的调用到底散落在哪里」。摸完的结论是：**全项目只有 13 个非测试文件 import 了 Docker SDK**，全部在 `internal/engine/` 里面：

| 文件 | 行数 | 它负责什么 |
|------|-----|-----------|
| `client.go` | 146 | 构造客户端、选端点、探测 Docker 装没装/开没开/有没有权限 |
| `container.go` | 225 | 建容器、启停、删除、等稳定 running |
| `image.go` | 109 | 查镜像在不在本机、save、load、pull、tag、remove |
| `network.go` | 34 | 网络的建 / 删 / 查 |
| `volume.go` | 31 | 卷的建 / 删 / 查 |
| `mount.go` | 23 | 把挂载表算成宿主⇄容器那串参数 |
| `exec.go` | 90 | 容器内执行命令，并把 Docker 那 8 字节帧头去掉后逐行分流 stdout / stderr |
| `copy.go` | 117 | 宿主⇄容器唯一的字节通道（扩展包回填与取回都走它） |
| `inspect.go` | 119 | 读容器状态、退出码、日志尾部 |
| `discover.go` | 367 | 数这台机器上 Docker 里此刻有哪些服务容器（含停着的、含不是 phpo 装的） |
| `registry.go` | 72 | 把镜像名换成「走某个镜像源」的名字（§5.21） |
| `clean_inventory.go` | 649 | 总览页那 60 行资源的逐项清点（§5.22） |
| `clean_delete.go` | 217 | Docker 侧那九类删除的唯一实现（§5.22 需求 ⑨） |

**不碰 SDK 的 engine 文件**：`audit.go`、`calibrate.go`、`cleaner.go`、`idempotent.go`、`trash.go`，以及 `mirror.go`——`mirror.go` 只是拿 `net/http` 去问镜像源「`/v2/` 在不在」，它连容器引擎都不是，**Podman 一套都不用改**（§5.21 那条能力天然是引擎无关的）。

### A.2 为什么这就够说明问题

`internal/app/di.go` 是**唯一的装配处**：`engine.New()` 在那里面只调一次，拿到的 `cli` 按位置传给大约 10 个构造函数，每个 service 自己声明它只要哪一小撮能力（窄接口）。一个很说明问题的细节：`service.NewDoctor(cli, cli, …)` 把同一个 `cli` 传了两次——因为 doctor 需要「探测」和「操作」是两个不同的能力面，各自声明了接口。

再关键的一条：**engine 不是路径**。`rootsKey()`（改自定义根时用来判断要不要重建整张对象图的指纹）算的是 `PHPOHome, WWWRoot, OfflineRoot, BackupRoot` 加上排序后的各服务数据目录，**引擎种类不在里面**。所以「这次用 Podman 还是 Docker」绝不能塞进 `rootsKey`，否则用户在设置页改个缓存根会把引擎也一起换掉，那是平白无故给 §5.15 加一条谁都没要的耦合。

### A.3 因此这次的形状（最小爆炸半径）

**保留 `*engine.Client` 这个类型名，保留全部 13 个文件的对外签名，一个字都不改**；只在 `Client` **内部**分岔两件事：

| 内部分岔 | 说什么 |
|---------|--------|
| 端点 | 连的是 Docker 的 socket 还是 Podman 的 socket |
| API 方言 | 少数几个字段 Podman 的兼容层不吃、或者语义不同（见 C 段分档） |

上面 13 个文件的每个方法仍然按 Docker SDK 的写法调用——因为 **Podman 提供的就是 Docker 兼容 REST API**，`/containers/json`、`/images/json`、`/containers/{id}/start` 这一批形状是一样的。这样做的直接好处：**Docker 用户走的那条代码路径一行都没变**，这就是需求 ① 里「绝对不能破坏原有功能」的可执行形式，不是口号。

**不做的三件事（避免越界，也避免把简单事做复杂）**：
1. **不**在 service 层加「如果是 Podman 就……」的分支（那会把 13 处的耦合摊成几十处）。
2. **不**引入 `Engine` 抽象接口层去包一遍所有能力（只有两个实现、且差异只在少数点，套一层接口是 §3.4.2 明令禁止的「为一次性需求创建抽象层」）。
3. **不**在本阶段支持「运行中切引擎」——见 B.4。

---

## B. 检测与仲裁：装好后 phpo 怎么知道该用谁

### B.1 这是干什么的

用户打开 phpo，界面要能回答三件事：**用的是哪个引擎**、**它现在能不能用**、**两个都没装的话我该去装什么**。

### B.2 Podman 的探测照 §5.7 那三条冻结口径写

Docker 那一侧已经有三条被总纲钉死的规矩（`internal/engine/client.go` + `health.go`），Podman 直接复用同一套形状，不另立标准：

| 规矩 | Docker 现在的做法 | Podman 照抄的部分 |
|------|------------------|------------------|
| **① 端点一次选定，全链路共用** | `DOCKER_HOST` 显式设了就用；否则按 `/var/run/docker.sock` → `/run/docker.sock` → `$XDG_RUNTIME_DIR/docker.sock` → `/run/user/<uid>/docker.sock` → `$HOME/.docker/run/docker.sock` 取第一个**盘上真是 socket** 的候选。必须覆盖后三条，因为从 `.desktop` 启动的进程拿不到用户 shell 里 `export` 的环境变量 | 候选顺序：**`CONTAINER_HOST` / Podman 配置里的 `URI`**（显式优先，同 §5.7 精神）→ **`unix:///run/user/<uid>/podman/podman.sock`**（rootless，最常见）→ **`unix:///run/podman/podman.sock`**（rootful）。同样必须是「盘上真是 socket」才算候选 |
| **② 三态不得合并** | `not_installed`（socket 不在盘上）／`not_running`（socket 在但连不上）／`no_permission`（`permission denied`）三态各一。`no_permission` **不得**报成「未运行」——apt 装的 `docker.io` 建了 `docker` 组但不把你加进去，这是 Ubuntu 上最常见的一种失败 | 同样三态。Podman 的 rootless socket 由用户自己的 systemd 用户实例拉起，`podman.socket` 没 enable 时那个 socket 文件**存在但连不上**，这**正好**落进 `not_running`——分档对了才说得出「请执行 `systemctl --user enable --now podman.socket`」这句有用的话 |
| **③ 消息自带原因，建议按平台分岔** | 一行「结论（{原始连接错误}）」，Linux 上绝不建议「启动 Docker Desktop」（那个产品在该平台不存在） | 同口径。Linux 建议装 `podman` 并启 socket unit；**不得**给一个该平台不存在的东西 |

**探活用什么**：Podman 的 Docker 兼容 API 也应答 `GET /_ping`，所以探活形状与 Docker 一致，不需要新写一套。

### B.3 仲裁规则（谁优先）

**推荐：Docker 优先（Docker-first）**。

| 现场 | 结论 |
|------|------|
| 只有 Docker 可用 | 用 Docker —— **与今天的行为逐字节一致** |
| 只有 Podman 可用 | 用 Podman |
| 两者都可用 | 用 Docker（不擅自把已有用户迁走） |
| 两者都不可用 | **应用照常启动**，服务卡片照常能看（读的是库里的账），但任何启动服务的动作都被硬红线 7 挡住，界面给一句人话：需要装 Podman 或 Docker，附本平台怎么装 |

为什么推荐 Docker-first：它是需求 ①「绝对不破坏原有功能」最硬的形式——今天所有能跑的用户，升级后拿到的引擎和以前是同一个，Podman 只服务「机器上根本没有 Docker」的那批新装机。

（这一条是**待你拍板的第一件事**，见文末 D0-1。）

### B.4 边界（明确不做什么）

1. **本阶段不做「运行中切引擎」**。切引擎等于把所有在跑的容器、网络、卷、以及缓存里 save 出来的镜像都变成另一个守护进程管的东西，用户会看到一整屏「容器缺失」（§5.19 那套缺失态本来是为「第三方工具改过 Docker」设计的，不该被我们自己在切引擎时滥用）。真要切，路径是：**先把另一边的容器清空，重启 phpo**。检测只在启动时做一次，把结论定下来。
2. **不新增事件名**（§5.6 冻结为 **17** 个）：引擎结论走权威快照里的一个字段，随 `state:changed` 回流；探测失败时那一行说明走现有的 doctor / 抽屉日志。
3. **不新增门面方法**：`app.go` 保持 **80 个导出方法 = 78 个生成绑定 + `ServiceStartup`/`ServiceShutdown` 两颗生命周期钩子**这一账不动。引擎选择与检测结果都挂在已有的 doctor / 设置页出口上，不加新 RPC。
4. **不进 `rootsKey`、不触发 `Rebind`**（见 A.3）。
5. **绝不**写用户的 Podman / Docker 全局配置（不改 `/etc/containers/containers.conf`、不改 `~/.config/containers/`、不改 `storage.conf`、不改宿主 `daemon.json` / `registry-mirrors`、**绝不**重启用户的 Docker 或 Podman socket）。这与 §5.21 已经钉过的立场同一条：**只影响 phpo 自己发起的那一次调用**。
6. **不碰 §5.20 的权限口径**：phpo 自己的产出物仍一律 `0777`；容器内进程写出来的文件照旧不归我们管（那是另一个 uid）。

---

## C. 风险清单（按「要不要适配、有没有等价物」分三档）

分档口径：
- **T0 = 不用动**：Podman 的兼容 API 行为一致，或这段代码本来就不碰引擎。
- **T1 = 必须适配**：Podman 有等价能力，但要写不同的参数或不同的字段解读，**语义必须与现在完全一致**（否则「幂等」会变成假话）。
- **T2 = Podman 没有等价物**：界面上**不得给 0**，必须显示「—」并附一句原因——这在 §5.22 需求 ㉘ 与 §0.2 规则 41 里**早就允许**（「禁止把『不知道』画成『没有』」），所以这一档**不需要改总纲**。

| # | 位置 | 档 | 具体是什么 | 用户看得见什么 / 怎么回来 |
|---|------|-----|-----------|--------------------------|
| **C1** | **容器网络的内嵌 DNS**（`vhost` 模板 + `network.go`） | **T1，最硬的一块** | vhost 里写的是 `set $php_upstream php-8.4-fpm:9000;` + `resolver 127.0.0.11 valid=10s;` + `fastcgi_pass $php_upstream;`。`127.0.0.11` 是 **Docker 内嵌 DNS 服务器**的固定地址，nginx 每次请求现查一次这个名字 → 所以「切 PHP 版本」只需改一行、reload 即可生效（§5.24 的先备上游再切流量靠它）。Podman 用的是 **aardvark-dns**，它不在 `127.0.0.11`，而是挂在网络的网关地址上，且对「网络别名」的支持范围与 Docker 不完全一致 | 错了就是全站 5xx。**必须先真机取证再动模板**（见 E 段 S3 的三条候选解法，取证选一条）。取证没结论之前**不改任何模板** |
| **C2** | **网络别名 `Aliases`**（`network.go` 的 `Connect`，§5.13.2 网络别名 `{kind}-{version}`） | **T1** | 别名是 phpo 让「同一网里的 nginx 叫得响 php」的机制。Podman 兼容 API 吃不吃这个字段、是走 alias 还是只能靠容器名解析，**必须现测**（与 C1 是同一次取证的两半） | 同上；不通就退到「用容器全名 `phpo-php-8.4` 作为上游主机名」这条路，仍然精确指向那颗容器，不违反硬红线 1 |
| **C3** | **端口发布模型**（`service` 层的 `nginxSpec` / `assertSitePortsBindable` / `PublishPorts`，`inspect.go` 的 `PublishedPorts`） | **T1** | phpo 的站点端口是 1:1 发布（宿主端口 == 容器端口，nginx 那侧把基端口 ∪ 站点端口全发出去）。**rootless Podman 里普通用户默认 bind 不了 1024 以下端口**，站点默认端口 80 首装即撞这个（§0.3 默认站点端口 80）。**phpo 不得改 `net.ipv4.ip_unprivileged_port_start` 这类系统 sysctl**（那是用户的环境）。做法：探测出来 → 给一行明确说明（这个端口 rootless 下要改系统参数或换 rootful），并且**沿用已有口径**：新建站点端口占用只告警 + 站点降级，**不顺延、不改用户填的端口**（§1.9 / §5.8），改已有站点端口才顺延 | 卡片与确认框里那行说明 + `portInUse` 那条服务端口告警照常给 |
| **C4** | **挂载的 SELinux 标签**（`mount.go`） | **T1** | Linux 上开 SELinux（Fedora/RHEL 系）时，bind 挂载要带 `:z` / `:Z`，否则容器内看得到目录但读不了——表现与 §5.18.1 那种「Permission denied → 进程 FATAL → 无限重启」几乎一样，但**原因完全不同**。只在 Podman 分支加这个后缀，**Docker 分支一个字节都不变** | 只在 Podman 且检测到 SELinux 开启时生效；读不到状态时不加后缀并给一行说明 |
| **C5** | **exec 流的帧头**（`exec.go` 的 `ExecStream`，§0.2 规则 26） | **T1** | Docker 的 exec attach 流每帧带 **8 字节二进制帧头**，现在靠 `stdcopy.StdCopy` 去帧并分流 stdout/stderr，界面才是「逐行、失败点名到具体扩展」。Podman 兼容 API 声称同样形状，**必须现测**——去帧去不干净，日志里就是二进制垃圾（规则 26 明令禁止的那种后果） | 现测方法见 E 段 S2 判据；不一致就在 engine 内部补一层，接口对外不动 |
| **C6** | **宿主⇄容器字节通道**（`copy.go`，Docker archive API；扩展包回填 `/tmp/phpo-ext/{apk,pecl}` 与取回都靠它） | **T1** | 走的是 `/containers/{id}/archive` 这条路（PUT/GET tar）。Podman 兼容 API 有同一个端点，但对「uid 映射」的处理不同：rootless 下容器里的 root 是宿主的另一个 uid，取回宿主后的文件属主与提升进缓存后的可读性要**现测** | 与 §5.14.3 的离线缓存链一致：任一环节失败只一行 `dim` 说明后退回在线编译，**绝不判死整单**（§0.2 规则 16） |
| **C7** | **镜像 save / load / pull / tag / remove**（`image.go`） | **T1** | Podman 兼容 API 提供等价端点。要现测的是：load 之后镜像在 Podman 存储里的**引用名形状**是否与 Docker 一致（phpo 到处按 `php:8.4-fpm`、`phpo/php:8.4` 精确算，§5.16.2 的「禁止模糊匹配」）。以及 §5.21 那条「拉回来必须归一回原始引用、删掉带前缀的别名」必须在 Podman 上同样成立，否则离线缓存里存进去的是带源前缀的名字，换机即失效 | 归一失败即 `cache:miss` 那行点名原因；缓存链失败只 `dim` |
| **C8** | **`docker commit` 固化扩展镜像**（`phpo/php:{version}`，§5.16.3） | **T1** | Podman 有 `commit` 等价能力。要现测两件事：commit 出来的镜像能不能被下一次 `create` 直接用作镜像引用；以及容器暂存目录 `/tmp/phpo-ext` 是否在 commit **之前**真的清空了（§0.2 规则 36：不清空就把包文件固化进镜像层，成为永远清不掉的垃圾） | 提升成功那一行 `ok` 必须点名缓存槽位全路径（`{committedRef} → …/image-extensions.tar`），一字不改 |
| **C9** | **发现态**（`discover.go` 的 `DiscoverServices`，§5.19.3a） | **T1** | 一次 `ContainerList{All:true}` 问全（含停着的、含不是 phpo 装的）。Podman 的兼容端点同样支持 `all`，但**必须现测**：① 停着的容器会不会被漏（漏了就是把「在着」画成「没有」，§0.2 规则 41）；② 我们那三层判据（容器名 → 镜像+tag 反查 → 宿主端口只作佐证）拿到的字段是否齐全 | 问不通错误**一律上抛**，绝不把「这次没问到」铺成空清单 |
| **C10** | **两种「资源不存在」的写法**（散在 engine 里） | **T1** | 现在同一个判断有两种写法：一种用 `cerrdefs.IsNotFound`，一种用字符串比对的 `isNotFound`。**必须在 `Client` 内部收成一个**——Podman 的错误文本与 Docker 不同，靠字符串比对的那一半会认不出「已经不在了」，于是 §5.13.4 的幂等（「删一颗不存在的容器 = 成功」）在 Podman 上**静默变成假话**，Pre-Clean 会因「删除失败」把整单判死 | 收成一处后补一条用例：对同一个不存在的名字，两引擎都返回「幂等成功」 |
| **C11** | **§5.22 那 60 行的 T2 部分**（`clean_inventory.go` / `clean_delete.go`） | **T2** | 这些在 Podman 上**拿不到等价物**：构建缓存 prune（`BuildCachePrune`）、镜像共用层字节数（`LayersSize`）、引擎插件（`PluginList`/`PluginRemove`）、Swarm 那四组（service / task / node / secret / config）、compose 与 stack 那两个 label。**总计面板 60 行 / 三档 25·19·16 与危险名单恰好 4 行的口径不改**——只是这些行在 Podman 下走「数不到」那条既有通道 | 显示「—」+ 一句原因 + 该行「重试」；**绝不给 0**。这是 §5.22 需求 ㉘ 的既有形状，不需要新增任何条款 |
| **C12** | 纯统计那批（`DanglingImages`、`usedImageIDs` 的 ParentID 定点迭代、`UnusedImages/Volumes/Networks`、`Groups`）；`runDeletes` 那颗循环（单项失败不中断 / 取消优先 / 回执等长同序）；`mirror.go` 全部 | **T0** | 它们只是把已经问到的数据加工，或本来就是引擎无关的 HTTP 握手 | 无需说明 |

---

## D. 总纲要走的流程（不能跳）

### D.1 这是干什么的

AGENTS.md 是 FROZEN，§13 规定改它要走「变更提案 → 至少 2 名核心开发者评审 → 递增文档版本 → 同步更新 `docs/` 受影响文档 → 涉及目录结构则同步 `docs/目录规范.md` 与 `scripts/check-*.go`」。

### D.2 哪些是**必须**走提案的，哪些**不用**

| 事项 | 要不要动总纲 | 说明 |
|------|-------------|------|
| **硬红线 7「Docker 未装则不能启动服务」** | **必须走提案** | 这条的字面就是 Docker。改成「容器引擎（Docker 或 Podman）未装则不能启动服务」是**改红线原文**，不是加实现细节。8 条红线的原文在总纲里被反复声明「明确未改」，因此**不能偷偷绕过** |
| **§5.7 三条口径的措辞** | 建议走提案，一并收 | 那三条规矩本身（端点一次选定 / 三态不合并 / 消息自带原因 + 平台分岔）对 Podman 完全成立，只是行文全是「Docker」。建议加一句「本节口径同等适用于 Podman 探测」，不重写三条本身 |
| **§5.13.2 资源命名** | **不用改** | 容器名 `phpo-{kind}-{version}`、网络 `phpo-network`、卷 `phpo-{kind}-{version}-{purpose}` 在两边**形状一致**，命名前缀仍是 `phpo-` |
| **§5.22 那 60 行 / 三档 / 危险 4 行** | **不用改** | T2 那一档走「数不到 →『—』+ 原因」的既有通道（需求 ㉘ / §0.2 规则 41），一个数字都不动 |
| **§5.19.3a 发现态、§5.19 幂等静默三条件** | **不用改** | 判据形状与「上抛错误」的口径照抄即可 |
| **§5.20 权限 0777** | **不用改** | 只管 phpo 自己的产出物，与引擎无关 |
| **§5.21 镜像源** | **不用改** | `mirror.go` 本来就不碰引擎（C12） |
| **§5.15 三根互斥唯一 / `rootsKey`** | **不用改** | 见 B.4 第 4 条：引擎不进指纹 |
| **§5.16.2 三档显示与「停用 = 删 ini」** | **不用改** | 判据是容器内实测 `php -m`（走 C5 那条 exec 通道），与引擎种类无关 |
| **17 事件名 / 4 任务状态 / 20 preflight action 与 NEEDS_HOME 18 / `pkg/errs` 28 码 / `app.go` 80=78+2 / 5 项门禁 / i18n 键数（以第 1 项门禁输出为准）** | **一律不动** | B.4 已经写明承载一个都不增。若实现过程中某个数字真的变了，必须回来改这份文档 |
| **§1.1 结论表 / §0.4 画像** | **建议补一句** | 画像 F（内网/离线）在 Linux 发行版上 Podman 常常是预装的——这条对画像 F 是真价值，值得在结论表「Docker 运行时」那行补一句 |

### D.3 因此本轮的建议顺序

**先走提案、再动代码**（不改总纲就写代码，等于把「明确未改 8 条红线原文」这条自己作废）。提案范围**只两条**：硬红线 7 的措辞、§5.7 加一句适用范围。其余全部是「新加一节说明」而非「改既有口径」。

---

## E. 分五阶段实施，每阶段自带验收判据

排序原则：**每阶段都可单独验收、可单独回滚**，且**前四阶段跑完 Docker 用户的行为必须与今天逐字节一致**（因为每阶段都在 Docker 分支上把断言跑一遍作为回归基线）。

### S1 — 零风险地基：检测、仲裁、引擎标识（**不改变任何行为**）

- **干什么**：`client.go` 内加 Podman 端点候选与三态分类（照 B.2）；启动时按 B.3 定下一个引擎；结论进权威快照一个字段；设置页/doctor 那一行显示「当前引擎：Docker / Podman / 都没装」。
- **判据**：① 只有 Docker 的机器 → 结论 Docker，`Detect`/`health` 全部既有断言不红；② 只有 Podman（`podman.socket` 起了）→ 结论 Podman；③ 两者都可用 → 结论 Docker；④ 两者都不可用 → 结论「无引擎」，应用照常启动、启动服务的动作按硬红线 7 被挡、消息带原因并按平台给安装建议；⑤ `podman.socket` 存在但没跑 → 必须落 `not_running`（**不得**塌进「未安装」）；⑥ **没有新增事件名、没有新增门面方法**（`app.go` 仍 80 导出 = 78 绑定 + 2 钩子，`grep -c` 现取）。

### S2 — 只读取证：把 C 段所有「必须现测」变成事实（**只查不改**）

- **干什么**：在一次性容器上跑只读探针（**不碰用户数据目录**），逐条取证：C1 的 `127.0.0.11` 与 aardvark-dns 地址、C2 的 `Aliases` 是否生效、C5 的 exec 流帧形状、C6 的 archive 双向通道与 rootless uid 映射、C7 的 save/load 引用名与镜像在存储里的名字、C8 的 commit 产物可否直接被下一次 create 使用、C9 的 `ContainerList{all}` 是否含停着的容器、C11 逐类确认 Podman 兼容层到底有没有这些端点。
- **落哪**：`test/integration/p1_podman_probe_live_test.go`（沿用既有 **13** 个 live 用例的双重 skip 守护：`PHPO_LIVE=1` + 引擎可用）。
- **判据**：一张表，每条写「一致 / 不一致 / 没有等价物」，并给出**不一致时的具体证据原文**（不是「大概能行」）。这张表是 S3 的唯一输入——**取证没结论就不许动模板**。

### S3 — 硬骨头：DNS / 上游解析（**唯一会碰 vhost 模板的阶段**）

三条候选解法，**由 S2 的取证选一条**，不凭印象决定：

| 候选 | 做法 | 代价 |
|------|------|------|
| **(a) resolver 地址按引擎分岔** | Podman 分支把 `resolver 127.0.0.11` 换成 aardvark-dns 的那个地址；vhost 其余形状（`set $php_upstream` + `fastcgi_pass $php_upstream`）一字不动，**§5.5 精确匹配与 §5.24「先备上游再切流量」全部不变** | 最贴近现状；前提是 S2 证明别名解析可用 |
| **(b) 退到容器全名** | 上游主机名用 `phpo-php-{version}`（仍是「精确指向那颗容器」，不模糊匹配） | 需现测全名能否被解析；切 PHP 变成改这一行 + reload，语义不变 |
| **(c) 容器 IP 直连** | 把 IP 写进 vhost | **否决倾向**：容器重建 IP 就变，会造出「界面说换了、nginx 还连旧 IP」的假成功——正是 §5.24「先备上游再切流量」要避免的那类后果，硬红线 1 的准确性也就没了 |

- **判据**：真 Podman 机器上「装 PHP 8.4 + 装 nginx + 建站 → 站点 200」；切 PHP 8.3（含**切到停着的 8.3**，§5.24 那条自动启动目标容器的路径）→ 200；改站点端口 → 200；抽屉日志与实际生效情况一致；**同一批断言在 Docker 上重跑一遍全绿**（回归基线）。任何一条不通就不进 S4。

### S4 — 执行链逐块过（C3–C10）

- **顺序**：容器启停与状态读取 → exec 流与去帧 → 镜像 save/load/pull/tag/归一 → commit 固化扩展镜像 → 宿主⇄容器字节通道 → 端口发布与 bind 校验 → SELinux 挂载后缀 → 发现态 → **并把 C10 那两种 not-found 写法收成一个**。
- **每块自带用例**，且必须**同时跑通两引擎**（Docker 那条是回归，Podman 那条是新增）。
- **判据**（按总纲已冻结的口径逐条对）：
  - 扩展链：包文件本体在安装**与**重新编译两条路径上都落进缓存根并登记 SHA256；命中即**零网络**回填 `/tmp/phpo-ext/{apk,pecl}` 并从该文件编译；`/tmp/phpo-ext` 在 commit **之前**清空；宿主临时目录无论成败取消都清空且只发一次 `cache:tempdir-cleared`；编译失败点名扩展 + 一行说清缺哪个系统开发包 + 一行说怎么回来；**phpo 不代装系统包**。
  - 离线链：优先级仍是「离线缓存 > 本机镜像库 > 网络」，镜像源只站在最后一格（`action=pull`），`local` 与 `download` 完全不碰源。
  - 幂等静默：发事件闸门仍是三条件（无运行态纠正 ∧ `Gaps` 未变 ∧ 发现态未变），**不用** `res.Changed()`；连点两次「同步状态」第二次不刷屏。
  - nginx 后置环节：缺席/未运行 → `dim` 跳过；重载失败 → 一行 `err` 后继续，**不撤回已编译生效的扩展**。
  - 状态链：可空集合仍序列化为 `[]`/`{}`（不发 `null`）；队列行标签仍只取 `TaskBrief.Label`。

### S5 — 清理面板的降级显示（C11）+ 界面文案 + 派生文档同步

- **干什么**：把 T2 那批行接到「数不到 →『—』+ 一句原因 + 该行重试」这条既有通道上；设置页/doctor 的引擎那一行中英两侧文案；`docs/` 受影响文档按 §13 第 4 步同步（至少 `Docker操作规范.md`、`跨平台差异.md`、`资源清洁机制.md`、`状态同步.md`、`接口契约.md`、`目录规范.md`（`engine` 行数与文件清单若变）、`用户手册.md`、`CHANGELOG.md`、`事件流协议.md`）。
- **判据**：Podman 机器上面板 60 行仍全数列出（**不得**因为够不着就把整行删掉，需求 ⑮ 同口径）、三档行数与危险名单 4 行一字不变、总计不把「数不到」那份算进去；五项门禁全过；`gofmt -l .` 空 · `go vet` · `go build` · `go test ./... -count=1` 全绿；**真宿主 GUI 走查另算**（照本项目一贯口径，编译与门禁不能替代点击级验收，须在装有 Podman 的机器上点一次）。

---

## 附：这份方案没打算做的事（避免被当成范围）

1. 不改任何 Docker 分支的既有行为、命名、缓存槽位、事件名、任务状态、preflight action、错误码。
2. 不写用户的 Docker / Podman 全局配置，不重启任何一个引擎，不改任何 sysctl。
3. 本阶段不支持运行中切引擎。
4. 不引入新的抽象接口层、不做「未来可能多一种引擎」的可配置框架。
5. 不在取证前动 vhost 模板与 `resolver`。
6. 不给 rootless 端口 80 自动提权或改系统参数，只把话说清楚并按既有降级口径走。

---

## 待你拍板的四件事（这四条定了我才动代码）

| # | 问题 | 我的推荐 | 为什么推荐它 |
|---|------|---------|-------------|
| **D0-1** | 两者都装了，用哪个？ | **Docker 优先** | 需求 ①「绝对不破坏原有功能」的最硬形式：今天所有能跑的用户升级后拿到的是同一个引擎，Podman 只服务「机器上根本没 Docker」的新装机 |
| **D0-2** | 要不要让用户自己在设置页点一下换引擎？ | **本阶段不做**（只读显示当前引擎） | 切引擎会把在跑的容器/网络/卷/缓存镜像全换成另一个守护进程管的账，界面会一屏「容器缺失」；真要换，干净路径是「先清空另一边 + 重启应用」 |
| **D0-3** | Linux rootless 之外的平台（macOS / Windows）这次做不做？ | **先只做 Linux** | Podman 在 macOS/Windows 上跑在虚拟机里，端口 80、bind 挂载性能、hosts 提权三处都是另一套差异（§8 那张矩阵已经在说这事）；先把 Linux 做扎实，跨平台另开一轮 |
| **D0-4** | 走不走 AGENTS.md §13 变更提案（硬红线 7 的措辞 + §5.7 补一句适用范围）？ | **走** | 硬红线原文在总纲里被反复声明「明确未改」；不走提案就改红线 7 的字面，等于把这条自己作废。提案范围很小，只两条 |

---

Qoder-AI


# 扩展 Podman 规划实施方案计划【追加】—— engine 层依赖全量摸底 + 逐端点实测

> **文档类型**：规划方案追加（对上文 Qoder-AI 方案的实测验证与修订）
> **写于**：2026-10-05
> **实测环境**：本机 podman 5.7.0（rootless）+ Docker 并存；`podman system service tcp://127.0.0.1:9321` 起 Docker 兼容 API，对 phpo 依赖的**每个 REST 端点逐个打真请求**（含建容器 → exec → archive 拷贝 → commit → 网络别名挂接的完整生命周期）
> **与上文的关系**：上文是推理版方案；本追加把其中「我以为 Podman 支持/不支持」全部换成「实测通过/实测不支持」，并据此修订分期方案。**结论先行：兼容面比预期干净——43 个 SDK 方法里，phpo 的命脉链路全部实测通过；真实适配点只有 3 个，且都找到了已验证的解法。**

## 一、Engine 层依赖全量清单（实测口径）

全项目只有 `internal/engine/` 的 13 个非测试文件 import Docker SDK（docker/docker v28.3.2），全部流量收口在 `engine.Client` 一个封装里；service / cache / task / vhost / preflight / store 各层零泄漏。对 SDK 的依赖分三个维度：

**① 客户端方法 43 个**：容器族 11（Create/Start/Stop/Restart/Remove/List×4/Inspect×6/Logs）、exec 族 3（ExecCreate/ExecAttach/ExecInspect）、archive 族 2（CopyToContainer/CopyFromContainer）、镜像族 7（Pull/List×2/Inspect×3/Save/Load/Tag/Remove）、网络族 4、卷族 4、系统族 3（Ping/ServerVersion/Info/DiskUsage）、清理族 7（BuildCachePrune/PluginList/PluginRemove/ServiceList/ServiceRemove/NodeList/SecretList/SecretRemove/ConfigList/ConfigRemove）、杂项 2（ContainerCommit/Close）。

**② SDK 类型 30 类**：容器装配核心 `container.Config` / `HostConfig{Binds, RestartPolicy: UnlessStopped, PortBindings}` / `network.NetworkingConfig{EndpointsConfig.Aliases}` / `nat.Port*`；列表与状态 `container.Summary/Port/StateRunning/StatePaused/ListOptions`；**流式** `types.HijackedResponse`（exec 劫持 + stdcopy 8 字节去帧）；清理侧 `types.DiskUsageOptions{Types}` 与 swarm.* 四类。

**③ 客户端选项**：`FromEnv` + `WithAPIVersionNegotiation()` + `WithHost`（显式端点优先）。

## 二、实测能力矩阵（✓=实测通过 ✗=不支持 △=有差异需适配）

| phpo 用途 | SDK 方法 | REST 端点 | Podman 5.7 实测 | 注记 |
|---|---|---|---|---|
| 存活探测/版本 | Ping / ServerVersion | `/_ping` `/version` | ✓ 200 | **引擎识别字段实测纠正**：`Platform.Name` 是宿主系统（"linux/amd64/ubuntu-26.04"），不是引擎名；**识别要用 `Components[0].Name == "Podman Engine"`** |
| 容器全生命周期 | Create/Start/Stop/Restart/Remove/Inspect/Logs | `/containers/*` | ✓ 全绿 | create 带自定义 label 201、start 204 |
| **label 过滤**（calibrate/discover/orphan 的 `managedFilter()`） | ContainerList+Filters | `?filters={"label":[...]}` | ✓ 200 | phpo 所有权判据直接可用 |
| 端口/状态三态 | inspect | `/containers/{id}/json` | ✓ | `PublishedPorts`/`StateRunning` 判据不变 |
| **容器内 exec**（扩展编译、nginx -t/reload、逻辑导出） | ExecCreate/ExecAttach(hijack)/ExecInspect | `/exec/*` | ✓ | stdcopy 8 字节去帧协议同源；exec inspect 必须 **GET**（探测脚本用 POST 吃了 405，phpo 本来就用 GET） |
| **宿主⇄容器字节通道**（扩展包离线化） | CopyTo/CopyFromContainer | `/containers/{id}/archive` PUT/GET | ✓ 双向 200 | tar 流原样 |
| **扩展固化** | ContainerCommit | `/commit` | ✓ 201 | php 扩展链路核心 |
| 镜像拉取（JSONMessage 进度流） | ImagePull | `/images/create` | ✓ 200 | 进度格式同源 |
| 镜像管理 | List/Inspect/Tag/Remove | `/images/*` | ✓ | |
| **网络别名**（硬红线 1：`php-{v}-fpm:9000`） | NetworkCreate + connect(Aliases) | `/networks/*` | ✓ **DNS 实测解析成功** | aardvark-dns 应答 `php-8.0-fpm`；容器侧 inspect 回显 Aliases |
| 卷 | VolumeCreate/List/Inspect/Remove | `/volumes/*` | ✓ | rootless 存储在 `~/.local/share/containers` |
| 磁盘用量（清理面板） | DiskUsage | `/system/df` | ✓ 200 | Docker 形状响应；字段口径 P2 核一遍 |
| Swarm secrets | SecretList/SecretRemove | `/secrets` | ✓ 200（空） | **意外**：podman 有 secrets 实现——能力按端点逐个判，不做整族假设 |
| **离线缓存恢复** | ImageLoad | `/images/load` | **✗ 500** | **适配点 ①**，见下 |
| 构建缓存清理 | BuildCachePrune | `/build/prune` | **✗ 404** | **适配点 ②** |
| 插件 | PluginList/Remove | `/plugins` | **✗ 404** "not supported" | **适配点 ③** |
| Swarm 服务/节点/配置 | ServiceList/NodeList/ConfigList | `/services` `/nodes` `/configs` | **✗ 404** | 同上（secrets 是例外） |

## 三、三个真实适配点（均已验证解法）

**① `/images/load` 500——离线缓存的核心断点。** 实测：compat `/images/get`（save）产出 **docker-archive** 布局（根目录 `{id}.tar` + `{id}.json`），而 compat `/images/load` 只收 **OCI** 布局（500 报 "supported image formats: oci"）→ **compat 上的 save→load 往返断裂**，直接命中离线缓存核心链路（save → 提升缓存 → 断网 load 恢复）。
**解法（已实测通过）**：方言位 `SaveLoadViaCLI`——podman 下 save/load 走 CLI（`podman save --format docker-archive -o` / `podman load -i`），往返实测成功（Loaded image）。**附带红利**：缓存 tar 统一 docker-archive 格式后，同一份离线缓存**两引擎通用**（Docker 侧 save 的 tar，podman 直接 load 得进去），用户换引擎时离线缓存零迁移。

**② `/build/prune` 404。** 清理面板「构建缓存」行在 podman 下降级为「—」+ 原因（§5.22「数不出来显示 —、绝不报 0」的既有机制直接承接），或用 `/system/df` 的 build-cache 段只读展示。零新机制。

**③ `/plugins`、`/swarm`、`/nodes`、`/configs` 404（`/secrets` 例外返回 200）。** 清理面板 Plugin/Swarm 相关行按**端点级能力位**逐行降级——这正是方言能力位表（`engine/dialect.go`，原 P2 计划）的设计用途，现在每个端点都有了实测依据。

## 四、检测设计的实测教训（直接写进 §5.7/检测器规范）

1. **判据必须是「真实拨号 + Ping」，绝不是 socket 文件存在性**。实测：podman 走 systemd **socket 激活**——`podman.service` 空闲约 5 秒即退（日志 "Received shutdown.Stop()"），systemd 按需再拉起；本机 podman.socket 显示 active(listening) 但 socket 文件时有时无（还观察到一个实例：unit 显示 listening、`%t/podman/podman.sock` 文件不可见，但 `podman info`/CLI 全通）。看文件存在性会把「装了且能用」误判成「未安装」。
2. **CLI 在而 socket 拨不通** = 「已安装未就绪」，修复建议给一条可复制的 `systemctl --user start podman.socket`（是否代用户拉起见问题 ⑦，推荐不代拉）。
3. 引擎识别用 `Components[0].Name`（"Podman Engine" vs Docker 的 "Engine"），**不要用 `Platform.Name`**。

## 五、据此修订后的分期方案（对上文 E 节的修订）

- **P0（零代码验证）已完成约 80%**——本追加的第二、三节就是产物。剩余两项：把 **13 个 `test/integration/*_live_test.go` 对着 podman TCP 服务跑一遍**（验证 Go SDK 客户端全链路而非裸 curl）；rootless 下 nginx 80 端口与 `enable-linger` 的实机确认。
- **P1（引擎检测与选择，1–2 天）**：`client.go` 的 `candidateSockets` 增加 podman 候选（rootless/rootful/machine 三族）；`pickEndpoint` 返回 `(端点, 引擎类型)`；识别用 `Components[0].Name`；`model.Snapshot` 增加 `Engine` 字段（additive）；前端 `DockerGate → EngineGate`（都没有 → 安装引导页，Docker/Podman 双列带链接与发行版命令）+ 引擎徽章 + doctor 引擎/linger 项；preflight 三态错误文案按引擎方言化。
- **P2（方言收口，范围收敛为三件）**：`engine/dialect.go` 能力位表 + ① `SaveLoadViaCLI`（已验证）+ ② `/build/prune` 与 plugins/swarm 行降级（§5.22 既有「—」机制承接）+ ③ rootless 项（端口 80/linger）。**其余 engine 层代码零改动双引擎共用。**
- **P3（设置页引擎切换，可选）**：切换 = 换一个容器世界（两引擎容器互不可见），建议 v1 不做、只读展示当前引擎。

## 六、待裁决问题（7 个；与上文 D0-1～D0-4 的关系附注）

| # | 问题 | 我的推荐 | 与上文的关系 |
|---|------|---------|-------------|
| ① | 双引擎并存谁优先？ | **Docker 优先**（存量零感知） | 与 D0-1 一致 |
| ② | 设置页 v1 要不要引擎切换？ | **不做**，只读展示 | 与 D0-2 一致 |
| ③ | rootless 绑不了 80（默认站点端口冲突）？ | 检测到 rootless 时建站端口建议自动改 8080 并警告 | 上文未覆盖，新增 |
| ④ | 首版平台范围？ | **Linux + macOS 全量，Windows podman 实验性** | 与 D0-3 有分歧：上文建议先只做 Linux——留给用户裁决 |
| ⑤ | 验收线？ | **13 个 live 测试 × 双引擎全绿** + 装机/建站/扩展三条手测链路双引擎通过 | 上文未给出硬验收线，新增 |
| ⑥ | 检测时机？ | 启动探测一次定引擎、运行中不热切；中途新装引擎提示「重启应用生效」 | 上文未覆盖，新增 |
| ⑦ | phpo 要不要代用户拉起引擎服务？ | **不代拉**（与「不写 daemon.json」同纪律），只给一键可复制修复命令 | 上文未覆盖，新增 |

另有一条与 D0-4 相关的补充：总纲修订范围除硬红线 7 措辞外，实测还要求 **§2 技术栈表加 Podman 行、§0.2 规则 3 加「引擎方言」边界说明**，一并入 v2.9.16 提案。

**一句话总结：引擎层 90% 代码双引擎免费共用；三个真实断点全部有已验证解法；最大的坑（compat load 500）反而换来「离线缓存格式两引擎通用」的额外红利。上文 4 问 + 本追加 7 问合并去重后共 9 个裁决点，拍板即可开 P1。**

—— ZCode-AI


## 【追加】—— 对这两份方案的分析总结（Qoder-AI）

**先说这一段是干什么的**：现在这份文件里是**两份方案叠在一起**——前半（第 1–232 行）是我写的结构摸底与集成形状，后半（第 235–312 行）是另一位 AI 写的逐端点实测与修订排期。这一段不重复任何一份的内容，只做一件事：**把两份对照读完之后，告诉你哪些结论被实测改写了、哪些东西两份都同意绝不许动、以及现在到底剩哪几件事要你拍板。**

### 一、两份各自定了什么（一句话）

| 份 | 定的事 | 性质 |
|----|--------|------|
| 我的前半 | 加 Podman **只能加在 `internal/engine/` 那 13 个文件里**，其余四层（service / task / vhost / preflight）一行都不许分支；引擎在装配层只有一个构造点，且引擎不是路径，所以改根换图那套逻辑不受影响 | 结构判定，从代码里读出来的，**这份结论没有被后半推翻，也不该被推翻** |
| 后半 | 把引擎客户端真正调用的每一个 Docker 端点逐个问到一遍，给出「哪些白送、哪些要改、哪些根本没解」的实测清单，并把排期收敛到三个改造点 | 实测取证，**把我原计划里三处凭推断写的东西改掉了**（见下） |

两份合起来的净结论是同一句话：**能共用，但共用的前提是那三个实测出来的断点各自有已验证的解法。**

### 二、实测改写了我的三处判断（这三处我以对方证据为准，不辩护）

**① 站点上游解析（DNS）——我把它排成最难的一关，实测证明它根本不是关。**
我原来担心的是：phpo 写进 nginx 的配置是靠 Docker 自带的 DNS 按容器名找人（`php-{版本}-fpm:9000`），换成 Podman 后这套名字能不能解析没人知道，所以我在 S3 里备了三条候选方案准备挨个试。后半实测报告：Podman 的网络别名（Aliases）生效、那个名字解析得通。**这意味着我准备的三条候选里，第一条（每个引擎各用各的解析器）就是终态**，另外两条可以划掉。
*这条为什么重要*：它是全案唯一一处「改错了会让界面说切换成功、实际还在连旧上游」的地方（我原本因此把「直接用容器 IP」那条倾向否决——重建容器 IP 会变，那等于给用户一句假的成功）。现在这个风险面收窄成一句：**S3 的验收从「三选一」变成「按第一条落地并实测一次切换」。**

**② 离线缓存的存/取——我写成「有等价端点」，实测是 500，性质完全不同。**
我原来在风险表里给这一格的说法是「Podman 提供等价端点，工作量在参数对齐」。实测打脸：compat 接口存镜像时吐的是 docker-archive 那一份「{id}.tar + {id}.json」的两件套，而 compat 接口读镜像时**只认 OCI 格式**，于是存得出来、读回去 500。**总纲决策 22 那条「未命中先探本机镜像库、已有即零网络重建缓存」的主路在 Podman 上是断的**——不是工作量问题，是这一条能力当前不存在。
*怎么回来*：后半给的已验证解法是把存和取都改走 Podman 自己的命令行，并显式指定 docker-archive 格式。附带红利值得单独说一句：**两份 tar 都是同一种格式，等于一份离线缓存两个引擎都能用**，将来换引擎不需要迁缓存。这一句碰到了 §5.14.2 的缓存槽位口径，所以它属于要走 §13 提案那一类改动，不是纯代码活。

**③ 检测引擎在不在——我的判据本身有缺陷。**
我把 §5.7 的三条口径直接映射给 Podman，其中「这个套接字在不在盘上」用的是 `client.go:71` 那个 `socketExists`——它查的是文件系统里那个 socket 文件的存在性。**在 Podman 上这个信号会骗人**：Podman 走 systemd 的按需拉起，服务闲置几秒就退出，单位状态可能还是 active 而那个文件已经没了（命令行照样能用）。按我这个判据，就会把「装了、只是此刻没起」误判成「没装」，然后给用户一句错误的安装提示。
*正确判据*：**真的连一次 + 问一次 `/_ping`**，永远不要看文件在不在。连带一条我的原案没写的关键事实：**引擎身份要从版本响应里的组件名读（`Components[0].Name`），不能读平台那一项**——那一项装的是宿主机操作系统，两台机器读出来一样。
*还有一条界面上的补*：命令行在、但连不上，那是第三种状态「已安装未就绪」，界面要把这一句说清楚并给出那条可复制的启动命令，而不是继续说「请先安装」。

**顺带一处两份都同意的收紧**：能力是**按端点逐个判**的，不是按家族整片判——那个「按端点算缺」的清单里，`/secrets` 回的是 200。所以「Podman 一定没有 Swarm 那一套」这种整片假设不能写进代码，否则 §5.22 那 60 行的清点会自己造出「这里没有」的假数字。

### 三、两份都同意、且明确不许动的东西（这节是给下一轮实现划的硬线）

这是我最想强调的一点：**两份方案在「改哪些」上有分歧，在「不许改哪些」上完全一致，而后者比前者更能保护这个项目。**

- **不改已有的 Docker 行为**：现有 13 个引擎文件的方法签名一律不动，只在内部分叉端点与方言；不做「运行中一键切引擎」（那是同时把两套状态机挂在一个在跑的任务队列上，风险远超收益）。
- **不加抽象层**：不新立一个 `Engine` 接口让 service 层去实现两份。**这次要的是少改，不是可扩展。**
- **冻结承载一个都不许多出来**：事件名仍 **17** 个、后端任务状态仍 **4** 个、preflight 仍 **20** action 与 NEEDS_HOME 仍 **18**、`pkg/errs` 仍 **28** 码、CI 门禁仍 **5** 项、`app.go` 仍 **80** 个导出方法 = **78** 个生成绑定 + `ServiceStartup`/`ServiceShutdown` 两颗钩子。引擎身份如果界面要说，就**只往快照里追加一个字段**（这是后半唯一新增的承载，性质是只增不改），不新增事件名。
- **不碰用户的引擎环境**：这条我把 §5.21 的口径原样延伸给 Podman——**绝不写宿主 Docker 的 `daemon.json`／`registry-mirrors`、不重启用户的 Docker**；同样**绝不写用户的 Podman 配置、不碰 `/etc/containers/` 与 `storage.conf`、不动那个 socket、不设任何 sysctl**。§5.22 面板那 25/19/16 三档与「数不到不给 0」的既有通道正好复用，**不需要为此改总纲**。
- **离线缓存三条铁律与临时目录四必清方向不变**（只允许换存取的实现手段）。

### 四、两处必须点名的冲突（我不擅自替你选）

**冲突 1：这一期做到哪些平台。** 我的 D0-3 是**先只做 Linux**；后半的 ④ 是 **Linux + macOS 全量、Windows 只做实验**。这是两份方案唯一的直接分歧，而且它决定后面所有工作量：macOS 上 Podman 跑在自己的虚拟机里，挂载性能和端口发布都和 Linux 不是一回事，把 macOS 纳进来等于把两个平台的差异矩阵各写一遍。**我的建议仍是先只做 Linux**（画像 A/B 的主力机器是 Windows + Docker Desktop，Linux 是内网/CI 画像 F 的地盘，而 Podman 真正的用户就在这一格；先在能实测的平台上收口，再谈第二个平台）。但这条归你定。

**冲突 2：rootless 下端口 80 绑不了，怎么办。** 后半的 ③ 倾向「建议自动改用 8080」。这和总纲 §1.9／§5.8 已冻结的口径直接冲突——那条写的是**新建站点端口被占用只弹框告警 + 把站点降级，不顺延、不改用户所填端口**。「80 绑不了」在技术上就是一次占用，按冻结口径应当告警降级而不是替用户改端口。而且修法上还没有授权：任何「自动改端口」或「建议你去设 `net.ipv4.ip_unprivileged_port_start`」都等于替用户改他机器的网络策略。**这一条我不能按后半的说法写进实现**；能做的只有：把「这个端口在 rootless 下绑不了」说清楚，并把恢复路径交给用户（换端口 / 换 rootful / 自己调系统参数），一次都不自动动。

### 五、现在剩哪几件事要你拍板（合并去重后 9 条）

| # | 要定的事 | 两份方案的态度 |
|---|---------|--------------|
| 1 | 两个引擎都装了，用哪个 | 一致：Docker 优先 |
| 2 | 本期是否支持运行中切引擎 | 一致：不支持，只在启动时定 |
| 3 | **做到哪些平台** | **分歧**：我=仅 Linux ／ 后半=Linux+macOS 全量、Windows 实验 |
| 4 | 总纲要走 §13 的哪几项 | 一致，但**范围要放大**：硬红线 7 原文只写 Docker 必须改；另外后半新增要动 §2 技术栈表加 Podman 一行、§0.2 规则 3 补一句「方言」、缓存槽位那处随 docker-archive 统一而动，并建议把总纲推进到 **v2.9.16** |
| 5 | **rootless 端口 80 怎么办** | **分歧**：后半建议自动改 8080 ／ 我按冻结口径只能告警降级、不改用户所填端口 |
| 6 | 验收线画在哪 | 一致方向：现有 **13** 个 live 用例在两个引擎上都要跑绿 + 三条手工链路（建站访问 / 装扩展 / 离线恢复） |
| 7 | 引擎检测的时机 | 后半补的口径：启动时定一次，界面上说明「改这个要重启应用生效」 |
| 8 | 要不要自动把引擎服务拉起来 | 一致：**绝不自动启动用户的引擎服务**，只给一句能复制的命令 |
| 9 | **后半那些实测，要不要我这边独立复核一遍再开工** | 待你定。最低复核方式：本机起一个 Podman 的 TCP 服务（`podman system service tcp://127.0.0.1:9321`），把现有 13 个 live 用例对着它跑一遍——那条「存得出来读回去 500」和那条「别名解析得通」都是**一次调用就能验真/验假**的断言 |

### 六、如实标注（这一段别当成已核实的事实读）

后半那些逐端点实测数字（哪些 200、哪些 404、哪些 500，以及「别名解析得通」）**出自另一位 AI，我没有独立复跑过**。我在上面第二节里按「以对方证据为准」改写了自己的三处判断，性质是**采纳一份我没验过的证据**，不是我自己验出来了。这个项目里还有其他 AI 在并行开发，所以我不会把「读到了别人的实测报告」写成「已验证」——第 9 条那个复核动作就是为这件事留的，做完它，上面第二节的三处改写才从「采纳」升级为「事实」。

另外两处仍然欠着的账（与 Podman 无关，但既然在写总结就一并说清）：一是**真宿主 GUI 从未走查**，本轮纯文档；二是此前那次全站 502 的其余成因里，php-fpm 那两行仍往宿主挂载目录写日志的配置（`php-fpm.conf.tmpl` 里的 access.log / slowlog）还没收到 §5.18.4 的口径下，那是另一轮的活，本轮没动。

**一句话总结这一段**：形状上两份是同一个答案（只在引擎层加，别处一行不分支）；差别只在后半把三处凭推断的判断换成了实测，其中**两处改掉了我的原案**（DNS 不是难关、检测不能看 socket 文件在不在），**一处把我说轻的严重性改重了**（离线缓存的存取在 Podman 上是断的，不是工作量问题）。真正的待定项收敛为 **9 条，其中 2 条是两份方案的分歧**（平台范围、rootless 端口 80），这两条我不会替你选。

Qoder-AI


# 扩展 Podman 规划实施方案计划【追加·分析总结】—— 两版方案对照与合并结论

> **文档类型**：分析总结追加（对全文两版方案——上文 Qoder-AI 推理版与本文件第二次追加 ZCode-AI 实测版——的对照分析与合并结论）
> **写于**：2026-10-05
> **分析口径**：通读全文 313 行，逐项对照两版方案的同异，产出共识清单、实测裁决表、独有价值吸收清单、一个两版都没点透的技术结论，以及合并后的统一裁决清单与实施路径。

## 一、文件概况

| | 上半：Qoder-AI 版（推理） | 下半：ZCode-AI 版【追加】（实测） |
|---|---|---|
| 方法 | 边界证明 + 风险三档表（T0/T1/T2）+ S1-S5 五阶段 | 起 podman TCP 兼容服务，对每个 REST 端点打真请求（含容器全生命周期） |
| 核心产出 | 13 文件边界证明、12 项风险分档、总纲「不用改」九项清单、4 问 | 43 方法/30 类型清单、17 行能力矩阵、3 个适配点（已验证解法）、7 问 |

## 二、两版高度共识的部分（可直接当定论）

13 文件收口 + `di.go` 唯一构造点；**最小爆炸半径 = 保留 `Client` 类型与 13 文件全部签名，只在内部养「端点 + 方言」两个分岔**；三个「不做」（不加 service 层分支 / 不加抽象接口层 / 不做运行中切引擎）；Docker 优先仲裁；检测三态照 §5.7 三条冻结口径；清理面板 T2 行走「—」既有机制；不写用户全局配置；总纲走 §13 提案。

## 三、实测版对推理版的验证结果

| Qoder 的担忧（C 段编号） | 实测裁决 |
|---|---|
| C2 网络别名「必须现测」 | **✓ 解除**——alias 挂接 + DNS 解析实测成功（aardvark-dns 应答 `php-8.0-fpm`） |
| C8 commit 可用性 | **✓ 大半解除**——commit 201 实测（「产物能否直接被下一次 create 使用」待 live 测试补） |
| C11 面板降级范围 | **✓ 精确到端点级**——`/build/prune` 404、plugins/swarm 404、**secrets 竟然 200**（「按端点判，不做整族假设」被证实） |
| C7 save/load 引用名 | **✗ 实测发现更深的问题**：compat save 产 docker-archive、load 只收 OCI → **往返断裂**；解法已验证：方言位 `SaveLoadViaCLI`，附带红利是**缓存 tar 格式两引擎通用** |
| 检测设计 | **实测纠正两条**：引擎识别用 `Components[0].Name` 而非 `Platform.Name`；判据必须拨号 Ping 而非 socket 文件存在性（socket 激活服务空闲 5 秒即退，文件时有时无） |

## 四、Qoder 版的独有价值（实测版没覆盖、必须吸收进实现）

1. **C10：not-found 双写法收口**——`cerrdefs.IsNotFound` 与字符串比对 `isNotFound` 并存；Podman 错误文本不同会让「删不存在的容器 = 成功」的幂等**静默变假**，Pre-Clean 会误判死。实测版漏掉的真隐患。
2. **A.2：引擎绝不进 `rootsKey`**——否则用户改缓存根会连引擎一起换掉（§5.15 的意外耦合）。
3. **C4：SELinux 挂载标签 `:z`/`:Z`**——Fedora/RHEL 上 bind 挂载不加标签就是「看得到读不了」，症状与 §5.18.1 崩溃循环酷似。本机 Ubuntu 未测到，但画像用户里有 Fedora。
4. **B.2：`CONTAINER_HOST` 环境变量显式优先**的候选顺序。
5. **S5 的文档同步清单**（九篇 docs 逐一点名）与 D 段「不用改总纲」九项清单（精确圈定提案范围）。

## 五、合并分析后浮现的关键技术结论（两版都没点透）

Qoder 的 C1（vhost 模板写死 `resolver 127.0.0.11`——那是 **Docker 专用**内嵌 DNS；podman 的 aardvark-dns 挂在**网桥网关**上）——**实测版并没有解除它**：实测验证的是「别名注册成功 + DNS 可解析」（走容器内 resolv.conf → 网关），但 nginx 里写死的 `127.0.0.11` 在 podman 容器内**没有服务监听**，`fastcgi_pass $变量` 依然会解析失败。

**合并结论：S3 的候选 (a)「resolver 地址按引擎动态化」是主路径**——Docker 分支保持 `127.0.0.11` 一个字节不动；podman 分支写入网关地址（`network inspect` 的 Gateway 字段，实测中即 `10.89.0.1`；`phpo-network` 由 phpo 自建、网关可预估）。候选 (c) IP 直连维持否决。这是两版方案互补得最关键的一处：Qoder 提出了正确的问题，实测版解决了其中一半（别名），剩下这一半（resolver 地址）由合并分析补齐。

## 六、合并后的裁决清单（9 个去重裁决点）

一致可默认三项：**① Docker 优先；② v1 不做引擎切换；③ 走总纲提案（v2.9.16）**。需要用户表态的六项：

| # | 问题 | 两版立场 |
|---|------|---------|
| ④ | 平台范围 | Qoder：先只做 Linux ／ ZCode：Linux+macOS，Windows 实验性 |
| ⑤ | rootless 绑不了 80 | ZCode：检测到 rootless 时建议端口自动改 8080 并警告（Qoder 未覆盖） |
| ⑥ | 验收硬线 | ZCode：13 live 测试 × 双引擎全绿 + 装机/建站/扩展三条手测链路 |
| ⑦ | 检测时机 | 启动一次定引擎，中途装引擎提示重启生效 |
| ⑧ | 代不代用户拉起引擎服务 | 两版一致倾向：不代拉，只给修复命令 |
| ⑨ | 总纲提案范围 | 硬红线 7 措辞 + §5.7 适用范围 + §2 技术栈表 + 规则 3 边界 |

## 七、合并后的实施路径（以 Qoder S1-S5 为骨架，填入实测成果）

| 阶段 | 内容 | 状态 |
|------|------|------|
| S1 检测地基 | 端点候选（含 `CONTAINER_HOST`）+ 三态 + 仲裁 + 快照 Engine 字段 | 待开工（识别字段与判据已由实测定型） |
| S2 只读取证 | C 段全部「必须现测」项 | **已完成约 80%**（能力矩阵即产物）；剩五项：exec 流帧内容、archive uid 属主、commit→create 直用、发现态停着容器、SELinux |
| S3 resolver 动态化 | 候选 (a)：Docker=`127.0.0.11` 不动，Podman=网关地址 | 前提（别名 DNS）已实测 ✓，待开工 |
| S4 执行链逐块 | C3-C10 + **C10 收口** + `SaveLoadViaCLI` | 待开工 |
| S5 面板降级 + 文案 + 九篇文档同步 | T2 行走「—」既有机制 | 待开工 |

**收尾一句：两版方案合并后互补闭环——Qoder 版贡献了边界证明、风险框架与 S1-S5 骨架，实测版贡献了端点级事实与三个已验证解法，合并分析补上了 resolver 地址这个共同盲区。9 个裁决点拍板即可按 S1 开工。**

—— ZCode-AI


# 扩展 Podman 规划实施方案计划【追加·分析总结】—— 需求覆盖核对与关键论断复验（第二轮）

> **文档类型**：分析总结追加（第二轮 · 事实核对版）
> **写于**：2026-10-05
> **核对口径**：把用户三条原始需求逐条对着全文三段方案查覆盖度；再把三段方案里的关键论断拿到**当前代码库**复验一遍（grep 到行），不一致处如实登记。

## 一、需求覆盖核对矩阵（三条原话 × 三段方案）

| 用户需求 | Qoder 推理版 | ZCode 实测版 | ZCode 总结 | 核对结论 |
|---------|-------------|-------------|-----------|---------|
| ① 绝不破坏原有功能 | A 段边界证明（13 文件收口）+ B.4 六条不做 | 17 行矩阵、Docker 路径零改动的实测基线 | 共识清单八项 | **覆盖充分**。补一条硬形式：回归基线 = 既有 110 个 `_test.go` 逐字全绿，每阶段必跑 |
| ② 探测定引擎，都没有给引导 | B 段三态 + Docker 优先仲裁 | 检测教训两条（拨号 Ping 判据 / Components[0].Name 识别） | ⑦⑧ 两项裁决 | **覆盖充分，但锚点引用需修正**（见核对 3） |
| ③ 方案怎么与当前项目拼在一起 | C 段 12 项风险三档 + E 段 S1-S5 | 矩阵 + 三个适配点已验证解法 | S1-S5 骨架 + 实测填充 | **互补闭环**。resolver 缺口由第一轮总结补齐（候选 a 动态化） |

## 二、关键论断的事实核对（对当前代码库，grep 到行）

| # | 论断 | 核对结果 |
|---|------|---------|
| 1 | C10「not-found 双写法并存」 | **属实**：`inspect.go` 三处用 `cerrdefs.IsNotFound`（containerd errdefs——按 HTTP 404 状态码映射，**引擎无关**），`image.go:103` 用字符串比对 `isNotFound`（"not found"/"no such image"/"no such"）。**风险集中在字符串那一半**：podman compat 的 404 错误文本若不同，镜像侧幂等静默变假。收口方案顺势定型：两处统一走 cerrdefs（状态码驱动天然跨引擎），并补一条「同一不存在的名字，两引擎均幂等成功」的用例 |
| 2 | 硬红线 7 位置与原文 | `AGENTS.md:640`（§3.3 表）：「Docker 未装则不能启动服务」。提案措辞改为「**容器引擎（Docker 或 Podman）未就绪则不能启动服务**」 |
| 3 | 「§5.7 三条冻结口径」的锚点 | **引用错位，核对发现**：§5.7 实际是「doctor 环境诊断」节（检查项表）；探测三态的**权威实现在 `engine/health.go`**（`ErrDockerNotInstalled`/`NoPermission`/`NotRunning` 三哨兵 + 消息分岔）。提案落点随之修正：`health.go` 建议文案按引擎方言化 + §5.7 doctor 表补「引擎」行 + 适用范围句加在表头 |
| 4 | vhost 模板写死 `resolver 127.0.0.11` | 属实（`vhost.conf.tmpl:8`）；podman 容器内该地址无服务监听（aardvark-dns 在网关）→ S3 候选 (a)「resolver 地址按引擎动态化」为主路径 |
| 5 | not_running 建议文案现状 | `health.go:115/130` 已硬编码 Docker 的 `sudo systemctl start docker` 建议——podman 分支**照抄形状**（`systemctl --user start podman.socket`），正是既有口径「消息自带原因、建议按平台分岔」的落点 |
| 6 | 13 文件收口 / rootsKey 不含引擎 / di 唯一构造 | 复验属实（本轮及此前 grep 均一致） |

## 三、三段之间的分歧与裁决建议

- **平台范围**（D0-3「先只做 Linux」vs ④「Linux+macOS，Windows 实验性」）：建议折中——**Linux 首发做扎实，macOS 紧随（podman machine 机制相近），Windows 列实验性**。
- **验收线**：采纳「13 live 测试 × 双引擎全绿 + 装机/建站/扩展三条手测链路」，并加一条：**每阶段 Docker 侧既有断言重跑全绿作为回归基线**（与需求 ① 同构）。
- 其余（Docker 优先 / 不做切换 / 不代拉 / 检测一次定引擎 / 提案范围）两版一致，按推荐默认。

## 四、收敛后的最终实施蓝图（定稿草案）

| 阶段 | 内容 | 新增核对修正 |
|------|------|-------------|
| S1 检测地基 | 端点候选（含 `CONTAINER_HOST`）+ 三态 + Docker 优先仲裁 + 快照 Engine 字段 + EngineGate | **锚点修正**：三态权威在 health.go，doctor 表补引擎行 |
| S2 只读取证 | C 段全部「必须现测」项 | **已完成约 80%**（能力矩阵）；剩 exec 流帧、archive uid 属主、commit→create 直用、停着容器发现、SELinux 五项 |
| S3 resolver 动态化 | Docker=`127.0.0.11` 不动；Podman=网关地址（自建网络可预估） | 前提（别名 DNS）已实测 |
| S4 执行链逐块 | C3-C10 逐块 + **not-found 收口到 cerrdefs** + `SaveLoadViaCLI` | **C10 收口方案定型**（本轮核对产出） |
| S5 面板降级 + 文案 + 九篇文档同步 | T2 行走「—」既有机制 | 不变 |

## 五、给用户的最后清单

- **需要表态的 6 项**：平台范围（建议折中案）/ rootless 80（建议 8080+警告）/ 验收硬线（建议 live×双引擎）/ 检测时机（建议一次定引擎）/ 代拉（建议不代拉）/ 提案范围（建议四件一并入 v2.9.16）。
- **全按推荐的话**：S1 可立即开工；文件中三段方案 + 本核对即为定稿依据。
- **流程提醒**：v2.9.16 提案先走、代码后动（硬红线 7 的字面在总纲里被多次声明「明确未改」，先改字面再写码才算闭环）。

—— Qoder-AI


# 扩展 Podman 规划实施方案计划【追加·规划落地】—— 结合 phpo 当前项目状态的落地规划

> **文档类型**：规划落地追加（第三轮 · ZCode-AI）
> **写于**：2026-10-05
> **锚定基线**：phpo 总纲 v2.9.15（含本会话新增的 §5.23 单实例/换血、§5.24 配置生效时序、§12.14 自查）+ 应用 v0.1.75（含 502 根治、配置保存即生效、切 PHP 备好上游）
> **与前四段的关系**：Qoder 版给了框架、实测版给了端点事实、两轮总结给了合并结论——本追加把这一切**锚到当前代码库**，产出「现状 → 目标」的精确改动清单，并核出前三段都没发现的一个新方言化落点（doctor 检查 ID 的命名）。

## 一、当前项目里已经为 Podman 准备好的地基（本会话新增资产的复用清单）

| 既有资产（均为本会话落地） | 如何服务 Podman 适配 |
|---------------------------|---------------------|
| **§5.24 配置生效时序**（三句原则 + 生效步失败不判死保存） | resolver 动态化就是「谁的脸变了动谁」的延伸——只动模板里 resolver 一个值的来源，vhost 其余形状不动；生效步的失败纪律同样约束 podman 侧 |
| **§5.23 单实例 + 换血闭环** | 「检测一次定引擎、运行中不热切」的裁决有了现成载体：引擎结论进快照，换血按版本比较、与引擎种类正交；中途装引擎「重启应用生效」恰好走换血同一条路 |
| **heal 先例**（`healPhpAllowedClients`，502 根治） | 证明「就地修复旧装机配置」模式成熟且带测试模板——podman 侧若发现同类 conf 差异可照抄骨架 |
| **doctor 15 项框架**（`doctor_service.go`） | 引擎项 / linger 项 / SELinux 项的现成落点（检查、建议、一键修复三件套齐备） |
| **DockerGate 提醒弹窗 + 常驻横幅**（可关闭、跃迁才重弹、后端硬红线权威拦截） | EngineGate 改造的骨架——新增「两个引擎都没装」状态时沿用同一交互纪律（可关闭、不重复打扰、后端裁决为准） |
| **live 测试双重 skip 守护**（`PHPO_LIVE=1` + 引擎可用，头注释自带运行说明） | P0 剩余取证（Go SDK 全链路对 podman）的现成载体，加一个引擎感知的 skip 即可 |
| **校准双档 + 发现态 + 缺失态**（§5.19/§5.19.3a） | 「切引擎 = 另一个容器世界」的快照语义已定义——即便未来 P3 做切换，缺失态/发现态机制直接承接 |
| **preflight 备好上游**（切 PHP 自动启动目标容器，§5.24） | 走 lifecycle.Start → engine，compat 层无需改动（实测容器启停全绿） |

## 二、当前代码里会被 Podman 撞到的点（现状 → 改法，精确到文件行）

| 落点（文件:行/符号） | 现状 | Podman 下的差异 | 改法 | 阶段 |
|---------------------|------|----------------|------|------|
| `internal/template/templates/vhost.conf.tmpl:8` | `resolver 127.0.0.11`（Docker 专用内嵌 DNS） | podman 下该地址无监听（aardvark 在网关） | 模板变量化 `{{ .Resolver }}`：Docker=`127.0.0.11` 不动，Podman=网关地址（自建网络可预估） | S3 |
| `internal/engine/client.go:73-93` `candidateSockets` | 候选全是 docker socket | 无 podman 候选 | 加 rootless/rootful/machine 三族；`pickEndpoint` 返回 (端点, 引擎) | P1 |
| `internal/engine/health.go:115/130` | not_running 建议写死 `sudo systemctl start docker` | podman 用户拿到的是无效建议 | 按引擎分岔：`systemctl --user start podman.socket` | P1 |
| `internal/engine/health.go` 三哨兵 | 识别靠 Docker 语义 | 需区分引擎 | `Components[0].Name` 判别 + `Client.Engine()` | P1 |
| `internal/engine/image.go:103` `isNotFound` | 字符串比对错误文本 | podman 404 文本可能不同 → 幂等静默变假 | 与 `cerrdefs.IsNotFound` 收口为一个判据 + 双引擎幂等用例 | S4 |
| `internal/engine/image.go` Save/Load | 走 compat `/images/get`+`/load` | compat load 只收 OCI、save 产 docker-archive → 往返断裂 | 方言位 `SaveLoadViaCLI`（已实测验证）；缓存 tar 格式两引擎通用 | S4 |
| `internal/service/doctor_service.go:146/156/173` | 检查 ID 写死 `docker_installed`/`docker_running`/`docker_version` | **前三段都没发现的落点**：ID 是前后端契约的一部分，podman 下语义要覆盖双引擎 | 两个选项：(a) ID 不动、文案按引擎方言化（推荐，零契约变动）；(b) 新增 podman_* 平行项（ID 增多需查前端消费点） | P1 |
| `internal/engine/clean_inventory.go:386-402` | swarm 四族 + `clean_delete.go` PluginRemove 等 | `/services` 等端点 404 | 方言能力位表 + 「—」+ 原因（§5.22 既有机制）；secrets 200 保留 | S5 |
| `internal/engine/clean_delete.go` BuildCachePrune | `/build/prune` | 404 | 面板构建缓存行降级（或 system/df 只读） | S5 |
| `internal/engine/container.go` `RestartPolicyUnlessStopped` | docker 语义 | rootless podman 需 enable-linger 否则登出全死 | doctor 新增 linger 检查项 | P2 |
| `internal/engine/mount.go` | Binds 无 SELinux 标签 | Fedora/RHEL 上「看得到读不了」 | podman 且 SELinux 开启时加 `:z/:Z` | S4 |
| `internal/model/snapshot.go` + `store/snapshot.go` | 无引擎字段 | 前端与 doctor 需要引擎结论 | `Engine` 字段（additive，normalize 兜底） | P1 |
| `internal/config/` 默认站点端口 80 | §0.3 产品语义 | rootless 绑不了 <1024 | 待裁决⑤（建议：检测到 rootless 时建站建议端口 8080 + 警告，不改默认值语义） | P2 |
| `frontend/src/components/business/DockerGate.vue` | 只有「Docker 不可用」提醒 | 「两引擎都没装」需要**安装引导**而非提醒 | EngineGate：双列引导（官方链接 + 发行版命令），交互纪律照抄（可关闭/跃迁重弹/后端权威） | P1 |
| `pkg/errs` 28 码 | 无引擎相关码 | — | **零新增**（三态哨兵已是 error，前端文案走 i18n） | — |

## 三、里程碑与验收（收敛定稿版）

| 里程碑 | 内容 | 验收 |
|--------|------|------|
| **M-P1 检测与引导** | 端点候选 + 引擎识别（`Components[0].Name`）+ 仲裁（Docker 优先）+ 快照 Engine + EngineGate + doctor 方言化 | S1 判据六条全过（含「socket 在没起 → not_running 不塌缩」）；两引擎都没装的机器看到引导页 |
| **M-P2 方言收口** | dialect 能力位 + SaveLoadViaCLI + not-found 收口 + 面板降级 + SELinux + linger + rootless 80（按裁决⑤） | **13 live 测试 × 双引擎全绿**；面板 60 行不缺行、三档与危险 4 行不变 |
| **M-P3 resolver 与全链路** | vhost resolver 动态化 + 四条链路双引擎实测 | 装机/建站/切 PHP（含切到停着版本）/应用扩展 双引擎通过；**Docker 侧既有断言逐字绿** |

每阶段收尾跑全量门禁（5 项）+ `go test ./... -count=1`；真宿主点击级走查照例单独记账。

## 四、三条需求的最终对账

1. **绝不破坏**：回归基线机制（每阶段 Docker 侧断言逐字绿）+ 最小爆炸半径（引擎分岔全部收在 engine 层内部，上层零改动）——机制上可证明，非承诺。
2. **探测定引擎**：P1 交付检测/仲裁/快照/引导页全链路；判据按实测教训定死「拨号 Ping」。
3. **合理拼在一起**：方言能力位表 + §5.24 时序原则延伸 + 既有机制（「—」降级、heal、doctor、DockerGate）最大复用——新增代码集中在 engine 层两个文件与前端一个组件。

## 五、执行顺序（拍板后）

1. 6 个裁决点表态（平台范围 / rootless 80 / 验收线 / 检测时机 / 代拉 / 提案范围）→ 2. **v2.9.16 总纲提案先行**（硬红线 7 措辞 + §2 技术栈表 + §5.7 锚点修正 + 规则 3 边界）→ 3. P0 收尾（13 live 对 podman TCP 跑 + rootless 80/linger 实测）→ 4. M-P1 → M-P2 → M-P3，每阶段独立可验收、可回滚。

—— ZCode-AI


【追加】第四轮 · Qoder-AI：只写前三轮没落到地上的那部分

**这是干什么的**：这一节不重复对方 P1/P2/P3 的阶段划分，也不重复我第一到第三轮已经写过的四条 Podman 坑。它只做一件事——把「四条坑」翻回本仓库里对应的那几条冻结条款，看清每一颗坑真踩下去会砸到哪一块已经建好的能力，然后列出**对方计划里没有、只能由本轮补上**的三件事，以及**必须由用户拍板、我不能替他选**的两个冲突点。

---

一、对方 P2 已经接住的部分（本轮不再展开）

我第一轮列的四条坑里，**① socket activation 让「看 socket 文件在不在」失效**、**③ Podman 兼容层的 image save/load 往返 500**、**④ 容器/卷的「找不到」两种引擎说法不同**，已经被对方 P2 的三件套接住了：`dialect` 能力位表 + `SaveLoadViaCLI` + not-found 收口。判据也定对了——探测引擎「拨号 Ping」而不是 stat 文件。这三条我认可，不再重述，避免同一件事在文档里写四遍。

第二条坑（**引擎族身份只认 `Info().Components[0].Name`，能力按端点不按族**）对方 P1 用「探测定引擎」承载，方向一致；唯一要补的是下面第五节那一句：能力位表**不能按「Docker / Podman」两个名字各存一份**，必须按「这一个端点答了什么」存。

---

二、本轮净新增之一：坑 ③ 真正的爆炸半径在 §5.14 离线缓存，不在 engine 层

**这是干什么的**：phpo 装镜像和恢复镜像，靠的是把镜像tar 文件搬进搬出缓存根。缓存根（默认 `./offline/`，用户可自定义）里每个 PHP 版本有**两份** tar，各占各的槽位、各带清单里一条记录：基座 `image.tar`（清单 `image`）与扩展固化镜像 `image-extensions.tar`（清单 `extensions_image`）。写入靠 `docker save`，恢复靠 `docker load`——§5.14.3 那条「离线缓存 > 本机 Docker 镜像库 > 网络」的优先级，第二级与「固化镜像不在本机即零网络恢复」那条自救路径，**全部走的是同一个 save/load 动作**。

**什么情况下会错、错了看得见什么**：如果 Podman 兼容层的 save/load 真的 500（对方实测报了 500，见第五节的数字声明），断的不是「装服务」这一格——断的是**画像 F（内网/断网/内网无法访问 Docker Hub）唯一的自救路径**。用户界面上的表现会是：离线缓存页明明显示这个版本已缓存，点「启用」却失败，抽屉日志一行 err；而按 §5.16.2 那条「固化镜像不在本机 → 容器退回基座重建」的恢复分支，`LoadExtImage` 也会在同一处失败。这两条路径在 Docker 上是零网络的，在 Podman 上如果没换实现就变成「必须联网」——而联网恰恰是画像 F 拿不到的东西。

**怎么回来**：`SaveLoadViaCLI` 必须同时覆盖两个槽位（基座 tar 与扩展固化 tar），不能只改基座那一路；两条提升/恢复链路上的日志一行都不能因为换引擎而少（§5.16.3 的「提升成功一行 ok 必须点名落点全路径」在 Podman 上同样成立）。**建议把这条写进 P2 的验收判据**，而不是留到真机才发现——验收就看两件事：断网状态下从缓存根装一次基座、再从缓存根恢复一次扩展固化镜像，两头都要过。

---

三、本轮净新增之二：需求 2 的「双引擎检测」不需要新机制，它长在第 5.7 节那条三态探测上

**这是干什么的**：用户要的是「装了 phpo 以后先检测他用的是 Podman 还是 Docker；有其中一个就用那个；两个都没有就提示装一个」。这件事在本仓库里**已经有一半**——§5.7 的 doctor 那三条冻结口径：端点在构造客户端时一次选定并全链路共用、`not_installed`／`not_running`／`no_permission` **三态不得合并**、消息必须自带原始原因且建议按平台分岔。

**什么情况下会错、错了看得见什么**：三条最容易做错的地方，都有真机前例。① 把「没问到」当成「这台机器上没有」——发现态那条（§5.19.3a）已经因为这件事立过禁止项，双引擎检测同一条规矩：一次探测失败**不得**判成「这个引擎没装」，否则用户装了两套、phpo 只认了一套，界面从此少一半服务列表。② 把 `no_permission` 报成「未运行」——apt 装 docker.io 建组却不把用户加进去，是 Ubuntu 最常见的一种失败；Podman rootless 侧同一形状（用户级 socket 没起、linger 没开）。③ 给出不存在的建议——Linux 上「Docker Desktop」这个产品并不存在，Podman 侧同理不得指向不存在的东西。**界面上用户看得见的是**：doctor 那一行结论 + 括号里的原始错误 + 按平台分岔的一句下一步，两个引擎各一行，**不得合并成一句「容器运行时不可用」**（合并即违反三态不合并）。

**怎么回来**：两个都没装 → 引导页给两种安装名（按平台分岔），装完点「重试」即回到检测；只装了一个 → 直接用它，不设「首选引擎」这种额外门禁（§0.2 规则 15/16：能警告的不要阻止，能放行的不要拦）。**两个都装了选哪个**——这一条是本轮新增的、对方计划里没有的判断项，我先给结论再给依据：按「已装且可连通」择一即可，并把选中的那一个**显示出来**（设置页与 doctor 各一处），因为后面所有删除/清理/挂载语义都跟着它走，用户必须知道自己此刻在用哪一族。是否允许用户手动改首选，留给第五节的裁决点。

---

四、本轮净新增之三：另外三处既有能力的半径（对方计划未点名）

| 既有条款 | 加 Podman 后要重读的一句话 | 风险档 |
|---|---|---|
| §5.19 发现态（服务页画「库里 ∪ Docker 上此刻」） | 每一档校准都**一次** `ContainerList{All:true}` 问全，含已停止、含不是 phpo 装的。这份清单的判据是容器名/镜像 tag/端口三层，**与引擎族无关**——Podman 上同一套分类代码照常能用 | **低**（只认不删、只认不建），但「问不通即上抛」那条不得因为换引擎而退化成「当成这台机器上没有服务」 |
| §5.21 镜像源 | 现在整套是 Docker SDK 的 pull 路径（`MirrorRef` 换前缀 → `ImageTag` 归一 → 删别名）。Podman 上「归一回原始引用」这一步**必须由 dialect 能力位决定实现**，不归一 = 离线缓存存进带源前缀的名字、换机即失效 | **中**（回归风险，非新增能力） |
| §5.24 配置生效时序 | 「谁的脸变了动谁、用最轻的手段、先备上游再切流量」这三句是**引擎无关**的原则；但「最轻的手段」在不同族上落点不同（systemd socket 激活下 reload 的可达性需要 dialect 声明） | **低**（原则不动，只补能力位） |

冻结立场的一处延伸，必须写进提案而不在代码里偷偷做：**§5.21 那条「绝不写宿主 Docker 的 `daemon.json`／`registry-mirrors`、绝不重启用户的 Docker」，对 Podman 同样成立**——不写用户的 `containers.conf`、`registries.conf`，不重启用户的 podman socket / 不改用户的 systemd 单元。这是「只改自己发起的那一次请求」这一立场的自然延伸，不是新规则。

---

五、事实声明与两处必须回到用户手上的冲突

1. **对方给出的实测数字我未重跑**（§0.2 规则 2 禁止臆测数字）：兼容层 save/load 报 500、`/secrets` 答 200、13 个 live 用例、rootless 80 与 linger 的那组现场，都是对方在用户机器上取的。**本文件保留它们作为线索，但不得读成「已二次验证的事实」**；P0 收尾那一步把它们重跑一遍才算闭环。
2. **平台范围**：Podman 是否要覆盖 Windows / macOS（那两族上 Docker 跑在虚拟机里，§5.22 已登记过「够不着」这一格），还是本轮**只做 Linux**。这一条决定 §5.7 探测链要不要在 win/mac 上长出第二个端点分支，成本差一个量级。我不替用户选。
3. **rootless 的 80 端口**：Podman rootless 绑不了 1024 以下端口，对方的方案里出现过「自动改 8080」这一类顺延。**这与总纲冻结的端口策略直接冲突**（§5.8：新建站点端口被占用**不顺延**，只告警 + 站点降级、站点照常创建；改已有站点的端口才顺延 1–65535 首个可用；服务端口占用报错）。把「自动改成 8080」当成实现细节塞进去，等于偷偷改了用户的端口——本轮明确**不做**这一条。可走的两条：让站点降级（现有口径，零改动）；或让用户以 rootful / 走 CAP 的方式自己解决（§8 矩阵里 80 端口那一行本来就是「需 root/CAP」）。**这一条必须由用户表态**。

另外登记一件事实，**只登记、不重写**（追加式协作的约束）：本文件第 **455–502 行**那一段的署名与内容来源不符。这是既成事实，就地改写会把当时的现场抹掉，也违反「不动既有段落」，因此保留原样，只在此处点名。

---

六、总纲 v2.9.16 提案要点（提案先行，不得代码先行）

按 §13 的变更流程（提案 → 两名核心开发者评审 → 递增文档版本 → 同步派生文档），这一节列出**提案必须改到的四处**，供用户直接抄成 Change Proposal：

| 落点 | 现状 | 提案措辞 |
|---|---|---|
| 硬红线 7（`AGENTS.md:640`） | 「Docker 未装则不能启动服务」 | 「**容器引擎（Docker 或 Podman）未就绪则不能启动服务**」——硬红线数量仍 8 条，只改这一条的措辞 |
| §2 技术栈表（冻结） | 容器引擎一行只写 Docker SDK | 补一行 Podman 兼容 API / CLI 通道，并注明「**save-load 走 CLI**」这条例外的落点 |
| §5.7 doctor 三条口径 | 端点探测候选链只覆盖 Docker | 补一句：同一套三态口径**按引擎各报一行**、不得合并，探测判据是拨号而非 stat 文件 |
| §0.2 规则 3（路径安全）/ 新增一条方言边界 | 未约定「哪些能力按端点声明」 | 新增规则：**方言能力位按端点算、不按引擎族名算**；且**不得**为任一引擎写用户的全局配置（`daemon.json`／`registry-mirrors`／`containers.conf`／`registries.conf`），也不得重启用户的运行时 |

**为什么必须提案先行**：本文件是 FROZEN、★★★ 最高权威，§0.2 规则 1 明写不得修改总纲除非用户在当轮显式要求。上面四处任何一处没先过评审就动代码，等于让实现跑在权威文档前面，后续每一轮对账都会把它当成「文档漂移」重新改一遍。

---

七、一句话收口

对方那份 P1→P3 的阶段划分我认可、不重述；本轮补上的是**坑踩在哪一条既有条款上**（§5.14 两个缓存槽位、§5.7 三态探测、§5.19/§5.21/§5.24 三处半径），**两处我不能替他选的冲突**（平台范围、rootless 80 与冻结端口策略），以及**提案必须先改总纲的四处措辞**。全部结论仍受一条硬约束支配：**原有的 Docker 功能一格都不能坏**——所有新增都收在 engine 层的方言边界里，向上一个快照出口、一套事件协议（17 个事件名）、四档任务状态都不变。

Qoder-AI


# 扩展 Podman 规划实施方案计划【追加·环境实测】—— 本机就是「podman-only + docker 垫片」的活样本：对需求②检测设计的实证修正

> **文档类型**：环境实测追加（第四轮 · ZCode-AI）
> **写于**：2026-10-05
> **触发**：为解释 vhost 模板 `resolver 127.0.0.11` 做本机演示时，意外揭穿本机环境真相——`/usr/bin/docker` 是 **podman-docker 仿真垫片**（shell 脚本，原话 "Emulate Docker CLI using podman"），`/var/run/docker.sock` 是**指向 rootful podman socket 的符号链接**（→ `/run/podman/podman.sock`），`docker version` 报 Server 5.7.0（podman）。**本机从来没有真正的 Docker 守护进程。**
> **为什么重要**：这台机器就是需求②要处理的场景的活样本——「用户装了 podman、没装 Docker，但 `docker` 命令存在」。朴素检测在此机器上必然误判，而误判方式恰好枚举了检测器设计必须避开的全部陷阱。

## 一、本机环境真相链（五条通道逐一实测）

| 通道 | 实测结果 | 教训 |
|------|---------|------|
| `/usr/bin/docker` | **shell 脚本**（podman-docker 垫片） | **「docker CLI 存在」绝不可作为 Docker 已装判据**——CLI 可能是别名/垫片/包装脚本 |
| `/var/run/docker.sock` | **symlink → `/run/podman/podman.sock`**（rootful podman） | **「socket 文件存在」也不可作判据**——symlink、权限、命名空间都会骗人 |
| dial `/var/run/docker.sock`（rootful） | **Permission denied**（kentrl 不在 rootful socket 的授权组，sudo 需交互密码） | 三态中的 `no_permission` 必须独立存在并给出对应修复话术 |
| `/run/user/1000/podman/podman.sock`（rootless） | **沙箱命名空间不可见**（no such file），但 `podman info` 自报 RemoteSocket.Path 就是它 | 文件可见性受命名空间影响；**唯一权威判据：真实拨号 + Ping + `Components[0].Name` 识别** |
| `podman` CLI | ✓ 全通（rootless 进程内库，不走 socket） | CLI 通 ≠ API 通，两者是不同通道 |

**按需求②的正确检测结论（本机）**：引擎 = **Podman（rootless）**。而朴素检测（看 docker CLI / 看 /var/run/docker.sock 文件）会分别误判成「Docker 已装」「Docker 已装但无权限」——两种都错。

## 二、live 测试守卫行为实测（意外验证了三态纪律）

`PHPO_LIVE=1 go test ./test/integration/ -run TestDockerSource` 在本沙箱：**SKIP（不判红）**——守卫经 `engine.New()` → `/var/run/docker.sock`（symlink）→ 拨号 → Permission denied → 三态 `no_permission` → 按测试头注释的既有纪律「环境不通就明说而不是判红」跳过。**这一跳恰好实证了三态守护在 podman 环境下的行为正确**：no_permission 没有塌缩成 not_installed，也没有假装可用。

**P0 收尾相应转为用户侧任务**：rootless socket（`/run/user/1000/podman/podman.sock`）只在真实桌面会话可见可用——请在你的终端跑：
```
PHPO_LIVE=1 go test ./test/integration/ -v
```
（你的会话里 rootless socket 可见可写，13 个 live 用例将对 podman compat 全链路执行；输出贴回来即可完成 S2 取证闭环。）

## 三、resolver 问题的教学沉淀（上一问答的结论归档）

`resolver 127.0.0.11` 是 **nginx 的运行时 DNS 配置**，只服务于「变量上游」（`fastcgi_pass $php_upstream`）的每请求解析；`valid=10s`（缓存 10 秒，容器重建换 IP 自动跟上）、`ipv6=off`（不发 AAAA）。**为什么不是 127.0.0.1**：容器内 127.0.0.1 是容器自己的环回、无 DNS 监听，宿主 DNS 经容器环回不可达；`127.0.0.11` 是 **Docker 在用户自定义网络容器里部署的内嵌 DNS 专属地址**（并自动写进容器 resolv.conf）。**podman 实测**：容器内 `127.0.0.11` Connection refused、aardvark-dns 在网关（10.89.0.1）应答——S3 候选 (a)「resolver 地址按引擎动态化（Docker=127.0.0.11 不动，Podman=网关）」由此获得本机实证。另注：静态上游（无变量）虽可免 resolver，但会引入「启动解析失败拒启」与「容器换 IP 需手动 reload」两个更坏的性质，否决。

## 四、对检测器规范的最终修正汇总（并入 v2.9.16 提案的四条实测纪律）

1. **CLI 存在 ≠ 引擎存在**（垫片/别名/包装脚本反例：本机）。
2. **socket 文件存在 ≠ 可用**（symlink/权限/命名空间三类假象：本机全遇到）。
3. **唯一判据 = 拨号 + Ping + `Components[0].Name`**；三态（未装/未运行/无权限）各自带可复制的修复命令（Docker 与 Podman 两套话术按引擎方言化）。
4. **「docker 命令存在但引擎是 podman」的机器，检测结果必须是 Podman**——判定读 socket 的应答身份，不读命令行工具的名字。

## 五、更新后的行动清单

1. **你**：真实会话跑 `PHPO_LIVE=1 go test ./test/integration/ -v`（P0 取证闭环的最后一步）。
2. **6 个裁决点表态**（平台范围 / rootless 80 / 验收线 / 检测时机 / 代拉 / 提案范围）。
3. **v2.9.16 提案**（硬红线 7 措辞 + §2 技术栈表 + §5.7 锚点修正 + 规则 3 边界 + 本追加的四条检测纪律入 §5.7/检测器规范）。
4. 提案通过 → S1（检测地基）→ S3（resolver 动态化，本机证据齐备）→ S4（执行链 + C10 收口 + SaveLoadViaCLI）→ S5（面板降级 + 文档）。

—— ZCode-AI
