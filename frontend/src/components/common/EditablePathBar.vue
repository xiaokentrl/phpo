<script setup lang="ts">
// 可编辑路径条（需求 1/7/8）：缓存根 / 备份根 / 服务数据目录共用
// 与只读的 PathInfoBar 分开：原型那条是纯展示，这里承载「编辑 + 浏览 + 保存 + 恢复默认」写链路。
// 硬红线 4：保存只发后端调用，回显随 state:changed 快照，不做本地乐观更新。
import { computed, ref, watch } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import { useI18n } from '@/composables/useI18n'
import { hasBackend } from '@/api/site'

const props = defineProps<{
  label: string
  path: string // 当前生效根（快照 env，已展开 ~）
  defaultPath: string // 未自定义时的默认根，作占位与「恢复默认」目标
  browseTitle: string
  saving?: boolean
}>()
const emit = defineEmits<{ save: [value: string]; reset: [] }>()

const { t } = useI18n()
const canBrowse = hasBackend()
const val = ref(props.path)
watch(() => props.path, (p) => { val.value = p })

const isCustom = computed(() => !!props.path && props.path !== props.defaultPath)
const dirty = computed(() => val.value.trim() !== props.path)

async function browse(): Promise<void> {
  const picked = await Dialogs.OpenFile({
    Title: props.browseTitle,
    CanChooseDirectories: true,
    CanChooseFiles: false,
    Directory: val.value.trim() || props.path || undefined,
  })
  const abs = String(picked || '').replace(/\/+$/, '')
  if (abs) val.value = abs
}

function save(): void {
  const v = val.value.trim().replace(/\/+$/, '')
  if (!v) { val.value = props.path; return }
  emit('save', v)
}
</script>

<template>
  <div class="path-info-bar epb">
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
    <span class="pib-label">{{ label }}</span>
    <input v-model="val" class="pib-input mono" type="text" :placeholder="defaultPath" :spellcheck="false" />
    <button v-if="canBrowse" class="btn btn-sm" type="button" @click="browse">{{ t('dir.browse') }}</button>
    <button class="btn btn-sm btn-primary" type="button" :disabled="!dirty || saving" @click="save">{{ t('root.save') }}</button>
    <button v-if="isCustom" class="btn btn-sm" type="button" :disabled="saving" @click="emit('reset')">{{ t('root.reset') }}</button>
    <span v-if="isCustom" class="chip chip-accent epb-tag">{{ t('root.custom') }}</span>
  </div>
</template>

<style scoped>
.epb { gap: 8px; }
.pib-input {
  flex: 1;
  min-width: 0;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text);
  font-size: 12px;
  padding: 5px 8px;
}
.pib-input:focus { outline: none; border-color: var(--accent); }
.epb-tag { flex: none; }
</style>
