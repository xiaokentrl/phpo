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
  source?: string
  download_page?: string
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
  labelCode?: string // i18n 消息码（v2.9.16 多语言）；前端优先 t(labelCode, labelParams)
  labelParams?: Record<string, string> // 消息码参数
  type: string
  kind?: string // 服务类任务的目标种类：卡片据此亮「执行中…/等待中」
  version?: string // 服务类任务的目标版本
  domain?: string // 站点类任务的目标域名：站点列表行据此亮「执行中…/等待中」
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
  hasExtImage: boolean
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

// ServiceGap 一条「库里记着已安装、宿主上却不在了」的点名项，与后端 model.ServiceGap 逐字对齐（§5.19）。
// 只上报缺失态、不改 installed：用第三方工具删掉容器不等于用户要卸载，数据卷与重建入口都必须留着。
export type GapReason = 'container' | 'image' | 'extensions_image'
export interface ServiceGap {
  kind: ServiceKind
  version: string
  reason: GapReason
  ref: string
}

// DiscoveredService 是「这台机器上 Docker 里此刻实际有一个服务容器」这一条事实，
// 与后端 model.DiscoveredService（internal/model/resource.go）JSON 逐字对齐。
// 它回答的是「现在到底有什么」：里面既有 phpo 自己装的，也有用户用 docker run / Docker Desktop /
// compose 自己起来的；容器停着也照样在这里出现，只是 running=false——界面要画成停止态，不能当作没装。
// 只由权威快照落地（硬红线 4），不改 installed：Docker 上有这个容器，不等于用户要 phpo 把它记成「我装的」。
export interface DiscoveredService {
  kind: ServiceKind
  version: string
  // name：容器在 Docker 上的真名，界面用它说明这一条是从哪儿来的
  name: string
  image: string
  running: boolean
  // matchedBy：凭什么认出这是哪种服务的哪个版本（容器名 / 镜像）
  matchedBy: string
  // phpoNamed：名字不合 phpo 的命名规矩（如外部用 docker run 起的）就只展示，不给启用/停用/卸载
  phpoNamed: boolean
}

// EngineInfo 是容器引擎检测结果，与后端 model.EngineInfo（internal/model/snapshot.go）JSON 逐字对齐（v2.9.16）。
// kind 为空 = 尚未识别（拨号失败/未装配）；派生态不落库，由装配层启动探测派生。
export interface EngineInfo {
  kind: string
  version: string
  endpoint: string
  rootless: boolean
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
  gaps: ServiceGap[]
  discovered: DiscoveredService[]
  engine: EngineInfo | null
  flatpak: boolean // 本进程跑在 Flatpak 沙箱里：DockerGate 据此给沙箱放行引导
}

// DockerStatus 与后端 model.DockerStatus（internal/model/dto.go）JSON 逐字对齐；
// 只读探测结果（首启/轮询门禁，硬红线 7 判定源），非可持久 Snapshot 字段、不走事件。
export interface DockerStatus {
  status: 'ok' | 'not_installed' | 'not_running' | 'no_permission' | 'old_version' | 'unknown'
  version?: string
  canStart: boolean
  warning: boolean
  message?: string
  hint?: string
}

// MirrorSource 一个 Docker 镜像源的测速结果，与后端 model.MirrorSource（internal/model/dto.go）JSON 逐字对齐。
// latencyMs 是「本机到该源建一次 HTTPS 连接 + /v2/ 往返」的耗时，不是下载带宽——它只回答哪个源离你近、此刻还活着。
// ok=false 时 error 给出不通的原因（超时 / 拒绝连接 / TLS / HTTP 状态码）；这一行地址写错了也走 error，
// 那种情况后端不会拨网络。设置页的「检测」按请求/响应回来，不进快照、不发事件。
export interface MirrorSource {
  host: string
  latencyMs: number
  ok: boolean
  error?: string
}

// ---- Docker 全量资源清理（总览页底部 60 行面板）----
// 形状与后端 internal/model/docker_clean.go 的 JSON 标签逐字对齐（硬红线 4：数字只来自后端）。

// CleanRow 面板上的一行：静态属性（档位/危险/风险）+ 这次扫到的真实数字。
// 按钮给不给、要不要单独确认，后端已经算好带上，前端不再推第二套判据。
export interface CleanRow {
  key: string
  group: string
  tier: number
  deletable: boolean
  dangerous: boolean
  isPhpo: boolean
  risk: 'low' | 'medium' | 'high' | 'critical' | string
  status: 'ok' | 'no_perm' | 'not_supported' | 'unavailable' | string
  count: number
  bytes: number
  hasBytes: boolean
  message?: string
  scannedAt: string
}

export interface CleanScanReport {
  rows: CleanRow[]
  totalCount: number
  totalBytes: number
  scannedAt: string
  deepScanned: boolean
  warnings: string[]
}

// CleanTarget 确认框里的一项具体对象（一个容器、一个卷、一份宿主目录…），带一颗开关。
export interface CleanTarget {
  row: string
  kind: string
  id: string
  name: string
  size: number
  inUse: boolean
  foreign: boolean
  needsRoot: boolean
}

export interface CleanInstalled {
  kind: string
  version: string
}

// CleanPreview 一次预览的凭据：token 是一次性的，过期或用过就得重新预览。
export interface CleanPreview {
  token: string
  expiresAt: string
  rows: string[]
  targets: CleanTarget[]
  warnings: string[]
  consentRequired: boolean
  installed: CleanInstalled[]
}

export interface CleanRequest {
  token: string
  ids: string[]
  consent: boolean
  uninstall: boolean
}

// CleanedItem（逐项结果）复用上面清理三模式那一份形状：type 是 DockerResourceType，
// 宿主那五种类型（host_path / host_netdev / host_netns / host_cgroup / host_group_user）
// 由后端原样透出，前端只当文字显示，不参与任何判定。

// CleanExecuteReport 一次彻底清空的收尾：逐项结果 + 四个计数。
// skipped 是「预览时还在、动手时已经不在了」的那些，不判死整单，只逐行说明。
export interface CleanExecuteReport {
  taskId: string
  status: string
  items: CleanedItem[]
  removed: number
  failed: number
  skipped: number
  freedBytes: number
  trashed: string[]
}
