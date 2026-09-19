// 启动权威快照拉取（T607 / 硬红线 4）：真实宿主下前端挂载后主动向后端取一次全量快照并落地，
// 使 dirReady / env / installed 等以 DB 为唯一权威（新用户首启 dirReady 缺 PHPO_HOME → NEEDS_HOME 全拦截 → 弹向导）。
// 无宿主（纯 Vite demo）返回 null，保留占位数据与 mock 事件驱动，不覆盖。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { StateSnapshot } from '@/types'

// getState 拉取后端当前权威快照；无宿主返回 null
export async function getState(): Promise<StateSnapshot | null> {
  if (!hasBackend()) return null
  const s = await app.GetState()
  return (s ?? null) as unknown as StateSnapshot | null
}
