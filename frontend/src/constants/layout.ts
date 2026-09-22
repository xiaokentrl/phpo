// 布局与缩放常量：逐字迁移自原型 LAYOUT_LIMITS / LAYOUT_PRESETS / UI_SCALE（§0.3：缩放 7 档）
export const LAYOUT_LIMITS = {
  sidebar: { min: 64, max: 380, default: 200 },
  drawer: { min: 120, max: 600, default: 160 },
  // split：抽屉展开体左栏（日志）占比，右栏（任务队列）取余。默认 70 即 §5.6.1 的 70%／30%
  split: { min: 40, max: 80, default: 70 },
} as const

export const LAYOUT_PRESETS = {
  compact: { sidebar: 200, drawer: 160 },
  default: { sidebar: 232, drawer: 220 },
  wide: { sidebar: 280, drawer: 320 },
} as const

export type PresetName = keyof typeof LAYOUT_PRESETS

export const UI_SCALE = {
  min: 0.75,
  max: 1.5,
  step: 0.05,
  snap: [0.75, 0.8, 0.9, 1.0, 1.1, 1.25, 1.5],
  default: 1.0,
} as const

export function clamp(v: number, min: number, max: number): number {
  return Math.min(Math.max(v, min), max)
}

// localStorage 键与原型 layout.load/save 保持一致
export const LS = {
  sidebar: 'phpo-sidebar-width',
  drawer: 'phpo-drawer-height',
  drawerSplit: 'phpo-drawer-split',
  scale: 'phpo-ui-scale',
} as const
