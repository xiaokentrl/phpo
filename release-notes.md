## English

### phpo 0.1.147 What's Changed from 0.1.146 to 0.1.147

**Version type: Stable**

**Added**

- Add an About card to Settings with project links, version info, data directory and check-update entry
- Drag the collapsed task drawer upward to expand it; the height follows the cursor
- Resize the sidebar by dragging its right edge; the footer tiles sync and settings buttons side by side
- Cleanup scan rows show how many in-use objects keep-mode spares, with a badge for fully spared rows
- Add a close button to the environment-diagnosis card that removes its placeholder

**Changed**

- Rename the cleanup scope cards to "Wipe Everything" and "Keep only what running phpo services need"
- Make cleanup select-all literal: every checkbox row is ticked, with dangerous items guarded by preview consent
- Run the deep scan by default so host rows carry real counts and checkboxes as soon as the panel opens
- Move the theme picker into Settings > Appearance and drop the sidebar theme button
- Align settings wording across the page header and sidebar footer labels
- Upgrade to Wails v3.1.0 across the Go module, the JS runtime and the CLI

**Fixed**

- Give keep-scope cleanup real semantics: objects used by installed phpo services are hard-filtered at the backend preview gate
- Stop the WebView engine's default context menu from appearing on right click
- Have the pre-push hook stop and relaunch the app, install the package via passwordless sudo and block the push on failure

**Removed**

- Drop the useless engine context menu and the per-row phpo lock flow in the cleanup panel

## 中文

### 0.1.147 相比 0.1.146 更新内容

**版本类型：稳定版**

**新增**

- 设置页新增「关于我们」卡片：项目主页、反馈与许可链接、版本信息、数据目录和检查更新入口
- 任务抽屉收起时向上拖动即可展开，高度实时跟手，拖回底部收回
- 侧栏右缘支持拖拽调整宽度，底部「同步 | 设置」左右平铺并随宽度自适应
- 清理面板扫描行显示保留模式将排除的在用对象数，整行在用的行单独标注
- 环境诊断卡右上角新增关闭按钮，点击即移除面板占位

**修改**

- 清理面板两张范围卡改名：「全部彻底清理」与「仅保留使用中的 phpo 所需服务，其他全部清理」
- 清理面板改为字面全选：凡有复选框的行一律勾选，危险项的安全网移到预览的知情确认
- 清理面板默认执行详细扫描，宿主行打开面板即有真实数字和勾选框
- 主题切换移入设置中心的外观卡片，侧栏不再单独放主题按钮
- 设置页与侧栏文案对齐：「设置中心」页头与底部按钮统一，标签简化为「同步 | 设置」
- 升级到 Wails v3.1.0（Go 模块、前端 runtime 与 CLI 三处同步）

**修复**

- 清理面板保留模式为真语义：phpo 已装服务占用的容器、数据卷、镜像与宿主路径由后端预览闸门硬性排除
- 全局右键不再弹出 WebView 内核自带的菜单
- pre-push 钩子自动关停并重拉应用，替换安装支持免密 sudo 且失败即阻断推送

**删除**

- 移除 WebView 环境下无意义的内核右键菜单与清理面板的 phpo 行锁流程
