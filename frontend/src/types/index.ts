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

// —— 应用升级（T604 / §5.9）——
export interface UpdateAvailable {
  version: string
  changelog: string
  size: number
}
export type UpdateStage = 'download' | 'verify' | 'install'
export interface UpdateProgress {
  stage: UpdateStage
  percent: number
  speed: number
}
export type UpdateDoneStatus = 'running' | 'success' | 'failed' | 'cancelled'
export interface UpdateDone {
  status: UpdateDoneStatus
  version: string
}

export interface OfflineTree {
  svc: string
  ver: string
  size: string
  items: number
  verified: string
}

// —— 清理 / 孤儿 / 回收站 / 审计（T605 / §5.13.6-7-10）——
export type CleanupMode = 'conservative' | 'standard' | 'aggressive'
export type DockerResourceType = 'container' | 'volume' | 'network' | 'image'
export interface DockerResource {
  type: DockerResourceType
  id: string
  name: string
  size: number
  inUse: boolean
}
export interface OrphanReport {
  containers: DockerResource[]
  volumes: DockerResource[]
  networks: DockerResource[]
  images: DockerResource[]
}
export interface CleanedItem {
  type: DockerResourceType
  name: string
  ok: boolean
  error?: string
}
export interface CleanupReport {
  mode: CleanupMode
  items: CleanedItem[]
  removed: number
  failed: number
  freedBytes: number
  trashPurged: number
}
export interface TrashEntry {
  id: number
  kind: string
  origPath: string
  trashPath: string
  movedAt: string
  expiresAt: string
  expired: boolean
}
export interface Operation {
  ts: string
  actor: string
  op: string
  args: unknown
  status: string
  durationMs: number
  error?: string
}

// —— 离线缓存（T606 / §5.14.10）——
export interface CacheEntry {
  kind: string
  version: string
  path: string
  hasImage: boolean
  apkCount: number
  peclCount: number
  totalSize: number
  lastVerify: string
  verifyOk: boolean
}
export interface CacheStats {
  totalBytes: number
  entryCount: number
  imageCount: number
  extCount: number
  corrupted: number
}
export interface VerifyResult {
  kind: string
  version: string
  ok: boolean
  failed: string[]
}
export interface VerifyAllResult {
  total: number
  ok: number
  failed: number
  entries: VerifyResult[]
}
export interface HomeVerifyResult {
  ok: boolean
  lines: string[]
  errors: string[]
}
export interface ImageCacheResult {
  hit: boolean
  path: string
  size: number
  corrupted: boolean
}
export interface ExtCacheResult {
  hit: boolean
  path: string
  size: number
  corrupted: boolean
}
// 缓存事件最近标记（CacheHitBadge 用）
export type CacheEventKind = 'hit' | 'miss'
export interface CacheEventMark {
  kind: CacheEventKind
  source?: string
  size?: number
  action?: string
  at: number
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
