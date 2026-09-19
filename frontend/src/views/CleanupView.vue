<script setup lang="ts">
// Cleanup 面板（T605 / §5.13.6-7-10）：孤儿资源三模式清理 + 回收站入口 + 操作审计。
// 非独立 NAV 路由（§0.3 路由数=10），作为 CleanupModal 主体，从 doctor 入口打开。
// 硬红线 4/5：清理/缓存清理是写操作走后端三段式；激进含数据卷删除、缓存清理均须 DangerConfirm 二次确认（§5.14.13 / 规则 20）。
import { computed, onMounted, ref } from 'vue'
import { useModalStore } from '@/stores/modalStore'
import DangerConfirm from '@/components/business/DangerConfirm.vue'
import TrashViewer from '@/components/business/TrashViewer.vue'
import { useI18n } from '@/composables/useI18n'
import { useCleanup, humanSize } from '@/composables/useCleanup'
import type { CleanupMode, DockerResource, DockerResourceType } from '@/types'

const { t } = useI18n()
const modal = useModalStore()
const { store, orphanTotal, scan, clean, purgeCache, loadOperations } = useCleanup()

const mode = ref<CleanupMode>('standard')
const cacheMode = ref<CleanupMode>('conservative')
const showTrash = ref(false)

const MODES: CleanupMode[] = ['conservative', 'standard', 'aggressive']
const RES_TYPES: DockerResourceType[] = ['container', 'volume', 'network', 'image']

const groups = computed(() =>
  RES_TYPES.map((ty) => ({
    type: ty,
    items: (store.orphans[ty + 's' as keyof typeof store.orphans] as DockerResource[]) ?? [],
  })),
)

function count(ty: DockerResourceType): number {
  return groups.value.find((g) => g.type === ty)?.items.length ?? 0
}

onMounted(() => {
  void scan()
  void loadOperations()
})

// run：保守/标准直接执行；激进涉数据卷删除先危险确认（勾选方生效）
function run(): void {
  if (mode.value !== 'aggressive') {
    void clean(mode.value)
    return
  }
  modal.open(DangerConfirm, {
    title: t('danger.cleanupAggressive.title'),
    description: t('danger.cleanupAggressive.desc'),
    warnings: [{ text: t('danger.cleanupAggressive.warn1') }],
    checkbox: { label: t('danger.cleanupAggressive.check') },
    confirmLabel: t('danger.cleanupAggressive.confirm'),
    onConfirm: () => void clean('aggressive'),
  })
}

function runCache(): void {
  modal.open(DangerConfirm, {
    title: t('danger.cacheCleanup.title'),
    description: t('danger.cacheCleanup.desc', { mode: t('cleanup.mode.' + cacheMode.value) }),
    checkbox: { label: t('danger.cacheCleanup.check') },
    confirmLabel: t('danger.cacheCleanup.confirm'),
    onConfirm: () => void purgeCache(cacheMode.value),
  })
}
</script>

