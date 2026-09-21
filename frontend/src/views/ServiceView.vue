<script setup lang="ts">
// 服务视图：1:1 迁移原型 renderService + versionCard（2636–2692）；php/mysql/pgsql/redis/nginx 共用
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useModals } from '@/composables/useModals'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { syncState } from '@/composables/useStateSync'
import { hasBackend } from '@/api/site'
import { envKeyPort, setPort } from '@/api/env'
import PasswordField from '@/components/common/PasswordField.vue'
import { DIR_ROWS, DEFAULT_FILE_COUNT, SVC_META } from '@/constants/service'
import type { ServiceKind } from '@/types'
import { needsPort, needsPassword, suggestPortFor } from '@/utils/format'
import { verRoot } from '@/utils/path'

const props = defineProps<{ kind: ServiceKind }>()
const { t } = useI18n()
const state = useAppState()
const modals = useModals()
const { preflight } = usePreflight()

const meta = computed(() => SVC_META[props.kind])
const versions = computed(() => state.installed[props.kind] || [])

// versionCard：端口键 {KIND}_{ver}_PORT（与后端 config.EnvKeyPort 同源，nginx 亦走此键）
function portValue(version: string): string {
  const key = envKeyPort(props.kind, version)
  return String(state.env[key] || suggestPortFor(props.kind, version) || '')
}
function dirPath(version: string, sub: string): string {
  return `${verRoot(state.env, props.kind, version)}/${sub}`
}
function extCount(version: string): number {
  return (state.phpExtensions[version] || []).length
}

// 端口行内编辑：仅数据服务（mysql/pgsql/redis）的宿主端口会进容器 spec；nginx 由站点端口自动发布，只读
const portEditing = ref('')
const portDraft = ref('')
const portSaving = ref('')

function startEditPort(version: string): void {
  portDraft.value = portValue(version)
  portEditing.value = version
}

function cancelPort(): void {
  portEditing.value = ''
}

