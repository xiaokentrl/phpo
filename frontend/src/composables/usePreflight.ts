// usePreflight：preflight() 的前端镜像（§0.2 #14：UI 即时反馈；最终裁决在 internal/preflight/）
// 逐字迁移原型 PF 文案 + validateVersion/Port/Domain/SiteRoot/Ext + preflight(action, ctx) 20 action。
import { useAppState } from '@/stores/appState'
import { useCacheStore } from '@/stores/cacheStore'
import { hasBackend } from '@/api/site'
import { SVC_META } from '@/constants/service'
import { t } from '@/composables/useI18n'
import type { ServiceKind } from '@/types'

// PF 的登记项（文案本体在 locales 的 pf.* 里，中英两侧各一份）：
// 这些句子是**在提交前就展示给用户**的即时反馈，必须跟着界面语言走，所以不能像旧版那样写死中文。
// 每次取用时现读一次语言，用户切完语言不必重开应用。
const PF_KEYS = [
  'homeNotReady', 'versionInvalid', 'versionDup', 'nginxSingle', 'portInvalid',
  'portInUse', 'portAdvance', 'domainInvalid', 'domainExists', 'rootEmpty',
  'rootOutsideWww', 'rootDuplicated', 'pathTraversal', 'notInstalled', 'isRunning',
  'notRunning', 'hasDependents', 'backupMissing', 'offlineMissing', 'svcMissing',
  'siteMissing', 'extInvalid', 'configEmpty', 'nginxNeeded', 'phpNeeded',
  'fileMissing', 'extTypeInvalid', 'dataDirPending', 'registryHost',
] as const

type PfKey = (typeof PF_KEYS)[number]

// PF 按当前语言现取：每访问一次就读一次 locales，所以用户切完语言不必重开应用。
// 用法与旧的字面量表一致（PF.portInUse），因此下面所有调用点不必跟着改。
const PF = {} as Record<PfKey, string>
for (const k of PF_KEYS) Object.defineProperty(PF, k, { get: () => t(`pf.${k}`), enumerable: true })

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

// PHP 未就绪的降级告警：语义与后端 rules_site.go#siteAdd 那句中文一致（建站不阻断，仅暂不写 vhost）
function phpPendingWarn(php?: string): string {
  return t('pf.phpPending', { notInstalled: PF.notInstalled, php: php || t('pf.phpUnspecified') })
}

// Nginx 已装但未运行的降级告警：语义与后端 rules_site.go#nginxNotRunningWarn 那句中文一致
// rootless 那一支说的是另一件事：容器引擎没有以 root 跑时，1024 以下的端口本来就绑不上，
// 那不是「Nginx 没在跑」造成的，文案不得混为一谈；也不替用户改他填的端口。
function nginxNotRunningWarn(rootless: boolean): string {
  return t(rootless ? 'pf.nginxNotRunningAddRootless' : 'pf.nginxNotRunningAdd', { notRunning: PF.notRunning })
}

// 编辑站点的降级告警：语义与后端 rules_site.go#nginxNotServingWarn 那句中文一致
// 模板生成的那三处（改端口 / 切 PHP / 伪静态）走这里——没在跑只告警、照常落盘。
function nginxNotServingWarn(rootless: boolean): string {
  return t(rootless ? 'pf.nginxNotServingEditRootless' : 'pf.nginxNotServingEdit', { notRunning: PF.notRunning })
}

// 编辑站点的拦截文案：语义与后端 rules_site.go#nginxNotServingErr 那句中文一致
function nginxNotServingErr(): string {
  return t('pf.nginxNotServingErr', { notRunning: PF.notRunning })
}

// 端口占用的降级告警：语义与后端 rules_site.go#siteAdd 的 pp.Occupied 那句中文一致
// §5.8：不改用户所填端口；配置照常落盘，只有这一个端口暂不发布（站点降级），腾出即自动补齐。
function portDegradeWarn(msg: string): string {
  return t('pf.portDegrade', { msg })
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
  if (!h) return { ok: false, msg: t('pf.registryHostEmpty', { registryHost: PF.registryHost }) }
  if (/[ \t\x00]/.test(h)) return { ok: false, msg: t('pf.registryHostSpace', { registryHost: PF.registryHost, value: s }) }
  if (h.includes('/')) return { ok: false, msg: t('pf.registryHostPath', { registryHost: PF.registryHost, value: s }) }
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
    // 「站点占用」不算占用：站点那个宿主端口本来就是自家 Nginx 发布出来的，同一颗 Nginx 按 server_name
    // 分流即可复用（与后端 validators.go#validatePort 的 conflictKeepWarn/conflictBlock 两档同口径）。
    // 装 Nginx / 改服务端口时拿「Nginx 正在发布的口」去拦它自己，等于让自家的东西占自家的口。
    // 数据服务（mysql/pgsql/redis）占的端口仍然照报。唯一保留站点占用的是「改已有站点的端口」那一路：
    // 它要在各站点之间挑一个空口，别的站点正站在那一口上就该往后让。
    if (!opts.autoAdvance) for (const [p, owner] of used) if (owner.startsWith('site ')) used.delete(p)
    if (!used.has(n)) return { ok: true, value: n }
    // 新建站点端口占用（§5.8）：端口原样保留，只回传占用信息交上层弹框告警 + 降级建站
    if (opts.keepOnConflict) {
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
        warnings.push(nginxNotServingWarn(app.engine?.rootless ?? false))
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
          if (used.length) warnings.push(t('pf.uninstallPhpSites', { version, list: used.join(', ') }))
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
            if (used.length) warnings.push(t('pf.stopPhpSites', { version, list: used.join(', ') }))
          }
          if (kind === 'nginx' && app.sites.length) warnings.push(t('pf.stopNginxSites', { n: app.sites.length }))
          if (['mysql', 'pgsql', 'redis'].includes(kind)) warnings.push(t('pf.stopDataService', { kind, version }))
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
        if (!installed('nginx').some((v) => app.isServiceRunning('nginx', v))) warnings.push(nginxNotRunningWarn(app.engine?.rootless ?? false))
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
        if (php && !installed('php').includes(php)) warnings.push(t('pf.vhostPhpPending', { notInstalled: PF.notInstalled, php }))
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
        if (field !== 'image' && field !== 'apk' && field !== 'pecl') errors.push(t('pf.extTypeGot', { extTypeInvalid: PF.extTypeInvalid, field }))
        else if ((field === 'apk' || field === 'pecl') && kind !== 'php') errors.push(t('pf.extImportPhpOnly', { field }))
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
        if (!kept.length) warnings.push(t('pf.registryHostEmptyList'))
        break
      }
    }

    return { ok: errors.length === 0, errors, warnings, adjusted }
  }

  return { preflight, validateVersion, validatePort, validateDomain, validateSiteRoot, validateExt, PF }
}
