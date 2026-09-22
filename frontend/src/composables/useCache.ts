// useCache（T606 / §5.14.10-11）：离线缓存的前端接线层。
// 订阅 6 个 cache:* 事件：命中/未命中写 marks 供徽标，提升/损坏/清理静默补刷权威列表
//（需求 6：界面随事件实时回流；硬红线 4：状态只来自后端读接口，前端不乐观更新）。
// 事件的逐行实时反馈统一进任务抽屉（taskStore.eventLine），本视图不再另立流水面板（需求 3）。
// 写操作（清理/删除）为破坏性且涉缓存，须由调用方先经 DangerConfirm 二次确认（§5.14.13 / 规则 20）后再触发。
import { EVENT, onEvent, type CacheHitPayload, type CacheMissPayload } from '@/api/events'
import { useCacheStore } from '@/stores/cacheStore'
import { listEntries, stats, verifyAll as apiVerifyAll, verifyEntry as apiVerifyEntry, cleanupCache, removeEntry, importEntry } from '@/api/offline'
import { toast } from '@/composables/useToast'
import { t } from '@/composables/useI18n'
import { humanSize } from '@/composables/useCleanup'
import type { CleanupMode, VerifyAllResult } from '@/types'

let subscribed = false
const offs: Array<() => void> = []

// pull 拉取权威列表 + 统计并写入 store。quiet=true 用于事件驱动的静默补刷：
// 后台事件到达时不打扰用户，读接口失败由下一次显式刷新兜住。
async function pull(quiet = false): Promise<boolean> {
  const s = useCacheStore()
  if (s.loading) return false
  s.loading = true
  try {
    const [list, st] = await Promise.all([listEntries(), stats()])
    if (list) s.setEntries(list)
    if (st) s.setStats(st)
    return true
  } catch (e) {
    if (!quiet) toast(String(e), 'err', 4600)
    return false
  } finally {
    s.loading = false
  }
}

// subscribeCache：注册 cache:* 事件订阅（幂等）。
export function subscribeCache(): void {
  if (subscribed) return
  subscribed = true
  const s = useCacheStore()
  offs.push(
    onEvent(EVENT.CacheHit, (p) => {
      const e = p as CacheHitPayload
      s.recordMark(e.kind, e.version, { kind: 'hit', source: e.source, size: e.size, at: Date.now() })
    }),
    onEvent(EVENT.CacheMiss, (p) => {
      const e = p as CacheMissPayload
      s.recordMark(e.kind, e.version, { kind: 'miss', action: e.action, at: Date.now() })
    }),
    // 提升 / 损坏 / 清理会改变条目与统计：随事件补刷权威数据，不在前端改写（硬红线 4）
    onEvent(EVENT.CachePromote, () => void pull(true)),
    onEvent(EVENT.CacheCorrupted, () => void pull(true)),
    onEvent(EVENT.CacheCleanup, () => void pull(true)),
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
    await pull()
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

  // doImport 手工导入任意文件为缓存条目（需求 1）：后端三段式任务，成功即补刷权威列表
  async function doImport(kind: string, version: string, extType: string, srcPath: string): Promise<boolean> {
    try {
      await importEntry(kind, version, extType, srcPath)
      toast(t('offline.import.done', { name: kind + '/' + version }), 'ok', 3200)
      await load()
      return true
    } catch (e) {
      toast(String(e), 'err', 4600)
      return false
    }
  }

  return { store: s, humanSize, load, doVerifyAll, doVerifyEntry, doCleanup, doRemove, doImport }
}
