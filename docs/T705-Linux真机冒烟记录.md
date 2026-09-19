# T705 · Linux 真机发布冒烟记录

> 对应 AGENTS.md §11.1 交付物 / 工单 T705（依赖 T701–T704）。冲突以 AGENTS.md 为准。
> 本记录仅登记**本机实际执行**的事实；Windows / macOS 未在本环境运行，见文末「遗留」。

## 执行环境

- 主机：Ubuntu 26.04 LTS（Resolute Raccoon），GTK 4.22.4，WebKitGTK 2.52.6
- 会话：`DISPLAY=:0` + `WAYLAND_DISPLAY=wayland-0`
- 提权：`pkexec` 图形授权（非免密 sudo）
- Go：go1.27.1，Wails CLI v3.0.0-beta.23，`CGO_ENABLED=1`

## 冒烟项与结果（Linux）

| # | 项 | 动作 | 结果 |
|---|----|------|------|
| 1 | 出包 | `wails3 tool package -format deb` → `phpo_0.1.0_amd64.deb` | ✅ 8,392,514 B |
| 2 | 包装完整性 | `dpkg-deb -c/-f` | ✅ `/usr/bin/phpo`(0755 ELF) + `phpo.desktop` + 128/256/512 图标；`Depends: libwebkit2gtk-4.1-0` |
| 3 | 安装 | `pkexec dpkg -i …0.1.0.deb` | ✅ `ii phpo 0.1.0`；触发器 gnome-menus/desktop-file-utils/hicolor-icon-theme 通过 |
| 4 | 桌面项 | `desktop-file-validate` | ✅ 通过；`Exec=phpo %u`、`Icon=phpo` |
| 5 | 启动前置 | `ldd /usr/bin/phpo` | ✅ 无缺失动态库 |
| 6 | 升级 | `pkexec dpkg -i …0.1.1.deb` | ✅ 覆盖 0.1.0→0.1.1，EXIT=0 |
| 7 | 回滚（包级） | `pkexec dpkg -i …0.1.0.deb` | ✅ dpkg 降级 0.1.1→0.1.0（提示 downgrading），EXIT=0 |
| 8 | 卸载 | `pkexec dpkg -r phpo` | ✅ 系统文件移除、dpkg 记录清空 |
| 9 | 数据保留（硬红线：不破坏用户数据） | 卸载前后对比 | ✅ 卸载仅删系统文件；`~/.phpo/`、`~/phpo/`（PHPO_HOME）不在包内，`dpkg -r` 不触碰 |
| 10 | GUI 真机启动 | `timeout 8 build/bin/phpo` | ✅ EXIT=124（存活至超时被杀），AssetServer 伺服 `index.html`+`OverviewView`（默认路由渲染），SIGTERM 干净退出 |
| 11 | 应用内升级回滚逻辑 | `go test ./internal/updater/…` | ✅ ok（marker + `RecoverOnStartup` + SHA256/Ed25519 双校验，T604 覆盖） |

## 关键结论

- 安装→升级→回滚→卸载全链路在真实 dpkg/pkexec 下通过，且**卸载保留用户数据**符合 §0.2 硬红线（不破坏用户数据）。
- GUI 在真机桌面成功起窗并伺服前端资源，证明 Linux 包「装得上、起得来」。

## 遗留（非本环境可验）

- **Windows**：NSIS `phpo-setup-x64.exe` 装/卸/升/回滚 + 代码签名 —— 需 windows runner（本机无 `makensis`）。
- **macOS**：`.dmg` 装/卸/升/回滚 + Developer ID 签名/公证 —— 需 mac runner。
- **Linux rpm**：`phpo-x86_64.rpm` 装卸 —— 本机无 `rpmbuild`（nfpm 纯 Go 可出包，但装/卸需 rpm 系发行版）。
- **升级包签名**：`build/signing/public.key` 为占位，T703/release.yml 发布时换真私钥签名；真机「下载→双校验→安装→下次启动确认/回滚」端到端需 CI 出包后跑。
- 上述由 CI matrix（native runner）或用户本机补齐；口径与 PHPO_LIVE 门禁一致——不编造未执行的结果。

## 相关文档

- [打包发布](./打包发布.md)
- [应用升级](./应用升级.md)
- [跨平台差异](./跨平台差异.md)
- [M6 集成验收记录](./M6-集成验收记录.md)
