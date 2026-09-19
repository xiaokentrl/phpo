<script setup lang="ts">
// Offline 视图（T606 / §5.14.7、10、11）：离线缓存真数据面板。
// 硬红线 4：条目/统计只来自后端读接口 + 6 类 cache:* 事件，前端不乐观更新；写后由 useCache 重拉列表。
// 缓存删除不可恢复且涉离线能力（§5.14.13 / 规则 20），单条删除与批量清理均先经 DangerConfirm 二次确认。
import { onMounted } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useModalStore } from '@/stores/modalStore'
import { useCache } from '@/composables/useCache'
import { humanSize } from '@/composables/useCleanup'
import { SVC_ICON } from '@/constants/service'
import PathInfoBar from '@/components/common/PathInfoBar.vue'
import CacheHitBadge from '@/components/common/CacheHitBadge.vue'
import DangerConfirm from '@/components/business/DangerConfirm.vue'
import CacheDetailModal from '@/components/business/CacheDetailModal.vue'
import CacheCleanupModal from '@/components/business/CacheCleanupModal.vue'
import type { CacheEntry } from '@/types'

const { t } = useI18n()
const state = useAppState()
const modal = useModalStore()
const { store, load, doVerifyAll, doVerifyEntry, doRemove } = useCache()

onMounted(load)

function fmt(ts: string): string {
  if (!ts || ts.startsWith('0001-')) return '-'
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString()
}

function openDetail(e: CacheEntry): void {
  modal.open(CacheDetailModal, { kind: e.kind, version: e.version })
}
function openCleanup(): void {
  modal.open(CacheCleanupModal, {})
}

// runRemove：删除单条缓存——不可恢复且影响该版本零网络安装，先危险确认（勾选方生效）再触发后端三段式。
function runRemove(e: CacheEntry): void {
  modal.open(DangerConfirm, {
    title: t('danger.cacheRemove.title', { svc: e.kind, ver: e.version }),
    description: t('danger.cacheRemove.desc'),
    warnings: [{ text: t('danger.cacheRemove.warn', { size: humanSize(e.totalSize) }) }],
    checkbox: { label: t('danger.cacheRemove.check') },
    confirmLabel: t('danger.cacheRemove.confirm'),
    onConfirm: () => void doRemove(e.kind, e.version),
  })
}
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('offline.title') }}</h1>
        <p class="view-sub">{{ t('offline.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button class="btn" type="button" :disabled="store.loading" @click="load()">{{ t('offline.refreshBtn') }}</button>
        <button class="btn" type="button" :disabled="store.loading" @click="doVerifyAll()">{{ t('offline.verifyAll') }}</button>
        <button class="btn btn-primary" type="button" @click="openCleanup()">{{ t('offline.cleanBtn') }}</button>
      </div>
    </header>

    <PathInfoBar :label="t('offline.pathLabel')" :path="state.env.OFFLINE_ROOT + '/'" />

    <div class="summary">
      <div class="summary-item"><div class="summary-num">{{ humanSize(store.stats.totalBytes) }}</div><div class="summary-label">{{ t('offline.stat.size') }}</div></div>
      <div class="summary-item"><div class="summary-num">{{ store.stats.entryCount }}</div><div class="summary-label">{{ t('offline.stat.entries') }}</div></div>
      <div class="summary-item"><div class="summary-num">{{ store.stats.imageCount }}</div><div class="summary-label">{{ t('offline.stat.images') }}</div></div>
      <div class="summary-item"><div class="summary-num">{{ store.stats.extCount }}</div><div class="summary-label">{{ t('offline.stat.exts') }}</div></div>
      <div class="summary-item"><div class="summary-num" :style="store.stats.corrupted ? 'color: var(--danger)' : 'color: var(--ok)'">{{ store.stats.corrupted }}</div><div class="summary-label">{{ t('offline.stat.corrupted') }}</div></div>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width: 15%">{{ t('offline.col.svc') }}</th>
            <th style="width: 10%">{{ t('offline.col.ver') }}</th>
            <th style="width: 27%">{{ t('offline.col.path') }}</th>
            <th style="width: 9%">{{ t('offline.col.size') }}</th>
            <th style="width: 9%">{{ t('offline.col.items') }}</th>
            <th style="width: 14%">{{ t('offline.col.lastVerify') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="store.entries.length === 0"><td colspan="7" class="off-empty">{{ t('offline.empty') }}</td></tr>
          <tr v-for="e in store.entries" :key="`${e.kind}/${e.version}`">
            <td>
              <div class="svc-cell">
                <span class="svc-icon">{{ SVC_ICON[e.kind] || '📦' }}</span>{{ e.kind }}
                <CacheHitBadge :kind="e.kind" :version="e.version" />
              </div>
            </td>
            <td><span class="chip chip-accent">{{ e.version }}</span></td>
            <td><code class="mono off-path" :title="e.path">{{ e.path }}</code></td>
            <td><span class="mono" style="color: var(--text-dim)">{{ humanSize(e.totalSize) }}</span></td>
            <td><span class="mono" style="color: var(--text-mute)">{{ e.apkCount + e.peclCount }}</span></td>
            <td>
              <span :class="e.verifyOk ? 'pill-ok' : 'pill-err'">{{ e.verifyOk ? t('offline.verify.ok') : t('offline.verify.bad') }}</span>
              <span class="mono off-time">{{ fmt(e.lastVerify) }}</span>
            </td>
            <td>
              <div class="row-actions">
                <button class="btn btn-sm" type="button" @click="doVerifyEntry(e.kind, e.version)">{{ t('offline.verify') }}</button>
                <button class="btn btn-sm" type="button" @click="openDetail(e)">{{ t('offline.detailBtn') }}</button>
                <button class="btn btn-sm btn-danger" type="button" @click="runRemove(e)">{{ t('offline.deleteBtn') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 实时缓存事件（§5.14.11：6 类 cache:* 事件全订阅展示） -->
    <section class="off-events">
      <strong>{{ t('offline.events.title') }}</strong>
      <p v-if="store.events.length === 0" class="off-empty">{{ t('offline.events.empty') }}</p>
      <ul v-else class="off-feed">
        <li v-for="(ev, i) in store.events" :key="i" class="off-feed-item">
          <span class="off-feed-time">{{ new Date(ev.at).toLocaleTimeString() }}</span>
          <span class="chip">{{ ev.name }}</span>
          <span v-if="ev.kind" class="mono">{{ ev.kind }}{{ ev.version ? '/' + ev.version : '' }}</span>
          <span class="off-feed-detail">{{ ev.detail }}</span>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped>
.off-empty { color: var(--text-mute); font-size: 13px; padding: 8px 0; text-align: left; }
.off-path { font-size: 11px; color: var(--text-dim); word-break: break-all; line-height: 1.4; }
.off-time { display: block; color: var(--text-mute); font-size: 11.5px; margin-top: 2px; }
.off-events { margin-top: 18px; display: flex; flex-direction: column; gap: 8px; }
.off-feed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; max-height: 220px; overflow: auto; }
.off-feed-item { display: flex; align-items: center; gap: 10px; font-size: 12.5px; }
.off-feed-time { color: var(--text-mute); font-size: 11.5px; min-width: 78px; }
.off-feed-detail { color: var(--text-dim); }
.pill-ok { color: var(--ok); font-size: 11px; padding: 1px 7px; border-radius: 999px; }
.pill-err { color: var(--danger); font-size: 11px; padding: 1px 7px; border-radius: 999px; }
</style>
