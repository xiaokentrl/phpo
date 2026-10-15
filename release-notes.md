**版本类型：稳定版**

## English

### phpo 0.1.118 What's Changed from 0.1.117 to 0.1.118

**Changed**

- Reorder release notes to English first and Chinese second, each title inside its own section

**Fixed**

- Stop re-hashing every cached image when opening the offline cache page
- Read the system hosts file once per site list refresh instead of once per site
- Stop building a full status snapshot just to resolve the nginx container name
- Cut the 12-second background state calibration to a single Docker call per beat

## 中文

### 0.1.118 相比 0.1.117 更新内容

**修改**

- 发布说明改成英文在前、中文在后，标题句各写进自己那一段

**修复**

- 进入离线缓存页不再把每份镜像逐字节重算一遍，风扇不再狂转
- 刷新站点列表时系统 hosts 只读一次，不再逐个站点重读整份文件
- 校验和重载 Nginx 配置不再为拿一个容器名重建整份状态快照
- 每 12 秒一次的后台状态校准只问 Docker 一遍，界面响应更利索
