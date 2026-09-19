<script setup lang="ts">
// 服务配置弹窗：忠实迁移原型 openConfigModal（3015–3052）
// 多文件切换 + 每文件脏标记 + 重置/批量保存；T505 接真：读后端回显、写走三段式（备份→写→失败回滚），mock 保留本地 demo
import { computed, onMounted, reactive, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import MountList from '@/components/common/MountList.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { useAppState } from '@/stores/appState'
import { SVC_META } from '@/constants/service'
import { hasBackend } from '@/api/site'
import { getConfigFiles, saveConfigFiles } from '@/api/config'
import type { ConfigFile } from '@/constants/configs'

const props = defineProps<{ kind: string; version: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

const meta = SVC_META[props.kind as keyof typeof SVC_META]
const files = ref<ConfigFile[]>([])
const loading = ref(true)

function key(name: string): string {
  return `${props.kind}:${props.version}:${name}`
}
const originals = reactive<Record<string, string>>({})
const drafts = reactive<Record<string, string>>({})
const activeFile = ref('')

// seed：以后端回显（或模板默认）填充基准与草稿；app.configs 里的本地覆盖仅在 mock 下参与
function seed(list: ConfigFile[]): void {
  files.value = list
  list.forEach((f) => {
    const saved = app.configs[key(f.name)]
    const c = saved != null ? saved : f.content
    originals[f.name] = c
    drafts[f.name] = c
  })
  activeFile.value = list[0]?.name || ''
}

onMounted(async () => {
  try {
    seed(await getConfigFiles(props.kind, props.version))
  } catch (e) {
    toast(String(e), 'err', 4600)
  } finally {
    loading.value = false
  }
})

function isModified(name: string): boolean {
  return drafts[name] !== originals[name]
}
const changed = computed(() => files.value.filter((f) => isModified(f.name)))
const currentFile = computed(() => files.value.find((f) => f.name === activeFile.value))
function fullPath(): string {
  const f = currentFile.value
  return f ? `${app.env.PHPO_HOME}/${f.path}` : ''
}

function onTab(e: KeyboardEvent): void {
  if (e.key !== 'Tab') return
  e.preventDefault()
  const ta = e.target as HTMLTextAreaElement
  const s = ta.selectionStart
  const en = ta.selectionEnd
  drafts[activeFile.value] = ta.value.slice(0, s) + '    ' + ta.value.slice(en)
  requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = s + 4 })
}
function selectFile(name: string): void {
  activeFile.value = name
}
function resetActive(): void {
  drafts[activeFile.value] = originals[activeFile.value]
}
async function apply(): Promise<void> {
  const list = changed.value
  if (!list.length) { toast(t('config.noChanges'), 'info', 1400); return }
  const names = list.map((f) => f.name)
  const check = preflight('service-config', { kind: props.kind, version: props.version, files: names })
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }

  // 纯 Vite demo：本地覆盖 + mock 任务
  if (!hasBackend()) {
    list.forEach((f) => { app.configs[key(f.name)] = drafts[f.name] })
    emit('close')
    runTask([props.kind, 'config', 'save', props.version, '--files', names.join(',')], `${props.kind} ${props.version} · ${t('config.title')} (${names.length})`, { type: 'service-config', kind: props.kind, version: props.version, files: names })
    toast(t('config.saved', { kind: props.kind, version: props.version, count: names.length }), 'ok', 2600)
    return
  }

  // 真实链路：后端原子写盘（备份→写→失败回滚）；重启建议为提示性
  try {
    await saveConfigFiles(props.kind, props.version, list.map((f) => ({ name: f.name, path: f.path, content: drafts[f.name] })))
    emit('close')
    toast(t('config.saved', { kind: props.kind, version: props.version, count: names.length }), 'ok', 2600)
    toast(t('config.restartHint', { kind: props.kind, version: props.version }), 'info', 3600)
  } catch (e) {
    toast(String(e), 'err', 4600)
  }
}
</script>

<template>
  <ModalShell size="xl" body-config @close="emit('close')">
    <template #head>
      <h3>{{ t(meta.titleKey) }} {{ version }} · {{ t('config.title') }}</h3>
      <p>{{ t('config.subtitle') }}</p>
    </template>
    <template #body>
      <div class="config-layout">
        <aside class="config-files">
          <div class="config-files-label">{{ t('config.files') }}</div>
          <button
            v-for="f in files"
            :key="f.name"
            class="config-file"
            :class="{ active: f.name === activeFile, modified: isModified(f.name) }"
            type="button"
            @click="selectFile(f.name)"
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0"><path d="M14 3v5h5" /><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /></svg>
            <span class="file-name">{{ f.name }}</span>
            <span class="file-dot" />
          </button>
        </aside>
        <div class="config-editor-wrap">
          <div class="config-editor-head">
            <span class="path-text" :title="fullPath()">{{ fullPath() }}</span>
            <span style="font-size: 11.5px; color: var(--text-mute); flex-shrink: 0">{{ t('config.editorHint') }}</span>
          </div>
          <textarea v-model="drafts[activeFile]" class="config-editor" spellcheck="false" :disabled="loading" @keydown="onTab" />
        </div>
      </div>
      <div style="padding: 12px 22px; border-top: 1px solid var(--border-2); background: var(--surface-2)">
        <div class="alert alert-info" style="padding: 8px 12px">{{ t('config.hint') }}</div>
        <div style="margin-top: 8px"><MountList :kind="kind" :version="version" /></div>
      </div>
    </template>
    <template #foot>
      <span style="flex: 1; font-size: 12px; color: var(--text-mute); font-family: var(--mono)">
        <span v-if="changed.length" style="color: var(--warn)">● {{ t('config.unsaved') }} · {{ changed.length }}/{{ files.length }}</span>
      </span>
      <button class="btn" type="button" :disabled="!isModified(activeFile)" @click="resetActive">{{ t('config.reset') }}</button>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!changed.length" @click="apply">{{ changed.length ? t('config.applyCount', { count: changed.length }) : t('config.apply') }}</button>
    </template>
  </ModalShell>
</template>
