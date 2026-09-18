// useStateSync：前端订阅后端事件、按事件落地状态的唯一入口（§5.6 / 硬红线 4）
// 取代原型 render() 全量重绘：状态变化只来自后端推送的事件，组件通过响应式 store 自动更新。
import { reactive, readonly, type DeepReadonly } from 'vue'
import { useAppState } from '@/stores/appState'
import { ALL_EVENTS, EVENT, onEvent, type EventName } from '@/api/events'
import type { ServiceKind } from '@/types'

export interface ReceivedRecord {
  count: number
  at: number
  payload: unknown
}

// 每个事件名的接收审计（验证「17 事件名收发」；亦是未来各 store 接线的观察点）
const received = reactive<Record<string, ReceivedRecord>>({})
let unsubscribers: Array<() => void> = []
let started = false

function record(event: EventName, payload: unknown): void {
  const cur = received[event]
  if (cur) {
    cur.count += 1
    cur.at = Date.now()
    cur.payload = payload
  } else {
    received[event] = { count: 1, at: Date.now(), payload }
  }
}

// startStateSync：注册全部 17 个事件订阅；幂等，重复调用不叠加监听。
export function startStateSync(): void {
  if (started) return
  started = true
  const app = useAppState()

  for (const event of ALL_EVENTS) {
    const off = onEvent(event, ((payload: unknown) => {
      record(event, payload)
      // 仅对「状态权威」事件落地到 appState；其余事件 M1 记录待后续工单接线各自 store。
      if (event === EVENT.StateChanged) {
        const p = payload as { snapshot?: import('@/types').StateSnapshot }
        if (p?.snapshot) app.applySnapshot(p.snapshot)
      } else if (event === EVENT.ServiceChanged) {
        const p = payload as { kind?: string; version?: string; running?: boolean }
        if (p?.kind && p?.version) app.setServiceRunning(p.kind as ServiceKind, p.version, !!p.running)
      }
    }) as (p: never) => void)
    unsubscribers.push(off)
  }
}

// stopStateSync：反订阅（测试/热更新用）。
export function stopStateSync(): void {
  unsubscribers.forEach((off) => off())
  unsubscribers = []
  started = false
}

export function useStateSync(): { received: DeepReadonly<Record<string, ReceivedRecord>> } {
  return { received: readonly(received) as DeepReadonly<Record<string, ReceivedRecord>> }
}

if (import.meta.env.DEV) {
  (globalThis as Record<string, unknown>).__phpoSync = { received, isStarted: () => started }
}
