// 事件总线前端侧：§5.6 事件协议全量 17 个事件名 + 载荷类型 + 订阅通道（唯一真实来源）
// 硬红线 4：前端只订阅后端事件，从不本地乐观更新；发射由后端（Wails Event）或 M1 mock 定时器负责。
import type { StateSnapshot } from '@/types'

// 与 internal/app/emitter.go 常量一一对应，禁止漂移（§5.6 顺序按类别）
export const EVENT = {
  StateChanged: 'state:changed',
  ServiceChanged: 'service:changed',
  TaskLog: 'task:log',
  TaskProgress: 'task:progress',
  TaskDone: 'task:done',
  UpdateAvailable: 'update:available',
  UpdateProgress: 'update:progress',
  UpdateDone: 'update:done',
  DockerCleanup: 'docker:cleanup',
  DockerOrphanFound: 'docker:orphan-found',
  DockerStateDrift: 'docker:state-drift',
  CacheHit: 'cache:hit',
  CacheMiss: 'cache:miss',
  CachePromote: 'cache:promote',
  CacheCorrupted: 'cache:corrupted',
  CacheCleanup: 'cache:cleanup',
  CacheTempdirCleared: 'cache:tempdir-cleared',
} as const

export type EventName = (typeof EVENT)[keyof typeof EVENT]

// ALL_EVENTS：协议定义的全部事件名，用于订阅对账（须恰为 17）
export const ALL_EVENTS: EventName[] = [
  EVENT.StateChanged, EVENT.ServiceChanged,
  EVENT.TaskLog, EVENT.TaskProgress, EVENT.TaskDone,
  EVENT.UpdateAvailable, EVENT.UpdateProgress, EVENT.UpdateDone,
  EVENT.DockerCleanup, EVENT.DockerOrphanFound, EVENT.DockerStateDrift,
  EVENT.CacheHit, EVENT.CacheMiss, EVENT.CachePromote,
  EVENT.CacheCorrupted, EVENT.CacheCleanup, EVENT.CacheTempdirCleared,
]

// —— 载荷类型（§5.6 事件表逐行）——
export interface StateChangedPayload { snapshot: StateSnapshot }
export interface ServiceChangedPayload { kind: string; version: string; running: boolean }
export interface TaskLogPayload { id: string; level: string; text: string }
export interface TaskProgressPayload { id: string; step: number; total: number }
export interface TaskDonePayload { id: string; status: string; duration: number }
export interface UpdateAvailablePayload { version: string; changelog: string; size: number }
export interface UpdateProgressPayload { stage: string; percent: number; speed: number }
export interface UpdateDonePayload { status: string; version: string }
export interface DockerCleanupPayload { stage: string; resource: string; action: string }
export interface DockerOrphanFoundPayload { resources: unknown[] }
export interface DockerStateDriftPayload { expected: unknown; actual: unknown }
export interface CacheHitPayload { kind: string; version: string; source: string; size: number }
export interface CacheMissPayload { kind: string; version: string; action: string }
export interface CachePromotePayload { kind: string; version: string; entries: unknown[] }
export interface CacheCorruptedPayload { kind: string; version: string; entry: unknown }
export interface CacheCleanupPayload { mode: string; freed_bytes: number }
export interface CacheTempdirClearedPayload { path: string; reason: string }

// Wails v3 在运行时向页面注入 window.runtime（EventsOn/EventsOff/EventsEmit）
interface WailsRuntime {
  EventsOn?: (event: string, cb: (data: unknown) => void) => unknown
  EventsOff?: (...events: string[]) => void
  EventsEmit?: (event: string, ...data: unknown[]) => void
}
function wailsRuntime(): WailsRuntime | undefined {
  return (globalThis as { window?: { runtime?: WailsRuntime } }).window?.runtime
}

type Handler = (payload: never) => void

// 浏览器独立运行（pnpm dev，无 Wails 宿主）时的进程内总线，供 M1 mock 驱动
const bus = new Map<string, Set<Handler>>()

function onLocal(event: string, handler: Handler): void {
  let set = bus.get(event)
  if (!set) {
    set = new Set()
    bus.set(event, set)
  }
  set.add(handler)
}

function offLocal(event: string, handler: Handler): void {
  bus.get(event)?.delete(handler)
}

// emitLocal：向进程内总线投递事件（M1 mock 定时器 / 测试驱动专用，非生产发射路径）
export function emitLocal(event: EventName, payload: unknown): void {
  bus.get(event)?.forEach((h) => (h as (p: unknown) => void)(payload))
}

// onEvent：订阅一个事件；返回反订阅函数。优先走 Wails 原生事件，退化到进程内总线。
export function onEvent(event: EventName, handler: Handler): () => void {
  const rt = wailsRuntime()
  if (rt?.EventsOn) {
    rt.EventsOn(event, handler as (data: unknown) => void)
    return () => rt.EventsOff?.(event)
  }
  onLocal(event, handler)
  return () => offLocal(event, handler)
}
