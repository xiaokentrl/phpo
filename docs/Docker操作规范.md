# Docker 操作规范

> 对应 AGENTS.md §1.12 / §3.3 硬红线 7/8 / §5.13 / 工单 T704。核心原则：phpo 对 Docker 的任何操作都必须干净、不影响后续操作。冲突以 AGENTS.md 为准。

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

Docker 未安装/未运行 → 不能启动任何服务。`engine/client.go` 建客户端前探测可用性；不可用报人话提示（「Docker 未运行。请启动 Docker Desktop。」），经 doctor/preflight 阻断（[最小限制原则](./最小限制原则.md) 例外之一）。

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

- 重装（§5.13.11-12）：卸载（**保留卷**）→ 安装（**复用卷**），不清用户数据。
- 导入：清空目标命名空间 → 恢复数据 → 创建容器。

## 6. 状态一致与校准（§5.13.9）

- `engine/calibrate.go`：启动 + 每次任务后 + 手动，Docker ≡ SQLite；漂移发 `docker:state-drift`。
- `engine/verify.go` + `engine/inspect.go`：操作后自检（§5.13.8，支持 CI 无人值守）。
- `engine/health.go`：就绪门控不只看容器 `State.Running`（entrypoint 前置脚本，见 project 记忆「container readiness race」）。

## 7. 明确禁止（§5.13.13）

不检查冲突 / 不回滚 / 留无名资源 / 删用户数据 / 重装清数据 / 导入不清空 / 状态不一致 / 静默失败 / 非幂等 / 无审计。

审计见 `docs/资源清洁机制.md`（孤儿/清理/回收站）与 `~/.phpo/logs/operations.log`（JSON Lines，§5.13.10）。

## 相关文档

- [资源清洁机制](./资源清洁机制.md)
- [回收站机制](./回收站机制.md)
- [离线缓存机制](./离线缓存机制.md)
- [任务取消语义](./任务取消语义.md)
