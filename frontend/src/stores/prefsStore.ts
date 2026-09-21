// 主题与托盘偏好：6 套 data-theme + 托盘开关，仅存 localStorage（§3.1 原则 5：UI 偏好留前端）
import { defineStore } from 'pinia'
import { reactive, ref, watchEffect } from 'vue'
import type { TrayPrefs } from '@/types'

export const THEME_IDS = ['midnight', 'light', 'oled', 'forest', 'ocean', 'sakura'] as const
export type ThemeId = (typeof THEME_IDS)[number]

const DEFAULT_THEME: ThemeId = 'midnight'
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

  // 托盘偏好持久化：设置页勾选必须跨重启保留，否则重启即回弹为默认（点击无长期效果）
  watchEffect(() => {
    try {
      localStorage.setItem(LS_TRAY, JSON.stringify(tray))
    } catch {
      /* 忽略 */
    }
  })

  return { theme, tray, setTheme, THEME_IDS }
})
