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

export type DoctorStatus = 'ok' | 'warn' | 'err'

export interface DoctorCheck {
  id: string
  title: string
  status: DoctorStatus
  detail: string
  hint: string
  fix: string // '' 无 / 'calibrate' 状态校准 / 'clear_temp' 清空临时目录残留
}

export interface DoctorReport {
  checks: DoctorCheck[]
  ok: number
  warnings: number
  errors: number
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

// StateSnapshot 与后端 model.Snapshot（internal/model/snapshot.go）JSON 逐字对齐；
// 是 state:changed 事件载荷，前端只按其落地、绝不本地乐观更新（硬红线 4）。
export interface StateSnapshot {
  installed: Partial<Record<ServiceKind, string[]>>
  running: Partial<Record<ServiceKind, string[]>>
  sites: Site[]
  env: Record<string, string>
  phpExtensions: Record<string, string[]>
  dirReady: Record<string, boolean>
}
