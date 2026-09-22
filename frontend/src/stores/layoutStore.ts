// 布局与缩放 store：迁移原型 layout 对象（sidebar 宽 / drawer 高 / drawer 左右占比 / ui 缩放）
// 偏好仅存 localStorage（§3.1 原则 5）；apply() 写 CSS 变量 + documentElement.zoom
import { defineStore } from 'pinia'
import { ref, watchEffect } from 'vue'
import { LAYOUT_LIMITS, LAYOUT_PRESETS, UI_SCALE, clamp, LS, type PresetName } from '@/constants/layout'

function readNum(key: string, fallback: number, min: number, max: number): number {
  let raw: string | null = null
  try {
    raw = localStorage.getItem(key)
  } catch {
    /* 忽略 */
  }
  if (raw == null) return fallback
  const n = parseInt(raw, 10)
  return Number.isNaN(n) ? fallback : clamp(n, min, max)
}

function readScale(): number {
  let raw: string | null = null
  try {
    raw = localStorage.getItem(LS.scale)
  } catch {
    /* 忽略 */
  }
  if (raw == null) return UI_SCALE.default
  const n = parseFloat(raw)
  return Number.isNaN(n) ? UI_SCALE.default : clamp(n, UI_SCALE.min, UI_SCALE.max)
}

export const useLayoutStore = defineStore('layout', () => {
  const sidebarWidth = ref(readNum(LS.sidebar, LAYOUT_PRESETS.compact.sidebar, LAYOUT_LIMITS.sidebar.min, LAYOUT_LIMITS.sidebar.max))
  const drawerHeight = ref(readNum(LS.drawer, LAYOUT_PRESETS.compact.drawer, LAYOUT_LIMITS.drawer.min, LAYOUT_LIMITS.drawer.max))
  const drawerSplit = ref(readNum(LS.drawerSplit, LAYOUT_LIMITS.split.default, LAYOUT_LIMITS.split.min, LAYOUT_LIMITS.split.max))
  const scale = ref(readScale())

  function apply(): void {
    const de = document.documentElement
    de.style.setProperty('--sidebar-width', sidebarWidth.value + 'px')
    de.style.setProperty('--drawer-height', drawerHeight.value + 'px')
    de.style.setProperty('--drawer-split', drawerSplit.value + '%')
    de.style.setProperty('--ui-scale', String(scale.value))
    de.style.zoom = String(scale.value)
  }

  function save(): void {
    try {
      localStorage.setItem(LS.sidebar, String(sidebarWidth.value))
      localStorage.setItem(LS.drawer, String(drawerHeight.value))
      localStorage.setItem(LS.drawerSplit, String(drawerSplit.value))
      localStorage.setItem(LS.scale, String(scale.value))
    } catch {
      /* 忽略 */
    }
  }

  function setSidebar(w: number): void {
    sidebarWidth.value = clamp(w, LAYOUT_LIMITS.sidebar.min, LAYOUT_LIMITS.sidebar.max)
  }
  function setDrawer(h: number): void {
    drawerHeight.value = clamp(h, LAYOUT_LIMITS.drawer.min, LAYOUT_LIMITS.drawer.max)
  }
  // setSplit：左栏日志占比（百分数），右栏任务队列取余；超出上下限即夹住
  function setSplit(p: number): void {
    drawerSplit.value = clamp(p, LAYOUT_LIMITS.split.min, LAYOUT_LIMITS.split.max)
  }

  // 返回是否发生实际改变（与原型 setScale 语义一致）
  function setScale(v: number): boolean {
    const nv = parseFloat(String(v))
    if (Number.isNaN(nv)) return false
    const clamped = clamp(nv, UI_SCALE.min, UI_SCALE.max)
    if (Math.abs(clamped - scale.value) < 1e-6) return false
    scale.value = clamped
    return true
  }

  function zoomIn(): boolean {
    const next = UI_SCALE.snap.find((v) => v > scale.value + 1e-6)
    return next == null ? false : setScale(next)
  }
  function zoomOut(): boolean {
    const prev = [...UI_SCALE.snap].reverse().find((v) => v < scale.value - 1e-6)
    return prev == null ? false : setScale(prev)
  }
  function zoomReset(): boolean {
    if (Math.abs(scale.value - UI_SCALE.default) < 1e-6) return false
    return setScale(UI_SCALE.default)
  }

  function applyPreset(name: PresetName): void {
    const p = LAYOUT_PRESETS[name]
    if (!p) return
    sidebarWidth.value = p.sidebar
    drawerHeight.value = p.drawer
  }
  function reset(): void {
    sidebarWidth.value = LAYOUT_PRESETS.compact.sidebar
    drawerHeight.value = LAYOUT_PRESETS.compact.drawer
    drawerSplit.value = LAYOUT_LIMITS.split.default
  }

  // 状态变化即应用并持久化
  watchEffect(() => {
    apply()
    save()
  })

  return {
    sidebarWidth,
    drawerHeight,
    drawerSplit,
    scale,
    setSidebar,
    setDrawer,
    setSplit,
    setScale,
    zoomIn,
    zoomOut,
    zoomReset,
    applyPreset,
    reset,
  }
})
