<script setup lang="ts">
// 缓存详情弹窗（T606 / §5.14.7）：展开单个 {kind}/{version} 缓存的 manifest 明细——镜像 tar + apk/pecl 包计数、
// 绝对路径、占用、校验态，以及镜像查找命中结果。硬红线 4：只读后端接口，不改数据。
import { onMounted, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import CacheHitBadge from '@/components/common/CacheHitBadge.vue'
import { useI18n } from '@/composables/useI18n'
import { useCache } from '@/composables/useCache'
import { getEntry } from '@/api/offline'
import { lookupImage } from '@/api/cache'
import { humanSize } from '@/composables/useCleanup'
import type { CacheEntry, ImageCacheResult } from '@/types'

const props = defineProps<{ kind: string; version: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { store } = useCache()

const entry = ref<CacheEntry | null>(null)
const img = ref<ImageCacheResult | null>(null)
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    entry.value = await getEntry(props.kind, props.version)
    img.value = await lookupImage(props.kind, props.version)
  } finally {
    loading.value = false
  }
})

function fmt(ts: string): string {
  if (!ts || ts.startsWith('0001-')) return '-'
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString()
}
</script>

<template>
  <ModalShell @close="emit('close')">
    <template #head>
      <h3>{{ t('offline.detail.title') }} · {{ kind }} {{ version }}</h3>
      <p>{{ t('offline.detail.subtitle') }}</p>
    </template>
    <template #body>
      <div v-if="loading" class="cd-loading">{{ t('common.loading') }}</div>
      <div v-else-if="!entry" class="cd-empty">{{ t('offline.detail.missing') }}</div>
      <dl v-else class="cd-list">
        <div class="cd-row"><dt>{{ t('offline.col.path') }}</dt><dd class="mono cd-path">{{ entry.path }}</dd></div>
        <div class="cd-row"><dt>{{ t('offline.col.size') }}</dt><dd>{{ humanSize(entry.totalSize) }}</dd></div>
        <div class="cd-row">
          <dt>{{ t('offline.detail.image') }}</dt>
          <dd>
            <span :class="entry.hasImage ? 'pill-ok' : 'pill-off'">{{ entry.hasImage ? t('offline.detail.hasImage') : t('offline.detail.noImage') }}</span>
            <CacheHitBadge :kind="kind" :version="version" />
          </dd>
        </div>
        <div v-if="kind === 'php'" class="cd-row">
          <dt>{{ t('offline.detail.extImage') }}</dt>
          <dd>
            <span :class="entry.hasExtImage ? 'pill-ok' : 'pill-off'">{{ entry.hasExtImage ? t('offline.detail.hasImage') : t('offline.detail.noImage') }}</span>
          </dd>
        </div>
        <div class="cd-row"><dt>{{ t('offline.detail.apk') }}</dt><dd>{{ entry.apkCount }}</dd></div>
        <div class="cd-row"><dt>{{ t('offline.detail.pecl') }}</dt><dd>{{ entry.peclCount }}</dd></div>
        <div class="cd-row">
          <dt>{{ t('offline.col.lastVerify') }}</dt>
          <dd>
            <span :class="entry.verifyOk ? 'pill-ok' : 'pill-err'">{{ entry.verifyOk ? t('offline.verify.ok') : t('offline.verify.bad') }}</span>
            <span class="cd-time">{{ fmt(entry.lastVerify) }}</span>
          </dd>
        </div>
        <div v-if="img && img.corrupted" class="cd-warn">{{ t('offline.detail.corruptedHint') }}</div>
      </dl>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.close') }}</button>
    </template>
  </ModalShell>
</template>

<style scoped>
.cd-loading, .cd-empty { color: var(--text-mute); font-size: 13px; padding: 8px 0; }
.cd-list { margin: 0; display: flex; flex-direction: column; gap: 8px; }
.cd-row { display: flex; align-items: baseline; gap: 12px; font-size: 13px; }
.cd-row dt { width: 110px; color: var(--text-mute); flex-shrink: 0; }
.cd-row dd { margin: 0; flex: 1; display: flex; align-items: center; gap: 8px; }
.cd-path { word-break: break-all; font-size: 11.5px; color: var(--text-dim); }
.cd-time { color: var(--text-mute); font-size: 12px; }
.cd-warn { color: var(--danger); font-size: 12.5px; }
.pill-ok { color: var(--ok); font-size: 11px; padding: 1px 7px; border-radius: 999px; }
.pill-err { color: var(--danger); font-size: 11px; padding: 1px 7px; border-radius: 999px; }
.pill-off { color: var(--text-mute); font-size: 11px; padding: 1px 7px; border-radius: 999px; }
</style>
