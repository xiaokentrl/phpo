<script setup lang="ts">
// 安装弹窗：忠实迁移原型 openInstallModal（2878–2940）
// ★ FIX #2：密码默认 123456、无长度校验；端口建议随版本变化；实时 cmd-preview
import { computed, ref } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import ModalShell from '@/components/common/ModalShell.vue'
import MountList from '@/components/common/MountList.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { submitWrite } from '@/composables/useTask'
import { installService } from '@/api/lifecycle'
import { applyExtensions } from '@/api/extension'
import { defaultDataDir, setServiceDataDir } from '@/api/env'
import { hasBackend } from '@/api/site'
import { useAppState } from '@/stores/appState'
import { DIR_ROWS, SVC_META } from '@/constants/service'
import { needsPassword, needsPort, suggestPortFor } from '@/utils/format'
import { DEFAULT_PASSWORD, genPassword } from '@/utils/str'
import type { ServiceKind } from '@/types'

const props = defineProps<{ kind: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

const meta = computed(() => SVC_META[props.kind as keyof typeof SVC_META])
const installed = computed<string[]>(() => (app.installed as Record<string, string[]>)[props.kind] || [])
const isSingle = computed(() => !!meta.value.single)
const needPort = computed(() => needsPort(props.kind))
const needPw = computed(() => needsPassword(props.kind))
const needExt = computed(() => props.kind === 'php')
// 数据目录只对有 data 挂载的服务开放（DIR_ROWS 是挂载布局的唯一来源，不另立清单）
const needDataDir = computed(() => (DIR_ROWS[props.kind as ServiceKind] || []).some(([sub]) => sub === 'data'))

const version = ref(isSingle.value ? meta.value.suggested[0] : '')
const port = ref(needPort.value ? String(suggestPortFor(props.kind, version.value || meta.value.suggested[0])) : '')
const password = ref(needPw.value ? DEFAULT_PASSWORD : '')
const extensions = ref('')
const dataDir = ref('')
const pwVisible = ref(true)

function refreshPort(): void {
  if (needPort.value) port.value = String(suggestPortFor(props.kind, version.value.trim() || meta.value.suggested[0]))
}

// browseDataDir 原生目录选择器：留空即用默认数据目录，自定义后该版本的数据只落在所选目录
async function browseDataDir(): Promise<void> {
  const v = version.value.trim() || meta.value.suggested[0]
  const picked = await Dialogs.OpenFile({
    Title: t('svc.dataDirBrowse'),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    Directory: dataDir.value.trim() || undefined,
  })
  const abs = String(picked || '').replace(/\/+$/, '')
  if (abs) dataDir.value = abs
}

function previewText(): string {
  const v = version.value.trim() || '<version>'
  const lines = [`phpo ${props.kind} install ${v}`]
  if (needPort.value) lines.push(`--port ${port.value.trim() || '<port>'}`)
  if (needPw.value) {
    const p = password.value
    lines.push(`--password ${p ? '****' + p.slice(-4) : '""'}`)
  }
  if (needDataDir.value && dataDir.value.trim()) lines.push(`--data-dir ${dataDir.value.trim()}`)
  if (needExt.value && extensions.value.trim()) lines.push(`--ext ${extensions.value.trim()}`)
  return lines.length > 1 ? lines.join(' \\\n  ') : lines[0]
}

function onOk(): void {
  const v = version.value.trim()
  if (!v) { toast(t('install.needVersion'), 'err'); return }
  if (needPort.value && !/^\d{1,5}$/.test(port.value.trim())) { toast(t('common.invalidPort'), 'err'); return }
  const dir = needDataDir.value ? dataDir.value.trim().replace(/\/+$/, '') : ''
  // 数据目录是另一条写链路（config.yaml + 重绑对象图），安装前先单独裁决，不混进 install 的判据里
  if (dir && dir !== defaultDataDir(props.kind, v)) {
    const rootPf = preflight('root-set', { field: 'data_dir', kind: props.kind, version: v, newValue: dir })
    if (!rootPf.ok) { toast(rootPf.errors.join('\n'), 'err', 4600); return }
  }
  const ctx = {
    kind: props.kind,
    version: v,
    port: needPort.value ? port.value.trim() : null,
    password: needPw.value ? password.value : null,
    extensions: needExt.value ? extensions.value.trim() : null,
    dataDir: dir,
  }
  const check = preflight('install', ctx)
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
  const args = [props.kind, 'install', v]
  if (ctx.port) args.push('--port', ctx.port)
  if (ctx.password !== null && ctx.password !== '') args.push('--password', ctx.password)
  if (ctx.dataDir) args.push('--data-dir', ctx.dataDir)
  if (ctx.extensions) args.push('--ext', ctx.extensions)
  const label = `${t('svc.install')} ${t(meta.value.titleKey)} ${v}`
  const taskMeta = { type: 'install', kind: props.kind, version: v, port: ctx.port, password: ctx.password, extensions: ctx.extensions, dataDir: ctx.dataDir }
  emit('close')
  // 真实链路：安装期填的端口/密码随 install 一次带过（后端先落 config.yaml 再建容器，状态由事件回流）；
  // 扩展是另一条链路（容器内编译 → 固化镜像 → 重建），排在安装任务之后作为第二个任务。
  const portNum = ctx.port ? parseInt(ctx.port, 10) : undefined
  const exts = ctx.extensions ? ctx.extensions.split(',').map((s) => s.trim()).filter(Boolean) : []
  submitWrite(args, label, taskMeta, async () => {
    // 数据目录必须先落库并重绑对象图：容器 binds 读的是装配期那份 Env，不重绑就挂到默认目录
    if (ctx.dataDir) await setServiceDataDir(props.kind, v, ctx.dataDir)
    await installService(props.kind, v, { port: portNum, password: ctx.password ?? undefined, hasPassword: ctx.password !== null })
    if (exts.length) await applyExtensions(v, exts)
  })
}
</script>

<template>
  <ModalShell @close="emit('close')">
    <template #head>
      <h3>{{ t('install.title', { name: t(meta.titleKey) }) }}</h3>
      <p>{{ t('install.subtitle') }}</p>
    </template>
    <template #body>
      <div class="field">
        <label>{{ t('install.version') }}</label>
        <input v-model="version" type="text" autocomplete="off" :placeholder="meta.suggested[0]" @input="refreshPort">
        <div class="quick-picks">
          <button
            v-for="v in meta.suggested"
            :key="v"
            class="pick"
            :class="{ selected: installed.includes(v) || v === version }"
            type="button"
            @click="version = v; refreshPort()"
          >{{ v }}{{ installed.includes(v) ? ' ✓' : '' }}</button>
        </div>
      </div>
      <div v-if="needPort" class="field">
        <label>{{ t('install.port') }}</label>
        <input v-model="port" type="text" autocomplete="off" spellcheck="false" inputmode="numeric">
        <div class="hint">{{ t('install.port.hint') }}</div>
      </div>
      <div v-if="needPw" class="field">
        <label>{{ t('install.password') }}</label>
        <div class="input-with-action">
          <input v-model="password" :type="pwVisible ? 'text' : 'password'" autocomplete="off" spellcheck="false">
          <button class="input-action-btn" type="button" :title="t('install.showPassword')" @click="pwVisible = !pwVisible">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" /></svg>
          </button>
          <button class="input-action-btn" type="button" :title="t('install.regen')" @click="password = genPassword()">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6" /></svg>
          </button>
        </div>
        <div class="hint">{{ t('install.password.hint') }}</div>
      </div>
      <div v-if="needDataDir" class="field">
        <label>{{ t('svc.dataDir') }} ({{ t('common.optional') }})</label>
        <div class="input-with-action">
          <input v-model="dataDir" type="text" autocomplete="off" spellcheck="false" :placeholder="defaultDataDir(kind, version || meta.suggested[0])">
          <button v-if="hasBackend()" class="input-action-btn" type="button" :title="t('svc.dataDirBrowse')" @click="browseDataDir">{{ t('dir.browse') }}</button>
        </div>
        <div class="hint">{{ t('install.dataDir.hint', { path: defaultDataDir(kind, version || meta.suggested[0]) }) }}</div>
      </div>
      <div v-if="needExt" class="field">
        <label>{{ t('php.extensions') }} ({{ t('common.optional') }})</label>
        <input v-model="extensions" type="text" autocomplete="off" placeholder="redis,gd,imagick">
        <div class="hint">{{ t('install.ext.hint') }}</div>
      </div>
      <div class="field">
        <label>{{ t('install.mounts') }}</label>
        <div class="cmd-preview"><MountList :kind="kind" :version="version || meta.suggested[0]" /></div>
      </div>
      <div class="field">
        <label>{{ t('install.preview') }}</label>
        <div class="cmd-preview"><span class="prompt">$ </span>{{ previewText() }}</div>
      </div>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" @click="onOk">{{ t('install.install') }}</button>
    </template>
  </ModalShell>
</template>
