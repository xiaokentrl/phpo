<script setup lang="ts">
// 缓存导入弹窗（需求 1：读写任意缓存文件）：把手工选定的文件登记为一条离线缓存条目。
// 三种落点：image → {kind}/{version}/image.tar（与在线安装同一条目）；apk / pecl → php/{version}/{extType}/（仅 php）。
// 硬红线 4/5：导入走 preflight → 后端任务 → 事件回流，列表由写后补刷，前端不本地插行。
import { computed, ref, watch } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { useAppState } from '@/stores/appState'
import { useCache } from '@/composables/useCache'
import { hasBackend } from '@/api/site'
import { offlineRoot } from '@/api/env'
import { SVC_META } from '@/constants/service'
import type { ServiceKind } from '@/types'

type ExtType = 'image' | 'apk' | 'pecl'
const KINDS: ServiceKind[] = ['php', 'mysql', 'pgsql', 'redis', 'nginx']

const props = defineProps<{ kind?: ServiceKind; version?: string }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const { preflight } = usePreflight()
const state = useAppState()
const { doImport } = useCache()

const kind = ref<ServiceKind>(props.kind ?? 'php')
const version = ref(props.version ?? '')
const extType = ref<ExtType>('image')
const file = ref('')
const busy = ref(false)

// 版本号只作候选提示（已装优先，其次建议表）；不做字符集限制（§1.6）
const options = computed(() => {
  const k = kind.value
  return Array.from(new Set([...(state.installed[k] ?? []), ...(SVC_META[k]?.suggested ?? [])]))
})
const extTypes = computed<ExtType[]>(() => (kind.value === 'php' ? ['image', 'apk', 'pecl'] : ['image']))
watch(extTypes, (v) => {
  if (!v.includes(extType.value)) extType.value = 'image'
})

const canPick = hasBackend()
const ready = computed(() => !!file.value.trim() && !!version.value.trim())

async function pick(): Promise<void> {
  const picked = await Dialogs.OpenFile({ Title: t('offline.import.pickTitle'), CanChooseFiles: true, CanChooseDirectories: false })
  const path = String(Array.isArray(picked) ? (picked[0] ?? '') : (picked ?? ''))
  if (path) file.value = path
}

async function run(): Promise<void> {
  if (!ready.value || busy.value) return
  const pf = preflight('cache-import', { kind: kind.value, version: version.value.trim(), field: extType.value, newValue: file.value.trim() })
  if (!pf.ok) {
    toast(pf.errors.join('\n'), 'err', 4600)
    return
  }
  busy.value = true
  try {
    if (await doImport(kind.value, version.value.trim(), extType.value, file.value.trim())) emit('close')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalShell @close="emit('close')">
    <template #head>
      <h3>{{ t('offline.import.title') }}</h3>
      <p>{{ t('offline.import.subtitle') }}</p>
    </template>
    <template #body>
      <div class="field">
        <label>{{ t('offline.import.kind') }}</label>
        <select v-model="kind">
          <option v-for="k in KINDS" :key="k" :value="k">{{ k }}</option>
        </select>
      </div>
      <div class="field">
        <label>{{ t('offline.import.version') }}</label>
        <input v-model="version" type="text" list="ci-versions" autocomplete="off" spellcheck="false" placeholder="8.4">
        <datalist id="ci-versions">
          <option v-for="v in options" :key="v" :value="v" />
        </datalist>
      </div>
      <div class="field">
        <label>{{ t('offline.import.type') }}</label>
        <div class="ci-types">
          <label v-for="e in extTypes" :key="e" class="ci-type">
            <input v-model="extType" type="radio" name="ci-type" :value="e">
            <span>{{ t('offline.import.type.' + e) }}</span>
          </label>
        </div>
      </div>
      <div class="field">
        <label>{{ t('offline.import.file') }}</label>
        <div class="input-with-action">
          <input v-model="file" type="text" autocomplete="off" spellcheck="false">
          <button v-if="canPick" class="input-action-btn" type="button" @click="pick">{{ t('offline.import.pick') }}</button>
        </div>
        <div class="hint">{{ t('offline.import.hint', { root: offlineRoot(), type: extType }) }}</div>
      </div>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!ready || busy" @click="run">{{ t('offline.import.run') }}</button>
    </template>
  </ModalShell>
</template>

<style scoped>
.ci-types { display: flex; gap: 16px; }
.ci-type { display: flex; align-items: center; gap: 6px; font-size: 13px; cursor: pointer; color: var(--text); }
</style>
