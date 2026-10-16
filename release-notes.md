## English

### phpo 0.1.125 What's Changed from 0.1.124 to 0.1.125

**Version type: Stable**

**Fixed**

- Move the engine /version dial off the startup thread so the main window appears about 0.7s sooner
- Resolve the container gateway address in vhosts by asking the engine kind at call time, so a Podman site never gets Docker's 127.0.0.11

## 中文

### 0.1.125 相比 0.1.124 更新内容

**版本类型：稳定版**

**修复**

- 把启动时引擎识别那一拨挪到后台，主窗口早约 0.7 秒出现，开机忙碌光标从约 0.89 秒降到约 0.13 秒
- vhost 里的容器网关地址改成就地现问引擎种类，避免识别晚于装配时把 Docker 的地址写进 Podman 站点
