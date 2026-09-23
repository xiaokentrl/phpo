# Docker 操作规范

> 对应 AGENTS.md §1.12 / §3.3 硬红线 7/8 / §5.13 / §5.19 / 工单 T704。核心原则：phpo 对 Docker 的任何操作都必须干净、不影响后续操作。冲突以 AGENTS.md 为准。

## 1. 六项保证（§5.13.1）

| # | 保证 | 说明 |
|---|------|------|
| 1 | 幂等性 | 任何操作重复执行结果一致 |
| 2 | 原子性 | 要么全成功，要么全回滚 |
| 3 | 隔离性 | `phpo-` 前缀命名空间 |
| 4 | 一致性 | Docker 实际状态 ≡ SQLite 状态 |
| 5 | 可清理性 | 一键清空 phpo 资源 |
| 6 | 可恢复性 | 失败回到操作前 |

## 2. 功能依赖前置（硬红线 7）

Docker 未安装/未运行/无权限 → 不能启动任何服务。`engine/client.go` 在构造客户端时一次性选定端点（显式 `DOCKER_HOST` 优先；否则按 `/var/run/docker.sock` → `/run/docker.sock` → `$XDG_RUNTIME_DIR/docker.sock` → `/run/user/<uid>/docker.sock` → `$HOME/.docker/run/docker.sock` 取第一个盘上确实是 unix socket 的候选），探测与后续所有 SDK 调用走同一份。不可用报**带原始原因、按平台分岔**的人话提示，三态不得合并（§5.7 三条冻结口径）：

| 状态 | 判据 | 提示（Linux） | 提示（Windows / macOS） |
|------|------|--------------|----------------------|
| `not_installed` | 候选 socket 全不在盘上 | 「Docker 未安装。（{原因}）请安装 Docker 引擎：`sudo apt install docker.io`…」 | 「请下载 Docker Desktop：[链接]」 |
| `not_running` | socket 在但连不上 | 「Docker 未运行。（{原因}）请启动 Docker 服务：`sudo systemctl start docker`…」 | 「请启动 Docker Desktop。」 |
| `no_permission` | `permission denied` | 「当前用户无权访问 Docker。（{原因}）请把用户加入 docker 组：`sudo usermod -aG docker $USER`，然后重新登录…」 | 「请重新启动 Docker Desktop；仍不行时以管理员身份运行本应用。」 |

`apt docker.io` 会建 `docker` 组但**不会**把当前用户加进去，因此 Linux 最常见的第一次失败是 `no_permission`——把它报成「未运行」等于把用户指向一个本来就在跑的 daemon。经 doctor/preflight 阻断（[最小限制原则](./最小限制原则.md) 例外之一）。

## 3. 资源命名规范（§5.13.2，隔离性）

`pkg/dockerutil/naming.go`：

| 资源 | 命名 | 常量/函数 |
|------|------|-----------|
| 前缀 | `phpo-` | `NamespacePrefix` |
| 网络 | `phpo-network` | `NetworkName` |
| 容器 | `phpo-{kind}-{version}` | `ContainerName` |
| 卷 | `phpo-{kind}-{version}-{purpose}` | `VolumeName` |
| 网络别名 | `{kind}-{version}`（如 `php-8.4-fpm`） | vhost 上游据此解析（硬红线 1） |

版本号原样进入名称（[版本策略](./版本策略.md)）；`scripts/check-docker-naming.go` 静态对账命名一致性。

## 4. 原子性与回滚（§5.13.3，三段式）

Task 三阶段（`engine/idempotent.go` + `task/`）：

```
Pre-Clean  抹平同名/冲突资源（幂等来自「PreClean 抹平差异 + Execute/Verify 对已达成态跳过」）
Execute    真正操作（创建容器/卷/网络…）
Post-Verify 校验达成态
失败       → 逆序 Rollback（task/rollback.go，30s 超时）→ 回到操作前
```

Step 五接口：`Name / Execute / Rollback / Cleanup / Cancelable`（[任务取消语义](./任务取消语义.md)）。

## 5. 幂等操作清单（§5.13.4，8 个）

