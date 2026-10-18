// 启动后最小化前端 API：读写 config.yaml 里那一个布尔开关的薄封装。
// 硬红线 4：权威值在后端配置文件，前端不落本地副本、不做乐观缓存——开关初始值每次进设置页现问一次。
// 勾选只改「下次启动时窗口显不显示」，正在跑的这一轮不受影响。无宿主（纯 Vite demo）两路都静默返回，
// 视图据此禁用这颗开关。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'

export async function getStartMinimized(): Promise<boolean | null> {
  if (!hasBackend()) return null
  return await app.StartMinimizedGet()
}

// setStartMinimized 保存开关；失败抛出后端原话（调用方 toast 并回滚开关）
export async function setStartMinimized(enable: boolean): Promise<void> {
  if (!hasBackend()) return
  await app.StartMinimizedSet(enable)
}
