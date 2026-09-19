// useCache（T606 / §5.14.10-11）：离线缓存的前端接线层。
// 订阅 6 个 cache:* 事件落地 cacheStore（硬红线 4：状态只来自后端）；提供统计/全部校验/清理/删除/清临时动作。
// 写操作（清理/删除）为破坏性且涉缓存，须由调用方先经 DangerConfirm 二次确认（§5.14.13 / 规则 20）后再触发。
import { EVENT, onEvent, type CacheHitPayload, type CacheMissPayload, type CachePromotePayload, type CacheCorruptedPayload, type CacheCleanupPayload, type CacheTempdirClearedPayload } from '@/api/events'
import { useCacheStore } from '@/stores/cacheStore'
import { listEntries, stats, verifyAll as apiVerifyAll, verifyEntry as apiVerifyEntry, cleanupCache, removeEntry } from '@/api/offline'
import { toast } from '@/composables/useToast'
import { t } from '@/composables/useI18n'
import { humanSize } from '@/composables/useCleanup'
import type { CleanupMode, VerifyAllResult } from '@/types'

let subscribed = false
const offs: Array<() => void> = []

// subscribeCache：注册 6 个 cache:* 事件订阅（幂等）。命中/未命中写入 marks 供 CacheHitBadge 实时标记。
export function subscribeCache(): void {
  if (subscribed) return
  subscribed = true
  const s = useCacheStore()
  offs.push(
    onEvent(EVENT.CacheHit, (p) => {
      const e = p as CacheHitPayload
      s.recordMark(e.kind, e.version, { kind: 'hit', source: e.source, size: e.size, at: Date.now() })
      s.pushEvent({ name: 'cache:hit', kind: e.kind, version: e.version, detail: `${e.source} · ${humanSize(e.size)}`, at: Date.now() })
    }),
    onEvent(EVENT.CacheMiss, (p) => {
      const e = p as CacheMissPayload
      s.recordMark(e.kind, e.version, { kind: 'miss', action: e.action, at: Date.now() })
      s.pushEvent({ name: 'cache:miss', kind: e.kind, version: e.version, detail: e.action, at: Date.now() })
    }),
    onEvent(EVENT.CachePromote, (p) => {
      const e = p as CachePromotePayload
      s.pushEvent({ name: 'cache:promote', kind: e.kind, version: e.version, detail: `${(e.entries ?? []).length}`, at: Date.now() })
    }),
    onEvent(EVENT.CacheCorrupted, (p) => {
      const e = p as CacheCorruptedPayload
      const name = (e.entry as { name?: string } | undefined)?.name ?? ''
      s.pushEvent({ name: 'cache:corrupted', kind: e.kind, version: e.version, detail: name, at: Date.now() })
    }),
    onEvent(EVENT.CacheCleanup, (p) => {
      const e = p as CacheCleanupPayload
      s.pushEvent({ name: 'cache:cleanup', kind: '', version: '', detail: `${e.mode} · ${humanSize(e.freed_bytes)}`, at: Date.now() })
    }),
    onEvent(EVENT.CacheTempdirCleared, (p) => {
      const e = p as CacheTempdirClearedPayload
      s.pushEvent({ name: 'cache:tempdir-cleared', kind: '', version: '', detail: `${e.reason}`, at: Date.now() })
    }),
  )
}

export function unsubscribeCache(): void {
  offs.forEach((off) => off())
  offs.length = 0
  subscribed = false
}

export function useCache() {
  const s = useCacheStore()

  // load 拉取权威列表 + 统计；打开视图与写完成后调用（无乐观更新）
  async function load(): Promise<void> {
    if (s.loading) return
    s.loading = true
    try {
      const [list, st] = await Promise.all([listEntries(), stats()])
      if (list) s.setEntries(list)
      if (st) s.setStats(st)
    } catch (e) {
      toast(String(e), 'err', 4600)
    } finally {
      s.loading = false
    }
  }

  // doVerifyAll 全量校验；检出损坏项后重拉列表（verifyOk 会变）
  async function doVerifyAll(): Promise<VerifyAllResult | null> {
    try {
      const r = await apiVerifyAll()
      if (!r) return null
      if (r.failed > 0) toast(t('offline.verifyFail', { n: r.failed }), 'err', 4200)
      else toast(t('offline.verifyPass', { n: r.ok }), 'ok', 3200)
      await load()
      return r
    } catch (e) {
      toast(String(e), 'err', 4600)
      return null
    }
  }

  async function doVerifyEntry(kind: string, version: string): Promise<void> {
    try {
      const r = await apiVerifyEntry(kind, version)
      if (!r) return
      toast(r.ok ? t('offline.verify.ok') : t('offline.verifyFail', { n: r.failed.length }), r.ok ? 'ok' : 'err', 3200)
      await load()
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  async function doCleanup(mode: CleanupMode): Promise<void> {
    try {
      const freed = await cleanupCache(mode)
      toast(t('offline.cleanupDone', { size: humanSize(freed) }), 'ok', 3200)
      await load()
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  async function doRemove(kind: string, version: string): Promise<void> {
    try {
      await removeEntry(kind, version)
      toast(t('offline.removed'), 'ok', 2600)
      await load()
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  return { store: s, humanSize, load, doVerifyAll, doVerifyEntry, doCleanup, doRemove }
}
