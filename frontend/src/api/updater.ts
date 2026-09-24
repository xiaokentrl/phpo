// 应用升级前端 API（T604 / §5.9）：检查更新 + 触发三段式升级的薄封装。
// 硬红线 4/5/6：读侧以后端为准；写侧 Apply 走后端任务引擎（下载→SHA256+签名双校验→备份→安装，失败自动回滚）。
// 无宿主（纯 Vite demo）时读侧返回 null/0，写侧直接返回，让视图不误报。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { UpdateAvailable } from '@/types'

// currentVersion 当前应用版本（比较基准）；无宿主返回空串
export async function currentVersion(): Promise<string> {
  if (!hasBackend()) return ''
  return await app.UpdateCurrentVersion()
}

// checkUpdate 拉取发布清单，返回可用更新信息；无更新或无宿主返回 null
// 整体透出后端 DTO（含 source / download_page）：徽标的「更新源」与「打开下载页」不再只依赖 update:available 事件到达的先后
export async function checkUpdate(): Promise<UpdateAvailable | null> {
  if (!hasBackend()) return null
  const [info, newer] = await app.UpdateCheck()
  return newer ? info : null
}

// applyUpdate 经后端任务引擎执行一次升级；进度/结果由 update:progress/done 事件回流
export async function applyUpdate(): Promise<void> {
  if (!hasBackend()) return
  await app.UpdateApply()
}