// commitPort：preflight 裁决 → 落库 → 拉权威快照回显（硬红线 4/5，无本地乐观更新）
async function commitPort(version: string): Promise<void> {
  if (portEditing.value !== version) return
  const prev = portValue(version)
  const next = portDraft.value.trim()
  portEditing.value = ''
  if (!next || next === prev) return
  const pf = preflight('update-config', { kind: props.kind, version, field: 'port', newValue: next })
  if (!pf.ok) {
    toast(pf.errors.join('\n'), 'err', 4600)
    return
  }
  portSaving.value = version
  try {
    await setPort(props.kind, version, parseInt(next, 10))
    if (hasBackend()) {
      await syncState()
      // Docker 端口绑定只能在建容器时确定，Start 不重建容器 → 必须重装才生效，如实告知不假装已切换
      toast(t('svc.portPending', { kind: props.kind, version }), 'info', 5600)
    } else {
      runTask([props.kind, 'port', 'set', version, next], `${props.kind} ${version} · port → ${next}`, {
        type: 'update-config', kind: props.kind, version, field: 'port', oldValue: prev, newValue: next,
      })
    }
  } catch (e) {
    toast(String(e), 'err', 4600)
  } finally {
    portSaving.value = ''
  }
}
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t(meta.titleKey) }}</h1>
        <p class="view-sub">{{ t(meta.subtitleKey) }}</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" data-action="install" :data-kind="kind" @click="modals.openInstallModal(kind)">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14" /></svg>
          {{ t('svc.install') }} {{ t(meta.titleKey) }}
        </button>
      </div>
    </header>

    <div v-if="versions.length === 0" class="empty">
      <div class="empty-icon">{{ meta.icon }}</div>
      <h2>{{ t(meta.emptyTitleKey) }}</h2>
      <p>{{ t(meta.hintKey) }}</p>
      <button class="btn btn-primary" data-action="install" :data-kind="kind" @click="modals.openInstallModal(kind)">{{ t('svc.install') }} {{ t(meta.titleKey) }}</button>
    </div>

    <div v-else class="grid grid-3">
      <article v-for="version in versions" :key="version" class="card version-card">
        <div class="version-card-head">
          <span class="version-tag">{{ version }}</span>
          <span v-if="state.isServiceRunning(kind, version)" class="status-pill pill-ok"><span class="pill-dot"></span>{{ t('svc.running') }}</span>
          <span v-else class="status-pill pill-off"><span class="pill-dot"></span>{{ t('svc.stopped') }}</span>
        </div>

        <div>
          <div v-if="needsPort(kind)" class="kv">
            <span class="k">{{ t('svc.port') }}</span>
            <!-- nginx 宿主端口 = 80 + 站点端口并集，由站点驱动、建容器时定死，不在卡片里改（改了没有消费方） -->
            <span v-if="kind === 'nginx'" class="v" :title="t('svc.portFixed')">{{ portValue(version) }}</span>
            <span
              v-else
              class="inline-edit"
              :class="{ editing: portEditing === version, saving: portSaving === version }"
              data-inline="port"
              :data-kind="kind"
              :data-version="version"
              data-field="port"
              :data-original="portValue(version)"
              tabindex="0"
              :title="t('common.edit')"
              @click="portEditing !== version && startEditPort(version)"
              @keydown.enter="portEditing !== version && startEditPort(version)"
            >
              <input
                v-if="portEditing === version"
                v-model="portDraft"
                class="port-input"
                type="text"
                inputmode="numeric"
                spellcheck="false"
                autocomplete="off"
                autofocus
                @keydown.enter.prevent="commitPort(version)"
                @keydown.esc.prevent="cancelPort"
                @blur="commitPort(version)"
              >
              <template v-else>{{ portValue(version) }}</template>
            </span>
          </div>
          <div v-if="needsPassword(kind)" class="kv">
            <span class="k">{{ t('svc.password') }}</span>
            <PasswordField :kind="kind" :version="version" />
          </div>

          <div v-for="[sub, labelKey] in DIR_ROWS[kind]" :key="sub" class="kv">
            <span class="k">{{ t(labelKey) }}</span>
            <span class="v" :title="dirPath(version, sub)">{{ dirPath(version, sub) }}</span>
          </div>

          <div v-if="kind === 'php' || kind === 'nginx'" class="kv">
            <span class="k">{{ t('svc.wwwDir') }}</span>
            <span class="v" :title="`${state.env.WWW_ROOT} → /var/www`">{{ state.env.WWW_ROOT }}</span>
          </div>
          <div v-if="kind === 'nginx'" class="kv">
            <span class="k">{{ t('svc.sitesDir') }}</span>
            <span class="v" :title="`${state.env.NGINX_SITES_ROOT} → /etc/nginx/sites`">{{ state.env.NGINX_SITES_ROOT }}</span>
          </div>

          <div v-if="kind === 'php'" class="kv">
            <span class="k">{{ t('php.extensions') }}</span>
            <button class="btn btn-sm" data-action="php-extensions" :data-version="version" :title="t('php.manageExt')" @click="modals.openPhpExtensionsModal(version)">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v18M3 12h18" /></svg>
              {{ t('php.manageExt') }}
              <span style="opacity: 0.6; font-family: var(--mono); font-size: 11px; margin-left: 2px">{{ extCount(version) }}</span>
            </button>
          </div>
        </div>

        <div class="version-card-foot">
          <button class="btn btn-sm" data-action="service-config" :data-kind="kind" :data-version="version" @click="modals.openConfigModal(kind, version)">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3v5h5" /><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><path d="M8 13h8M8 17h5" /></svg>
            {{ t('svc.manageConfig') }}
            <span style="opacity: 0.55; font-family: var(--mono); font-size: 11px; margin-left: 2px">{{ DEFAULT_FILE_COUNT[kind] }}</span>
          </button>
          <button v-if="state.isServiceRunning(kind, version)" class="btn btn-sm" data-action="stop-service" :data-kind="kind" :data-version="version" @click="modals.stopService(kind, version)">{{ t('svc.stop') }}</button>
          <button v-else class="btn btn-sm btn-primary" data-action="start-service" :data-kind="kind" :data-version="version" @click="modals.startService(kind, version)">{{ t('svc.start') }}</button>
          <!-- 端口与密码只在建容器时落定：数据服务给一条把配置送进容器的路（重建，数据卷保留） -->
          <button v-if="needsPassword(kind)" class="btn btn-sm" data-action="rebuild-service" :data-kind="kind" :data-version="version" :title="t('svc.rebuild.hint')" @click="modals.openRebuildModal(kind, version)">{{ t('svc.rebuild') }}</button>
          <button class="btn btn-sm btn-danger" data-action="uninstall" :data-kind="kind" :data-version="version" @click="modals.openUninstallModal(kind, version)">{{ t('svc.uninstall') }}</button>
        </div>
      </article>
    </div>
  </div>
</template>