安装 / 卸载 / 启动 / 停止 / 重装 / 创建站点 / 删除站点 / 导入。每个都满足重复执行收敛同一终态、零残留。

- 重装（§5.13.11-12）：卸载（**保留卷**）→ 安装（**复用卷**），不清用户数据。落地为 `LifecycleService.Reinstall`（`app.go` 的 `Reinstall`），是**服务端口/密码改后唯一生效路径**——两者只在建容器时落进端口发布与 `env`，`Start`/`Stop` 改不动。占用预检排在 Pre-Clean **之前**：新端口被占即整次操作拒绝、在跑的容器不受影响（否则等于白丢一个服务）。
- 导入：清空目标命名空间 → 恢复数据 → 创建容器。

## 6. 状态一致与校准（§5.13.9）

- `engine/calibrate.go`：启动 + 每次任务后 + 手动，Docker ≡ SQLite；漂移发 `docker:state-drift`。它只做**纯比对**（期望运行态 ≢ 实际运行态即出修正项）；核查范围分**两档**，见 §6.3。
- `engine/verify.go` + `engine/inspect.go`：操作后自检（§5.13.8，支持 CI 无人值守）。
- `engine/health.go`：就绪门控不只看容器 `State.Running`（entrypoint 前置脚本，见 project 记忆「container readiness race」）。

### 6.1 启动就绪与失败取证（§5.18.3）

| 环节 | 口径 |
|------|------|
| `StartContainer`（`engine/container.go`） | 按当前 `ContainerStatus` 分流：已 `running` 不动 → `restarting`／其他一律 `ContainerRestart` → 其余 `ContainerStart` |
| `waitRunning` | 轮询到 running 后再静默 `startHold = 2s` **复验**（躲开「起来即崩」窗口）；超时 `startWait = 12s`、间隔 `startInterval = 300ms` |
| 失败消息 | `startFailureMsg`：容器名 + 状态 + **退出码** + **容器日志尾部 5 行**（`LogTail`，经 `ExecStream`/stdcopy 去帧）——报错自带证据，不用用户去翻 `docker logs` |
| `StopContainer` | 只对 `needsStop`（`running`／`restarting`）执行，其余幂等跳过 |

**容器内进程不得往宿主 bind 目录写日志文件**（§5.18.4）：那些文件由容器内 uid 创建、权限常为 `0700`/`0600`，宿主侧既读不动、也会在写不下时把服务打进 FATAL 崩溃循环。所有服务模板的日志一律走标准输出/标准错误，由 Docker 收集。

> 权限口径的分界（§5.20 / 总纲 v2.9.14 需求 ②）：**phpo 自己创建的**目录与文件一律 `0777`（经 `internal/util/fs.go` 的四个助手，`umask` 不得削位）；**容器内进程写出来的**文件不在本策略范围内——那是镜像内 uid 的 umask 决定的，phpo 不做 chown/chmod 事后修正（§5.20.2）。因此备份归档仍会遇到读不动的条目，处置口径不变：跳过 + 按目录聚合告警（§5.17.1）。

### 6.2 容器内命令输出口径（§5.16.3）

Docker exec attach 流每帧带 **8 字节二进制帧头**，直读原始流即把垃圾打进日志。唯一出口是 `engine.ExecStream(ctx, name, argv, stdout, stderr io.Writer)`（内部 `stdcopy.StdCopy` 去帧并分流）；扩展编译与备份逻辑导出共用它。无换行的超长进度条按 **4 KiB** 强制断行——攒成整串等于让用户盯着一段时长未知的「执行中」。

