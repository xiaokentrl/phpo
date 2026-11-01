// 主题与托盘偏好：9 套 data-theme + 托盘开关，仅存 localStorage（§3.1 原则 5：UI 偏好留前端）；
// 托盘两项额外投影到原生外壳（api/state.ts#setTrayPrefs），localStorage 仍是唯一权威。
import { defineStore } from 'pinia'
import { reactive, ref, watchEffect } from 'vue'
import { getTrayStatus, setTrayPrefs, type TrayStatus } from '@/api/state'
import { toast } from '@/composables/useToast'
import { t } from '@/composables/useI18n'
import type { TrayPrefs, TrayStatusFailure } from '@/types'

export const THEME_IDS = ['light', 'midnight', 'oled', 'forest', 'ocean', 'sakura', 'amber', 'teal', 'titanium'] as const
export type ThemeId = (typeof THEME_IDS)[number]

const DEFAULT_THEME: ThemeId = 'light'
const LS_THEME = 'phpo-theme'
const LS_TRAY = 'phpo-tray'

function isThemeId(v: unknown): v is ThemeId {
  return (THEME_IDS as readonly string[]).includes(v as string)
}

function readTheme(): ThemeId {
  try {
    const v = localStorage.getItem(LS_THEME)
    return isThemeId(v) ? v : DEFAULT_THEME
  } catch {
    return DEFAULT_THEME
  }
}

// 托盘偏好默认全开；缺项按默认补，避免旧存档把开关读成 undefined 后界面误判
function readTray(): TrayPrefs {
  const def: TrayPrefs = { enabled: true, minimizeOnClose: true }
  try {
    const raw = JSON.parse(localStorage.getItem(LS_TRAY) || 'null')
    if (!raw) return def
    return { enabled: raw.enabled !== false, minimizeOnClose: raw.minimizeOnClose !== false }
  } catch {
    return def
  }
}

export const usePrefsStore = defineStore('prefs', () => {
  const theme = ref<ThemeId>(readTheme())
  const tray = reactive<TrayPrefs>(readTray())
  const trayStatus = ref<TrayStatus | null>(null)
  const trayStatusFailure = ref<TrayStatusFailure | null>(null)

  function setTheme(id: ThemeId): void {
    if (isThemeId(id)) theme.value = id
  }

  // 响应式落地：<html data-theme> 决定生效变量，并持久化
  watchEffect(() => {
    document.documentElement.setAttribute('data-theme', theme.value)
    try {
      localStorage.setItem(LS_THEME, theme.value)
    } catch {
      /* 忽略 */
    }
  })

  // 后端挂载完成前不投影：绑定注入晚于组件 setup，抢跑会拿到「服务尚未初始化」。
  // 由 App.vue 在 waitForBackend() 之后调 syncTrayPrefs() 起闸，此后勾选变化自动跟随。
  let mirroring = false
  function mirrorTray(): void {
    if (!mirroring || trayStatus.value === null) return
    // 下发失败不得静默：勾选已写进 localStorage 并显示为「已生效」，原生托盘却没变＝界面失真（硬红线 4 的同一口径）
    void setTrayPrefs(tray.enabled, tray.minimizeOnClose).catch(() => {
      toast(t('settings.tray.pushFailed'), 'err', 4200)
    })
  }

  function syncTrayPrefs(): void {
    mirroring = true
    mirrorTray()
  }

  async function refreshTrayStatus(): Promise<void> {
    try {
      const status = await getTrayStatus()
      if (status === null) {
        trayStatus.value = null
        trayStatusFailure.value = 'backendUnavailable'
        return
      }
      trayStatus.value = status
      trayStatusFailure.value = null
      if (!status.canToggleVisibility) tray.enabled = true
      if (!status.available || !tray.enabled) tray.minimizeOnClose = false
    } catch (error: unknown) {
      trayStatus.value = null
      trayStatusFailure.value = 'probeFailed'
      toast(`${t('settings.tray.statusFailed')} ${String(error)}`, 'err', 5200)
    }
  }

  function setTrayEnabled(enabled: boolean): void {
    if (!trayStatus.value?.canToggleVisibility) return
    tray.enabled = enabled
    if (!enabled) tray.minimizeOnClose = false
  }

  function setTrayMinimizeOnClose(enabled: boolean): void {
    if (!trayStatus.value?.available || !tray.enabled) return
    tray.minimizeOnClose = enabled
  }

  // 托盘偏好持久化：设置页勾选必须跨重启保留，否则重启即回弹为默认（点击无长期效果）
  watchEffect(() => {
    try {
      localStorage.setItem(LS_TRAY, JSON.stringify(tray))
    } catch {
      /* 忽略 */
    }
    mirrorTray()
  })

  return {
    theme,
    tray,
    trayStatus,
    trayStatusFailure,
    setTheme,
    syncTrayPrefs,
    refreshTrayStatus,
    setTrayEnabled,
    setTrayMinimizeOnClose,
    THEME_IDS,
  }
})
