// useStateSync：前端订阅后端事件、按事件落地状态的唯一入口（§5.6 / 硬红线 4）
// 取代原型 render() 全量重绘：状态变化只来自后端推送的事件，组件通过响应式 store 自动更新。
// syncState 是同一条落地路径的「主动拉取」公共入口：启动、手动同步、装机向导完成后均复用。
// 任务实时反馈（Q5）：task:log / task:progress / task:done 与快照 tasks 全部落到 taskStore，
// 队列详情、实时进度、成功/失败与失败原因因此与后端严格同步。
// 需求 6（全局实时同步收口）：17 个协议事件里除 update:progress（已由升级弹窗进度条承载）外的
// cache:* / docker:* / update:available / update:done 也逐行进抽屉日志——有运行任务归该任务，
// 无运行任务进系统日志通道；docker:state-drift 额外重取一次权威快照让界面跟着校准结果回流。
import { reactive, readonly, type DeepReadonly } from 'vue'
import { useAppState } from '@/stores/appState'
import { useTaskStore, type LineType, type TaskStatus } from '@/stores/taskStore'
import { getState, syncAll } from '@/api/state'
import {
  ALL_EVENTS, EVENT, onEvent, type EventName,
  type TaskLogPayload, type TaskProgressPayload, type TaskDonePayload,
  type CacheHitPayload, type CacheMissPayload, type CachePromotePayload,
  type CacheCorruptedPayload, type CacheCleanupPayload, type CacheTempdirClearedPayload,
  type UpdateAvailablePayload, type UpdateDonePayload,
  type DockerCleanupPayload, type DockerOrphanFoundPayload, type DockerStateDriftPayload,
} from '@/api/events'
import { humanSize } from '@/composables/useCleanup'
import { t } from '@/composables/useI18n'
import { GAP_REASON_KEYS } from '@/constants/service'
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

// eventNote：把 §5.6 协议事件如实记成一行抽屉日志——缓存 6 个（§5.14 离线优先的实时证据）、
// docker: 3 个（§5.13 清洁与漂移）、update:available / update:done 2 个（§5.9 升级）。
// update:progress 刻意不进日志：它是连续量，已由升级弹窗的进度条实时承载，逐行落日志只会淹没任务流水。
// 归属唯一的依据是后端串行队列——同一时刻至多一个运行中任务；无运行任务时由 store 落系统日志通道。
function eventNote(event: EventName, payload: unknown): void {
  const task = useTaskStore()
  switch (event) {
    case EVENT.CacheHit: {
      const e = payload as CacheHitPayload
      task.eventLine('ok', t('task.cacheHit', { kind: e.kind, version: e.version, source: e.source, size: humanSize(e.size ?? 0) }))
      return
    }
    case EVENT.CacheMiss: {
      const e = payload as CacheMissPayload
      // 点了镜像源就点名用的哪一台；没点（本机重建 / 扩展包下载）沿用不带源的那条文案
      task.eventLine('dim', e.source
        ? t('task.cacheMissSource', { kind: e.kind, version: e.version, action: e.action, source: e.source })
        : t('task.cacheMiss', { kind: e.kind, version: e.version, action: e.action }))
      return
    }
    case EVENT.CachePromote: {
      const e = payload as CachePromotePayload
      task.eventLine('ok', t('task.cachePromote', { kind: e.kind, version: e.version, n: (e.entries ?? []).length }))
      return
    }
    case EVENT.CacheCorrupted: {
      const e = payload as CacheCorruptedPayload
      task.eventLine('err', t('task.cacheCorrupted', { kind: e.kind, version: e.version }))
      return
    }
    case EVENT.CacheCleanup: {
      const e = payload as CacheCleanupPayload
      task.eventLine('ok', t('task.cacheCleanup', { mode: e.mode, size: humanSize(e.freed_bytes ?? 0) }))
      return
    }
    case EVENT.CacheTempdirCleared: {
      const e = payload as CacheTempdirClearedPayload
      task.eventLine('dim', t('task.cacheTempdir', { path: e.path, reason: e.reason }))
      return
    }
    case EVENT.DockerCleanup: {
      const e = payload as DockerCleanupPayload
      task.eventLine('dim', t('task.dockerCleanup', { stage: e.stage, resource: e.resource, action: e.action }))
      return
    }
    case EVENT.DockerOrphanFound: {
      const e = payload as DockerOrphanFoundPayload
      task.eventLine('meta', t('task.dockerOrphan', { n: (e.resources ?? []).length }))
      return
    }
    case EVENT.DockerStateDrift: {
      const e = payload as DockerStateDriftPayload
      // 全量同步点名的缺失项逐行铺开（§5.19）：一行说清「哪一样不在了」，比 expected→actual 那串比对值可读。
      // 有缺失项时不再补那行汇总——同一次漂移说两遍，等于把抽屉日志当重复输出通道。
      const gaps = e.gaps ?? []
      for (const g of gaps) {
        const reason = GAP_REASON_KEYS[g.reason] ? t(GAP_REASON_KEYS[g.reason]) : g.reason
        task.eventLine('meta', t('task.dockerGap', { kind: g.kind, version: g.version, reason, ref: g.ref }))
      }
      if (!gaps.length) {
        const detail = e.expected || e.actual ? `${String(e.expected ?? '?')} → ${String(e.actual ?? '?')}` : String(e.error ?? '')
        task.eventLine('meta', t('task.dockerDrift', { detail }))
      }
      // 漂移即「Docker 实际状态 ≢ 库里状态」：随即重取权威快照，让界面跟着校准结果走（硬红线 4）
      void syncState()
      return
    }
    case EVENT.UpdateAvailable: {
      const e = payload as UpdateAvailablePayload
      task.eventLine('meta', t('task.updateAvailable', { version: e.version, size: humanSize(e.size ?? 0) }))
      return
    }
    case EVENT.UpdateDone: {
      const e = payload as UpdateDonePayload
      const detail = e.version || e.error || ''
      task.eventLine(e.status === 'failed' ? 'err' : e.status === 'cancelled' ? 'dim' : 'ok', t('task.updateDone', { status: e.status, detail }))
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
  eventNote(event, payload)
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

// runSync：手动「同步状态」的唯一入口（侧栏按钮与 ⌘R 同源）。
// 先让后端跑一次全量校准（容器 + 基座镜像 + 扩展固化镜像，§5.19），缺失项随 docker:state-drift 逐行进抽屉；
// 校准无变化时不发事件，故其后仍拉一次权威快照——点了同步就一定看到一次落地（值只来自后端，硬红线 4）。
export async function runSync(): Promise<boolean> {
  await syncAll().catch(() => false)
  return syncState()
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
