// 站点前端 API：对生成的 Wails 绑定（bindings/phpo/app.ts）的薄封装（T403/T404/T405/T406 接线点）。
// 硬红线 5：写操作一律经后端三段式；此处只发起调用，状态由后端 state:changed 事件回流（硬红线 4）。
import * as app from '../../bindings/phpo/app.js'
import { AddInput } from '../../bindings/phpo/internal/service/models.js'

// 后端运行环境探测：Wails 宿主注入 window.runtime；纯 Vite 独立运行（pnpm dev）时缺席，退化为 mock 演示。
export function hasBackend(): boolean {
  return typeof (globalThis as { window?: { runtime?: unknown } }).window?.runtime !== 'undefined'
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
