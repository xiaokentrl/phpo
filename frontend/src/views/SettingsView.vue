<script setup lang="ts">
// Settings 视图：1:1 迁移原型 renderSettings（2706–2712）；4 组：布局 / 缩放 / 外观 / 托盘
// 追加「应用升级」组（T604 / §5.9）：当前版本展示 + 打开升级弹窗（后端为唯一权威）
// 追加「Docker 镜像源」组：一行一个地址 + 保存 + 检测（延迟分级）；清单权威值在 config.yaml，回显只认后端
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { usePrefsStore } from '@/stores/prefsStore'
import { useLayoutStore } from '@/stores/layoutStore'
import { useModals } from '@/composables/useModals'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { backendMsg } from '@/utils/backendMsg'
import { currentVersion } from '@/api/updater'
import { getAutoStart, setAutoStart } from '@/api/autostart'
import { getStartMinimized, setStartMinimized } from '@/api/startMinimized'
import { getAutostartServices, setAutostartServices } from '@/api/autostartServices'
import { getSources, setSources, probeSources } from '@/api/dockerSource'
import { LAYOUT_LIMITS, LAYOUT_PRESETS, UI_SCALE, type PresetName } from '@/constants/layout'
import { SVC_META } from '@/constants/service'
import { useAppState } from '@/stores/appState'
import type { Locale } from '@/locales'
import type { MirrorSource, ServiceKind } from '@/types'

const { t, locale, setLocale } = useI18n()
const prefs = usePrefsStore()
const layout = useLayoutStore()
const { openUpdateModal } = useModals()
const { preflight } = usePreflight()
const version = ref('')
onMounted(async () => { version.value = await currentVersion() })

// 开机自启动：OS 注册态是唯一权威（硬红线 4）——每次进页现问一次，不落本地缓存。
// 切换即写（Enable/Disable 同步返回）；写失败回滚开关并 toast 后端原话现译，绝不留在假状态上。
const autoStart = ref(false)
const autoStartReady = ref(true)
const autoStartBusy = ref(false)

onMounted(async () => {
  const v = await getAutoStart()
  if (v === null) { autoStartReady.value = false; return }
  autoStart.value = v
})

async function toggleAutoStart(): Promise<void> {
  if (autoStartBusy.value || !autoStartReady.value) return
  const next = !autoStart.value
  autoStartBusy.value = true
  try {
    await setAutoStart(next)
    autoStart.value = next
    toast(t('settings.autostart.done', { state: t(next ? 'common.on' : 'common.off') }), 'ok', 2200)
  } catch (e: unknown) {
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    autoStartBusy.value = false
  }
}

// 启动后最小化：勾上则下次 phpo 启动时不显示主窗口、也不占任务栏，只在托盘待着，点托盘图标唤回。
// 权威值在 config.yaml，每次进页现问一次；正在跑的这一轮不受影响（窗口已经建出来了）。
const startMin = ref(false)
const startMinReady = ref(true)
const startMinBusy = ref(false)

onMounted(async () => {
  const v = await getStartMinimized()
  if (v === null) { startMinReady.value = false; return }
  startMin.value = v
})

async function toggleStartMin(): Promise<void> {
  if (startMinBusy.value || !startMinReady.value) return
  const next = !startMin.value
  startMinBusy.value = true
  try {
    await setStartMinimized(next)
    startMin.value = next
    toast(t('settings.startMin.done', { state: t(next ? 'common.on' : 'common.off') }), 'ok', 2200)
  } catch (e: unknown) {
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    startMinBusy.value = false
  }
}

// 勾选哪些服务在 phpo 启动后自动加入任务队列（真正的启动在后端，界面只登记名单）。
// 名单的权威值在 config.yaml，每次进页现问一次；点一下就整份保存、保存后以后端回读为准（硬红线 4）。
const appState = useAppState()
const svcChecked = ref<string[]>([])
const svcReady = ref(true)
const svcBusy = ref(false)

