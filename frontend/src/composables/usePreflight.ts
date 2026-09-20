// usePreflight：preflight() 的前端镜像（§0.2 #14：UI 即时反馈；最终裁决在 internal/preflight/）
// 逐字迁移原型 PF 文案 + validateVersion/Port/Domain/SiteRoot/Ext + preflight(action,ctx) 17 action。
import { useAppState } from '@/stores/appState'
import { SVC_META } from '@/constants/service'
import type { ServiceKind } from '@/types'

const PF = {
  homeNotReady: '请先完成 phpo 工作目录初始化',
  versionInvalid: '版本号不合法（禁止路径分隔符与 ..）',
  versionDup: '该版本已安装',
  nginxSingle: 'Nginx 为单例服务，已安装',
  portInvalid: '端口格式不正确（1–65535）',
  portInUse: '端口已被占用',
  portAdvance: '端口 {from} 已被占用，自动顺延至 {to}',
  domainInvalid: '域名格式不正确',
  domainExists: '域名已存在',
  rootEmpty: '站点目录不能为空',
  rootOutsideWww: '站点目录不在 WWW_ROOT 内（允许，但请确认挂载与访问路径正确）',
  rootDuplicated: '该目录已被其他站点使用',
  pathTraversal: '路径不合法（不允许 .. 或空段）',
  notInstalled: '目标服务/版本未安装',
  isRunning: '服务正在运行，请先停止',
  notRunning: '服务未运行',
  hasDependents: '存在依赖该服务的站点，无法继续',
  lastPhp: '至少保留一个 PHP 版本',
  backupMissing: '备份归档不存在',
  offlineMissing: '离线缓存条目不存在',
  svcMissing: '服务不存在',
  siteMissing: '站点不存在',
  extInvalid: '扩展名不合法',
  configEmpty: '配置内容不能为空',
  nginxNeeded: '请先安装 Nginx',
  phpNeeded: '请先安装一个 PHP 版本',
}

export interface PreflightResult {
  ok: boolean
  errors: string[]
  warnings: string[]
  adjusted: Record<string, unknown>
}

function normPath(p: string): string {
  return String(p || '').trim().replace(/\/+/g, '/').replace(/\/+$/, '')
}
function hasTraversal(p: string): boolean {
  return /(^|\/)\.\.(\/|$)/.test(String(p || ''))
}

// NEEDS_HOME：15 个动作（§0.3）
const NEEDS_HOME = new Set([
  'install', 'uninstall', 'service-stop', 'service-start', 'update-config',
  'site-add', 'site-remove', 'site-port', 'site-vhost', 'rewrite',
  'extensions', 'service-config', 'backup', 'restore', 'offline-prune',
])

interface ValResult { ok: boolean; msg?: string; value?: string | number; outsideWww?: boolean }

