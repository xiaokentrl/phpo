// 按 colors[0] 背景色相对亮度由亮到暗排序
export interface ThemeDef {
  id: string
  nameKey: string
  descKey: string
  colors: string[]
}

export const THEMES: ThemeDef[] = [
  { id: 'light', nameKey: 'theme.light', descKey: 'theme.lightDesc', colors: ['#f6f7f9', '#ffffff', '#3b82f6', '#16a34a'] },
  { id: 'sakura', nameKey: 'theme.sakura', descKey: 'theme.sakuraDesc', colors: ['#fdf7f8', '#ffffff', '#ec4899', '#059669'] },
  { id: 'amber', nameKey: 'theme.amber', descKey: 'theme.amberDesc', colors: ['#faf6ec', '#fffdf7', '#d97706', '#16a34a'] },
  { id: 'teal', nameKey: 'theme.teal', descKey: 'theme.tealDesc', colors: ['#071713', '#0b211c', '#2dd4bf', '#4ade80'] },
  { id: 'forest', nameKey: 'theme.forest', descKey: 'theme.forestDesc', colors: ['#0b1512', '#101d18', '#4ade80', '#22c55e'] },
  { id: 'titanium', nameKey: 'theme.titanium', descKey: 'theme.titaniumDesc', colors: ['#101214', '#16191c', '#8fb0c9', '#34d399'] },
  { id: 'ocean', nameKey: 'theme.ocean', descKey: 'theme.oceanDesc', colors: ['#080e1a', '#0d1625', '#38bdf8', '#2dd4bf'] },
  { id: 'midnight', nameKey: 'theme.midnight', descKey: 'theme.midnightDesc', colors: ['#0a0c10', '#11141b', '#5b9cff', '#3dd68c'] },
  { id: 'oled', nameKey: 'theme.oled', descKey: 'theme.oledDesc', colors: ['#000000', '#08090b', '#60a5fa', '#34d399'] },
]