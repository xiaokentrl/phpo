// useStateSync：前端订阅后端事件、按事件落地状态的唯一入口（§5.6 / 硬红线 4）
// 取代原型 render() 全量重绘：状态变化只来自后端推送的事件，组件通过响应式 store 自动更新。
// syncState 是同一条落地路径的「主动拉取」公共入口：启动、手动同步、装机向导完成后均复用。
// 任务实时反馈（Q5）：task:log / task:progress / task:done 与快照 tasks 全部落到 taskStore，
// 队列详情、实时进度、成功/失败与失败原因因此与后端严格同步；cache:* 也如实记入当前任务日志。
import { reactive, readonly, type DeepReadonly } from 'vue'
import { useAppState } from '@/stores/appState'
import { useTaskStore, type LineType, type TaskStatus } from '@/stores/taskStore'
import { getState } from '@/api/state'
import {
  ALL_EVENTS, EVENT, onEvent, type EventName,
  type TaskLogPayload, type TaskProgressPayload, type TaskDonePayload,
  type CacheHitPayload, type CacheMissPayload, type CachePromotePayload,
  type CacheCorruptedPayload, type CacheCleanupPayload, type CacheTempdirClearedPayload,
} from '@/api/events'
import { humanSize } from '@/composables/useCleanup'
import { t } from '@/composables/useI18n'
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

// cacheNote：把 6 个 cache:* 事件转成当前任务的一行可读日志（§5.14 离线优先的实时证据）。
// 归属唯一的依据是后端串行队列——同一时刻至多一个运行中任务；无运行任务时由 store 丢弃。
function cacheNote(event: EventName, payload: unknown): void {
  const task = useTaskStore()
  switch (event) {
    case EVENT.CacheHit: {
      const e = payload as CacheHitPayload
      task.cacheNote('ok', t('task.cacheHit', { kind: e.kind, version: e.version, source: e.source, size: humanSize(e.size ?? 0) }))
      return
    }
    case EVENT.CacheMiss: {
      const e = payload as CacheMissPayload
      task.cacheNote('dim', t('task.cacheMiss', { kind: e.kind, version: e.version, action: e.action }))
      return
    }
    case EVENT.CachePromote: {
      const e = payload as CachePromotePayload
      task.cacheNote('ok', t('task.cachePromote', { kind: e.kind, version: e.version, n: (e.entries ?? []).length }))
      return
    }
    case EVENT.CacheCorrupted: {
      const e = payload as CacheCorruptedPayload
      task.cacheNote('err', t('task.cacheCorrupted', { kind: e.kind, version: e.version }))
      return
    }
    case EVENT.CacheCleanup: {
      const e = payload as CacheCleanupPayload
      task.cacheNote('ok', t('task.cacheCleanup', { mode: e.mode, size: humanSize(e.freed_bytes ?? 0) }))
      return
    }
    case EVENT.CacheTempdirCleared: {
      const e = payload as CacheTempdirClearedPayload
      task.cacheNote('dim', t('task.cacheTempdir', { path: e.path, reason: e.reason }))
      return
    }
  }
}

// landEvent：按事件把后端事实落地到对应 store（唯一落地处，绝不本地乐观更新）。
function landEvent(event: EventName, payload: unknown): void {
  const app = useAppState()
  const task = useTaskStore()
  if (event === EVENT.StateChanged) {
    const p = payload as { snapshot?: import('@/types').StateSnapshot }
    if (!p?.snapshot) return
    app.applySnapshot(p.snapshot)
    task.syncBoard(p.snapshot.tasks) // 队列详情实时落地：新任务开始即自动跟随并展开抽屉
    return
  }
  if (event === EVENT.ServiceChanged) {
    const p = payload as { kind?: string; version?: string; running?: boolean }
    if (p?.kind && p?.version) app.setServiceRunning(p.kind as ServiceKind, p.version, !!p.running)
    return
  }
  if (event === EVENT.TaskLog) {
    const p = payload as TaskLogPayload
    task.appendLog(p.id, p.level as LineType, p.text)
    return
  }
  if (event === EVENT.TaskProgress) {
    const p = payload as TaskProgressPayload
    task.setProgress(p.id, p.step, p.total)
    return
  }
  if (event === EVENT.TaskDone) {
    const p = payload as TaskDonePayload
    task.finish(p.id, p.status as TaskStatus, p.duration)
    return
  }
  cacheNote(event, payload)
}

// startStateSync：注册全部 17 个事件订阅；幂等，重复调用不叠加监听。
export function startStateSync(): void {
  if (started) return
  started = true

  for (const event of ALL_EVENTS) {
    const off = onEvent(event, (payload: unknown) => {
      record(event, payload)
      landEvent(event, payload)
    })
    unsubscribers.push(off)
  }
  // 任务记录跨重启可见：账本历史在订阅建立后异步补齐（失败静默，不影响实时通道）
  void useTaskStore().loadHistory()
}

// syncState：主动同步状态——向后端取一次权威快照并落地（与 state:changed 走同一条 landEvent 落地路径，
// 含队列详情 tasks；硬红线 4：不本地乐观更新）。
// 返回是否取到并落地；无宿主或后端报错返回 false，调用方据此决定兜底（如装机向导刷新失败即重启）。
export async function syncState(): Promise<boolean> {
  const snap = await getState().catch(() => null)
  if (!snap) return false
  landEvent(EVENT.StateChanged, { snapshot: snap })
  return true
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
