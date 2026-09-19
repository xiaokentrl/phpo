// 离线缓存视图 API（T606 / §5.14.10）：OfflineView 面向的读侧（列表/统计/校验）与写侧（清理/单条删除）。
// 硬红线 4：读侧以后端为准；写侧走后端三段式任务 + 审计。底层查找/提升原语见 api/cache.ts。
// 无宿主（纯 Vite demo）时读侧返回 null、写侧直接返回，让视图保留占位不误报。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { CacheEntry, CacheStats, VerifyResult, VerifyAllResult } from '@/types'

// listEntries 遍历全部缓存条目（含 manifest 聚合计数与校验态）；无宿主返回 null
export async function listEntries(): Promise<CacheEntry[] | null> {
  if (!hasBackend()) return null
  return (await app.OfflineListEntries()) as unknown as CacheEntry[]
}

// getEntry 读取单条缓存条目（不存在返回 null）
export async function getEntry(kind: string, version: string): Promise<CacheEntry | null> {
  if (!hasBackend()) return null
  const r = await app.OfflineGetEntry(kind, version)
  return (r ?? null) as unknown as CacheEntry | null
}

// stats 缓存根目录总览（占用 / 条目 / 镜像 / 扩展 / 损坏数）
export async function stats(): Promise<CacheStats | null> {
  if (!hasBackend()) return null
  return (await app.OfflineStats()) as unknown as CacheStats
}

// verifyEntry 逐文件重校验单条缓存（后端顺带发射 cache:corrupted）
export async function verifyEntry(kind: string, version: string): Promise<VerifyResult | null> {
  if (!hasBackend()) return null
  const r = await app.OfflineVerifyEntry(kind, version)
  return { kind: r.kind, version: r.version, ok: r.ok, failed: r.failed ?? [] } as unknown as VerifyResult
}

// verifyAll 全量校验所有缓存条目，返回汇总与明细
export async function verifyAll(): Promise<VerifyAllResult | null> {
  if (!hasBackend()) return null
  const r = await app.OfflineVerifyAll()
  return {
    total: r.total, ok: r.ok, failed: r.failed,
    entries: (r.entries ?? []).map((e) => ({ kind: e.kind, version: e.version, ok: e.ok, failed: e.failed ?? [] })),
  } as unknown as VerifyAllResult
}

// cleanupCache 按三模式清理缓存（§5.14.6），返回释放字节；涉删除由前端 DangerConfirm 二次确认后调用
export async function cleanupCache(mode: string): Promise<number> {
  if (!hasBackend()) return 0
  const r = await app.OfflineCleanupCache(mode)
  return r.freedBytes
}

// removeEntry 删除单条 {kind}/{version} 缓存目录（不可恢复，前端确认后调用）
export async function removeEntry(kind: string, version: string): Promise<void> {
  if (!hasBackend()) return
  await app.OfflineRemoveEntry(kind, version)
}
