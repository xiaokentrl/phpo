# 第三方依赖许可证清单（THIRD-PARTY LICENSES）

> 本文件对应 AGENTS.md §4.1 `third_party/licenses/` 与 §11.2 交付物。
> phpo 桌面二进制随包分发的第三方组件许可证汇总。**发布前须重新核对**（版本或依赖变动时，见文末再生成方式）。

## 一、Go 后端依赖（直接依赖）

编译进 `phpo` 单二进制的直接模块：

| 模块 | 版本 | 许可证 | 版权归属 | 用途 |
|------|------|--------|---------|------|
| `github.com/wailsapp/wails/v3` | v3.0.0-beta.23 | MIT | Wails project | 桌面框架（窗口/事件/托盘/绑定） |
| `github.com/docker/docker` | v28.3.2+incompatible | Apache-2.0 | Docker Inc. | Docker Engine SDK（容器/镜像/网络/卷） |
| `github.com/docker/go-connections` | v0.8.1 | Apache-2.0 | Docker Inc. | Docker SDK 传输层依赖 |
| `modernc.org/sqlite` | v1.59.0 | BSD-3-Clause | The Sqlite Authors | 纯 Go SQLite 驱动（CGO_ENABLED=0 持久化） |
| `github.com/Masterminds/semver/v3` | v3.5.0 | MIT | Matt Butcher, Matt Farina | 版本号比较 |
| `golang.org/x/sys` | v0.47.0 | BSD-3-Clause | The Go Authors | 跨平台系统调用（磁盘 statfs 等） |

Go 标准库（`crypto/ed25519`、`crypto/sha256`、`archive/tar`、`compress/gzip` 等）随二进制分发，许可证为 **Go BSD-3-Clause**（The Go Authors）。

> 间接依赖（transitive）数量较多，均在上述许可证族（MIT / Apache-2.0 / BSD）之内；完整列表以 `go list -m all` 为准，发布归档应附其 SPDX 清单。

## 二、前端依赖（打包进 `frontend/dist` 随二进制 `go:embed` 分发）

| 包 | 版本 | 许可证 | 类型 |
|----|------|--------|------|
| `vue` | ^3.5.13 | MIT | 运行时（入包） |
| `pinia` | ^2.2.6 | MIT | 运行时（入包） |
| `vue-router` | ^4.5.0 | MIT | 运行时（入包） |
| `@wailsio/runtime` | 3.0.0-beta.23 | MIT | 运行时（入包） |
| `typescript` | ~5.7.2 | Apache-2.0 | 构建期 |
| `vite` | ^5.4.11 | MIT | 构建期 |
| `@vitejs/plugin-vue` | ^5.2.1 | MIT | 构建期 |
| `vue-tsc` | ^2.1.10 | MIT | 构建期 |
| `@types/node` | ^22.10.1 | MIT | 构建期（类型，不入包） |

构建期依赖不产出到用户机分发的字节（仅 `dist` 里的编译结果），列出以尽告知义务。

## 三、许可证全文

- MIT：`Permission is hereby granted, free of charge, to any person obtaining a copy ...`
- Apache-2.0：`http://www.apache.org/licenses/LICENSE-2.0`
- BSD-3-Clause：Go / modernc 变体，含三条 redistributions 条件与免责条款。

各依赖许可证全文以其模块目录内 `LICENSE` / `LICENSE.txt` 为准，位于 `$GOMODCACHE/<module>@<version>/`。

## 四、再生成方式

发布前刷新本清单：

```bash
# Go 侧直接依赖 + 许可证文件定位
go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all | grep -v '^phpo'
M=$(go env GOMODCACHE); # 对每个模块读 $M/<path>@<ver>/LICENSE* 的 SPDX 头
# 前端依赖
node -e "const p=require('./frontend/package.json');console.log(JSON.stringify({deps:p.dependencies,dev:p.devDependencies},null,2))"
```
