<script setup lang="ts">
// Sites 视图：1:1 迁移原型 renderSites + siteRow（2610–2634）
// T107：接入站点/伪静态/nginx 配置/删除模态；行操作弹层忠实原型 openRowMenu
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useModals } from '@/composables/useModals'
import { useLayoutStore } from '@/stores/layoutStore'
import { usePhpSwitch } from '@/composables/usePhpSwitch'
import { usePortSuggest } from '@/composables/usePortSuggest'
import type { Site } from '@/types'
import { cmpVer, siteUrl, suggestPortFor } from '@/utils/format'
import { hostToContainer } from '@/utils/path'
import { addSiteHosts, hasBackend } from '@/api/site'
import { envKeyPort } from '@/api/env'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { syncState } from '@/composables/useStateSync'

const { t } = useI18n()
const state = useAppState()
const modals = useModals()
const layout = useLayoutStore()
const { switchPhp } = usePhpSwitch()
const { applyPort } = usePortSuggest()

const phpVers = computed(() => state.installed.php)
const healthy = computed(() => state.sites.filter((s) => s.health === 'up').length)
// nginxPort：真实快照无 NGINX_PORT 键（那是 demo 占位），按已装 nginx 版本的端口键取值，未落库回落建议端口
const nginxPort = computed(() => {
  const v = state.installed.nginx[0] || ''
  if (!v) return '—'
  return String(state.env[envKeyPort('nginx', v)] || suggestPortFor('nginx', v) || '—')
})

const menuDomain = ref<string | null>(null)
const menuPos = ref<{ left: number; top: number }>({ left: 0, top: 0 })
// 加 hosts 进行中的域名：提权弹窗可能停在系统侧，期间禁止重复点击
const hostsBusy = ref('')

// T404：端口行内编辑；改端口走 usePortSuggest（占用则顺延 + DangerConfirm）
const editingPort = ref<string | null>(null)
const portDraft = ref('')
function startEditPort(domain: string, current: number): void {
  editingPort.value = domain
  portDraft.value = String(current)
}
function commitPort(site: Site): void {
  if (editingPort.value !== site.domain) return
  const v = parseInt(portDraft.value.trim(), 10)
  editingPort.value = null
  if (!Number.isInteger(v) || v === site.port) return
  applyPort(site.domain, v)
}
function onPortKey(e: KeyboardEvent, site: Site): void {
  if (e.key === 'Escape') { editingPort.value = null; return }
  if (e.key === 'Enter') commitPort(site)
}
// T405：PHP 版本切换（精确上游，硬红线 1）
function onPhpChange(site: Site, e: Event): void {
  switchPhp(site.domain, (e.target as HTMLSelectElement).value, site.php)
}

function healthPill(h: Site['health']) {
  return { up: 'pill-ok', warn: 'pill-warn', down: 'pill-err' }[h]
}
function healthLabel(h: Site['health']) {
  return t('sites.health.' + h)
}
function phpOptions(site: Site): string[] {
  return [...new Set([...phpVers.value, site.php].filter(Boolean))].sort(cmpVer)
}
function isUninstalled(v: string): boolean {
  return !!v && !phpVers.value.includes(v)
}

// 「加 hosts」：写系统 hosts 走后端三段式；Hosts 列由快照真值回流，此处不做乐观更新（硬红线 4）
function onAddHosts(site: Site): void {
  if (hostsBusy.value) return
  if (!hasBackend()) {
    runTask(['site', 'hosts', site.domain], `${t('sites.hosts.add')} ${site.domain}`, { type: 'site-hosts', domain: site.domain })
    return
  }
  hostsBusy.value = site.domain
  addSiteHosts(site.domain)
    .then((warn) => toast(warn || t('sites.hosts.toast', { domain: site.domain }), warn ? 'err' : 'ok', warn ? 6000 : 2400))
    .catch((e: unknown) => toast(String(e), 'err', 4600))
    .finally(() => {
      hostsBusy.value = ''
      // 写后拉权威快照收口：Hosts 列真值以后端探针为准，不靠本地推断（硬红线 4）
      syncState()
    })
}

