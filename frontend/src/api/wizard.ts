// 装机向导 API（T607 / §5.13）：HomeVerify 纯探测（试建子树 + 可写检查），HomeEnsure 三段式落库。
// 硬红线 4：确认后不本地乐观更新，等后端 state:changed 回流 env + dirReady。
// 无宿主（纯 Vite demo）时 homeVerify 返回 null、homeEnsure 直接返回，让向导回退到本地占位行为。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { HomeVerifyResult } from '@/types'

// homeVerify 校验并试建工作目录子树，返回逐条进度与错误；无宿主返回 null
export async function homeVerify(home: string, www: string): Promise<HomeVerifyResult | null> {
  if (!hasBackend()) return null
  const r = await app.HomeVerify(home, www)
  return { ok: r.ok, lines: r.lines ?? [], errors: r.errors ?? [] }
}

// homeEnsure 装机确认：后端建子树 + 写 env + 置 dirReady + 广播 state:changed；无宿主直接返回
export async function homeEnsure(home: string, www: string): Promise<void> {
  if (!hasBackend()) return
  await app.HomeEnsure(home, www)
}

// getHomeDefaults 取后端已解析的工作目录默认值（config.yaml 的 phpo_home > ~/phpo）；无宿主返回 null
export async function getHomeDefaults(): Promise<{ home: string; www: string } | null> {
  if (!hasBackend()) return null
  const m = await app.HomeDefaults()
  return { home: m?.['PHPO_HOME'] ?? '', www: m?.['WWW_ROOT'] ?? '' }
}
