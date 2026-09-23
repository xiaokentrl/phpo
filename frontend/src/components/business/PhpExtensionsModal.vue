<script setup lang="ts">
// PHP 扩展管理弹窗：列全量目录（不只当前已启用那几个），默认勾选 = 本版本已应用的扩展，
// 点选即改启用集；应用后由后端编译 → commit 固化镜像 → 重建容器 → 重载 Nginx。
// T601 接真：有宿主时走后端三段式，状态由 state:changed 回流；无宿主回落本地 mock
import { computed, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import ExtPicker from '@/components/common/ExtPicker.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { useAppState } from '@/stores/appState'
import { catalogFor } from '@/constants/ext'
import { hasBackend } from '@/api/site'
import { syncState } from '@/composables/useStateSync'
import { applyExtensions } from '@/api/extension'

const props = defineProps<{ version: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

const originalExts = [...(app.phpExtensions[props.version] || [])]
const selected = ref<string[]>([...originalExts])
const catalogCount = computed(() => catalogFor(props.version).length)
const addName = ref('')

const dirty = computed(() => {
  if (selected.value.length !== originalExts.length) return true
  return selected.value.some((e) => !originalExts.includes(e))
})

// addExt 目录之外的扩展名同样允许启用（最小限制原则）：过白名单格式即入列并选中
function addExt(): void {
  const name = addName.value.trim()
  if (!name) return
  if (!/^[a-zA-Z0-9._-]+$/.test(name)) { toast(t('toast.extInvalid'), 'err'); return }
  if (selected.value.includes(name)) { toast(t('toast.extAdded', { name }), 'info', 1600); addName.value = ''; return }
  selected.value = [...selected.value, name]
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

  // 真实链路：编译→固化→重建→重载是数十秒级的后台任务，弹窗提交即关闭，
  // 进度与失败由抽屉日志/队列承载（§5.6.4）；完成后后端广播 state:changed 把扩展列表归位。
  // 失败不做「留着弹窗改一项重试」——重开弹窗的默认勾选即上次成功应用的权威集。
  emit('close')
  applyExtensions(props.version, finalExts)
    .then(async () => {
      await syncState()
      toast(t('ext.applied', { version: props.version }), 'ok', 2600)
    })
    .catch((e: unknown) => {
      toast(String(e), 'err', 4600)
    })
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
          <span class="mono" style="font-size: 11.5px; color: var(--text-mute)">{{ t('ext.pickedTotal', { count: selected.length, total: catalogCount }) }}</span>
        </div>
        <ExtPicker v-model="selected" :version="version" />
        <div class="hint">{{ t('ext.picker.hint') }}</div>
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
      <button class="btn btn-primary" type="button" :disabled="!dirty" @click="apply">{{ dirty ? t('ext.applyWithCount', { count: selected.length }) : t('rw.current') }}</button>
    </template>
  </ModalShell>
</template>
