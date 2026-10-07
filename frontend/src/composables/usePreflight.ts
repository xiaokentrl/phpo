// usePreflight：preflight() 的前端镜像（§0.2 #14：UI 即时反馈；最终裁决在 internal/preflight/）
// 逐字迁移原型 PF 文案 + validateVersion/Port/Domain/SiteRoot/Ext + preflight(action, ctx) 20 action。
import { useAppState } from '@/stores/appState'
import { useCacheStore } from '@/stores/cacheStore'
import { hasBackend } from '@/api/site'
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
  backupMissing: '备份归档不存在',
  offlineMissing: '离线缓存条目不存在',
  svcMissing: '服务不存在',
  siteMissing: '站点不存在',
  extInvalid: '扩展名不合法',
  configEmpty: '配置内容不能为空',
  nginxNeeded: '请先安装 Nginx',
  phpNeeded: '请先安装一个 PHP 版本',
  fileMissing: '待导入的文件不存在',
  extTypeInvalid: '缓存文件类型只能是 image / apk / pecl',
  dataDirPending: '正在运行，数据目录要重建容器后才生效',
  registryHost: '镜像源地址不正确',
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

// portKey：与后端 config.EnvKeyPort 对齐（大写种类 + 去点版本）；nginx 同键，无版本无关键
function portKey(kind: string, version: string): string {
  return `${kind.toUpperCase()}_${String(version).replace(/\./g, '')}_PORT`
}

// PHP 未就绪的降级告警：与后端 rules_site.go#siteAdd 文案逐字对齐（建站不阻断，仅暂不写 vhost）
function phpPendingWarn(php?: string): string {
  return `${PF.notInstalled}: PHP ${php || '未指定'}（站点仍会创建，安装或切换到可用 PHP 版本后生效）`
}

// Nginx 已装但未运行的降级告警：与后端 rules_site.go#nginxNotRunningWarn 文案逐字对齐
function nginxNotRunningWarn(): string {
  return `${PF.notRunning}: Nginx（站点仍会创建，站点配置先落盘但未经 Nginx 校验；启动 Nginx 后重新校验并补齐端口发布）`
}

// 编辑站点的降级告警：与后端 rules_site.go#nginxNotServingWarn 文案逐字对齐
// 模板生成的那三处（改端口 / 切 PHP / 伪静态）走这里——没在跑只告警、照常落盘。
function nginxNotServingWarn(): string {
  return `${PF.notRunning}: Nginx（站点配置仍会落盘，但未经 Nginx 校验、端口暂不发布；启动 Nginx 后自动重新校验并补齐）`
}

// 编辑站点的拦截文案：与后端 rules_site.go#nginxNotServingErr 文案逐字对齐
function nginxNotServingErr(): string {
  return `${PF.notRunning}: Nginx（vhost 改动须经运行中的 Nginx 校验后才能落盘，请先启动 Nginx 再重试）`
}

// 端口占用的降级告警：与后端 rules_site.go#siteAdd 文案逐字对齐
// §5.8：不改用户所填端口；配置照常落盘，只有这一个端口暂不发布（站点降级），腾出即自动补齐。
function portDegradeWarn(msg: string): string {
  return `${msg}（站点仍会创建，站点配置照常落盘，只是这个端口暂不发布；腾出该端口或改用空闲端口后自动补齐）`
}

function hasTraversal(p: string): boolean {
  return /(^|\/)\.\.(\/|$)/.test(String(p || ''))
}

// reRegistryHost 与 config.reRegistryHost 同一条正则：只认「主机」或「主机:端口」，不带路径
const reRegistryHost = /^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?(:[0-9]{1,5})?$/

// normalizeRegistryHost 与 config.NormalizeRegistryHost 同步：去空白 → 剥 http(s):// → 去结尾斜杠 → 去尾部 /v2
function normalizeRegistryHost(s: string): string {
  let h = String(s ?? '').trim()
  for (const p of ['https://', 'http://']) if (h.startsWith(p)) h = h.slice(p.length)
  h = h.replace(/\/+$/, '')
  if (h.endsWith('/v2')) h = h.slice(0, -3)
  return h.trim()
}

// validateRegistryHost 与 config.ValidateRegistryHost 同判据、同文案（UI 这层只作即时反馈，最终裁决在后端）
function validateRegistryHost(s: string): ValResult {
  const h = normalizeRegistryHost(s)
  if (!h) return { ok: false, msg: `${PF.registryHost}: 不能为空` }
  if (/[ \t\x00]/.test(h)) return { ok: false, msg: `${PF.registryHost}: ${s}（不能含空白字符）` }
  if (h.includes('/')) return { ok: false, msg: `${PF.registryHost}: ${s}（只填主机名或「主机:端口」，不要带路径）` }
  if (!reRegistryHost.test(h)) return { ok: false, msg: `${PF.registryHost}: ${s}` }
  return { ok: true, value: h }
}

