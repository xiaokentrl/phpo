// 离线缓存底层能力 API（T606 / §5.14.10）：查找 / 提升 / 临时目录清空——供安装管道与详情弹窗复用。
// 与面向视图的 api/offline.ts（列表/统计/校验/清理/删除）分层：本文件是缓存原语。
// 无宿主（纯 Vite demo）时读侧返回 null、写侧直接返回。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { ImageCacheResult, ExtCacheResult } from '@/types'

// lookupImage 查镜像缓存命中情况（后端命中前已校验 SHA256）
export async function lookupImage(kind: string, version: string): Promise<ImageCacheResult | null> {
  if (!hasBackend()) return null
  const r = await app.OfflineLookupImage(kind, version)
  return { hit: r.hit, path: r.path, size: r.size, corrupted: r.corrupted } as unknown as ImageCacheResult
}

// lookupExtension 查扩展包（apk/pecl）缓存命中情况
export async function lookupExtension(phpVersion: string, extType: string, name: string): Promise<ExtCacheResult | null> {
  if (!hasBackend()) return null
  const r = await app.OfflineLookupExtension(phpVersion, extType, name)
  return { hit: r.hit, path: r.path, size: r.size, corrupted: r.corrupted } as unknown as ExtCacheResult
}

// promoteImage 把临时镜像 tar 提升到离线缓存
export async function promoteImage(kind: string, version: string, tarPath: string): Promise<void> {
  if (!hasBackend()) return
  await app.OfflinePromoteImage(kind, version, tarPath)
}

// promoteExtension 把临时扩展包提升到离线缓存
export async function promoteExtension(phpVersion: string, extType: string, filePath: string): Promise<void> {
  if (!hasBackend()) return
  await app.OfflinePromoteExtension(phpVersion, extType, filePath)
}

// clearTempDir 清空指定 kind/version 临时目录（发射 cache:tempdir-cleared）
export async function clearTempDir(kind: string, version: string, reason: string): Promise<void> {
  if (!hasBackend()) return
  await app.OfflineClearTempDir(kind, version, reason)
}
