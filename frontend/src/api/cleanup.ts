// 清理前端 API（T605 / §5.13.6-7-10）：孤儿扫描 + 三模式清理 + 缓存清理 + 回收站 + 审计的薄封装。
// 硬红线 4/5：读侧（扫描/列表）以后端为准；写侧（清理/恢复/清空）走后端三段式任务。
// 无宿主（纯 Vite demo）时读侧返回 null、写侧直接返回，让视图保留占位不误报。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { OrphanReport, CleanupReport, TrashEntry, Operation } from '@/types'

// scanOrphans 全量孤儿扫描（并触发后端 docker:orphan-found 广播）；无宿主返回 null
export async function scanOrphans(): Promise<OrphanReport | null> {
  if (!hasBackend()) return null
  const r = await app.CleanupScan()
  // 绑定类的 ResourceType 枚举与本地字面量联合运行时同值，显式收口为本地形状
  return {
    containers: r.containers ?? [],
    volumes: r.volumes ?? [],
    networks: r.networks ?? [],
    images: r.images ?? [],
  } as unknown as OrphanReport
}

// runCleanup 按三模式删除孤儿资源（激进涉卷删除由前端 DangerConfirm 二次确认后调用）
export async function runCleanup(mode: string): Promise<CleanupReport | null> {
  if (!hasBackend()) return null
  const r = await app.CleanupRun(mode)
  return {
    mode: r.mode,
    items: r.items ?? [],
    removed: r.removed,
    failed: r.failed,
    freedBytes: r.freedBytes,
    trashPurged: r.trashPurged,
  } as unknown as CleanupReport
}

// cleanCache 按三模式清理离线缓存（§5.14.6）
export async function cleanCache(mode: string): Promise<number> {
  if (!hasBackend()) return 0
  const r = await app.CleanupCache(mode)
  return r.freedBytes
}

// listTrash 回收站条目（7 天保留，含是否过期）；无宿主返回 null
export async function listTrash(): Promise<TrashEntry[] | null> {
  if (!hasBackend()) return null
  const rows = await app.TrashList()
  return (rows ?? []).map((x) => ({
    id: x.id, kind: x.kind, origPath: x.origPath, trashPath: x.trashPath,
    movedAt: x.movedAt, expiresAt: x.expiresAt, expired: x.expired,
  }))
}

// restoreTrash 从回收站恢复误删条目到原位
export async function restoreTrash(id: number): Promise<void> {
  if (!hasBackend()) return
  await app.TrashRestore(id)
}

// emptyExpired 立即永久删除全部到期回收站条目，返回删除数
export async function emptyExpired(): Promise<number> {
  if (!hasBackend()) return 0
  return await app.TrashEmptyExpired()
}

// listOperations 最近审计历史（operations 表，含任务账本三项）；无宿主返回 null
export async function listOperations(limit: number): Promise<Operation[] | null> {
  if (!hasBackend()) return null
  const rows = await app.OperationList(limit)
  return (rows ?? []).map((x) => ({
    ts: x.ts, actor: x.actor, op: x.op, args: x.args,
    status: x.status, durationMs: x.durationMs, error: x.error,
    taskId: x.taskId, label: x.label, logs: x.logs,
  }))
}
