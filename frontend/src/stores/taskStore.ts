// taskStore：任务状态仓，双通道。
// ① 实时通道（Q5，真实宿主）：记录、日志、进度、终态与失败原因全部来自后端 task:* 事件 + 权威快照 tasks 队列，
//    前端不造任务、不改状态、不推断终态（硬红线 4）；写请求的排队/并发由后端 FIFO 队列裁决，前端不再本地忙锁。
// ② demo 通道（无宿主，纯 Vite 浏览器）：保留原型 buildScript/playTask 的逐行回放，仅用于界面演示与验收。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { useAppState } from '@/stores/appState'
import { hasBackend } from '@/api/site'
import { cancelRunning, withdrawQueued, listTaskHistory } from '@/api/task'
import { verRoot, hostToContainer } from '@/utils/path'
import { resolveMounts, MOUNTS } from '@/constants/mounts'
import { getDefaultFiles } from '@/constants/configs'
import { VERSION_SUBDIRS } from '@/constants/service'
import { REWRITE_PRESETS } from '@/constants/rewrite'
import { toast } from '@/composables/useToast'
import { t as i18nT } from '@/composables/useI18n'
import type { Env, Operation, ServiceKind, TaskBoard, TaskBrief } from '@/types'

export type TaskStatus = 'running' | 'success' | 'failed' | 'cancelled'
// TaskDisplayStatus：抽屉队列行的 UI 显示态（§5.6.1）。它是「权威快照分区 + task:done 终态」的派生结果，
// 不是第 5 个后端任务状态（§0.3 冻结为 4 个）。unknown 是预留兜底位：映射未覆盖的新增态一律落此，
// 不得留空白、不得当作已完成；新增显示态只改 displayOf 与这张表，不动 model.TaskStatus。
export type TaskDisplayStatus = 'waiting' | 'running' | 'done' | 'failed' | 'cancelled' | 'unknown'

export const DISPLAY_LABEL: Record<TaskDisplayStatus, string> = {
  waiting: 'task.queued',
  running: 'task.running',
  done: 'task.done',
  failed: 'task.failed',
  cancelled: 'task.cancelled',
  unknown: 'task.dsUnknown',
}
export type LineType = 'cmd' | 'meta' | 'ok' | 'dim' | 'err'
export interface TaskLine {
  t: LineType
  s: string
}
export interface TaskMeta {
  type: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [k: string]: any
}
export interface TaskRecord {
  id: string
  args: string[]
  label: string
  meta: TaskMeta
  lines: TaskLine[]
  status: TaskStatus
  cursor: number
  startedAt: number
  endedAt: number | null
  cancelled: boolean
  abortReason: string | null
  timer: ReturnType<typeof setTimeout> | null
  // —— 实时通道字段（后端权威，Q5）——
  live: boolean // true=来自后端事件；false=纯浏览器 demo 回放
  step: number // 已完成步骤数（task:progress / 快照 tasks）
  total: number // 总步骤数；0=未知（如账本回放的历史任务）
  lastErr: string | null // 最近一条 err 日志（终态为 failed 时即失败原因）
  error: string | null // 失败原因：仅由终态判定写入，避免中途告警被误读为失败
  durationMs: number | null // 后端 task:done 的权威耗时
}

// 忙锁文案（原型 PF.taskBusy，1594 行）
export const TASK_BUSY = '已有任务运行中，请等待完成后再操作'

function sitesRootOf(env: Env): string {
  return env.NGINX_SITES_ROOT || `${env.PHPO_HOME}/nginx/sites`
}

