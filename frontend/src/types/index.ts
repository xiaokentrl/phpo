// 前端共享类型：对齐原型 DEFAULT_STATE 与 SVC_META 的领域模型
export type ServiceKind = 'php' | 'mysql' | 'pgsql' | 'redis' | 'nginx'
export type Health = 'up' | 'warn' | 'down'

export interface Site {
  domain: string
  port: number
  php: string
  root: string
  hosts: boolean
  health: Health
  rewrite: string
  rewriteRule?: string
}

export interface Backup {
  file: string
  size: string
  at: string
  items: number
}

export interface OfflineTree {
  svc: string
  ver: string
  size: string
  items: number
  verified: string
}

export interface TrayPrefs {
  enabled: boolean
  minimizeOnClose: boolean
}

// env 为扁平键值表（derivePaths 产物 + 各服务端口/密码键）
export type Env = Record<string, string>

export interface SvcMeta {
  titleKey: string
  icon: string
  subtitleKey: string
  hintKey: string
  emptyTitleKey: string
  suggested: string[]
  single?: boolean
}
