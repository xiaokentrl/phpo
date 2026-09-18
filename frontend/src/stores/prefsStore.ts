// 主题偏好：6 套 data-theme，仅存 localStorage（§3.1 原则 5：UI 偏好留前端）
import { defineStore } from 'pinia'
import { ref, watchEffect } from 'vue'

export const THEME_IDS = ['midnight', 'light', 'oled', 'forest', 'ocean', 'sakura'] as const
export type ThemeId = (typeof THEME_IDS)[number]

const DEFAULT_THEME: ThemeId = 'midnight'
const LS_THEME = 'phpo-theme'

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

export const usePrefsStore = defineStore('prefs', () => {
  const theme = ref<ThemeId>(readTheme())

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

  return { theme, setTheme, THEME_IDS }
})
