<script setup lang="ts">
// PHP 扩展管理弹窗：忠实迁移原型 openPhpExtensionsModal（3054–3099）
// 已启用药丸（开关/删除）+ 推荐开关 + 自定义新增；脏态应用；preflight('extensions')
// T601 接真：有宿主时经后端三段式（编译→固化→重建→重载），状态由 state:changed 回流；无宿主回落本地 mock
import { computed, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { useAppState } from '@/stores/appState'
import { EXT_LIB } from '@/constants/ext'
import { hasBackend } from '@/api/site'
import { syncState } from '@/composables/useStateSync'
import { applyExtensions } from '@/api/extension'

const props = defineProps<{ version: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

const originalExts = [...(app.phpExtensions[props.version] || [])]
const enabledList = ref<string[]>([...originalExts])
const selected = ref<Set<string>>(new Set(originalExts))

const suggestions = computed(() => EXT_LIB.filter((e) => !enabledList.value.includes(e)).slice(0, 12))
const enabledOnCount = computed(() => enabledList.value.filter((e) => selected.value.has(e)).length)
const suggestOnCount = computed(() => suggestions.value.filter((e) => selected.value.has(e)).length)

const addName = ref('')
const dirty = computed(() => {
  if (selected.value.size !== originalExts.length) return true
  for (const e of selected.value) if (!originalExts.includes(e)) return true
  return false
})

function toggle(e: string): void {
  const s = new Set(selected.value)
  if (s.has(e)) s.delete(e)
  else s.add(e)
  selected.value = s
}
function removeExt(e: string): void {
  enabledList.value = enabledList.value.filter((x) => x !== e)
  const s = new Set(selected.value)
  s.delete(e)
  selected.value = s
}
function addExt(): void {
  const name = addName.value.trim()
  if (!name) return
  if (!/^[a-zA-Z0-9._-]+$/.test(name)) { toast(t('toast.extInvalid'), 'err'); return }
  if (enabledList.value.includes(name)) { toast(t('toast.extAdded', { name }), 'info', 1600); addName.value = ''; return }
  enabledList.value = [...enabledList.value, name]
  const s = new Set(selected.value)
  s.add(name)
  selected.value = s
  addName.value = ''
}

async function apply(): Promise<void> {
  if (!dirty.value) { emit('close'); return }
  const finalExts = [...selected.value]
  const added = finalExts.filter((x) => !originalExts.includes(x))
  const removed = originalExts.filter((x) => !finalExts.includes(x))
  const check = preflight('extensions', { version: props.version, finalExts })
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }

  // 纯 Vite demo：本地覆盖 + mock 任务
  if (!hasBackend()) {
    app.phpExtensions[props.version] = finalExts
    emit('close')
    const parts: string[] = []
    if (added.length) parts.push(`+${added.join(',')}`)
    if (removed.length) parts.push(`-${removed.join(',')}`)
    runTask(['php', 'extension', 'sync', props.version, '--ext', finalExts.join(',')], `PHP ${props.version} ext (${parts.join(' ') || 'no change'})`, { type: 'extensions', version: props.version, added, removed })
    return
  }

  // 真实链路：后端编译→固化→重建→重载；勿本地乐观更新，扩展列表写后拉权威快照归位
  try {
    await applyExtensions(props.version, finalExts)
    await syncState()
    emit('close')
    toast(t('ext.applied', { version: props.version }), 'ok', 2600)
  } catch (e) {
    toast(String(e), 'err', 4600)
  }
}
</script>

<template>
  <ModalShell size="lg" @close="emit('close')">
    <template #head>
      <h3>PHP {{ version }} · {{ t('ext.title') }}</h3>
      <p>{{ t('ext.subtitle') }}</p>
    </template>
    <template #body>
      <div class="field">
        <div style="display: flex; justify-content: space-between; align-items: center">
          <label>{{ t('ext.enabled') }}</label>
          <span class="mono" style="font-size: 11.5px; color: var(--text-mute)">{{ t('ext.selected', { count: enabledOnCount, total: enabledList.length }) }}</span>
        </div>
        <div class="ext-toggle-grid">
          <template v-if="enabledList.length">
            <span v-for="e in enabledList" :key="e" class="ext-pill" :class="selected.has(e) ? 'on' : 'off'">
              <button type="button" class="ext-pill-main" :title="selected.has(e) ? t('ext.disable') : t('ext.enable')" @click="toggle(e)">{{ e }}</button>
              <button type="button" class="ext-pill-del" :title="t('ext.remove')" @click="removeExt(e)">×</button>
            </span>
          </template>
          <span v-else style="color: var(--text-mute); font-size: 12.5px">{{ t('ext.empty.enabled') }}</span>
        </div>
        <div class="hint">{{ t('ext.enabled.hint') }}</div>
      </div>
      <div class="field">
        <div style="display: flex; justify-content: space-between; align-items: center">
          <label>{{ t('ext.suggestions') }}</label>
          <span class="mono" style="font-size: 11.5px; color: var(--text-mute)">{{ t('ext.selected', { count: suggestOnCount, total: suggestions.length }) }}</span>
        </div>
        <div class="ext-toggle-grid">
          <template v-if="suggestions.length">
            <button v-for="e in suggestions" :key="e" type="button" class="ext-toggle" :class="selected.has(e) ? 'on' : 'off'" @click="toggle(e)">{{ e }}</button>
          </template>
          <span v-else style="color: var(--text-mute); font-size: 12.5px">{{ t('ext.empty.suggestions') }}</span>
        </div>
        <div class="hint">{{ t('ext.suggestions.hint') }}</div>
      </div>
      <div class="field">
        <label>{{ t('ext.add') }}</label>
        <div style="display: flex; gap: 8px">
          <input v-model="addName" type="text" :placeholder="t('ext.add.placeholder')" autocomplete="off" @keydown.enter="addExt()">
          <button class="btn btn-primary" type="button" @click="addExt">{{ t('ext.add.btn') }}</button>
        </div>
        <div class="hint">{{ t('ext.add.hint') }}</div>
      </div>
      <div class="alert alert-warn"><strong>{{ t('rw.applyHint') }}</strong></div>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!dirty" @click="apply">{{ dirty ? t('ext.applyWithCount', { count: selected.size }) : t('rw.current') }}</button>
    </template>
  </ModalShell>
</template>
