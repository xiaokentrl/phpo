<script setup lang="ts">
// 安装弹窗：忠实迁移原型 openInstallModal（2878–2940）
// ★ FIX #2：密码默认 123456、无长度校验；端口建议随版本变化；实时 cmd-preview
import { computed, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import MountList from '@/components/common/MountList.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { hasBackend } from '@/api/site'
import { installService } from '@/api/lifecycle'
import { setPassword, setPort } from '@/api/env'
import { useAppState } from '@/stores/appState'
import { SVC_META } from '@/constants/service'
import { needsPassword, needsPort, suggestPortFor } from '@/utils/format'
import { DEFAULT_PASSWORD, genPassword } from '@/utils/str'

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

const version = ref(isSingle.value ? meta.value.suggested[0] : '')
const port = ref(needPort.value ? String(suggestPortFor(props.kind, version.value || meta.value.suggested[0])) : '')
const password = ref(needPw.value ? DEFAULT_PASSWORD : '')
const extensions = ref('')
const pwVisible = ref(true)

function refreshPort(): void {
  if (needPort.value) port.value = String(suggestPortFor(props.kind, version.value.trim() || meta.value.suggested[0]))
}

function previewText(): string {
  const v = version.value.trim() || '<version>'
  const lines = [`phpo ${props.kind} install ${v}`]
  if (needPort.value) lines.push(`--port ${port.value.trim() || '<port>'}`)
  if (needPw.value) {
    const p = password.value
    lines.push(`--password ${p ? '****' + p.slice(-4) : '""'}`)
  }
  if (needExt.value && extensions.value.trim()) lines.push(`--ext ${extensions.value.trim()}`)
  return lines.length > 1 ? lines.join(' \\\n  ') : lines[0]
}

function onOk(): void {
  const v = version.value.trim()
  if (!v) { toast(t('install.needVersion'), 'err'); return }
  if (needPort.value && !/^\d{1,5}$/.test(port.value.trim())) { toast(t('common.invalidPort'), 'err'); return }
  const ctx = {
    kind: props.kind,
    version: v,
    port: needPort.value ? port.value.trim() : null,
    password: needPw.value ? password.value : null,
    extensions: needExt.value ? extensions.value.trim() : null,
  }
  const check = preflight('install', ctx)
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
  const args = [props.kind, 'install', v]
  if (ctx.port) args.push('--port', ctx.port)
  if (ctx.password !== null && ctx.password !== '') args.push('--password', ctx.password)
  if (ctx.extensions) args.push('--ext', ctx.extensions)
  const label = `${t('svc.install')} ${t(meta.value.titleKey)} ${v}`
  const taskMeta = { type: 'install', kind: props.kind, version: v, port: ctx.port, password: ctx.password, extensions: ctx.extensions }
  emit('close')
  if (!hasBackend()) { runTask(args, label, taskMeta); return }
  // 真实链路：先落库端口/密码（Install 时容器据此装配），再安装；状态由后端事件回流。
  const portNum = ctx.port ? parseInt(ctx.port, 10) : 0
  const chain = Promise.resolve()
    .then(() => (ctx.port ? setPort(props.kind, v, portNum) : undefined))
    .then(() => (ctx.password !== null ? setPassword(props.kind, v, ctx.password) : undefined))
    .then(() => installService(props.kind, v))
  chain.catch((e: unknown) => toast(String(e), 'err', 4600))
}
</script>

<template>
  <ModalShell>
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
