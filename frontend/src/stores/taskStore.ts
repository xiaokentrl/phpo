// taskStore：任务引擎 mock 状态仓（T109）。忠实迁移原型 buildScript/runTask/playTask/cancelTask。
// 硬红线 4：日志逐行回放为纯展示；真实状态落地（applyStateChange）由后端事件驱动，属 T110，此处不做前端乐观更新。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { useAppState } from '@/stores/appState'
import { verRoot, hostToContainer } from '@/utils/path'
import { resolveMounts, MOUNTS } from '@/constants/mounts'
import { getDefaultFiles } from '@/constants/configs'
import { VERSION_SUBDIRS } from '@/constants/service'
import { REWRITE_PRESETS } from '@/constants/rewrite'
import { toast } from '@/composables/useToast'
import { t as i18nT } from '@/composables/useI18n'
import type { Env, ServiceKind } from '@/types'

export type TaskStatus = 'running' | 'success' | 'failed' | 'cancelled'
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
        const key = kind === 'nginx' ? 'NGINX_PORT' : `${kind.toUpperCase()}_${version.replace(/\./g, '')}_PORT`
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

export const useTaskStore = defineStore('task', () => {
  const task = ref<TaskRecord | null>(null)
  const expanded = ref(false)

  const isRunning = computed(() => task.value?.status === 'running')
  const visibleLines = computed<TaskLine[]>(() => {
    const t = task.value
    return t ? t.lines.slice(0, t.cursor) : []
  })

  function play(): void {
    const t = task.value
    if (!t) return
    const next = () => {
      if (!task.value || task.value !== t || t.cancelled) {
        t.status = 'cancelled'
        t.endedAt = Date.now()
        t.timer = null
        return
      }
      if (t.cursor >= t.lines.length) {
        // 硬红线 4：到达末尾后不落地状态；真实落地由后端事件驱动（T110）。mock 仅标记成功。
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

  // start：忠实原型 runTask（2363–2389）。忙锁阻止并发写任务；构建日志行；展开抽屉；逐行回放。
  function start(args: string[], label?: string, meta: TaskMeta = { type: args[0] || 'task' }): boolean {
    if (task.value && task.value.status === 'running') {
      toast(TASK_BUSY, 'err', 2200)
      return false
    }
    if (task.value && task.value.timer) clearTimeout(task.value.timer)
    task.value = {
      args,
      label: label || args.join(' '),
      meta,
      lines: buildScript(args, meta),
      status: 'running',
      cursor: 0,
      startedAt: Date.now(),
      endedAt: null,
      cancelled: false,
      abortReason: null,
      timer: null,
    }
    expanded.value = true
    play()
    return true
  }

  // cancel：忠实原型 cancelTask（2433–2443）。仅运行中可取消；清空定时器、置 cancelled、提示。
  function cancel(): void {
    const t = task.value
    if (!t || t.status !== 'running') return
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

  function toggle(): void {
    expanded.value = !expanded.value
  }

  function setExpanded(v: boolean): void {
    expanded.value = v
  }

  return { task, expanded, isRunning, visibleLines, start, cancel, toggle, setExpanded }
})
