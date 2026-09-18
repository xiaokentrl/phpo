<script setup lang="ts">
// Settings 视图：1:1 迁移原型 renderSettings（2706–2712）；4 组：布局 / 缩放 / 外观 / 托盘
// 追加「应用升级」组（T604 / §5.9）：当前版本展示 + 打开升级弹窗（后端为唯一权威）
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { usePrefsStore } from '@/stores/prefsStore'
import { useLayoutStore } from '@/stores/layoutStore'
import { useModals } from '@/composables/useModals'
import { currentVersion } from '@/api/updater'
import { LAYOUT_LIMITS, LAYOUT_PRESETS, UI_SCALE, type PresetName } from '@/constants/layout'
import type { Locale } from '@/locales'

const { t, locale, setLocale } = useI18n()
const state = useAppState()
const prefs = usePrefsStore()
const layout = useLayoutStore()
const { openUpdateModal } = useModals()
const version = ref('')
onMounted(async () => { version.value = await currentVersion() })

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

    <div class="card" style="margin-bottom: 16px">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px">
        <h3 style="font-size: 14px; font-weight: 600">{{ t('settings.layout') }}</h3>
        <span class="chip">{{ t('settings.layout.hint') }}</span>
      </div>
      <div style="display: flex; flex-direction: column; gap: 16px">
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

    <div class="card" style="margin-bottom: 16px">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px">
        <h3 style="font-size: 14px; font-weight: 600">{{ t('settings.zoom') }}</h3>
        <span class="chip">{{ t('settings.zoom.hint') }}</span>
      </div>
      <div style="display: flex; flex-direction: column; gap: 16px">
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

    <div class="card" style="margin-bottom: 16px">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px">
        <h3 style="font-size: 14px; font-weight: 600">{{ t('settings.appearance') }}</h3>
        <span class="chip">{{ t('settings.appearance.hint') }}</span>
      </div>
      <div class="field">
        <label>{{ t('settings.language') }}</label>
        <div class="quick-picks" id="lang-picker">
          <button class="pick" :class="{ selected: locale === 'zh-CN' }" data-lang="zh-CN" @click="pickLang('zh-CN')">简体中文</button>
          <button class="pick" :class="{ selected: locale === 'en-US' }" data-lang="en-US" @click="pickLang('en-US')">English</button>
        </div>
      </div>
    </div>

    <div class="card" style="margin-bottom: 16px">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px">
        <h3 style="font-size: 14px; font-weight: 600">{{ t('settings.tray') }}</h3>
        <span class="chip">{{ t('settings.tray.hint') }}</span>
      </div>
      <div style="display: flex; flex-direction: column; gap: 12px">
        <label class="danger-check" style="background: var(--bg); border-color: var(--border); cursor: pointer">
          <input v-model="state.tray.minimizeOnClose" type="checkbox" id="tray-minimize" style="accent-color: var(--accent)" />
          <span class="check-label" style="color: var(--text-dim)">{{ t('settings.tray.minimize') }}</span>
        </label>
        <label class="danger-check" style="background: var(--bg); border-color: var(--border); cursor: pointer">
          <input v-model="state.tray.enabled" type="checkbox" id="tray-show" style="accent-color: var(--accent)" />
          <span class="check-label" style="color: var(--text-dim)">{{ t('settings.tray.show') }}</span>
        </label>
      </div>
    </div>

    <div class="card">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px">
        <h3 style="font-size: 14px; font-weight: 600">{{ t('settings.upgrade') }}</h3>
        <span class="chip">{{ t('settings.upgrade.hint') }}</span>
      </div>
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 12px">
        <span style="color: var(--text-dim)">{{ t('update.subtitle', { version }) }}</span>
        <button class="btn btn-primary" type="button" id="check-update-btn" @click="openUpdateModal()">{{ t('update.check') }}</button>
      </div>
    </div>
  </div>
</template>
