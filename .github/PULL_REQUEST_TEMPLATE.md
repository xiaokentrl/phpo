## 变更概述

<!-- 一句话说明这个 PR 做了什么、为什么。 -->

关联工单 / Issue：`T___` / `#___`

## 类型

- [ ] 新功能　- [ ] Bug 修复　- [ ] 重构　- [ ] 文档　- [ ] 构建/CI

## 验收门禁（提交前请确保本地已跑）

- [ ] `go build ./...`
- [ ] `go test ./...`（无 Docker 环境全绿；集成 live 测以 `PHPO_LIVE` 门禁，默认 skip）
- [ ] `go vet ./...`
- [ ] `gofmt -l .` 为空
- [ ] `go run ./scripts/check-i18n-keys.go`（zh/en 键数对齐）
- [ ] `go run ./scripts/check-templates.go` / `check-docker-naming.go` / `check-cache-manifest.go`
- [ ] `pnpm -C frontend build`（改动前端时；`frontend/dist/index.html` 已还原为占位）

## §3.4 编码准则自查

- [ ] 编码前思考：不确定处已询问、歧义已列出、矛盾已报告
- [ ] 简洁优先：无过度抽象、无为「未来可能」的设计
- [ ] 精准修改：仅改任务相关代码，未顺手改无关代码/格式/死代码
- [ ] 目标驱动：先写失败用例再转绿 / 重构后原测试全过

## 硬红线自查（§3.3，触及则勾选）

- [ ] 未破坏 PHP 切换精确匹配 `php-{version}-fpm:9000`
- [ ] vhost 写入前经 `nginx -t`
- [ ] 路径穿越 `..` 防御完好
- [ ] 状态同步以后端为唯一权威（无前端乐观更新）
- [ ] 写操作走三段式（preflight → task → applyStateChange）
- [ ] 升级包 SHA256 + 签名校验完好
- [ ] Docker 未装不启动服务；操作幂等/原子/可回滚/可清理
- [ ] 离线缓存：装前必查、命中零网络、临时目录无论成败必清

## 备注 / 风险

<!-- 跨平台差异、需真机/CI 验证的项、已知遗留。 -->
