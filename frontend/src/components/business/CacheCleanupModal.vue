<script setup lang="ts">
// 缓存清理弹窗（T606 / §5.14.6）：三模式清理离线缓存。涉缓存删除不可恢复，须 DangerConfirm 二次确认后执行
// （§5.14.13 / 规则 20）。执行走后端三段式任务（在用条目受保护），成功 toast 并刷新列表后关闭。
import { ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import DangerConfirm from '@/components/business/DangerConfirm.vue'
import { useModalStore } from '@/stores/modalStore'
import { useI18n } from '@/composables/useI18n'
import { useCache } from '@/composables/useCache'
import type { CleanupMode } from '@/types'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const modal = useModalStore()
const { store, doCleanup } = useCache()

const MODES: CleanupMode[] = ['conservative', 'standard', 'aggressive']
const mode = ref<CleanupMode>('conservative')
const busy = ref(false)

function confirmRun(): void {
  modal.open(DangerConfirm, {
    title: t('danger.cacheCleanup.title'),
    description: t('danger.cacheCleanup.desc', { mode: t('cleanup.mode.' + mode.value) }),
    warnings: [{ text: t('offline.cleanup.warn', { n: store.corruptedCount }) }],
    checkbox: { label: t('danger.cacheCleanup.check') },
    confirmLabel: t('danger.cacheCleanup.confirm'),
    onConfirm: () => void run(),
  })
}

async function run(): Promise<void> {
  if (busy.value) return
  busy.value = true
  try {
    await doCleanup(mode.value)
    emit('close')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalShell @close="emit('close')">
    <template #head>
      <h3>{{ t('offline.cleanup.title') }}</h3>
      <p>{{ t('offline.cleanup.subtitle') }}</p>
    </template>
    <template #body>
      <div class="cc-modes">
        <label v-for="m in MODES" :key="m" class="cc-mode">
          <input v-model="mode" type="radio" name="cc-mode" :value="m" />
          <span>
            <b>{{ t('cleanup.mode.' + m) }}</b>
            <em>{{ t('offline.cleanup.mode.' + m + '.desc') }}</em>
          </span>
        </label>
      </div>
      <p class="cc-stat">{{ t('offline.cleanup.stat', { corrupted: store.corruptedCount, total: store.stats.entryCount }) }}</p>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="busy" @click="confirmRun">{{ t('offline.cleanup.run') }}</button>
    </template>
  </ModalShell>
</template>

<style scoped>
.cc-modes { display: flex; flex-direction: column; gap: 8px; }
.cc-mode { display: flex; align-items: flex-start; gap: 8px; cursor: pointer; }
.cc-mode b { font-size: 13px; }
.cc-mode em { display: block; color: var(--text-mute); font-size: 12px; font-style: normal; }
.cc-stat { margin: 12px 0 0; color: var(--text-mute); font-size: 12.5px; }
</style>
