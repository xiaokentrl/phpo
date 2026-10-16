## English

### phpo 0.1.123 What's Changed from 0.1.122 to 0.1.123

**Version type: Stable**

**Added**

- Add a service multi-select to the Startup card in Settings so the services you check start automatically when phpo starts
- Wait up to two minutes for a late-starting Docker or Podman engine before those services start, and say so in the drawer log when the wait runs out

## 中文

### 0.1.123 相比 0.1.122 更新内容

**版本类型：稳定版**

**新增**

- 设置页「开机自启动」新增服务多选，勾过的服务在 phpo 启动后自动进任务队列并逐个启动
- Docker 或 Podman 晚起来时最多等两分钟再启动这些服务，等满仍未就绪就在抽屉日志说明原因并跳过
