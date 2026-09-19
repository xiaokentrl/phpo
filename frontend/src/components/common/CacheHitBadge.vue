<script setup lang="ts">
// CacheHitBadge（T606 / §5.14.11）：安装任务里实时标记某 kind/version 是否命中离线缓存。
// 数据源为后端 cache:hit / cache:miss 事件（硬红线 4：前端只看事件落地，不自判）。
// 无事件时静默（不占位），命中绿色、未命中走网络为琥珀色。
import { computed } from 'vue'
import { useCacheStore } from '@/stores/cacheStore'
import { useI18n } from '@/composables/useI18n'

const props = defineProps<{ kind: string; version: string }>()
const { t } = useI18n()
const store = useCacheStore()

const mark = computed(() => store.marks[`${props.kind}/${props.version}`])
const state = computed(() => (mark.value ? mark.value.kind : ''))
</script>

<template>
  <span v-if="state === 'hit'" class="chb chb-hit" :title="t('offline.badge.hitTip', { source: mark?.source ?? 'offline' })">
    ⚡ {{ t('offline.badge.hit') }}
  </span>
  <span v-else-if="state === 'miss'" class="chb chb-miss" :title="t('offline.badge.missTip')">
    🌐 {{ t('offline.badge.miss') }}
  </span>
</template>

<style scoped>
.chb {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 999px;
  white-space: nowrap;
}
.chb-hit { color: var(--ok); background: var(--ok-bg, rgba(46, 160, 67, 0.12)); }
.chb-miss { color: var(--warn); background: var(--warn-bg, rgba(219, 171, 9, 0.12)); }
</style>
