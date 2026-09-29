// Docker 全量资源清理 API（总览页底部面板）：扫描 / 逐行重数 / 预览 / 彻底清空。
// 分层与 api/cache.ts 同口径：这一层只搬运后端给的事实，不加工数字、不在前端推「能不能删」。
// 无宿主（纯 Vite demo）时读侧返回 null、写侧照旧抛错——「拿不到」不能画成「没有」（§0.2 规则 37 同口径）。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { CleanExecuteReport, CleanPreview, CleanRequest, CleanRow, CleanScanReport } from '@/types'

// scanClean 数一遍 60 行。deep=false 只问 Docker（快）；deep=true 再去读宿主上的文件，
// 那一步要管理员授权，只在用户点「详细扫描」时做一次。
export async function scanClean(deep: boolean): Promise<CleanScanReport | null> {
  if (!hasBackend()) return null
  const r = await app.DockerCleanScan(deep)
  return (r ?? null) as unknown as CleanScanReport | null
}

// cleanHostSupported 回答「详细扫描这一颗能不能点」：macOS / Windows 上 Docker 在虚拟机里，
// 宿主那 28 行读不到东西，点了也不会多出一数（这些行照样显示，数字给「—」，不给删除按钮）。
export async function cleanHostSupported(): Promise<boolean> {
  if (!hasBackend()) return false
  return !!(await app.DockerCleanSupported())
}

// refreshCleanRow 只重数一行——授权被拒的那几行给一个逐行重试的入口，不必整页再来一遍。
export async function refreshCleanRow(key: string): Promise<CleanRow | null> {
  if (!hasBackend()) return null
  const r = await app.DockerCleanRefreshRow(key)
  return (r ?? null) as unknown as CleanRow | null
}

// previewClean 把勾中的那几行摊成具体对象清单，并发一张一次性凭据。
// 后端会拒：一行都没勾、勾到了没有安全删法的行、行名对不上——这些都原样抛给界面。
export async function previewClean(rows: string[]): Promise<CleanPreview | null> {
  if (!hasBackend()) return null
  const r = await app.DockerCleanPreview(rows)
  return (r ?? null) as unknown as CleanPreview | null
}

// executeClean 按一次性凭据删掉勾中的那些东西：一个任务、逐项一行、单颗失败不中断其余。
export async function executeClean(req: CleanRequest): Promise<CleanExecuteReport | null> {
  if (!hasBackend()) return null
  const r = await app.DockerCleanExecute({
    token: req.token,
    ids: req.ids ?? [],
    consent: !!req.consent,
    uninstall: !!req.uninstall,
  } as unknown as Parameters<typeof app.DockerCleanExecute>[0])
  return (r ?? null) as unknown as CleanExecuteReport | null
}
