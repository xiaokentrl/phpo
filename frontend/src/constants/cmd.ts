// 命令面板项：逐字迁移原型 CMD_ITEMS（3233–3240，§0.3 权威值 20 命令）
// 每项含 labelKey + kbd 提示，并按 route/action/layout/pf+task/task 之一分发。
export type CmdAction = 'site-add' | 'theme' | 'zoom-in' | 'zoom-out' | 'zoom-reset'
export type CmdLayout = 'compact' | 'default' | 'wide' | 'reset'

export interface CmdItem {
  labelKey: string
  kbd: string
  route?: string
  action?: CmdAction
  layout?: CmdLayout
  pf?: string
  // [argsString, labelKey]
  task?: [string, string]
}

export const CMD_ITEMS: CmdItem[] = [
  { labelKey: 'nav.sites', kbd: '⌘1', route: 'sites' },
  { labelKey: 'nav.php', kbd: '⌘2', route: 'php' },
  { labelKey: 'nav.mysql', kbd: '⌘3', route: 'mysql' },
  { labelKey: 'nav.pgsql', kbd: '⌘4', route: 'pgsql' },
  { labelKey: 'nav.redis', kbd: '⌘5', route: 'redis' },
  { labelKey: 'nav.nginx', kbd: '⌘6', route: 'nginx' },
  { labelKey: 'nav.backup', kbd: '⌘7', route: 'backup' },
  { labelKey: 'nav.overview', kbd: '⌘8', route: 'overview' },
  { labelKey: 'nav.settings', kbd: '⌘9', route: 'settings' },
  { labelKey: 'cmd.newSite', kbd: '', action: 'site-add' },
  { labelKey: 'cmd.backup', kbd: '', pf: 'backup', task: ['backup', 'backup.nowTask'] },
  { labelKey: 'cmd.doctor', kbd: '', task: ['doctor', 'overview.doctorTask'] },
  { labelKey: 'cmd.theme', kbd: '', action: 'theme' },
  { labelKey: 'cmd.layoutCompact', kbd: '', layout: 'compact' },
  { labelKey: 'cmd.layoutDefault', kbd: '', layout: 'default' },
  { labelKey: 'cmd.layoutWide', kbd: '', layout: 'wide' },
  { labelKey: 'cmd.layoutReset', kbd: '', layout: 'reset' },
  { labelKey: 'cmd.zoomIn', kbd: '⌘+', action: 'zoom-in' },
  { labelKey: 'cmd.zoomOut', kbd: '⌘-', action: 'zoom-out' },
  { labelKey: 'cmd.zoomReset', kbd: '⌘0', action: 'zoom-reset' },
]
