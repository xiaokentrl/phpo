// cacheStore（T606 / §5.14.11）：离线缓存状态仓。数据只来自后端读接口 + 6 个 cache:* 事件（硬红线 4）。
// marks：按 kind/version 记录最近一次命中/未命中，供 CacheHitBadge 实时标记安装任务；
// events：6 类缓存事件的滚动流水（要点「全订阅展示」）；前端不乐观更新，写后重拉 list/stats。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { CacheEntry, CacheEventMark, CacheStats } from '@/types'

const emptyStats: CacheStats = { totalBytes: 0, entryCount: 0, imageCount: 0, extCount: 0, corrupted: 0 }

export interface CacheEventLog {
  name: string
  kind: string
  version: string
  detail: string
  at: number
}

export const useCacheStore = defineStore('cache', () => {
  const entries = ref<CacheEntry[]>([])
  const stats = ref<CacheStats>({ ...emptyStats })
  const loading = ref(false)
  const marks = ref<Record<string, CacheEventMark>>({})
  const events = ref<CacheEventLog[]>([])

  function setEntries(list: CacheEntry[]): void {
    entries.value = list
  }
  function setStats(s: CacheStats): void {
    stats.value = s
  }
  function setEntry(e: CacheEntry): void {
    const i = entries.value.findIndex((x) => x.kind === e.kind && x.version === e.version)
    if (i >= 0) entries.value[i] = e
    else entries.value.push(e)
  }
  function dropEntry(kind: string, version: string): void {
    entries.value = entries.value.filter((x) => !(x.kind === kind && x.version === version))
  }

  const keyOf = (kind: string, version: string): string => `${kind}/${version}`

  // recordMark 更新某 kind/version 的最近命中/未命中标记（CacheHitBadge 依据）
  function recordMark(kind: string, version: string, mark: CacheEventMark): void {
    marks.value = { ...marks.value, [keyOf(kind, version)]: mark }
  }
  function markOf(kind: string, version: string): CacheEventMark | undefined {
    return marks.value[keyOf(kind, version)]
  }

  // pushEvent 追加一条缓存事件流水（上限 100，防无界增长）
  function pushEvent(ev: CacheEventLog): void {
    events.value = [ev, ...events.value].slice(0, 100)
  }

  const corruptedCount = computed(() => stats.value.corrupted)

  function reset(): void {
    entries.value = []
    stats.value = { ...emptyStats }
    marks.value = {}
    events.value = []
  }

  return {
    entries, stats, loading, marks, events, corruptedCount,
    setEntries, setStats, setEntry, dropEntry, recordMark, markOf, pushEvent, reset,
  }
})
