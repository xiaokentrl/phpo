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

// UI_COMMAND 与内部 ui:command 广播对齐（§5.6 之外的 UI→UI 本地命令，见 internal/ui/menu.go）
export const UI_COMMAND = 'ui:command'
export const UICmd = {
  OpenLogs: 'openLogs',
  BackupNow: 'backupNow',
  Quit: 'quit',
} as const
export type UICmdName = (typeof UICmd)[keyof typeof UICmd]

// —— 载荷类型（§5.6 事件表逐行）——
export interface StateChangedPayload { snapshot: StateSnapshot }
export interface ServiceChangedPayload { kind: string; version: string; running: boolean }
export interface TaskLogPayload { id: string; level: string; text: string }
export interface TaskProgressPayload { id: string; step: number; total: number }
export interface TaskDonePayload { id: string; status: string; duration: number }
export interface UpdateAvailablePayload { version: string; changelog: string; size: number }
export interface UpdateProgressPayload { stage: string; percent: number; speed: number }
export interface UpdateDonePayload { status: string; version?: string; error?: string }
export interface DockerCleanupPayload { stage: string; resource: string; action: string }
export interface DockerOrphanFoundPayload { resources: unknown[] }
// error 是启动校准失败这一路的实际载荷（internal/app/di.go）：拿不到比对值只有一个 error 字符串，
// 协议表里的 expected / actual 此时缺席，故两者皆为可选。
export interface DockerStateDriftPayload { expected?: unknown; actual?: unknown; error?: string }
export interface CacheHitPayload { kind: string; version: string; source: string; size: number }
export interface CacheMissPayload { kind: string; version: string; action: string }
export interface CachePromotePayload { kind: string; version: string; entries: unknown[] }
export interface CacheCorruptedPayload { kind: string; version: string; entry: unknown }
export interface CacheCleanupPayload { mode: string; freed_bytes: number }
export interface CacheTempdirClearedPayload { path: string; reason: string }

// Wails v3 不注入 v2 的 window.runtime 全局：宿主把事件送到 window._wails.dispatchWailsEvent，
// 由 @wailsio/runtime 的 Events 分发给监听器（大载荷 >8KB 走 payload store 取回，也只有官方 Events 处理）。
// 故订阅必须走 Events.On；进程内总线仅服务无宿主的纯 Vite demo。
import { Events } from '@wailsio/runtime'

type Handler = (payload: unknown) => void

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
export function emitLocal(event: string, payload: unknown): void {
  bus.get(event)?.forEach((h) => h(payload))
}

// onEvent：订阅一个事件，返回反订阅函数。同时挂 Wails 宿主通道与进程内总线——
// 前者只在真实宿主触发，后者只被 DEV mock（__phpoMock）触发，两者不同时发射，不会重复投递。
export function onEvent(event: string, handler: Handler): () => void {
  const offRuntime = Events.On(event, (ev) => handler(ev.data))
  onLocal(event, handler)
  return () => {
    offRuntime()
    offLocal(event, handler)
  }
}