onMounted(async () => {
  const list = await getAutostartServices()
  if (list === null) { svcReady.value = false; return }
  svcChecked.value = list
})

// svcOptions 只列本机已安装的版本：没装的服务勾了也不会启动，摆出来只会误导。
// 值用后端的 "kind/version" 引用，标签是「服务名 版本号」。
const svcOptions = computed(() => {
  const rows: Array<{ svcRef: string; label: string }> = []
  for (const kind of Object.keys(SVC_META) as ServiceKind[]) {
    for (const ver of appState.installed[kind]) {
      rows.push({ svcRef: `${kind}/${ver}`, label: `${t(SVC_META[kind].titleKey)} ${ver}` })
    }
  }
  return rows
})

async function toggleSvc(svcRef: string) {
  if (svcBusy.value || !svcReady.value) return
  const next = svcChecked.value.includes(svcRef)
    ? svcChecked.value.filter((x) => x !== svcRef)
    : [...svcChecked.value, svcRef]
  svcBusy.value = true
  try {
    await setAutostartServices(next)
    // 回读后端落库的那份，不用本地乐观值（保存可能被 preflight 判回）
    svcChecked.value = (await getAutostartServices()) ?? []
    toast(t('settings.autostart.svcSaved'), 'ok', 2200)
  } catch (e) {
    svcChecked.value = (await getAutostartServices()) ?? []
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    svcBusy.value = false
  }
}

// Docker 镜像源（联网拉取那一级）：清单的权威值在 config.yaml，回显只认后端读回来的那一份（硬红线 4）。
const dockerText = ref('')
const dockerSaved = ref<string[]>([])
const dockerResults = ref<MirrorSource[] | null>(null)
const dockerSaving = ref(false)
const dockerTesting = ref(false)
const dockerReady = ref(true)

onMounted(async () => {
  const list = await getSources()
  if (list === null) { dockerReady.value = false; return }
  dockerSaved.value = list
  dockerText.value = list.join('\n')
})

