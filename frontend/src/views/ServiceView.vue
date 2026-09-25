<script setup lang="ts">
// 服务视图：1:1 迁移原型 renderService + versionCard（2636–2692）；php/mysql/pgsql/redis/nginx 共用
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useTaskStore } from '@/stores/taskStore'
import { useModals } from '@/composables/useModals'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { syncState } from '@/composables/useStateSync'
import { hasBackend } from '@/api/site'
import { Dialogs } from '@wailsio/runtime'
import { dataDirOf, defaultDataDir, envKeyPort, setPort, setServiceDataDir } from '@/api/env'
import PasswordField from '@/components/common/PasswordField.vue'
import { DIR_ROWS, DEFAULT_FILE_COUNT, GAP_REASON_KEYS, SVC_META } from '@/constants/service'
import { catalogFor } from '@/constants/ext'
import type { ServiceKind, TaskBrief } from '@/types'
import { needsPort, needsPassword } from '@/utils/format'
import { verRoot } from '@/utils/path'

const props = defineProps<{ kind: ServiceKind }>()
const { t } = useI18n()
const state = useAppState()
const tasks = useTaskStore()
const modals = useModals()
const { preflight } = usePreflight()

const meta = computed(() => SVC_META[props.kind])
const versions = computed(() => state.installed[props.kind] || [])

// versionCard：端口键 {KIND}_{ver}_PORT（与后端 config.EnvKeyPort 同源，nginx 亦走此键）。
// 键未落库时回退 SVC_META.defaultPort——它与后端 registry 的 Spec.HostPort 同值，即容器真正发布的端口；
// 不再用 suggestPortFor 猜（那是原型给安装弹窗预填的算法，mysql 8.4 会算成 3384，卡片却显示 3306）。
function portValue(version: string): string {
  const key = envKeyPort(props.kind, version)
  return String(state.env[key] || meta.value.defaultPort || '')
}
function dirPath(version: string, sub: string): string {
  // 数据目录可整体自定义（需求 7）：自定义即唯一生效路径，未自定义才随服务根派生
  if (sub === 'data') return dataDirOf(props.kind, version)
  return `${verRoot(state.env, props.kind, version)}/${sub}`
}
// extCount 卡片上「已启用扩展」的颗数：只数扩展目录里那些（管理扩展弹窗铺的就是这一份）。
// 快照里现在是容器内实测的全集，含 Core／date 这类目录管不到的名字，照直数会比弹窗对不上。
function extCount(version: string): number {
  const cat = new Set(catalogFor(version).map((e) => e.name))
  return (state.phpExtensions[version] || []).filter((n) => cat.has(n)).length
}

// busyPill 该版本卡片上有没有任务在跑/在排队：判据是后端 TaskBrief 的 kind+version（§5.6 队列详情走快照），
// 不按 label 文案反推——文案要 i18n，一改「进行中」就丢。
// 卡片上**其他**按钮照常可点：并发写操作按 FIFO 排队是合法路径（总纲 §5.6），拦它们是非必要限制。
// 唯一禁用的是「同一个正在等结果的那一颗」（tasks.isBusy 按 type+kind+version 判定，v2.9.13）——
// 重复点同一件事只会排出一单同样的任务，那是重复提交而不是并发需求。
function busyPill(version: string): string | null {
  const targets = (b?: TaskBrief | null) => !!b && b.kind === props.kind && b.version === version
  const run = tasks.runningBrief
  if (targets(run) && run) {
    const total = run.total || 0
    return total > 0 ? `${t('task.running')} ${Math.min(run.step, total)}/${total}` : t('task.running')
  }
  if ((tasks.pendingBriefs || []).some(targets)) return t('task.queued')
  return null
}

// gapReason 该版本缺失项的人话名词（§5.19）。未知取值原样透出——后端将来多一种原因，
// 界面最坏显示枚举名而不是留空白或误报成别的。
function gapReason(version: string): string {
  const g = state.gapOf(props.kind, version)
  if (!g) return ''
  const key = GAP_REASON_KEYS[g.reason]
  return key ? t(key) : g.reason
}

// gapTip 悬浮说明「哪一样不在了 + 怎么回来」。库内 installed 不因外部删除而自动改（硬红线 4），
// 缺失态是派生态；要恢复由用户自己点「启用」（Start 走幂等重建，§5.13.4）。
function gapTip(version: string): string {
  const g = state.gapOf(props.kind, version)
  if (!g) return ''
  return t('svc.gapTip', { reason: gapReason(version), ref: g.ref, start: t('svc.start') })
}

// 端口行内编辑：needsPort 的服务端口都会进容器 spec（nginx 是基准端口，与站点端口并集一起发布）
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

// 数据目录行内编辑（需求 7）：编辑 + 浏览两条入口都指向同一个落库动作（config.yaml → Rebind → 快照回流）。
// 自定义与默认互斥且只有一个生效：填回默认路径等于清除自定义，「恢复默认」走同一条写链路。
const dirEditing = ref('')
const dirDraft = ref('')
const dirSaving = ref('')

function isCustomDataDir(version: string): boolean {
  return dataDirOf(props.kind, version) !== defaultDataDir(props.kind, version)
}

function startEditDir(version: string): void {
  dirDraft.value = dataDirOf(props.kind, version)
  dirEditing.value = version
}

function cancelDir(): void {
  dirEditing.value = ''
}