**宿主 ⇄ 容器的唯一字节通道是 `engine/copy.go`**（§5.14.3 / 总纲 v2.9.14 追加）：`CopyTo(ctx, name, dstDir, hostFiles...)` 把宿主的扩展包文件送进容器、`CopyFrom(ctx, name, srcDir, dstDir)` 把容器内下载/编译产物取回宿主，两者都走 **Docker archive API**（`CopyToContainer` / `CopyFromContainer`）——不经 shell、不依赖容器内装有 `scp`/`tar` 之类工具，因此基座是 Alpine 还是 Debian 都能用（php 容器的挂载表**不含** `ext/` bind，除这条通道外宿主与容器之间没有别的路径可搬字节）。tar 条目**只保 basename**：宿主侧的绝对路径与 `..` 不可能被带进容器（硬红线 3）。用途即扩展包本体离线化的两条腿——命中缓存时回填 `/tmp/phpo-ext/{apk,pecl}/`，未命中时把容器内 `pecl download` / `apk add --cache-dir` 的产物取回归档到缓存根。

### 6.3 全量同步与缺失态（§5.19，总纲 v2.9.14 需求 ①/④）

用户用 Docker Desktop / `docker rm` / `docker rmi` 把容器和镜像清掉后，phpo 的服务列表不能照旧只说「已安装」。现按两档核查：

| 档位 | 入口 | 核查范围 |
|------|------|---------|
| **轻量** | `LifecycleService.Calibrate` → `calibrate(ctx, false)`（每任务后 / 启动 / 每 24h） | **只判容器存在性**，复用本次已取到的 `ManagedContainers` 实际态——零额外 Docker 调用 |
| **全量** | `App.Calibrate` → `AppService.Calibrate` → `LifecycleService.SyncAll` → `calibrate(ctx, true)`（手动点「同步状态」） | 容器 + **该版本应运行的镜像** + php 的扩展固化镜像 `phpo/php:{ver}` |

**镜像判定用「该版本实际会跑的那一份」**：php 有固化镜像即以 `phpo/php:{ver}` 为准，没有才回官方基座 `php:{ver}-fpm`——否则会把「装了扩展的版本」说成基座镜像缺失。

缺失以 `model.ServiceGap{Kind, Version, Reason, Ref}` 表达，`Reason` 冻结为 **3** 种：`container` / `image` / `extensions_image`。四条硬口径：

1. **派生态、不落库**：`store.SetGaps` 挂到 `Snapshot.Gaps`；落库即伪造第二份权威。零缺失序列化为 `[]`（§5.6.3）。
2. **绝不自动改 `installed`**：第三方工具删掉容器不等于「卸载了这个版本」——配置、卷、缓存都在，点「启用」即按当前配置幂等重建（§5.13.4）。只标记、只点名，恢复由用户决定。
3. **探针报错一律上抛**：`ImageExists` 失败不能静默当作「本机没有」——把一次正常在机的镜像说成缺失，比不报更糟。
4. **幂等静默**：发不发只看本次比上次多说了什么——`if len(res.Corrections) == 0 && sameGaps(snap.Gaps, gaps) { return &res, nil }`。**不得**用 `res.Changed()` 作闸门：它把「存在性漂移」也算进变更，而容器缺席是常态化的（停了就是缺席），于是每次校准都重发同样的 drift + 快照，抽屉被同一行刷屏。

发事件时两条一起走：`docker:state-drift`（载荷 `model.StateDrift{Expected, Actual, Gaps}`，**不新增事件名**）+ `state:changed`（新快照带 `gaps`）。前端唯一入口是 `useStateSync.runSync()`（侧栏按钮与命令面板 ⌘R 共用），缺口见 [状态同步](./状态同步.md)。

## 7. 明确禁止（§5.13.13）

不检查冲突 / 不回滚 / 留无名资源 / 删用户数据 / 重装清数据 / 导入不清空 / 状态不一致 / 静默失败 / 非幂等 / 无审计。

另加 §5.19 三条：缺席只标不改库（不得自动降级 `installed`、自动删记录、自动重建容器来「修好」缺失态）；探针失败不得静默当作「本机没有」；发事件闸门不得用 `res.Changed()`。

审计见 `docs/资源清洁机制.md`（孤儿/清理/回收站）与 `<用户数据目录>/logs/operations.log`（JSON Lines，§5.13.10）。

## 相关文档

- [资源清洁机制](./资源清洁机制.md)
- [回收站机制](./回收站机制.md)
- [离线缓存机制](./离线缓存机制.md)
- [任务取消语义](./任务取消语义.md)