// cleanMirrorLine 清理用户粘贴的镜像地址：去首尾引号/逗号/空白（从网页复制常带格式杂物）
function cleanMirrorLine(line: string): string {
  return line.replace(/^[\s"',，]+|[\s"',，]+$/g, '')
}

// dockerLines 与后端 config.ValidateRegistryHosts 同口径：忽略空行，只把非空行当地址。
// 每行经 cleanMirrorLine 清洗——去引号/逗号/空白后仍非空才算有效地址。
const dockerLines = computed(() => dockerText.value.split(/\r?\n/).map(cleanMirrorLine).filter((x) => x !== ''))
const dockerDirty = computed(() => dockerLines.value.join('\n') !== dockerSaved.value.join('\n'))
// fastestOf 是本次检测结果里延迟最小的可用源；检测结果的行序与文本框的非空行一一对应，故按下标记。
const dockerFastest = computed(() => {
  const rs = dockerResults.value ?? []
  let best = -1
  rs.forEach((r, i) => { if (r.ok && (best < 0 || r.latencyMs < rs[best].latencyMs)) best = i })
  return best
})

async function saveDockerSources() {
  if (dockerSaving.value) return
  const pf = preflight('docker-source-set', { sources: dockerLines.value })
  if (!pf.ok) { toast(pf.errors.join('\n'), 'err', 4600); return }
  dockerSaving.value = true
  try {
    await setSources(dockerLines.value)
    const list = await getSources()
    dockerSaved.value = list ?? []
    dockerText.value = dockerSaved.value.join('\n')
    // 地址集变了，旧检测结果就不再对应这些行，清空等用户重新点检测
    dockerResults.value = null
    toast(t('settings.docker.saved'), 'ok', 2600)
  } catch (e) {
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    dockerSaving.value = false
  }
}

async function testDockerSources() {
  if (dockerTesting.value) return
  if (!dockerLines.value.length) { toast(t('settings.docker.none'), 'err', 2600); return }
  dockerTesting.value = true
  try {
    dockerResults.value = await probeSources(dockerLines.value)
  } catch (e) {
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    dockerTesting.value = false
  }
}

const presetKeys = Object.keys(LAYOUT_PRESETS) as PresetName[]
const currentPct = computed(() => Math.round(layout.scale * 100))

function isPresetActive(name: PresetName): boolean {
  const p = LAYOUT_PRESETS[name]
  return p.sidebar === layout.sidebarWidth && p.drawer === layout.drawerHeight
}
function onSidebar(e: Event) {
  layout.setSidebar(parseInt((e.target as HTMLInputElement).value, 10))
}
function onDrawer(e: Event) {
  layout.setDrawer(parseInt((e.target as HTMLInputElement).value, 10))
}
function onZoom(e: Event) {
  layout.setScale(parseInt((e.target as HTMLInputElement).value, 10) / 100)
}
function pickLang(l: Locale) {
  setLocale(l)
}
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('settings.title') }}</h1>
        <p class="view-sub">{{ t('settings.subtitle') }}</p>
      </div>
    </header>

    <div class="card card-gap">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.layout') }}</h3>
        <span class="chip">{{ t('settings.layout.hint') }}</span>
      </div>
      <div class="stack-16">
        <div class="field">
          <label>{{ t('settings.layout.presets') }}</label>
          <div class="quick-picks">
            <button v-for="key in presetKeys" :key="key" class="pick layout-preset-btn" :class="{ selected: isPresetActive(key) }" :data-preset="key" @click="layout.applyPreset(key)">{{ t('settings.layout.preset.' + key) }}</button>
            <button class="pick" id="layout-reset-btn" @click="layout.reset()">{{ t('settings.layout.reset') }}</button>
          </div>
          <div class="hint">{{ t('settings.layout.hint2') }}</div>
        </div>
        <div class="field">
          <label>{{ t('settings.layout.sidebar') }}</label>
          <div class="slider-row">
            <input type="range" id="setting-sidebar-width" :min="LAYOUT_LIMITS.sidebar.min" :max="LAYOUT_LIMITS.sidebar.max" step="2" :value="layout.sidebarWidth" @input="onSidebar" />
            <span class="slider-value" id="sidebar-width-val">{{ layout.sidebarWidth }}px</span>
          </div>
        </div>
        <div class="field">
          <label>{{ t('settings.layout.drawer') }}</label>
          <div class="slider-row">
            <input type="range" id="setting-drawer-height" :min="LAYOUT_LIMITS.drawer.min" :max="LAYOUT_LIMITS.drawer.max" step="2" :value="layout.drawerHeight" @input="onDrawer" />
            <span class="slider-value" id="drawer-height-val">{{ layout.drawerHeight }}px</span>
          </div>
        </div>
      </div>
    </div>

    <div class="card card-gap">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.zoom') }}</h3>
        <span class="chip">{{ t('settings.zoom.hint') }}</span>
      </div>
      <div class="stack-16">
        <div class="field">
          <label>{{ t('settings.zoom.presets') }}</label>
          <div class="quick-picks">
            <button v-for="v in UI_SCALE.snap" :key="v" class="pick zoom-preset-btn" :class="{ selected: Math.round(v * 100) === currentPct }" :data-scale="v" @click="layout.setScale(v)">{{ Math.round(v * 100) }}%</button>
            <button class="pick" id="zoom-reset-btn" @click="layout.zoomReset()">{{ t('settings.zoom.reset') }}</button>
          </div>
        </div>
        <div class="field">
          <label>{{ t('settings.zoom.slider') }}</label>
          <div class="slider-row">
            <input type="range" id="setting-zoom" :min="Math.round(UI_SCALE.min * 100)" :max="Math.round(UI_SCALE.max * 100)" :step="Math.round(UI_SCALE.step * 100)" :value="currentPct" @input="onZoom" />
            <span class="slider-value" id="zoom-value">{{ currentPct }}%</span>
          </div>
        </div>
      </div>
    </div>

    <div class="card card-gap">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.appearance') }}</h3>
        <span class="chip">{{ t('settings.appearance.hint') }}</span>
      </div>
      <div class="field">
        <label>{{ t('settings.language') }}</label>
        <div class="quick-picks" id="lang-picker">
          <button class="pick" :class="{ selected: locale === 'en-US' }" data-lang="en-US" @click="pickLang('en-US')">English</button>
          <button class="pick" :class="{ selected: locale === 'zh-CN' }" data-lang="zh-CN" @click="pickLang('zh-CN')">简体中文</button>
          <button class="pick" :class="{ selected: locale === 'zh-TW' }" data-lang="zh-TW" @click="pickLang('zh-TW')">繁體中文</button>
          <button class="pick" :class="{ selected: locale === 'ko' }" data-lang="ko" @click="pickLang('ko')">한국어</button>
          <button class="pick" :class="{ selected: locale === 'fr' }" data-lang="fr" @click="pickLang('fr')">Français</button>
          <button class="pick" :class="{ selected: locale === 'de' }" data-lang="de" @click="pickLang('de')">Deutsch</button>
          <button class="pick" :class="{ selected: locale === 'es' }" data-lang="es" @click="pickLang('es')">Español</button>
          <button class="pick" :class="{ selected: locale === 'tr' }" data-lang="tr" @click="pickLang('tr')">Türkçe</button>
        </div>
      </div>
    </div>

    <div class="card card-gap">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.tray') }}</h3>
        <span class="chip">{{ t('settings.tray.hint') }}</span>
      </div>
      <div class="stack-12">
        <label class="danger-check tray-check">
          <input v-model="prefs.tray.minimizeOnClose" type="checkbox" id="tray-minimize" class="tray-check-input" />
          <span class="check-label text-dim">{{ t('settings.tray.minimize') }}</span>
        </label>
        <label class="danger-check tray-check">
          <input v-model="prefs.tray.enabled" type="checkbox" id="tray-show" class="tray-check-input" />
          <span class="check-label text-dim">{{ t('settings.tray.show') }}</span>
        </label>
      </div>
    </div>

    <div class="card card-gap">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.autostart') }}</h3>
        <span class="chip">{{ t('settings.autostart.hint') }}</span>
      </div>
      <div class="stack-12">
        <label class="danger-check tray-check">
          <input
            type="checkbox"
            id="start-minimized"
            class="tray-check-input"
            :checked="startMin"
            :disabled="!startMinReady || startMinBusy"
            @change="toggleStartMin"
          />
          <span class="check-label text-dim">{{ t('settings.startMin') }}</span>
        </label>
        <div class="alert alert-warn alert-gap" v-if="!startMinReady">{{ t('settings.startMin.noBackend') }}</div>
        <label class="danger-check tray-check">
          <input
            type="checkbox"
            id="autostart"
            class="tray-check-input"
            :checked="autoStart"
            :disabled="!autoStartReady || autoStartBusy"
            @change="toggleAutoStart"
          />
          <span class="check-label text-dim">{{ t('settings.autostart.enable') }}</span>
        </label>
      </div>

      <div class="field">
        <label>{{ t('settings.autostart.svcLabel') }}</label>
        <div class="alert alert-warn alert-gap" v-if="!svcReady">{{ t('settings.autostart.noBackend') }}</div>
        <div class="hint hint-empty" v-else-if="!svcOptions.length">{{ t('settings.autostart.svcEmpty') }}</div>
        <div v-else class="quick-picks">
          <button
            v-for="opt in svcOptions"
            :key="opt.svcRef"
            class="pick"
            :class="{ selected: svcChecked.includes(opt.svcRef) }"
            type="button"
            :disabled="svcBusy"
            @click="toggleSvc(opt.svcRef)"
          >{{ opt.label }}</button>
        </div>
        <div class="hint">{{ t('settings.autostart.svcHint') }}</div>
      </div>
    </div>

    <div class="card card-gap">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.docker') }}</h3>
        <span class="chip">{{ t('settings.docker.hint') }}</span>
      </div>
      <div class="alert alert-warn alert-gap" v-if="!dockerReady">{{ t('settings.docker.noBackend') }}</div>
      <div class="field">
        <label>{{ t('settings.docker.label') }}</label>
        <textarea v-model="dockerText" rows="4" spellcheck="false" :disabled="!dockerReady"
          class="source-textarea"
          :placeholder="t('settings.docker.placeholder')" />
        <div class="hint">{{ t('settings.docker.hint2') }}</div>
      </div>
      <div class="source-actions">
        <button class="btn btn-primary" type="button" id="docker-source-save" :disabled="!dockerReady || dockerSaving || !dockerDirty" @click="saveDockerSources">{{ t('settings.docker.save') }}</button>
        <button class="btn" type="button" id="docker-source-test" :disabled="!dockerReady || dockerTesting || !dockerLines.length" @click="testDockerSources">{{ dockerTesting ? t('settings.docker.testing') : t('settings.docker.test') }}</button>
      </div>

      <div v-if="dockerResults" class="table-wrap probe-grid">
        <table class="probe-table">
          <thead>
            <tr>
              <th class="th-source">{{ t('settings.docker.col.source') }}</th>
              <th class="th-latency">{{ t('settings.docker.col.latency') }}</th>
              <th>{{ t('settings.docker.col.verdict') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in dockerResults" :key="i">
              <td><span class="mono mono-size">{{ r.host }}</span></td>
              <td><span class="mono text-dim">{{ r.ok ? `${r.latencyMs} ms` : '—' }}</span></td>
              <td>
                <span v-if="i === dockerFastest" class="chip chip-accent">{{ t('settings.docker.fastest') }}</span>
                <span v-if="r.ok" class="chip">{{ t('settings.docker.ok') }}</span>
                <span v-else class="cell-error">{{ r.error }}</span>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="hint hint-table">{{ t('settings.docker.hint3') }}</div>
      </div>
      <div class="hint hint-empty" v-else-if="dockerSaved.length === 0">{{ t('settings.docker.empty') }}</div>
    </div>

    <div class="card">
      <div class="section-head">
        <h3 class="section-title">{{ t('settings.upgrade') }}</h3>
        <span class="chip">{{ t('settings.upgrade.hint') }}</span>
      </div>
      <div class="upgrade-row">
        <span class="text-dim">{{ t('update.subtitle', { version }) }}</span>
        <button class="btn btn-primary" type="button" id="check-update-btn" @click="openUpdateModal()">{{ t('update.check') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.card-gap {
  margin-bottom: 16px;
}

.section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 14px;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
}

.stack-16 {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.stack-12 {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.tray-check {
  background: var(--bg);
  border-color: var(--border);
  cursor: pointer;
}

.tray-check-input {
  accent-color: var(--accent);
}

.text-dim {
  color: var(--text-dim);
}

.alert-gap {
  margin-bottom: 12px;
}

.source-textarea {
  font-family: var(--mono);
  font-size: 12.5px;
  resize: vertical;
}

.source-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.probe-grid {
  margin-top: 14px;
}

.probe-table {
  min-width: 0;
}

.th-source {
  width: 44%;
}

.th-latency {
  width: 20%;
}

.mono-size {
  font-size: 12.5px;
}

.cell-error {
  color: var(--danger);
  font-size: 12.5px;
}

.hint-table {
  padding: 0 12px 10px;
  font-size: 11.5px;
  color: var(--text-mute);
  line-height: 1.5;
}

.hint-empty {
  margin-top: 10px;
  font-size: 11.5px;
  color: var(--text-mute);
  line-height: 1.5;
}

.upgrade-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
</style>
