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
// —— 任务队列（与 internal/model/task.go 逐字对齐；§0.3 任务状态冻结为 4 个）——
export type TaskStatus = 'running' | 'success' | 'failed' | 'cancelled'
export interface TaskBrief {
  id: string
  label: string
  type: string
  kind?: string // 服务类任务的目标种类：卡片据此亮「运行中…/排队中」
  version?: string // 服务类任务的目标版本
  domain?: string // 站点类任务的目标域名：站点列表行据此亮「运行中…/排队中」
  step: number // 已完成步骤数
  total: number
  startedAt: string
}
// TaskBoard 队列详情：所在分区（running / pending）即排队态，不设第 5 个状态
export interface TaskBoard {
  running: TaskBrief | null
  pending: TaskBrief[]
}

export interface Operation {
  ts: string
  actor: string
  op: string
  args: unknown
  status: string
  durationMs: number
  error?: string
  // 任务账本三项：由后端在任务终态写入，供历史列表展示与失败原因回放
  taskId?: string
  label?: string
  logs?: string
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
  // defaultPort：端口键未落库时容器实际发布的宿主端口，与 internal/service/registry.go 的 Spec.HostPort 同值；
  // 0 = 该服务不发布宿主端口（php-fpm 只在 phpo-network 内可达）
  defaultPort: number
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
  tasks: TaskBoard
}

// DockerStatus 与后端 model.DockerStatus（internal/model/dto.go）JSON 逐字对齐；
// 只读探测结果（首启/轮询门禁，硬红线 7 判定源），非可持久 Snapshot 字段、不走事件。
export interface DockerStatus {
  status: 'ok' | 'not_installed' | 'not_running' | 'old_version' | 'unknown'
  version?: string
  canStart: boolean
  warning: boolean
  message?: string
  hint?: string
}