// NEEDS_HOME：18 个动作（后端 preflight.needsHome 镜像；root-set 写 config.yaml、cache-import 写缓存根、
// docker-source-set 写 config.yaml，两根未就绪即拒绝）
const NEEDS_HOME = new Set([
  'install', 'uninstall', 'service-stop', 'service-start', 'update-config',
  'site-add', 'site-remove', 'site-port', 'site-vhost', 'rewrite',
  'extensions', 'service-config', 'backup', 'restore', 'offline-prune',
  'root-set', 'cache-import', 'docker-source-set',
])

interface ValResult { ok: boolean; msg?: string; value?: string | number; outsideWww?: boolean }

export function usePreflight() {
  const app = useAppState()
  const cache = useCacheStore()

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
        const key = portKey(kind, v)
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

  function validatePort(port: unknown, opts: { exclude?: number | number[]; excludeDomains?: string[]; autoAdvance?: boolean; keepOnConflict?: boolean } = {}): ValResult & { adjusted?: boolean; occupied?: boolean; original?: number } {
    const s = String(port ?? '').trim()
    if (!/^\d{1,5}$/.test(s)) return { ok: false, msg: PF.portInvalid }
    const n = parseInt(s, 10)
    if (n < 1 || n > 65535) return { ok: false, msg: PF.portInvalid }
    const exRaw = opts.exclude == null ? [] : Array.isArray(opts.exclude) ? opts.exclude : [opts.exclude]
    const exclude = exRaw.map((x) => parseInt(String(x), 10))
    if (exclude.includes(n)) return { ok: true, value: n }
    const used = collectUsedPorts(opts.excludeDomains || [])
    if (!used.has(n)) return { ok: true, value: n }
    // 新建站点端口占用（§5.8）：端口原样保留，只回传占用信息交上层弹框告警 + 降级建站
    if (opts.keepOnConflict) {
      // 别的站点已经把这个端口交给自己的 Nginx 发布了，同一颗 Nginx 按 server_name 分流即可复用，
      // 所以「站点占用」不算占用——只剔本地这一份抄件，共用占用表一字不动（与后端 validators.go 同口径）。
      // 数据服务占用的端口、以及本机其它进程占用的端口仍然照报。
      for (const [p, owner] of used) if (owner.startsWith('site ')) used.delete(p)
      if (!used.has(n)) return { ok: true, value: n }
      return { ok: true, value: n, occupied: true, msg: `${PF.portInUse}: ${n} (${used.get(n)})` }
    }
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

    // vhost 写链分两档门禁（与后端 rules_site.go 同口径）：
    // 手改正文（site-vhost）必须已装且运行——改的是用户自己写的东西，没有运行中的 Nginx 校验就落盘，
    // 等于把一份没人验过的正文塞进去（硬红线 2）。
    // 模板生成的那三处（改端口 / 切 PHP / 伪静态）只要求已装：没在跑就只告警、照常落盘，
    // 端口暂不发布、站点标降级，启动 Nginx 后自动重新校验并补齐。
    const nginxServing = (): boolean => {
      const vs = installed('nginx')
      if (!vs.length) {
        errors.push(PF.nginxNeeded)
        return false
      }
      if (!vs.some((v) => app.isServiceRunning('nginx', v))) {
        errors.push(nginxNotServingErr())
        return false
      }
      return true
    }

    // 模板生成那三处的门禁：nginx 未装仍然阻断（没有 nginx 就无所谓校验与发布），未运行只降级告警。
    const nginxServingWarn = (): boolean => {
      const vs = installed('nginx')
      if (!vs.length) {
        errors.push(PF.nginxNeeded)
        return false
      }
      if (!vs.some((v) => app.isServiceRunning('nginx', v))) {
        warnings.push(nginxNotServingWarn())
      }
      return true
    }

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
          // 依赖站点与「最后一个版本」只告警不阻止（§0.2 规则 16 / §1.11）；文案与后端 rules_service.go 逐字对齐
          const used = app.sites.filter((s) => s.php === version).map((s) => s.domain)
          if (used.length) warnings.push(`以下站点正在使用 PHP ${version}：${used.join(', ')} —— 卸载后这些站点的 vhost 上游失效`)
        }
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
      case 'update-config': {
        // 与后端 rules_service.go#updateConfig 对齐：服务端口占用直接报错，不顺延（§5.8）
        const { kind, version, field, newValue } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        if (!installed(kind).includes(version)) { errors.push(`${PF.notInstalled}: ${kind} ${version}`); break }
        if (field === 'port') {
          const cur = app.env[portKey(kind, version)]
          const pp = validatePort(newValue, { exclude: parseInt(String(cur), 10) })
          if (!pp.ok) errors.push(pp.msg!)
        }
        break
      }
      case 'site-add': {
        const { domain, port, php, root } = c
        // 建站的唯一服务门禁是 nginx 未装（阻断）；未运行只降级告警，PHP 等其余服务缺失同样只告警不阻断
        if (!installed('nginx').length) { errors.push(PF.nginxNeeded); break }
        if (!installed('nginx').some((v) => app.isServiceRunning('nginx', v))) warnings.push(nginxNotRunningWarn())
        const dd = validateDomain(domain)
        if (!dd.ok) { errors.push(dd.msg!); break }
        if (app.sites.some((s) => s.domain === dd.value)) errors.push(`${PF.domainExists}: ${dd.value}`)
        if (port != null) {
          // 新建站点端口占用：不顺延、不阻断，仅告警并以降级态建站（§5.8）
          const pp = validatePort(port, { keepOnConflict: true })
          if (!pp.ok) errors.push(pp.msg!)
          else if (pp.occupied) warnings.push(portDegradeWarn(pp.msg!))
        }
        if (!installed('php').includes(php ?? '')) warnings.push(phpPendingWarn(php))
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
        if (!nginxServingWarn()) break
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
        if (!nginxServing()) break
        if (content != null && !String(content).trim()) errors.push(PF.configEmpty)
        if (php && !installed('php').includes(php)) warnings.push(`${PF.notInstalled}: PHP ${php}（站点配置仍可保存，但需安装该版本才能生效）`)
        break
      }
      case 'php-switch': {
        const { domain, newPhp } = c
        const site = app.sites.find((s) => s.domain === domain)
        if (!site) { errors.push(`${PF.siteMissing}: ${domain}`); break }
        if (!nginxServingWarn()) break
        if (!installed('php').includes(newPhp)) errors.push(`${PF.notInstalled}: PHP ${newPhp}`)
        break
      }
      case 'rewrite': {
        const site = app.sites.find((s) => s.domain === c.domain)
        if (!site) { errors.push(`${PF.siteMissing}: ${c.domain}`); break }
        if (!nginxServingWarn()) break
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
        // 真宿主的缓存权威是 cacheStore.entries（OfflineView/useCache 写入）；app.offline 只是 demo 占位，
        // 在真宿主下恒为空，拿它判定会把每个真实条目都报成「缓存条目不存在」。
        const hit = hasBackend()
          ? cache.entries.some((e) => e.kind === c.svc && e.version === c.ver)
          : app.offline.trees.some((x) => x.svc === c.svc && x.ver === c.ver)
        if (!hit) errors.push(`${PF.offlineMissing}: ${c.svc}/${c.ver}`)
        break
      }
      // root-set：自定义缓存根 / 备份根 / 每服务版本数据目录（需求 1/2/7/8）。与后端 rules_root.go 同判据：
      // 唯一限制是路径安全，空串合法（= 清除自定义、回落默认根）；数据目录才校 kind/version。
      case 'root-set': {
        const { field, kind, version, newValue } = c
        if (field === 'data_dir') {
          if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
          const vv = validateVersion(version)
          if (!vv.ok) { errors.push(vv.msg!); break }
          if (app.isServiceRunning(kind, version)) warnings.push(`${kind} ${version} ${PF.dataDirPending}`)
        }
        const p = normPath(String(newValue ?? ''))
        if (p && (hasTraversal(p) || p.includes('\0'))) errors.push(PF.pathTraversal)
        break
      }
      // cache-import：手工导入任意文件为缓存条目（需求 1）。源文件是否存在由后端导入步骤给人话报错，
      // UI 这层只拦「没选文件」这一必然失败的输入，不去猜文件系统状态。
      case 'cache-import': {
        const { kind, version, field, newValue } = c
        if (!SVC_META[kind as ServiceKind]) { errors.push(PF.svcMissing); break }
        const vv = validateVersion(version)
        if (!vv.ok) { errors.push(vv.msg!); break }
        if (field !== 'image' && field !== 'apk' && field !== 'pecl') errors.push(`${PF.extTypeInvalid}，得 ${field}`)
        else if ((field === 'apk' || field === 'pecl') && kind !== 'php') errors.push(`${field} 扩展只能导入到 php 缓存`)
        if (!String(newValue ?? '').trim()) errors.push(PF.fileMissing)
        break
      }
      // docker-source-set：设置页的 Docker 镜像源清单（一行一个「主机[:端口]」）。与后端 rules_docker.go 同判据
      // （共用 config.ValidateRegistryHosts：忽略空行、逐行合形、首个坏值点名）；可达性不在这里判——
      // 填了连不上的源照样保存，拉取时逐个点名后回落直连官方（最小限制：不拿「此刻不通」拦住用户的配置）。
      case 'docker-source-set': {
        const rows: string[] = Array.isArray(c.sources) ? c.sources : []
        const kept = rows.filter((x) => String(x ?? '').trim() !== '')
        for (const raw of kept) {
          const hv = validateRegistryHost(raw)
          if (!hv.ok) { errors.push(hv.msg!); break }
        }
        if (!kept.length) warnings.push('未填写镜像源，拉取将直连官方（docker.io）')
        break
      }
    }

    return { ok: errors.length === 0, errors, warnings, adjusted }
  }

  return { preflight, validateVersion, validatePort, validateDomain, validateSiteRoot, validateExt, PF }
}
