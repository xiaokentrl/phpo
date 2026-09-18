// 装机向导目录树常量：逐字迁移自原型 HOME_SUBDIRS（533–542）
export interface HomeSubdir {
  path: string
  label: string
  depth: number
}

export const HOME_SUBDIRS: HomeSubdir[] = [
  { path: 'php', label: 'PHP runtime root', depth: 0 },
  { path: 'nginx', label: 'nginx root', depth: 0 },
  { path: 'nginx/sites', label: 'nginx vhosts (global)', depth: 1 },
  { path: 'mysql', label: 'MySQL root', depth: 0 },
  { path: 'pgsql', label: 'PostgreSQL root', depth: 0 },
  { path: 'redis', label: 'Redis root', depth: 0 },
  { path: 'backups', label: 'backup archives', depth: 0 },
  { path: 'offline', label: 'offline cache', depth: 0 },
]