// commitDir preflight → 落库 → 权威快照回显；数据目录进容器 binds，故必须重建容器才挂到新目录
async function commitDir(version: string, nextArg?: string): Promise<void> {
  if (dirSaving.value) return
  const raw = (nextArg ?? dirDraft.value).trim().replace(/\/+$/, '')
  dirEditing.value = ''
  const custom = isCustomDataDir(version) ? dataDirOf(props.kind, version) : ''
  // 填回默认路径 = 清除自定义（互斥唯一，不留两条数据目录）
  const next = !raw || raw === defaultDataDir(props.kind, version) ? '' : raw
  if (next === custom) return
  const pf = preflight('root-set', { field: 'data_dir', kind: props.kind, version, newValue: next })
  if (!pf.ok) {
    toast(pf.errors.join('\n'), 'err', 4600)
    return
  }
  if (pf.warnings.length) toast(pf.warnings.join('\n'), 'info', 4600)
  dirSaving.value = version
  try {
    await setServiceDataDir(props.kind, version, next)
    if (hasBackend()) await syncState()
    toast(next
      ? t('svc.dataDirSet', { kind: props.kind, version, path: next })
      : t('svc.dataDirReset', { kind: props.kind, version }), 'info', 5600)
  } catch (e) {
    toast(String(e), 'err', 4600)
  } finally {
    dirSaving.value = ''
  }
}

// browseDir 原生目录选择器：选中即提交，不经过行内输入框（避免点按钮触发的 blur 先落一次旧值）
async function browseDir(version: string): Promise<void> {
  const picked = await Dialogs.OpenFile({
    Title: t('svc.dataDirBrowse'),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    Directory: dataDirOf(props.kind, version) || undefined,
  })
  const abs = String(picked || '').replace(/\/+$/, '')
  if (abs) await commitDir(version, abs)
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
          <span v-if="busyPill(version)" class="status-pill pill-warn" data-task-busy>{{ busyPill(version) }}</span>
          <!-- 缺失态（§5.19）：Docker 侧已被外部停/删，但库里仍记已安装——卡片点名缺的是哪一样，不自动改权威态 -->
          <span v-if="state.gapOf(kind, version)" class="status-pill pill-warn" data-gap :title="gapTip(version)">
            <span class="pill-dot"></span>{{ t('svc.gapMissing', { reason: gapReason(version) }) }}
          </span>
        </div>

        <div>
          <div v-if="needsPort(kind)" class="kv">
            <span class="k">{{ t('svc.port') }}</span>
            <!-- 与原型一致：needsPort 的服务端口一律可行内改。nginx 的这项是「基准端口」，与站点端口并集一起发布 -->
            <span
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
            <template v-if="sub === 'data'">
              <span
                class="v inline-edit"
                :class="{ editing: dirEditing === version, saving: dirSaving === version }"
                data-inline="data-dir"
                :data-kind="kind"
                :data-version="version"
                tabindex="0"
                :title="t('svc.dataDirEdit')"
                @click="dirEditing !== version && startEditDir(version)"
                @keydown.enter="dirEditing !== version && startEditDir(version)"
              >
                <input
                  v-if="dirEditing === version"
                  v-model="dirDraft"
                  class="port-input"
                  type="text"
                  spellcheck="false"
                  autocomplete="off"
                  autofocus
                  :placeholder="defaultDataDir(kind, version)"
                  @keydown.enter.prevent="commitDir(version)"
                  @keydown.esc.prevent="cancelDir"
                  @blur="commitDir(version)"
                >
                <template v-else>{{ dirPath(version, sub) }}</template>
              </span>
              <button v-if="hasBackend()" class="btn btn-sm" type="button" data-action="browse-data-dir" :data-kind="kind" :data-version="version" :title="t('svc.dataDirBrowse')" @click="browseDir(version)">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
                {{ t('dir.browse') }}
              </button>
              <span v-if="isCustomDataDir(version)" class="chip chip-accent">{{ t('root.custom') }}</span>
            </template>
            <span v-else class="v" :title="dirPath(version, sub)">{{ dirPath(version, sub) }}</span>
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
          <button v-if="state.isServiceRunning(kind, version)" class="btn btn-sm" data-action="stop-service" :data-kind="kind" :data-version="version" :disabled="tasks.isBusy({ type: 'service-stop', kind, version })" @click="modals.stopService(kind, version)">{{ t('svc.stop') }}</button>
          <button v-else class="btn btn-sm btn-primary" data-action="start-service" :data-kind="kind" :data-version="version" :disabled="tasks.isBusy({ type: 'service-start', kind, version })" @click="modals.startService(kind, version)">{{ t('svc.start') }}</button>
          <!-- 端口与密码只在建容器时落定：凡有宿主端口发布/密码的服务都给一条把配置送进容器的路（重建，数据卷保留）。
               nginx 的基准端口同属这一类，改完同样要重建才重新绑宿主端口 -->
          <button v-if="needsPort(kind)" class="btn btn-sm" data-action="rebuild-service" :data-kind="kind" :data-version="version" :title="t('svc.rebuild.hint')" :disabled="tasks.isBusy({ type: 'update-config', kind, version })" @click="modals.openRebuildModal(kind, version)">{{ t('svc.rebuild') }}</button>
          <button class="btn btn-sm btn-danger" data-action="uninstall" :data-kind="kind" :data-version="version" :disabled="tasks.isBusy({ type: 'uninstall', kind, version })" @click="modals.openUninstallModal(kind, version)">{{ t('svc.uninstall') }}</button>
        </div>
      </article>
    </div>
  </div>
</template>
