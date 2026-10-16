## English

### phpo 0.1.124 What's Changed from 0.1.123 to 0.1.124

**Version type: Stable**

**Added**

- The new drawer line says the step actually succeeded and that the engine's complaint came from the network cleanup afterwards, and offers the recovery command you can run yourself: systemctl --user restart podman

**Fixed**

- Stopping and uninstalling a service no longer fail when the engine complains after the work is done: if the container is really stopped or removed, the step counts as succeeded, the state is written back and the view refreshes as usual, and the engine's own words become a single line in the task drawer
- Backing up (pausing data services), the cleanup panel's container removal and the pre-create cleanup now share one rule: a verified end state never blocks the remaining steps, while a container still running — or a state the engine refuses to report — still fails and rolls back as before

## 中文

### 0.1.124 相比 0.1.123 更新内容

**版本类型：稳定版**

**新增**

- 抽屉里那一行提醒说清「这一步其实已经做成，引擎抱怨的是停止之后的网络收尾」，并给出你自己能执行的恢复命令 systemctl --user restart podman

**修复**

- 手动「停止」「卸载」不再被引擎的收尾报错判死：容器确实停了或删了，这一步就算成，状态照常回写、界面照常刷新，引擎那句原文只在抽屉里落一行提醒
- 备份暂停数据服务、清理面板删容器、建容器前清同名这几处清理用同一个判据——实测已经达成就不拦住后面的步骤；实测仍在跑或现场问不出来，照旧判失败并回滚
