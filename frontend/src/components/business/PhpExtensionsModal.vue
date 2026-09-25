<script setup lang="ts">
// PHP 扩展管理弹窗：每一颗开关只回答一件事——「这个扩展在本 PHP 版本上此刻启用了没有」。
// 打开时让后端到容器里现查一次（php -m），查到的结果写回权威库再随快照回流，界面只读快照那一份。
// 已经开着但停不掉的（静态编进 PHP 本体、没有 ini 可删）画成 on + 「内建」，点不动也不进提交集。
// 应用后由后端编译 → commit 固化镜像 → 重建容器 → 重载 Nginx；提交即关窗，进度与失败进抽屉日志。
import { computed, onMounted, ref } from 'vue'
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
import { applyExtensions, extStatus } from '@/api/extension'

const props = defineProps<{ version: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

// builtIn = 内建停不掉的那几颗；live = 这次是否真查到容器里的值；两者只在这次弹窗里有效
const builtIn = ref<string[]>([])
const live = ref(true)
const loading = ref(true)
const lockedSet = computed(() => new Set(builtIn.value))

const catalogNames = computed(() => new Set(catalogFor(props.version).map((e) => e.name)))

// 基线只认权威快照（后端实测 php -m 回写后推的那一份），并且只取目录内、可停用的那些：
// 内建项点不动所以不进提交集，目录外的名字（Core、date…）本产品的开关管不到它们。
const originalExts = computed(() =>
  (app.phpExtensions[props.version] ?? []).filter((n) => catalogNames.value.has(n) && !lockedSet.value.has(n)),
)

// edits 为用户本次的手工改动；null 表示「还没动过」，此时 selected 一路跟着权威快照走
// （快照可能晚于现查响应到达，用计算属性而不是 ref 初值才不会被过期值钉住）
const edits = ref<string[] | null>(null)
const selected = computed(() => edits.value ?? originalExts.value)
const catalogCount = computed(() => catalogFor(props.version).length)
const addName = ref('')

const dirty = computed(() => {
  const cur = selected.value
  const base = originalExts.value
  if (cur.length !== base.length) return true
  return cur.some((e) => !base.includes(e))
})

onMounted(async () => {
  try {
    const r = await extStatus(props.version)
    live.value = r.live
    builtIn.value = r.builtIn
  } catch (e: unknown) {
    // 现查失败：留着库里那一份可看，但这次不许提交（改一次容器状态再打开即可）
    live.value = false
    toast(String(e), 'err', 4600)
  } finally {
    loading.value = false
  }
})

// setSelection ExtPicker 的每次点选都落进 edits：一经手改即冻结为用户那一份，未手改则一直跟权威快照
function setSelection(v: string[]): void {
  edits.value = v
}

// addExt 目录之外的扩展名同样允许启用（最小限制原则）：过白名单格式即入列并选中
function addExt(): void {
  const name = addName.value.trim()
  if (!name) return
  if (!/^[a-zA-Z0-9._-]+$/.test(name)) { toast(t('toast.extInvalid'), 'err'); return }
  if (selected.value.includes(name)) { toast(t('toast.extAdded', { name }), 'info', 1600); addName.value = ''; return }
  edits.value = [...selected.value, name]
  addName.value = ''
}

async function apply(): Promise<void> {
  if (loading.value || !live.value) return
  if (!dirty.value) { emit('close'); return }
  const finalExts = [...selected.value]
  const base = originalExts.value
  const added = finalExts.filter((x) => !base.includes(x))
  const removed = base.filter((x) => !finalExts.includes(x))
  const check = preflight('extensions', { version: props.version, finalExts })
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }

  // 纯 Vite demo：mock 任务回放（不改写 store——扩展列表的回显只认快照，硬红线 4）
  if (!hasBackend()) {
    emit('close')
    const parts: string[] = []
    if (added.length) parts.push(`+${added.join(',')}`)
    if (removed.length) parts.push(`-${removed.join(',')}`)
    runTask(['php', 'extension', 'sync', props.version, '--ext', finalExts.join(',')], `PHP ${props.version} ext (${parts.join(' ') || 'no change'})`, { type: 'extensions', version: props.version, added, removed })
    return
  }

  // 真实链路：编译→固化→重建→重载是数十秒级的后台任务，弹窗提交即关闭，
  // 进度与失败由抽屉日志/队列承载（§5.6.4）；完成后后端广播 state:changed 把扩展列表归位。
  // 失败不做「留着弹窗改一项重试」——重开弹窗的默认勾选即容器内此刻已启用的那一套（实测集）。
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
        <div v-if="loading" class="alert alert-info"><strong>{{ t('ext.fetching') }}</strong></div>
        <div v-else-if="!live" class="alert alert-warn"><strong>{{ t('ext.nonLive') }}</strong></div>
        <ExtPicker :model-value="selected" :version="version" :locked="builtIn" @update:model-value="setSelection" />
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
      <button class="btn btn-primary" type="button" :disabled="!dirty || loading || !live" @click="apply">{{ dirty ? t('ext.applyWithCount', { count: selected.length }) : t('rw.current') }}</button>
    </template>
  </ModalShell>
</template>
