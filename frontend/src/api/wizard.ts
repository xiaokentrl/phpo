// 装机向导 API（T607 / §5.13）：HomeVerify 纯只读预检（判存在 + 判可写，不建目录不落文件），HomeEnsure 三段式建树 + 落库。
// 硬红线 4：确认后不本地乐观更新，等后端 state:changed 回流 env + dirReady。
// 无宿主（纯 Vite demo）时 homeVerify 返回 null、homeEnsure 直接返回，让向导回退到本地占位行为。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { HomeVerifyResult } from '@/types'

// homeVerify 只读预检工作目录子树（不创建任何目录/文件），返回逐条预检行与错误行；无宿主返回 null
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

// getHomeDefaults 取后端已解析的工作目录默认值（config.yaml 的 phpo_home > ~/phpo）+ 工作目录是否已设置；无宿主返回 null
// configured=true 即「已设置」：向导只显示当前两根并禁止再次进入设置流程（禁止重复创建）
export async function getHomeDefaults(): Promise<{ home: string; www: string; configured: boolean } | null> {
  if (!hasBackend()) return null
  const m = await app.HomeDefaults()
  return { home: m?.['PHPO_HOME'] ?? '', www: m?.['WWW_ROOT'] ?? '', configured: m?.['CONFIGURED'] === 'true' }
}

// restartApp 请后端按 config.yaml 的新根重新拉起进程并退出本实例；已重启过一次时后端拒绝（防重启循环）
export async function restartApp(): Promise<void> {
  if (!hasBackend()) return
  await app.Restart()
}