// buildScript：逐字迁移原型 buildScript（2152–2335），产出 5 类型日志行。
// 适配前端签名：verRoot(env,kind,ver) / resolveMounts(env,kind,ver) / hostToContainer(env,p) / getDefaultFiles(kind,ver)。
function buildScript(args: string[], meta: TaskMeta): TaskLine[] {
  const app = useAppState()
  const env = app.env
  const lines: TaskLine[] = [{ t: 'cmd', s: `$ phpo ${args.join(' ')}` }]
  const kind = meta?.kind as ServiceKind | undefined
  const version = meta?.version as string | undefined

  if (meta?.type === 'install' && kind && version) {
    if (MOUNTS[kind]) {
      lines.push({ t: 'meta', s: '[1/6] Precheck' })
      lines.push({ t: 'ok', s: '  ✓ docker ready' })
      lines.push({ t: 'ok', s: `  ✓ PHPO_HOME=${env.PHPO_HOME}` })
      lines.push({ t: 'meta', s: '[2/6] Create version dirs' })
      for (const sub of VERSION_SUBDIRS[kind] || []) lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, kind, version)}/${sub}/` })
      lines.push({ t: 'meta', s: '[3/6] Write config.yaml' })
      if (meta.port) {
        const key = `${kind.toUpperCase()}_${version.replace(/\./g, '')}_PORT`
        lines.push({ t: 'ok', s: `  ✓ ${key}=${meta.port}` })
      }
      if (meta.password) {
        const key = `${kind.toUpperCase()}_${version.replace(/\./g, '')}_ROOT_PASSWORD`
        lines.push({ t: 'ok', s: `  ✓ ${key}=****${String(meta.password).slice(-4)}` })
      }
      if (kind === 'php' && meta.extensions) lines.push({ t: 'ok', s: `  ✓ EXTENSIONS=${meta.extensions}` })
      lines.push({ t: 'meta', s: '[4/6] Write config' })
      for (const f of getDefaultFiles(kind, version)) lines.push({ t: 'ok', s: `  ✓ ${env.PHPO_HOME}/${f.path}` })
      lines.push({ t: 'meta', s: '[5/6] Mounts' })
      for (const m of resolveMounts(env, kind, version)) lines.push({ t: 'ok', s: `  ✓ [${m.mode}] ${m.host} → ${m.to}   (${m.label})` })
      lines.push({ t: 'meta', s: '[6/6] Start' })
      lines.push({ t: 'ok', s: `  ✓ ${kind}-${version} started${meta.port ? ' on :' + meta.port : ''}` })
      lines.push({ t: 'ok', s: `✓ ${kind} ${version} installed` })
    } else {
      lines.push({ t: 'meta', s: '[1/4] Precheck' })
      lines.push({ t: 'ok', s: '  ✓ docker ready' })
      lines.push({ t: 'meta', s: '[2/4] Configure' })
      lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, kind, version)}/` })
      lines.push({ t: 'meta', s: '[3/4] Mounts' })
      for (const m of resolveMounts(env, kind, version)) lines.push({ t: 'ok', s: `  ✓ [${m.mode}] ${m.host} → ${m.to}   (${m.label})` })
      lines.push({ t: 'meta', s: '[4/4] Start' })
      lines.push({ t: 'ok', s: `  ✓ ${kind}-${version} started` })
      lines.push({ t: 'ok', s: `✓ ${kind} ${version} installed` })
    }
  } else if (meta?.type === 'uninstall' && kind && version) {
    lines.push({ t: 'meta', s: '[1/3] Stop container' })
    lines.push({ t: 'ok', s: '  ✓ stopped' })
    lines.push({ t: 'meta', s: '[2/3] Remove container' })
    lines.push({ t: 'ok', s: `  ✓ container phpo-${kind}-${version} removed` })
    lines.push({ t: 'meta', s: '[3/3] Clean config' })
    lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, kind, version)}/conf/ removed` })
    lines.push({ t: 'dim', s: `  · ${verRoot(env, kind, version)}/data/ kept` })
    lines.push({ t: 'ok', s: `✓ ${kind} ${version} uninstalled` })
  } else if (meta?.type === 'service-stop' && kind && version) {
    lines.push({ t: 'meta', s: `[1/2] Stop container` })
    lines.push({ t: 'ok', s: `  ✓ phpo-${kind}-${version} stopped` })
    lines.push({ t: 'meta', s: '[2/2] Verify' })
    lines.push({ t: 'ok', s: '  ✓ status: exited' })
    lines.push({ t: 'ok', s: `✓ ${kind} ${version} stopped` })
  } else if (meta?.type === 'service-start' && kind && version) {
    lines.push({ t: 'meta', s: `[1/3] Start container` })
    lines.push({ t: 'ok', s: `  ✓ phpo-${kind}-${version} started` })
    lines.push({ t: 'meta', s: '[2/3] Health check' })
    lines.push({ t: 'ok', s: '  ✓ healthy' })
    lines.push({ t: 'meta', s: '[3/3] Reload Nginx' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'ok', s: `✓ ${kind} ${version} running` })
  } else if (meta?.type === 'update-config' && kind && version) {
    lines.push({ t: 'meta', s: `Update ${kind} ${version} ${meta.field}` })
    if (meta.field === 'port') lines.push({ t: 'ok', s: `  ✓ port ${meta.oldValue} → ${meta.newValue}` })
    else if (meta.field === 'password') lines.push({ t: 'ok', s: `  ✓ password updated (****${String(meta.newValue).slice(-4) || 'empty'})` })
    lines.push({ t: 'meta', s: 'Write config.yaml' })
    lines.push({ t: 'ok', s: '  ✓ saved' })
    lines.push({ t: 'meta', s: 'Recreate container' })
    lines.push({ t: 'ok', s: '  ✓ restarted' })
    lines.push({ t: 'meta', s: 'Health check' })
    lines.push({ t: 'ok', s: '  ✓ healthy' })
    lines.push({ t: 'ok', s: `✓ ${kind} ${version} config updated` })
  } else if (meta?.type === 'site-add' && meta.domain) {
    lines.push({ t: 'meta', s: '[1/6] Precheck' })
    lines.push({ t: 'ok', s: `  ✓ PHPO_HOME=${env.PHPO_HOME}` })
    lines.push({ t: 'ok', s: `  ✓ WWW_ROOT=${env.WWW_ROOT}  → /var/www` })
    lines.push({ t: 'ok', s: `  ✓ NGINX_SITES_ROOT=${sitesRootOf(env)}` })
    lines.push({ t: 'meta', s: '[2/6] Validate domain' })
    lines.push({ t: 'ok', s: `  ✓ ${meta.domain}` })
    lines.push({ t: 'meta', s: '[3/6] Generate vhost' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/${meta.domain}.conf` })
    const hostRoot = meta.root || `${env.WWW_ROOT}/${meta.domain}`
    lines.push({ t: 'ok', s: `  ✓ root ${hostToContainer(env, hostRoot)}  (host: ${hostRoot})` })
    lines.push({ t: 'meta', s: '[4/6] Inject rewrite' })
    lines.push({ t: 'ok', s: '  ✓ rules written' })
    lines.push({ t: 'meta', s: '[5/6] Validate' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'meta', s: '[6/6] Reload' })
    lines.push({ t: 'ok', s: '  ✓ reloaded' })
    lines.push({ t: 'ok', s: `✓ Site ${meta.domain} created` })
  } else if (meta?.type === 'site-vhost' && meta.domain) {
    lines.push({ t: 'meta', s: '[1/3] Write vhost' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/${meta.domain}.conf` })
    lines.push({ t: 'meta', s: '[2/3] Validate' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'meta', s: '[3/3] Reload' })
    lines.push({ t: 'ok', s: '  ✓ reloaded' })
    lines.push({ t: 'ok', s: `✓ ${meta.domain} vhost updated` })
  } else if (meta?.type === 'site-port' && meta.domain) {
    lines.push({ t: 'meta', s: `Update ${meta.domain} port` })
    lines.push({ t: 'ok', s: `  ✓ listen ${meta.oldValue} → ${meta.newValue}` })
    lines.push({ t: 'meta', s: 'Write vhost' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/${meta.domain}.conf` })
    lines.push({ t: 'meta', s: 'Validate & Reload' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'ok', s: '  ✓ reloaded' })
    lines.push({ t: 'ok', s: `✓ ${meta.domain} port = ${meta.newValue}` })
  } else if (meta?.type === 'php-switch' && meta.domain) {
    lines.push({ t: 'meta', s: `Switch ${meta.domain} PHP` })
    lines.push({ t: 'ok', s: `  ✓ ${meta.oldPhp} → ${meta.newPhp}` })
    lines.push({ t: 'meta', s: 'Update vhost' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/${meta.domain}.conf` })
    lines.push({ t: 'meta', s: 'Validate & Reload' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'ok', s: '  ✓ reloaded' })
    lines.push({ t: 'ok', s: `✓ ${meta.domain} PHP = ${meta.newPhp}` })
  } else if (meta?.type === 'site-remove' && meta.domain) {
    lines.push({ t: 'meta', s: '[1/3] Remove vhost' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/${meta.domain}.conf` })
    lines.push({ t: 'meta', s: '[2/3] Validate' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'meta', s: '[3/3] Reload' })
    lines.push({ t: 'ok', s: '  ✓ reloaded' })
    lines.push({ t: 'dim', s: `  · site sources at ${env.WWW_ROOT}/${meta.domain} kept` })
    lines.push({ t: 'ok', s: `✓ Site ${meta.domain} deleted` })
  } else if (meta?.type === 'rewrite' && meta.domain) {
    const p = REWRITE_PRESETS[meta.preset as string]
    lines.push({ t: 'meta', s: '[1/4] Locate vhost' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/${meta.domain}.conf` })
    lines.push({ t: 'meta', s: '[2/4] Inject rewrite' })
    String(meta.rule || p?.rule || '').split('\n').forEach((l) => lines.push({ t: 'dim', s: '  │ ' + l }))
    lines.push({ t: 'meta', s: '[3/4] Validate' })
    lines.push({ t: 'ok', s: '  ✓ nginx -t passed' })
    lines.push({ t: 'meta', s: '[4/4] Reload' })
    lines.push({ t: 'ok', s: '  ✓ reloaded' })
    lines.push({ t: 'ok', s: `✓ ${meta.domain} rewrite updated` })
  } else if (meta?.type === 'extensions' && version) {
    lines.push({ t: 'meta', s: '[1/5] Write extensions.env' })
    ;(meta.added || []).forEach((e: string) => lines.push({ t: 'ok', s: `  + ${e}` }))
    ;(meta.removed || []).forEach((e: string) => lines.push({ t: 'dim', s: `  - ${e}` }))
    lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, 'php', version)}/conf/extensions.env` })
    lines.push({ t: 'meta', s: '[2/5] Rebuild image' })
    lines.push({ t: 'ok', s: '  ✓ cached (fast)' })
    lines.push({ t: 'meta', s: '[3/5] Rebuild container' })
    lines.push({ t: 'ok', s: `  ✓ php-${version} restarted` })
    lines.push({ t: 'meta', s: '[4/5] nginx -t' })
    lines.push({ t: 'ok', s: '  ✓ passed' })
    lines.push({ t: 'meta', s: '[5/5] reload' })
    lines.push({ t: 'ok', s: '  ✓ done' })
    lines.push({ t: 'ok', s: '✓ Extensions applied' })
  } else if (meta?.type === 'service-config' && kind && version) {
    lines.push({ t: 'meta', s: `[1/4] Write ${meta.files.length} file(s)` })
    ;(meta.files as string[]).forEach((f) => lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, kind, version)}/conf/${f}` }))
    lines.push({ t: 'meta', s: '[2/4] Validate config' })
    lines.push({ t: 'ok', s: `  ✓ ${kind} -t passed` })
    lines.push({ t: 'meta', s: '[3/4] Reload service' })
    lines.push({ t: 'ok', s: `  ✓ phpo-${kind}-${version} reloaded` })
    lines.push({ t: 'meta', s: '[4/4] Health check' })
    lines.push({ t: 'ok', s: '  ✓ healthy' })
    lines.push({ t: 'ok', s: `✓ ${kind} ${version} config updated` })
  } else if (meta?.type === 'restore') {
    lines.push({ t: 'meta', s: '[1/3] Extract archive' })
    lines.push({ t: 'ok', s: `  ✓ ${meta.file}` })
    lines.push({ t: 'meta', s: '[2/3] Restore conf/data/sites' })
    lines.push({ t: 'ok', s: '  ✓ config.yaml / conf / data / sites' })
    lines.push({ t: 'meta', s: '[3/3] Rebuild vhosts' })
    lines.push({ t: 'ok', s: '  ✓ all vhosts regenerated' })
    lines.push({ t: 'ok', s: '✓ Restore completed' })
  } else if (args[0] === 'backup') {
    lines.push({ t: 'meta', s: '[1/6] Pause databases' })
    lines.push({ t: 'ok', s: '  ✓ MySQL / PostgreSQL / Redis stopped' })
    lines.push({ t: 'meta', s: '[2/6] Pack service confs' })
    for (const k of ['php', 'nginx', 'mysql', 'pgsql', 'redis']) {
      for (const v of app.installed[k as ServiceKind] || []) lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, k, v)}/conf/` })
    }
    lines.push({ t: 'meta', s: '[3/6] Pack sites' })
    lines.push({ t: 'ok', s: `  ✓ ${sitesRootOf(env)}/` })
    lines.push({ t: 'meta', s: '[4/6] Pack data' })
    for (const k of ['mysql', 'pgsql', 'redis']) {
      for (const v of app.installed[k as ServiceKind] || []) lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, k, v)}/data/` })
    }
    lines.push({ t: 'meta', s: '[5/6] Pack initdb' })
    for (const k of ['mysql', 'pgsql']) {
      for (const v of app.installed[k as ServiceKind] || []) lines.push({ t: 'ok', s: `  ✓ ${verRoot(env, k, v)}/initdb/` })
    }
    lines.push({ t: 'meta', s: '[6/6] Archive & Restart' })
    lines.push({ t: 'ok', s: `  ✓ ${env.BACKUP_ROOT}/backup-20260913-093015.tar.gz (48.2 MB)` })
    lines.push({ t: 'ok', s: '  ✓ all restarted' })
    lines.push({ t: 'ok', s: '✓ Backup completed' })
  } else if (args[0] === 'doctor') {
    lines.push({ t: 'meta', s: 'Environment doctor' })
    lines.push({ t: 'ok', s: '  ✓ Docker 28.0.1' })
    lines.push({ t: 'ok', s: '  ✓ Ports 80 / 3306 / 5432 / 6379 free' })
    lines.push({ t: 'ok', s: '  ✓ Disk free 128 GB' })
    lines.push({ t: 'ok', s: `  ✓ PHPO_HOME=${env.PHPO_HOME}` })
    lines.push({ t: 'ok', s: `  ✓ WWW_ROOT=${env.WWW_ROOT}` })
    lines.push({ t: 'ok', s: '✓ No issues' })
  } else if (args[0] === 'offline' && args[1] === 'verify') {
    lines.push({ t: 'meta', s: 'Verify each tar' })
    lines.push({ t: 'ok', s: '  ✓ all passed' })
  } else {
    lines.push({ t: 'dim', s: 'Running…' })
    lines.push({ t: 'ok', s: '✓ Done' })
  }
  return lines
}

// RECORDS_MAX 记录池上限：终态记录留池供回看，超出裁最旧（正在跑 / 正在看的绝不裁）
const RECORDS_MAX = 50
// SYS_MAX 系统日志通道上限：不属于任何任务的事件逐行流水，滚动裁尾防无界增长
const SYS_MAX = 200

export const useTaskStore = defineStore('task', () => {
  const app = useAppState()

  // 记录池（newest-first）：实时通道（后端事件 / 任务账本）与 demo 回放共用同一形状
  const records = ref<TaskRecord[]>([])
  const activeId = ref('')
  const expanded = ref(false)
  // sysLines：系统日志通道（需求 3／6）——无任务归属的事件（cache:* / docker:* / update:*）流水，
  // 左栏在无选中任务时显示它
  const sysLines = ref<TaskLine[]>([])
  // 等效命令是前端展示信息、不进后端队列载荷：label 为队列去重键（与任务 1:1），据此关联弹窗提交的 args
  const cmdByLabel = new Map<string, string[]>()
  let followedId = '' // 已自动跟随过的运行中任务 ID
  let demoSeq = 0

  const task = computed<TaskRecord | null>(() => records.value.find((r) => r.id === activeId.value) ?? null)
  // displayOf：§5.6.1 显示态的唯一映射出口。队列归属认后端权威 ID（Running / Pending 分区），
  // 绝不认记录内部的 status 字段——排队项的记录只是占位，其 status 从未被裁决过。
  function displayOf(r: TaskRecord | null | undefined): TaskDisplayStatus {
    if (!r) return 'unknown'
    if (hasBackend()) {
      if (r.id === (app.tasks.running?.id ?? '')) return 'running'
      if ((app.tasks.pending ?? []).some((p) => p.id === r.id)) return 'waiting'
    }
    switch (r.status) {
      case 'success':
        return 'done'
      case 'failed':
        return 'failed'
      case 'cancelled':
        return 'cancelled'
      case 'running':
        // 有日志＝确实跑起来了（快照可能比 task:log 晚一帧）；无日志且不在队列里＝已被撤回，交 queue 滤掉
        return r.lines.length ? 'running' : 'unknown'
      default:
        return 'unknown' // 预留兜底：后端新增终态未经映射时不空白、不误报已完成
    }
  }
  // dequeued：已撤回排队项的判据——后端对未获执行权的任务不发事件、不落账（internal/task/task.go），
  // 故「真实记录 + 仍标执行中 + 零日志 + 不在权威队列」四项同时成立只可能是被撤回，行随下一次快照消失。
  function dequeued(r: TaskRecord): boolean {
    if (!r.live || r.status !== 'running' || r.lines.length > 0) return false
    return displayOf(r) === 'unknown'
  }
  const display = computed(() => displayOf(task.value))
  const isRunning = computed(() => display.value === 'running')
  // queue：抽屉右栏（30%）的队列行，**最新提交永远在最上面**——记录池本身即 newest-first
  // （新记录 unshift、撤回出队的占位行滤掉），运行中的任务不因开始执行而下移，行位只随提交先后决定。
  const queue = computed(() =>
    records.value
      .filter((r) => !dequeued(r))
      .map((r) => {
        const ds = displayOf(r)
        return {
          id: r.id,
          label: r.label,
          step: r.step,
          total: r.total,
          display: ds,
          statusText: i18nT(DISPLAY_LABEL[ds]),
          withdrawable: ds === 'waiting',
          active: r.id === activeId.value,
        }
      })
  )
  // queueRunning：队列级忙（托盘退出等判定源）。真实宿主只认权威快照，绝不信本地记录
  const queueRunning = computed(() =>
    hasBackend() ? !!app.tasks.running : records.value.some((r) => r.status === 'running')
  )
  const runningBrief = computed<TaskBrief | null>(() => app.tasks.running ?? null)
  const pendingBriefs = computed<TaskBrief[]>(() => app.tasks.pending ?? [])
  // visibleLines：左栏正文。选中任务时是该任务的日志；无选中任务时退化为系统日志通道，
  // 使不属于任何任务的事件也实时逐行可见（需求 3／6）。
  const visibleLines = computed<TaskLine[]>(() => {
    const t = task.value
    if (!t) return sysLines.value
    return t.live ? t.lines : t.lines.slice(0, t.cursor)
  })
  // progress：实时步骤进度（total=0 表示未知，如账本回放的历史任务，此时不显示进度）
  const progress = computed(() => {
    const t = task.value
    if (!t || t.total <= 0) return null
    const step = Math.min(t.step, t.total)
    return { step, total: t.total, percent: Math.round((step / t.total) * 100) }
  })

  function find(id: string): TaskRecord | undefined {
    return records.value.find((r) => r.id === id)
  }

  function trim(): void {
    while (records.value.length > RECORDS_MAX) {
      const last = records.value[records.value.length - 1]
      if (last.id === activeId.value || last.status === 'running') return // 绝不裁掉正在看/正在跑的记录
      records.value.pop()
    }
  }

  function newRecord(id: string, label: string, type: string, live: boolean): TaskRecord {
    return {
      id,
      args: label ? cmdByLabel.get(label) ?? [] : [],
      label: label || id,
      meta: { type },
      lines: [],
      status: 'running',
      cursor: 0,
      startedAt: Date.now(),
      endedAt: null,
      cancelled: false,
      abortReason: null,
      timer: null,
      live,
      step: 0,
      total: 0,
      lastErr: null,
      error: null,
      durationMs: null,
    }
  }

  // ensureLive 取/建一条后端任务记录。task:log 可能先于快照到达（跨事件顺序不保证），
  // 此时以 ID 占位，待队列详情落地后补 label / 类型 / 等效命令。
  function ensureLive(id: string, label = '', type = '', total = 0): TaskRecord {
    const cur = find(id)
    if (cur) {
      if (label) cur.label = label
      if (type && !cur.meta.type) cur.meta = { ...cur.meta, type }
      if (total && !cur.total) cur.total = total
      if (label && !cur.args.length) cur.args = cmdByLabel.get(label) ?? []
      return cur
    }
    const r = newRecord(id, label, type, true)
    if (total) r.total = total
    records.value.unshift(r)
    trim()
    return r
  }

  // ---- 实时通道入口（由 composables/useStateSync.ts 按 §5.6 事件调用；硬红线 4：只落地，不推断）----

  // syncBoard：权威快照的队列详情落地。运行中项出现即建/更新记录并自动跟随展开抽屉；
  // **排队项同样建行**（§5.6.1：一经入队就出现在列表里，不等它取得执行权），只是没有日志。
  // 终态一律由 task:done 判定——绝不靠「从队列消失」推断成败。
  function syncBoard(b?: TaskBoard | null): void {
    const cur = b?.running
    if (cur?.id) {
      const r = ensureLive(cur.id, cur.label, cur.type, cur.total)
      r.step = cur.step || r.step
      if (cur.total) r.total = cur.total
      const started = Date.parse(cur.startedAt ?? '')
      if (Number.isFinite(started) && started > 0) r.startedAt = started
      if (followedId !== cur.id) {
        followedId = cur.id
        activeId.value = cur.id
        expanded.value = true
      }
    }
    for (const p of b?.pending ?? []) ensureLive(p.id, p.label, p.type, p.total)
    // 撤回不留幽灵选中：选中项已不在权威队列且从未有日志（= 被撤回的排队项）时，随同一快照回到运行中任务
    const sel = task.value
    if (sel && dequeued(sel)) activeId.value = cur?.id ?? ''
  }

  // appendLog：task:log 一行落地。err 行记为「最近错误」；失败原因只在终态为 failed 时成立
  // （§5.6 的 task:done 载荷不带 error，可用来源是这一行与账本 error 列）。
  function appendLog(id: string, level: LineType, text: string): void {
    if (!id) return
    const r = ensureLive(id)
    r.lines.push({ t: level, s: text })
    if (level === 'err') r.lastErr = text
  }

  // setProgress：task:progress 落地（快照 tasks 亦带同一进度，两者同源不冲突）
  function setProgress(id: string, step: number, total: number): void {
    if (!id) return
    const r = ensureLive(id)
    if (total) r.total = total
    r.step = step
  }

  // finish：task:done 终态落地。duration 为后端 time.Duration 的 JSON 值（纳秒）。
  function finish(id: string, status: TaskStatus, durationNs: number): void {
    if (!id) return
    const r = ensureLive(id)
    r.status = status
    r.endedAt = Date.now()
    r.durationMs = Number.isFinite(durationNs) ? Math.round(durationNs / 1e6) : null
    if (status === 'cancelled') {
      r.cancelled = true
      r.abortReason = r.abortReason || 'user'
    }
    // 失败原因实时收口：优先任务内最后一条 err 日志，退化用取消标记
    r.error = status === 'failed' ? r.lastErr : null
    if (!r.total && r.step) r.total = r.step // 进度行已到即总步数收口，避免停在 x/0
    trim()
  }

  // eventLine：把一条协议事件如实记成一行日志（§5.14 离线优先、§5.13 清洁、§5.9 升级的实时证据）。
  // 有运行中任务时归属该任务（串行队列 ⇒ 归属唯一）；没有任务时（doctor 离线校验、手动清理缓存、
  // 启动校准等）进系统日志通道——需求 6 要求这类事件也实时可见，但仍不得凭空造任务记录（硬红线 4）。
  function eventLine(level: LineType, text: string): void {
    const id = app.tasks.running?.id
    if (id) {
      appendLog(id, level, text)
      return
    }
    sysLines.value.push({ t: level, s: text })
    if (sysLines.value.length > SYS_MAX) sysLines.value.splice(0, sysLines.value.length - SYS_MAX)
  }

  // select：查看某条记录（队列条 / 历史点击）。不改动任何状态。
  function select(id: string): void {
    if (!find(id)) return
    activeId.value = id
    expanded.value = true
  }

  // cancel：真实通道只请求后端取消，终态等 task:done（硬红线 4）；demo 通道照原型本地停止回放。
  // 判定用显示态而非记录内部态：等待中的行没有执行权，只能走撤回（withdraw），不得发 Cancel。
  function cancel(): void {
    const t = task.value
    if (!t || displayOf(t) !== 'running') return
    if (t.live) {
      void cancelRunning()
      toast(i18nT('task.cancelHint'), 'info', 2600)
      return
    }
    t.cancelled = true
    if (t.timer) {
      clearTimeout(t.timer)
      t.timer = null
    }
    t.status = 'cancelled'
    t.endedAt = Date.now()
    t.abortReason = 'user'
    toast(i18nT('task.cancelHint'), 'info', 2600)
  }

  // withdraw：撤回排队项。命中与否由后端裁决，前端只在后端确认出队后随快照消失（不本地删行）
  async function withdraw(id: string): Promise<boolean> {
    const ok = await withdrawQueued(id)
    if (!ok) toast(i18nT('task.withdrawStale'), 'err', 2600)
    return ok
  }

  // loadHistory：从任务账本补齐跨重启的历史记录（含失败原因与日志原文），已在池中的按 ID 跳过。
  async function loadHistory(): Promise<void> {
    const rows = await listTaskHistory().catch(() => [] as Operation[])
    for (const op of rows) {
      const id = op.taskId
      if (!id || find(id)) continue
      const r = newRecord(id, op.label || op.op, op.op, true)
      r.status = (op.status === 'success' ? 'success' : op.status === 'cancelled' ? 'cancelled' : 'failed') as TaskStatus
      r.error = op.error || null
      r.durationMs = op.durationMs || null
      const started = Date.parse(op.ts)
      r.startedAt = Number.isFinite(started) ? started : r.startedAt
      r.endedAt = r.startedAt + (op.durationMs || 0)
      r.lines = String(op.logs || '')
        .split('\n')
        .filter((s) => s !== '')
        .map((s) => ({ t: /失败|错误|error/i.test(s) ? ('err' as LineType) : ('dim' as LineType), s }))
      r.cursor = r.lines.length
      records.value.push(r)
    }
    records.value.sort((a, b) => b.startedAt - a.startedAt)
    trim()
  }

  function toggle(): void {
    expanded.value = !expanded.value
  }

  function setExpanded(v: boolean): void {
    expanded.value = v
  }

  // ---- demo 通道（无宿主）：忠实原型 runTask/playTask 的本地逐行回放 ----

  function play(t: TaskRecord): void {
    const next = () => {
      if (t.cancelled) {
        t.status = 'cancelled'
        t.endedAt = Date.now()
        t.timer = null
        return
      }
      if (t.cursor >= t.lines.length) {
        // 硬红线 4：到达末尾后不落地状态；真实落地由后端事件驱动。demo 仅标记成功。
        t.status = 'success'
        t.endedAt = Date.now()
        t.timer = null
        return
      }
      t.cursor += 1
      const entry = t.lines[t.cursor - 1]
      t.timer = setTimeout(next, entry.t === 'cmd' ? 120 : 90 + Math.random() * 200)
    }
    next()
  }

  // start：写操作的前端入口（runTask 委托到此）。
  // 真实宿主：任务由后端三段式执行并经事件回流，此处只登记等效命令 + 展开抽屉，绝不伪造日志。
  // 无宿主：按原型 buildScript 造日志行本地回放，仅用于演示。
  function start(args: string[], label?: string, meta: TaskMeta = { type: args[0] || 'task' }): boolean {
    const lbl = label || args.join(' ')
    cmdByLabel.set(lbl, args)
    if (hasBackend()) {
      expanded.value = true
      return true
    }
    const busy = records.value.find((r) => r.status === 'running')
    if (busy) {
      toast(TASK_BUSY, 'err', 2200)
      return false
    }
    const t: TaskRecord = { ...newRecord(`demo-${++demoSeq}`, lbl, meta.type, false), args, meta, lines: buildScript(args, meta) }
    records.value.unshift(t)
    // 回放必须改数组里的响应式代理：raw 对象上的 cursor 写入不触发更新，日志会冻在首行
    const rec = records.value[0]
    activeId.value = rec.id
    trim()
    expanded.value = true
    play(rec)
    return true
  }

  return {
    records,
    task,
    activeId,
    expanded,
    isRunning,
    display,
    queue,
    displayOf,
    queueRunning,
    runningBrief,
    pendingBriefs,
    visibleLines,
    progress,
    syncBoard,
    appendLog,
    setProgress,
    finish,
    eventLine,
    select,
    start,
    cancel,
    withdraw,
    loadHistory,
    toggle,
    setExpanded,
  }
})
