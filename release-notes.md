## English

### phpo 0.1.139 What's Changed from 0.1.128 to 0.1.139

**Version type: Stable**

**Added**

- Make tray settings reflect platform capabilities and add an explicit “Show Main Window” tray-menu action

**Changed**

- Upgrade to Wails v3.0.0 and handle tray visibility and close-to-background behavior according to each platform's actual capabilities
- Align the stable desktop identity used by the Linux window, launcher, single-instance handling, and Dock grouping

**Fixed**

- Preserve the Linux Wayland window position when restoring from the tray and prevent duplicate presentation on tray double-click
- Prevent the application from remaining in an unreachable background state when tray availability is missing or unknown

## 中文

### 0.1.139 相比 0.1.128 更新内容

**版本类型：稳定版**

**新增**

- 托盘设置按平台能力显示可用操作，并在托盘菜单中提供明确的“显示主界面”入口

**修改**

- 升级到 Wails v3.0.0 正式版，并按平台实际能力处理托盘图标显隐与关闭时驻留
- 统一 Linux 应用窗口、桌面启动器、单实例和 Dock 归组使用的稳定桌面身份

**修复**

- 修复 Linux Wayland 关闭后唤回窗口位置变化，并避免托盘双击重复呈现窗口
- 托盘不可用或能力无法确认时，不再让关闭窗口留下无法唤回的后台进程
