## English

### phpo 0.1.149 What's Changed from 0.1.148 to 0.1.149

**Version type: Stable**

**Changed**

- Extract cleanup checkbox pure logic into a dedicated module locked by 6 cases (no behavior change)
- Teach the pre-push hook to tell a missing NOPASSWD rule apart from a failed install (e.g. held dpkg lock)
- Relax the weekly Actions cleanup from keeping one run to keeping the latest ten

**Fixed**

- Rewrite deb/rpm dependencies to match the actual GTK4 / WebKitGTK-6.0 linkage so fresh systems start right after install

**Removed**

- Drop the stale 30MB binary and a duplicate prototype copy; file the Wails feedback note under docs/

## 中文

### 0.1.149 相比 0.1.148 更新内容

**版本类型：稳定版**

**修改**

- 清理面板勾选纯逻辑抽为独立模块并以 6 个用例锁死（无行为变化）
- pre-push 钩子区分「免密规则缺失」与「安装失败（如 dpkg 锁被占）」
- 每周 Actions 运行记录清理由「只剩 1 条」放宽为保留最近 10 条

**修复**

- deb/rpm 依赖声明按 GTK4 / WebKitGTK-6.0 实际链接重写，干净系统上装完即可启动（旧声明装上也起不来）

**删除**

- 移除仓库遗留的 30MB 旧二进制与重复的原型副本，Wails 反馈文档归档进 docs/
