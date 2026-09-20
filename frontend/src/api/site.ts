// 站点前端 API：对生成的 Wails 绑定（bindings/phpo/app.ts）的薄封装（T403/T404/T405/T406 接线点）。
// 硬红线 5：写操作一律经后端三段式；此处只发起调用，状态由后端 state:changed 事件回流（硬红线 4）。
import * as app from '../../bindings/phpo/app.js'
import { AddInput } from '../../bindings/phpo/internal/service/models.js'

// 后端运行环境探测：Wails v3 不注入 window.runtime（那是 v2 全局）。真实宿主在文档加载完成后
// 由 runtime.Core 注入 window._wails.flags / .environment / .invoke；纯 Vite 浏览器里 @wailsio/runtime
// 只建 window._wails 空壳、不填这些字段。故以宿主独有字段为判据，命中才走真实后端，否则退化 demo。
export function hasBackend(): boolean {
  const w = (globalThis as { window?: { _wails?: { flags?: unknown; environment?: unknown; invoke?: unknown } } }).window
  const bridge = w?._wails
  return !!(bridge && (bridge.flags || bridge.environment || bridge.invoke))
}

// 宿主 Core 在 WindowLoadFinished 注入，可能晚于 Vue 挂载。挂载期门禁（向导/Docker 首探测）需等它就绪：
// 命中 wails:runtime-config-ready 或轮询到 bridge 即放行；浏览器 demo 永不就绪，超时后按无宿主继续。
export function waitForBackend(timeout = 1500): Promise<boolean> {
  if (hasBackend()) return Promise.resolve(true)
  return new Promise((resolve) => {
    let settled = false
    let poll: ReturnType<typeof setInterval>
    let timer: ReturnType<typeof setTimeout>
    const settle = () => {
      if (settled) return
      settled = true
      clearInterval(poll)
      clearTimeout(timer)
      window.removeEventListener('wails:runtime-config-ready', onReady)
      resolve(hasBackend())
    }
    const onReady = () => settle()
    poll = setInterval(() => {
      if (hasBackend()) settle()
    }, 30)
    timer = setTimeout(settle, timeout)
    window.addEventListener('wails:runtime-config-ready', onReady, { once: true })
  })
}

export interface SiteAddInput {
  domain: string
  port: number
  php: string
  root?: string
  rewrite?: string
  rewriteRule?: string
}

// addSite 幂等建站
export function addSite(in0: SiteAddInput): Promise<void> {
  return app.SiteAdd(new AddInput({
    Domain: in0.domain,
    Port: in0.port,
    PHP: in0.php,
    Root: in0.root ?? '',
    Rewrite: in0.rewrite ?? '',
    RewriteRule: in0.rewriteRule ?? '',
  }))
}

export function removeSite(domain: string): Promise<void> {
  return app.SiteRemove(domain)
}

export function setSitePort(domain: string, port: number): Promise<void> {
  return app.SiteSetPort(domain, port)
}

export function switchSitePhp(domain: string, php: string): Promise<void> {
  return app.SiteSwitchPHP(domain, php)
}

export function setSiteRewrite(domain: string, preset: string, rule = ''): Promise<void> {
  return app.SiteSetRewrite(domain, preset, rule)
}

export function setSiteVhostContent(domain: string, content: string): Promise<void> {
  return app.SiteSetVhostContent(domain, content)
}

// addSiteHosts 手动补写系统 hosts；后端回空串表示已生效，非空是需转达给用户的人话警告（如要以管理员身份运行）
export function addSiteHosts(domain: string): Promise<string> {
  return app.SiteAddHosts(domain)
}
