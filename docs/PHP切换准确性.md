# PHP 切换准确性

> 对应 AGENTS.md §1.7 / §5.5 / §3.3 硬红线 1 / 工单 T704。核心约束：站点切换 PHP 版本时，vhost 的 fastcgi 上游**唯一且精确**指向目标版本容器。冲突以 AGENTS.md 为准。

## 1. 硬红线 1：精确匹配（禁止模糊匹配）

**上游格式固定**（§0.3 / §5.5）：

```
php-{version}-fpm:9000
```

- 版本号**原样保留**（含点）：`8.4` → `php-8.4-fpm:9000`；`7.4` → `php-7.4-fpm:9000`。
- 不做前缀/近似/最新兼容匹配（§0.2-9）：切换 `8.4` 绝不落到 `8.4.1` 或 `8.3`。
- 网络别名 `{kind}-{version}`（如 `php-8.4-fpm`）+ 端口 `9000`（§5.13.2），上游唯一。

## 2. 映射与就地改写

- `UpstreamFor(version) = "php-{version}-fpm:9000"`（§5.5）。
- `internal/vhost/upstream.go#ReplacePhpUpstream(content, php)`：就地把首个 `$php_upstream` 改写为 `set $php_upstream php-{php}-fpm:9000;`（保留点号原样）。
- `ReplaceListen(content, port)`：改站点 `listen` 端口时复用同一「唯一替换」原则，避免多上游歧义。

## 3. 切换流程（§5.5 十步，落到 `SiteService.SwitchPHP` → `writeVHost`）

```
1 preflight("php-switch")            # 唯一裁决层
2 读取当前 vhost（vhosts.Sync(sites)）
3 计算上游 php-{php}-fpm:9000
4 校验（站点存在 + 内容非空）
5 写入临时 → nginx -t 校验（硬红线 2）
6 通过才落盘 vhost（steps.NewWriteVHost）
7 docker exec nginx reload（FuncStep "重载 Nginx"）
8 （端口有变时）republish 站点端口
9 applyStateChange：store.UpsertSite + emit state:changed
10 返回（后端权威，前端订阅回流）
```

写盘前 `nginx -t` 必过（硬红线 2），失败则整任务回滚，不留半写状态（§5.13.1 可恢复性）。

## 4. 前端接线

- `PhpSelect.vue` + `usePhpSwitch.ts` → `App.SiteSwitchPHP(domain, php)`。
- 不做乐观更新：等 `state:changed` 回流后刷新展示（硬红线 4）；上游展示串直接取后端快照，不在前端拼接（防止与后端漂移）。

## 5. 与其它约束的边界

- 与「版本策略」：版本号仅路径安全校验后原样进入上游串（[版本策略](./版本策略.md)）。
- 与「Docker 清洁」：目标 PHP 容器须已安装并加入 `phpo-network`；切换前 preflight 校验其存在，否则 `NotInstalled`。
- vhost 被手改过（`VhostCustomized`）：仍按精确上游改写单行，不覆盖用户其余指令（§5.10 放开 vhost 指令）。

## 相关文档

- [版本策略](./版本策略.md)
- [状态同步](./状态同步.md)
- [Docker操作规范](./Docker操作规范.md)
- [接口契约](./接口契约.md)
