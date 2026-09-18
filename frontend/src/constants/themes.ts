// 主题常量：逐字迁移自原型 THEMES（§0.3：THEMES 数 = 6）
export interface ThemeDef {
  id: string
  nameKey: string
  descKey: string
  colors: string[]
}

export const THEMES: ThemeDef[] = [
  { id: 'midnight', nameKey: 'theme.midnight', descKey: 'theme.midnightDesc', colors: ['#0a0c10', '#11141b', '#5b9cff', '#3dd68c'] },
  { id: 'light', nameKey: 'theme.light', descKey: 'theme.lightDesc', colors: ['#f6f7f9', '#ffffff', '#3b82f6', '#16a34a'] },
  { id: 'oled', nameKey: 'theme.oled', descKey: 'theme.oledDesc', colors: ['#000000', '#08090b', '#60a5fa', '#34d399'] },
  { id: 'forest', nameKey: 'theme.forest', descKey: 'theme.forestDesc', colors: ['#0b1512', '#101d18', '#4ade80', '#22c55e'] },
  { id: 'ocean', nameKey: 'theme.ocean', descKey: 'theme.oceanDesc', colors: ['#080e1a', '#0d1625', '#38bdf8', '#2dd4bf'] },
  { id: 'sakura', nameKey: 'theme.sakura', descKey: 'theme.sakuraDesc', colors: ['#fdf7f8', '#ffffff', '#ec4899', '#059669'] },
]