<template>
  <div class="cln">
    <!-- 孤儿资源 -->
    <section class="cln-sec">
      <div class="cln-sec-head">
        <strong>{{ t('cleanup.section.orphans') }}</strong>
        <button class="btn btn-sm" type="button" :disabled="store.scanning" @click="scan()">{{ store.scanning ? t('cleanup.scanning') : t('cleanup.scan') }}</button>
      </div>
      <p v-if="orphanTotal === 0" class="cln-empty">{{ t('cleanup.none') }}</p>
      <div v-else class="cln-grid">
        <div v-for="g in groups" :key="g.type" class="cln-group">
          <div class="cln-group-title">{{ t('cleanup.res.' + g.type) }} · {{ count(g.type) }}</div>
          <ul class="cln-list">
            <li v-for="r in g.items" :key="r.name" class="cln-item">
              <span class="mono">{{ r.name }}</span>
              <span class="cln-meta">
                <span v-if="r.inUse" class="pill-warn">{{ t('cleanup.inUse') }}</span>
                <span v-if="r.size">{{ humanSize(r.size) }}</span>
              </span>
            </li>
          </ul>
        </div>
      </div>
    </section>

    <!-- 三模式 -->
    <section v-if="orphanTotal > 0" class="cln-sec">
      <div class="cln-modes">
        <label v-for="m in MODES" :key="m" class="cln-mode">
          <input v-model="mode" type="radio" name="cln-mode" :value="m" />
          <span>
            <b>{{ t('cleanup.mode.' + m) }}</b>
            <em>{{ t('cleanup.mode.' + m + '.desc') }}</em>
          </span>
        </label>
      </div>
      <button class="btn btn-primary" type="button" :disabled="store.cleaning" @click="run()">
        {{ store.cleaning ? t('cleanup.cleaning') : t('cleanup.run') }}
      </button>
    </section>

    <!-- 回收站入口 -->
    <section class="cln-sec">
      <div class="cln-sec-head">
        <div>
          <strong>{{ t('cleanup.trash.title') }}</strong>
          <p class="cln-sub">{{ t('cleanup.trash.subtitle') }}</p>
        </div>
        <button class="btn btn-sm" type="button" @click="showTrash = true">{{ t('cleanup.trash.open') }}</button>
      </div>
    </section>

    <!-- 缓存清理（§5.14.6，需二次确认） -->
    <section class="cln-sec">
      <div class="cln-sec-head">
        <div>
          <strong>{{ t('cleanup.cache.title') }}</strong>
          <p class="cln-sub">{{ t('cleanup.cache.subtitle') }}</p>
        </div>
      </div>
      <div class="cln-inline">
        <select v-model="cacheMode">
          <option v-for="m in MODES" :key="m" :value="m">{{ t('cleanup.mode.' + m) }}</option>
        </select>
        <button class="btn" type="button" @click="runCache()">{{ t('cleanup.cache.run') }}</button>
      </div>
    </section>

    <!-- 操作审计 -->
    <section class="cln-sec">
      <strong>{{ t('cleanup.ops.title') }}</strong>
      <p v-if="store.operations.length === 0" class="cln-empty">{{ t('cleanup.ops.empty') }}</p>
      <ul v-else class="cln-ops">
        <li v-for="(o, i) in store.operations" :key="i" class="cln-op">
          <span class="cln-op-time">{{ o.ts ? new Date(o.ts).toLocaleString() : '' }}</span>
          <span class="chip">{{ o.op }}</span>
          <span :class="o.status === 'success' ? 'pill-ok' : 'pill-err'">{{ o.status }}</span>
        </li>
      </ul>
    </section>

    <TrashViewer v-if="showTrash" @close="showTrash = false" />
  </div>
</template>

<style scoped>
.cln { display: flex; flex-direction: column; gap: 16px; }
.cln-sec { display: flex; flex-direction: column; gap: 10px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
.cln-sec:last-child { border-bottom: none; }
.cln-sec-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.cln-sub { margin: 2px 0 0; color: var(--text-mute); font-size: 12px; }
.cln-empty { color: var(--text-mute); font-size: 13px; margin: 0; }
.cln-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px 18px; }
.cln-group-title { font-size: 12.5px; color: var(--text-mute); margin-bottom: 4px; }
.cln-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.cln-item { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; font-size: 12.5px; }
.cln-meta { display: flex; gap: 8px; color: var(--text-mute); font-size: 11.5px; }
.cln-modes { display: flex; flex-direction: column; gap: 8px; }
.cln-mode { display: flex; align-items: flex-start; gap: 8px; cursor: pointer; }
.cln-mode b { font-size: 13px; }
.cln-mode em { display: block; color: var(--text-mute); font-size: 12px; font-style: normal; }
.cln-inline { display: flex; align-items: center; gap: 10px; }
.cln-ops { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; max-height: 200px; overflow: auto; }
.cln-op { display: flex; align-items: center; gap: 10px; font-size: 12.5px; }
.cln-op-time { color: var(--text-mute); font-size: 11.5px; min-width: 150px; }
.pill-warn, .pill-ok, .pill-err { font-size: 11px; padding: 1px 7px; border-radius: 999px; }
.pill-ok { color: var(--ok); }
.pill-err { color: var(--danger); }
.pill-warn { color: var(--warn); }
</style>