export function usePreflight() {
  const app = useAppState()

  function validateVersion(version: unknown): ValResult {
    const v = String(version ?? '').trim()
    if (!v) return { ok: false, msg: PF.versionInvalid }
    if (v === '.' || v === '..' || v.includes('..')) return { ok: false, msg: `${PF.versionInvalid}: ${v}` }
    if (/[/\\\x00-\x1f\s]/.test(v)) return { ok: false, msg: `${PF.versionInvalid}: ${v}` }
    if (v.length > 128) return { ok: false, msg: `${PF.versionInvalid}: ${v}` }
    return { ok: true, value: v }
  }

  function collectUsedPorts(excludeDomains: string[] = []): Map<number, string> {
    const used = new Map<number, string>()
    for (const kind of ['mysql', 'pgsql', 'redis']) {
      for (const v of (app.installed as Record<string, string[]>)[kind] || []) {
        const key = `${kind.toUpperCase()}_${String(v).replace(/\./g, '')}_PORT`
        const p = parseInt(app.env[key], 10)
        if (!isNaN(p) && !used.has(p)) used.set(p, `${kind} ${v}`)
      }
    }
    for (const s of app.sites) {
      if (excludeDomains.includes(s.domain)) continue
      const p = parseInt(String(s.port), 10)
      if (!isNaN(p) && !used.has(p)) used.set(p, `site ${s.domain}`)
    }
    return used
  }

  function findNextAvailablePort(desired: number, maxPort = 65535, minPort = 1, used?: Map<number, string>): number | null {
    for (let p = desired + 1; p <= maxPort; p++) if (!used!.has(p)) return p
    for (let p = minPort; p < desired; p++) if (!used!.has(p)) return p
    return null
  }

  function validatePort(port: unknown, opts: { exclude?: number | number[]; excludeDomains?: string[]; autoAdvance?: boolean } = {}): ValResult & { adjusted?: boolean; original?: number } {
    const s = String(port ?? '').trim()
    if (!/^\d{1,5}$/.test(s)) return { ok: false, msg: PF.portInvalid }
    const n = parseInt(s, 10)
    if (n < 1 || n > 65535) return { ok: false, msg: PF.portInvalid }
    const exRaw = opts.exclude == null ? [] : Array.isArray(opts.exclude) ? opts.exclude : [opts.exclude]
    const exclude = exRaw.map((x) => parseInt(String(x), 10))
    if (exclude.includes(n)) return { ok: true, value: n }
    const used = collectUsedPorts(opts.excludeDomains || [])
    if (!used.has(n)) return { ok: true, value: n }
    if (opts.autoAdvance) {
      const next = findNextAvailablePort(n, 65535, 1, used)
      if (next != null) return { ok: true, value: next, adjusted: true, original: n }
    }
    return { ok: false, msg: `${PF.portInUse}: ${n} (${used.get(n)})` }
  }

  function validateDomain(domain: unknown): ValResult {
    const d = String(domain || '').trim().toLowerCase()
    if (!d || d.length > 253) return { ok: false, msg: PF.domainInvalid }
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/.test(d)) return { ok: false, msg: `${PF.domainInvalid}: ${d}` }
    return { ok: true, value: d }
  }

  function validateSiteRoot(root: string): ValResult {
    const r = normPath(root)
    if (!r) return { ok: false, msg: PF.rootEmpty }
    if (hasTraversal(r)) return { ok: false, msg: PF.pathTraversal }
    const www = normPath(app.env.WWW_ROOT)
    if (!(r === www || r.startsWith(www + '/'))) return { ok: true, value: r, outsideWww: true }
    return { ok: true, value: r }
  }

  function validateExt(name: string): boolean {
    return /^[a-zA-Z0-9._-]+$/.test(String(name || '').trim())
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  function preflight(action: string, c: any = {}): PreflightResult {
    const errors: string[] = []
    const warnings: string[] = []
    const adjusted: Record<string, unknown> = {}
    c = c || {}

    if (NEEDS_HOME.has(action) && !app.homeReady) errors.push(PF.homeNotReady)

    const installed = (k: string) => (app.installed as Record<string, string[]>)[k] || []

    switch (action) {
      case 'install': {
        const { kind, version, port, extensions } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        const vv = validateVersion(version)
        if (!vv.ok) { errors.push(vv.msg!); break }
        if (installed(kind).includes(vv.value as string)) errors.push(`${PF.versionDup}: ${kind} ${vv.value}`)
        if (kind === 'nginx' && installed('nginx').length) errors.push(PF.nginxSingle)
        if (port != null && String(port).trim() !== '') {
          const pp = validatePort(port)
          if (!pp.ok) errors.push(pp.msg!)
        }
        if (extensions) {
          const exts = String(extensions).split(',').map((x: string) => x.trim()).filter(Boolean)
          for (const e of exts) if (!validateExt(e)) { errors.push(`${PF.extInvalid}: ${e}`); break }
        }
        break
      }
      case 'uninstall': {
        const { kind, version } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        if (!installed(kind).includes(version)) { errors.push(`${PF.notInstalled}: ${kind} ${version}`); break }
        if (kind === 'php') {
          const used = app.sites.filter((s) => s.php === version).map((s) => s.domain)
          if (used.length) errors.push(`${PF.hasDependents}: PHP ${version} ← ${used.join(', ')}`)
          if (installed('php').length <= 1) errors.push(PF.lastPhp)
        }
        if (kind === 'nginx' && app.sites.length) errors.push(`${PF.hasDependents}: ${app.sites.length} 个站点依赖 Nginx`)
        break
      }
      case 'service-stop': {
        const { kind, version } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        if (!installed(kind).includes(version)) { errors.push(`${PF.notInstalled}: ${kind} ${version}`); break }
        if (app.isServiceRunning(kind, version)) {
          if (kind === 'php') {
            const used = app.sites.filter((s) => s.php === version).map((s) => s.domain)
            if (used.length) warnings.push(`以下站点正在使用 PHP ${version}：${used.join(', ')}`)
          }
          if (kind === 'nginx' && app.sites.length) warnings.push(`${app.sites.length} 个站点依赖 Nginx，停用后无法访问`)
          if (['mysql', 'pgsql', 'redis'].includes(kind)) warnings.push(`停用 ${kind} ${version} 将中断正在使用该服务的应用`)
        } else {
          errors.push(`${PF.notRunning}: ${kind} ${version}`)
        }
        break
      }
      case 'service-start': {
        const { kind, version } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        if (!installed(kind).includes(version)) { errors.push(`${PF.notInstalled}: ${kind} ${version}`); break }
        if (app.isServiceRunning(kind, version)) { errors.push(`${PF.isRunning}: ${kind} ${version}`); break }
        break
      }
      case 'site-add': {
        const { domain, port, php, root } = c
        if (!installed('php').length) { errors.push(PF.phpNeeded); break }
        if (!installed('nginx').length) { errors.push(PF.nginxNeeded); break }
        const dd = validateDomain(domain)
        if (!dd.ok) { errors.push(dd.msg!); break }
        if (app.sites.some((s) => s.domain === dd.value)) errors.push(`${PF.domainExists}: ${dd.value}`)
        if (port != null) {
          const pp = validatePort(port, { autoAdvance: true })
          if (!pp.ok) errors.push(pp.msg!)
          else if (pp.adjusted) {
            adjusted.port = pp.value
            warnings.push(PF.portAdvance.replace('{from}', String(pp.original)).replace('{to}', String(pp.value)))
          }
        }
        if (php && !installed('php').includes(php)) errors.push(`${PF.notInstalled}: PHP ${php}`)
        if (root) {
          const rr = validateSiteRoot(root)
          if (!rr.ok) errors.push(rr.msg!)
          else {
            if (rr.outsideWww) warnings.push(`${PF.rootOutsideWww}: ${rr.value}`)
            if (app.sites.some((s) => normPath(s.root) === rr.value)) errors.push(`${PF.rootDuplicated}: ${rr.value}`)
          }
        }
        break
      }
      case 'site-remove': {
        if (!app.sites.some((s) => s.domain === c.domain)) errors.push(`${PF.siteMissing}: ${c.domain}`)
        break
      }
      case 'site-port': {
        const site = app.sites.find((s) => s.domain === c.domain)
        if (!site) { errors.push(`${PF.siteMissing}: ${c.domain}`); break }
        const pp = validatePort(c.newValue, { exclude: site.port, excludeDomains: [c.domain], autoAdvance: true })
        if (!pp.ok) errors.push(pp.msg!)
        else if (pp.adjusted) {
          adjusted.port = pp.value
          warnings.push(PF.portAdvance.replace('{from}', String(pp.original)).replace('{to}', String(pp.value)))
        }
        break
      }
      case 'site-vhost': {
        const { domain, content, php } = c
        const site = app.sites.find((s) => s.domain === domain)
        if (!site) { errors.push(`${PF.siteMissing}: ${domain}`); break }
        if (content != null && !String(content).trim()) errors.push(PF.configEmpty)
        if (php && !installed('php').includes(php)) warnings.push(`${PF.notInstalled}: PHP ${php}（站点配置仍可保存，但需安装该版本才能生效）`)
        break
      }
      case 'php-switch': {
        const { domain, newPhp } = c
        const site = app.sites.find((s) => s.domain === domain)
        if (!site) { errors.push(`${PF.siteMissing}: ${domain}`); break }
        if (!installed('php').includes(newPhp)) errors.push(`${PF.notInstalled}: PHP ${newPhp}`)
        break
      }
      case 'rewrite': {
        const site = app.sites.find((s) => s.domain === c.domain)
        if (!site) { errors.push(`${PF.siteMissing}: ${c.domain}`); break }
        if (!installed('nginx').length) errors.push(PF.nginxNeeded)
        break
      }
      case 'extensions': {
        const { version, finalExts } = c
        if (!installed('php').includes(version)) { errors.push(`${PF.notInstalled}: PHP ${version}`); break }
        for (const e of finalExts || []) if (!validateExt(e)) { errors.push(`${PF.extInvalid}: ${e}`); break }
        break
      }
      case 'service-config': {
        const { kind, version, files } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        if (!installed(kind).includes(version)) { errors.push(`${PF.notInstalled}: ${kind} ${version}`); break }
        if (!files || !files.length) errors.push(PF.configEmpty)
        break
      }
      case 'restore':
      case 'backup-delete': {
        if (!app.backups.some((b) => b.file === c.file)) errors.push(`${PF.backupMissing}: ${c.file}`)
        break
      }
      case 'offline-prune': {
        if (!app.offline.trees.some((x) => x.svc === c.svc && x.ver === c.ver)) errors.push(`${PF.offlineMissing}: ${c.svc}/${c.ver}`)
        break
      }
    }

    return { ok: errors.length === 0, errors, warnings, adjusted }
  }

  return { preflight, validateVersion, validatePort, validateDomain, validateSiteRoot, validateExt, PF }
}
