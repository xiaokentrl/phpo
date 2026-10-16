// 开机自启「服务清单」前端 API：读/写 config.yaml 里勾选了自启的那些服务版本。
// 与 api/autostart.ts 是两件事——那一头管「登录时自动启动 phpo 自己」（OS 注册态），
// 这一头管「phpo 启动后自动拉起哪些服务」（config.yaml 的 autostart 标记）。
// 硬红线 4：勾选集的唯一权威在后端，前端不落本地副本、不做乐观回填——每次进页现问一次。
// 「kind/version」是这一对值在线上的唯一形状，与后端 config.JoinSvcRef 同口径。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'

export async function getAutostartServices(): Promise<string[] | null> {
  if (!hasBackend()) return null
  return await app.ServiceAutostartGet()
}

// setAutostartServices 全量覆盖勾选集；失败抛后端原话（backendMsg 现译），由调用方 toast 并重新读回
export async function setAutostartServices(refs: string[]): Promise<void> {
  if (!hasBackend()) return
  await app.ServiceAutostartSet(refs)
}
