<script setup lang="ts">
// 服务视图：1:1 迁移原型 renderService + versionCard（2636–2692）；php/mysql/pgsql/redis/nginx 共用
import { computed } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useModals } from '@/composables/useModals'
import PasswordField from '@/components/common/PasswordField.vue'
import { DIR_ROWS, DEFAULT_FILE_COUNT, SVC_META } from '@/constants/service'
import type { ServiceKind } from '@/types'
import { needsPassword, needsPort, suggestPortFor } from '@/utils/format'
import { verRoot } from '@/utils/path'

const props = defineProps<{ kind: ServiceKind }>()
const { t } = useI18n()
const state = useAppState()
const modals = useModals()

const meta = computed(() => SVC_META[props.kind])
const versions = computed(() => state.installed[props.kind] || [])

// versionCard：端口键 {KIND}_{ver}_PORT（nginx 走 NGINX_PORT）
function portValue(version: string): string {
  if (props.kind === 'nginx') return state.env.NGINX_PORT || '80'
  const key = `${props.kind.toUpperCase()}_${version.replace(/\./g, '')}_PORT`
  return String(state.env[key] || suggestPortFor(props.kind, version) || '')
}
function dirPath(version: string, sub: string): string {
  return `${verRoot(state.env, props.kind, version)}/${sub}`
}
function extCount(version: string): number {
  return (state.phpExtensions[version] || []).length
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
            <span class="inline-edit" data-inline="port" :data-kind="kind" :data-version="version" data-field="port" :data-original="portValue(version)" tabindex="0" :title="t('common.edit')">{{ portValue(version) }}</span>
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
          <button class="btn btn-sm btn-danger" data-action="uninstall" :data-kind="kind" :data-version="version" @click="modals.openUninstallModal(kind, version)">{{ t('svc.uninstall') }}</button>
        </div>
      </article>
    </div>
  </div>
</template>