function closeRowMenu(): void {
  menuDomain.value = null
}
// 忠实原型 openRowMenu：按缩放把视口像素换算成布局像素，右对齐锚点下方
function openRowMenu(anchor: HTMLElement, domain: string): void {
  closeRowMenu()
  const s = layout.scale || 1
  const r = anchor.getBoundingClientRect()
  const gap = 6 * s
  const edge = 8 * s
  const menuW = 190
  const vw = window.innerWidth
  let left = r.right - menuW
  let top = r.bottom + gap
  if (left < edge) left = edge
  if (left + menuW > vw - edge) left = vw - menuW - edge
  if (top < edge) top = edge
  menuPos.value = { left: left / s, top: top / s }
  menuDomain.value = domain
}
function onDocClick(e: MouseEvent): void {
  if (menuDomain.value && !(e.target as HTMLElement).closest('.row-menu') && !(e.target as HTMLElement).closest('[data-action="row-menu"]')) closeRowMenu()
}
function onDocKey(e: KeyboardEvent): void {
  if (e.key === 'Escape') closeRowMenu()
}
onMounted(() => {
  document.addEventListener('click', onDocClick, true)
  document.addEventListener('keydown', onDocKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick, true)
  document.removeEventListener('keydown', onDocKey)
})
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('sites.title') }}</h1>
        <p class="view-sub">{{ t('sites.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" data-action="site-add" @click="modals.openSiteAddModal()">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14" /></svg>
          {{ t('sites.actions.add') }}
        </button>
      </div>
    </header>

    <div v-if="state.sites.length === 0" class="empty">
      <div class="empty-icon">🌍</div>
      <h2>{{ t('sites.empty.title') }}</h2>
      <p>{{ t('sites.empty.desc') }}</p>
      <button class="btn btn-primary" data-action="site-add" @click="modals.openSiteAddModal()">{{ t('sites.empty.action') }}</button>
    </div>

    <template v-else>
      <div class="summary">
        <div class="summary-item"><div class="summary-num">{{ state.sites.length }}</div><div class="summary-label">{{ t('sites.count') }}</div></div>
        <div class="summary-item"><div class="summary-num">{{ phpVers.length }}</div><div class="summary-label">{{ t('sites.phpVersions') }}</div></div>
        <div class="summary-item"><div class="summary-num" style="color: var(--ok)">{{ healthy }}</div><div class="summary-label">{{ t('sites.healthy') }}</div></div>
        <div class="summary-item"><div class="summary-num">{{ nginxPort }}</div><div class="summary-label">{{ t('sites.nginxPort') }}</div></div>
      </div>

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th style="width: 22%">{{ t('sites.col.domain') }}</th>
              <th class="col-port" style="width: 9%">{{ t('sites.col.port') }}</th>
              <th style="width: 11%">{{ t('sites.col.php') }}</th>
              <th style="width: 22%">{{ t('sites.col.root') }}</th>
              <th style="width: 10%">{{ t('sites.col.health') }}</th>
              <th style="width: 10%">{{ t('sites.col.hosts') }}</th>
              <th class="col-actions" style="width: 16%"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="site in state.sites" :key="site.domain">
              <td>
                <a class="site-domain" :href="siteUrl(site)" target="_blank" rel="noopener noreferrer" :title="siteUrl(site)">
                  <span class="favicon">{{ site.domain.charAt(0).toUpperCase() }}</span>
                  <span class="domain-text">{{ site.domain }}</span>
                  <svg class="external-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" /><path d="M15 3h6v6M10 14 21 3" /></svg>
                </a>
              </td>
              <td class="col-port">
                <input
                  v-if="editingPort === site.domain"
                  v-model="portDraft"
                  class="port-input"
                  type="text"
                  inputmode="numeric"
                  autofocus
                  @blur="commitPort(site)"
                  @keyup="onPortKey($event, site)"
                >
                <span
                  v-else
                  class="port-edit"
                  :data-domain="site.domain"
                  :data-original="site.port"
                  tabindex="0"
                  :title="t('common.edit')"
                  @click="startEditPort(site.domain, site.port)"
                  @keyup.enter="startEditPort(site.domain, site.port)"
                >{{ site.port }}</span>
              </td>
              <td>
                <select class="php-select" :data-domain="site.domain" @change="onPhpChange(site, $event)">
                  <!-- 未选 PHP 的降级站点：以「未选择」占位，不把空值伪装成某个版本 -->
                  <option v-if="!site.php" value="" selected>{{ t('sites.php.none') }}</option>
                  <option v-for="v in phpOptions(site)" :key="v" :value="v" :selected="v === site.php">{{ v }}{{ isUninstalled(v) ? t('sites.php.uninstalled') : '' }}</option>
                </select>
              </td>
              <td>
                <button class="path-btn" data-action="open-path" :data-path="site.root" :title="`${site.root} → ${hostToContainer(state.env, site.root)}`">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
                  <span class="path-text">{{ site.root }}</span>
                </button>
              </td>
              <td>
                <span class="status-pill" :class="healthPill(site.health)"><span class="pill-dot"></span>{{ healthLabel(site.health) }}</span>
              </td>
              <td>
                <span v-if="site.hosts" class="chip chip-accent">{{ t('sites.hosts.resolved') }}</span>
                <button v-else class="btn btn-sm" data-action="hosts-add" :data-domain="site.domain" :disabled="!!hostsBusy" @click="onAddHosts(site)">{{ t('sites.hosts.add') }}</button>
              </td>
              <td class="col-actions">
                <div class="row-actions">
                  <button class="icon-btn" :title="t('sites.actions.more')" data-action="row-menu" :data-domain="site.domain" @click="openRowMenu($event.currentTarget as HTMLElement, site.domain)">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="5" r="1.7" /><circle cx="12" cy="12" r="1.7" /><circle cx="12" cy="19" r="1.7" /></svg>
                  </button>
                  <button class="icon-btn" :title="t('sites.actions.delete')" data-action="site-remove" :data-domain="site.domain" @click="modals.openSiteRemoveModal(site.domain)">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M6 6l1 14a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-14" /></svg>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>

  <Teleport to="body">
    <div v-if="menuDomain" class="row-menu" :style="{ left: menuPos.left + 'px', top: menuPos.top + 'px' }">
      <button class="row-menu-item" type="button" @click="modals.openRewriteModal(menuDomain!); closeRowMenu()">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16v6H4z" /><path d="M4 14h10v6H4z" /><path d="M18 14l3 3-3 3" /></svg>{{ t('sites.actions.rewrite') }}
      </button>
      <div class="row-menu-sep" />
      <button class="row-menu-item" type="button" @click="modals.openSiteConfigModal(menuDomain!); closeRowMenu()">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9" /><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4z" /></svg>{{ t('sites.actions.editConfig') }}
      </button>
    </div>
  </Teleport>
</template>
