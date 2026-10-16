// 开机自启动前端 API：读/写 OS 注册态的薄封装（Wails AutostartManager 三平台实现）。
// 硬红线 4：注册态唯一权威在 OS（IsEnabled 现读现答），前端不落本地副本、不做乐观缓存——
// 开关的初始值每次进设置页都现问一次。无宿主（demo 通道）两路都静默返回，视图据此禁用。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'

export async function getAutoStart(): Promise<boolean | null> {
  if (!hasBackend()) return null
  return await app.AutoStartGet()
}

// setAutoStart 切换注册；失败抛原始错误（backendMsg 现译），调用方负责回滚开关并 toast
export async function setAutoStart(enable: boolean): Promise<void> {
  if (!hasBackend()) return
  await app.AutoStartSet(enable)
}
