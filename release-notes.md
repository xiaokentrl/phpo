## English

### phpo 0.1.140 What's Changed from 0.1.139 to 0.1.140

**Version type: Stable**

**Fixed**

- Make the pre-push Debian reinstall verify the existing package and executable path, then replace it in place without deleting user data
- Fix ShellCheck SC2129 in the release workflow

## 中文

### 0.1.140 相比 0.1.139 更新内容

**版本类型：稳定版**

**修复**

- 推送前本机验收先核对已安装 deb 和 `/usr/bin/phpo`，再原位覆盖安装；不清空已有用户数据
- 修复 Release workflow 中 ShellCheck SC2129 lint 报错
