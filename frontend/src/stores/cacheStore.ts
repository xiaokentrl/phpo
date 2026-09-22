// cacheStore（T606 / §5.14.11）：离线缓存状态仓。数据只来自后端读接口 + 6 个 cache:* 事件（硬红线 4）。
// marks：按 kind/version 记录最近一次命中/未命中，供 CacheHitBadge 实时标记安装任务；
// 缓存事件的逐行实时反馈走任务抽屉（taskStore），本仓不再持有流水（需求 3）；前端不乐观更新，写后重拉 list/stats。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { CacheEntry, CacheEventMark, CacheStats } from '@/types'

const emptyStats: CacheStats = { totalBytes: 0, entryCount: 0, imageCount: 0, extCount: 0, corrupted: 0 }

export const useCacheStore = defineStore('cache', () => {
  const entries = ref<CacheEntry[]>([])
  const stats = ref<CacheStats>({ ...emptyStats })
  const loading = ref(false)
  const marks = ref<Record<string, CacheEventMark>>({})

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

  const corruptedCount = computed(() => stats.value.corrupted)

  function reset(): void {
    entries.value = []
    stats.value = { ...emptyStats }
    marks.value = {}
  }

  return {
    entries, stats, loading, marks, corruptedCount,
    setEntries, setStats, setEntry, dropEntry, recordMark, markOf, reset,
  }
})
