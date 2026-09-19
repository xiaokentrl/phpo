<script setup lang="ts">
// 建站弹窗：忠实迁移原型 openSiteAddModal（3102–3155）
// ★ FIX #4/#5：端口自动顺延（站点端口占用不报错）；root 外置降级为 warning → 确认弹窗
import { computed, ref } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import ModalShell from '@/components/common/ModalShell.vue'
import DangerConfirm from '@/components/business/DangerConfirm.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { addSite, hasBackend } from '@/api/site'
import { useModalStore } from '@/stores/modalStore'
import { useAppState } from '@/stores/appState'
import { REWRITE_PRESETS } from '@/constants/rewrite'
import { hostToContainer } from '@/utils/path'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()
const modal = useModalStore()
const canBrowse = hasBackend() // 有宿主才提供原生目录选择器

const wwwRoot = computed(() => app.env.WWW_ROOT.replace(/\/$/, ''))
const phpVers = computed(() => app.installed.php)
const rwOptions = computed(() => Object.entries(REWRITE_PRESETS).filter(([k]) => k !== 'custom'))

const domain = ref('')
const rootSub = ref('')
let rootTouched = false
const port = ref('80')
const php = ref(phpVers.value[0] || '')
const rewrite = ref('none')

function subDir(): string {
  return rootSub.value.trim().replace(/^\/+/, '').replace(/\/+$/, '')
}
function fullPath(): string {
  const sub = subDir()
  return sub ? `${wwwRoot.value}/${sub}` : wwwRoot.value
}
function containerPath(): string {
  return hostToContainer(app.env, fullPath())
}
function previewText(): string {
  const d = domain.value.trim() || '<domain>'
  const p = port.value.trim() || '80'
  return `phpo site add ${d} --port ${p} --php ${php.value} --rewrite ${rewrite.value} --root ${fullPath()}`
}
function onDomain(): void {
  if (!rootTouched) rootSub.value = domain.value.trim()
}
function onRoot(): void {
  rootTouched = true
}
function onPort(): void {
  if (!/^\d{0,5}$/.test(port.value)) port.value = port.value.replace(/[^\d]/g, '').slice(0, 5)
}

// 打开原生目录选择器，默认定位到相对根目录（WWW_ROOT）；选定的子目录折算为 rootSub
async function browseRoot(): Promise<void> {
  const picked = await Dialogs.OpenFile({
    Title: t('siteAdd.root'),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    Directory: fullPath(),
  })
  const abs = String(picked || '').replace(/\/+$/, '')
  if (!abs) return
  const root = wwwRoot.value
  if (abs === root) { rootSub.value = ''; onRoot(); return }
  if (abs.startsWith(root + '/')) { rootSub.value = abs.slice(root.length + 1); onRoot(); return }
  toast(t('siteAdd.browse.outsideRoot', { root }), 'err', 4600)
}

function onOk(): void {
  const d = domain.value.trim().toLowerCase()
  const pv = port.value.trim()
  if (!d) { toast(t('siteAdd.needDomain'), 'err'); return }
  if (!/^[a-z0-9.-]+$/.test(d)) { toast(t('siteAdd.invalidDomain'), 'err'); return }
  if (!/^\d{1,5}$/.test(pv)) { toast(t('common.invalidPort'), 'err'); return }
  const portNum = parseInt(pv, 10)
  const rootPath = fullPath()
  const ctx = { domain: d, port: portNum, php: php.value, root: rootPath, rewrite: rewrite.value }
  const check = preflight('site-add', ctx)
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
  const finalPort = (check.adjusted.port as number | undefined) ?? portNum
  const finalArgs = ['site', 'add', d, '--port', String(finalPort), '--php', php.value, '--rewrite', rewrite.value, '--root', rootPath]
  const finalMeta = { type: 'site-add', domain: d, port: finalPort, php: php.value, rewrite: rewrite.value, root: rootPath }
  const label = `${t('siteAdd.title')} ${d}`
  const submit = (): void => {
    if (!hasBackend()) { runTask(finalArgs, label, finalMeta); return }
    addSite({ domain: d, port: finalPort, php: php.value, root: rootPath, rewrite: rewrite.value })
      .catch((e: unknown) => toast(String(e), 'err', 4600))
  }
  if (check.warnings.length) {
    emit('close')
    modal.open(DangerConfirm, { title: label, warnings: check.warnings.map((w) => ({ text: w })), confirmLabel: t('common.confirm'), onConfirm: submit })
    return
  }
  emit('close')
  submit()
}
</script>

<template>
  <ModalShell @close="emit('close')">
    <template #head>
      <h3>{{ t('siteAdd.title') }}</h3>
      <p>{{ t('siteAdd.subtitle') }}</p>
    </template>
    <template #body>
      <div class="field">
        <label>{{ t('siteAdd.domain') }}</label>
        <input v-model="domain" type="text" autocomplete="off" placeholder="demo.test" @input="onDomain">
        <div class="hint">{{ t('siteAdd.domain.hint') }}</div>
      </div>
      <div class="field">
        <label>{{ t('siteAdd.root') }}</label>
        <div class="input-with-action">
          <input v-model="rootSub" type="text" autocomplete="off" spellcheck="false" placeholder="demo.test" @input="onRoot">
          <button v-if="canBrowse" class="input-action-btn" type="button" @click="browseRoot">{{ t('siteAdd.browse') }}</button>
        </div>
        <div class="hint">{{ t('siteAdd.root.hint', { root: wwwRoot }) }}</div>
        <div class="root-full-path" :title="t('siteAdd.root.full')">
          <span class="rp-label">{{ t('siteAdd.root.full') }}:</span>
          <span class="rp-value" :title="fullPath()">{{ fullPath() }}</span>
          <span class="rp-arrow">→</span>
          <span class="rp-container" :title="containerPath()">{{ containerPath() }}</span>
        </div>
      </div>
      <div class="field">
        <label>{{ t('siteAdd.port') }}</label>
        <input v-model="port" type="text" inputmode="numeric" autocomplete="off" spellcheck="false" @input="onPort">
        <div class="hint">{{ t('siteAdd.port.hint') }}</div>
      </div>
      <div class="field">
        <label>{{ t('siteAdd.phpVersion') }}</label>
        <select v-model="php">
          <option v-for="v in phpVers" :key="v" :value="v">{{ v }}</option>
        </select>
      </div>
      <div class="field">
        <label>{{ t('siteAdd.framework') }}</label>
        <select v-model="rewrite">
          <option v-for="[k, p] in rwOptions" :key="k" :value="k">{{ p.icon }} {{ t(p.nameKey) }}</option>
        </select>
        <div class="hint">{{ t('siteAdd.framework.hint') }}</div>
      </div>
      <div class="field">
        <label>{{ t('siteAdd.preview') }}</label>
        <div class="cmd-preview"><span class="prompt">$ </span>{{ previewText() }}</div>
      </div>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" @click="onOk">{{ t('siteAdd.create') }}</button>
    </template>
  </ModalShell>
</template>
