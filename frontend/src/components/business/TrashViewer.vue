<script setup lang="ts">
// 回收站弹窗（T605 / §5.13.7）：误删内容 7 天保留，逐条恢复或批量清空过期项。
// 硬红线 4：列表以后端为准；恢复/清空走后端三段式任务后重新拉取。
import { onMounted } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { useCleanup } from '@/composables/useCleanup'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { store, loadTrash, restore, clearExpired } = useCleanup()

onMounted(loadTrash)

function fmt(ts: string): string {
  if (!ts) return '-'
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString()
}
</script>

<template>
  <ModalShell>
    <template #head>
      <h3>{{ t('cleanup.trash.title') }}</h3>
      <p>{{ t('cleanup.trash.subtitle') }}</p>
    </template>
    <template #body>
      <div class="trash">
        <div v-if="store.trash.length === 0" class="trash-empty">{{ t('cleanup.trash.empty') }}</div>
        <div v-else class="table-wrap">
          <table>
            <thead>
              <tr>
                <th style="width: 14%">{{ t('cleanup.trash.col.kind') }}</th>
                <th style="width: 46%">{{ t('cleanup.trash.col.path') }}</th>
                <th style="width: 26%">{{ t('cleanup.trash.col.expires') }}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="it in store.trash" :key="it.id">
                <td><span class="chip">{{ it.kind }}</span></td>
                <td><span class="mono" style="font-size: 11.5px">{{ it.origPath }}</span></td>
                <td>
                  {{ fmt(it.expiresAt) }}
                  <span v-if="it.expired" class="trash-expired">{{ t('cleanup.trash.expired') }}</span>
                </td>
                <td>
                  <button class="btn btn-sm" type="button" @click="restore(it.id)">{{ t('cleanup.trash.restore') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
    <template #foot>
      <button class="btn" type="button" :disabled="store.trash.filter((x) => x.expired).length === 0" @click="clearExpired()">{{ t('cleanup.trash.clearExpired') }}</button>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.close') }}</button>
    </template>
  </ModalShell>
</template>

<style scoped>
.trash { min-height: 60px; }
.trash-empty { color: var(--text-mute); font-size: 13px; padding: 8px 0; }
.trash-expired { margin-left: 6px; color: var(--danger); font-size: 11.5px; }
</style>
