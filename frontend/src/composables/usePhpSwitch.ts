// usePhpSwitch：站点 PHP 版本切换的调用入口（T405）。
// 有后端（Wails 宿主）走 SiteService.SwitchPHP 三段式；纯 Vite 演示退化为 mock runTask。
// 硬红线 4：切换后（含失败——vhost 可能已落盘而 reload 失败）拉一次权威快照收口，绝不本地乐观更新。
import { runTask } from './useTask'
import { syncState } from '@/composables/useStateSync'
import { switchSitePhp, hasBackend } from '@/api/site'
import { toast } from './useToast'

export function usePhpSwitch() {
  async function switchPhp(domain: string, php: string, original: string): Promise<void> {
    if (php === original) return
    if (!hasBackend()) {
      runTask(['site', 'php', domain, php], `PHP ${domain} → ${php}`, { type: 'site-php', domain, php })
      return
    }
    try {
      await switchSitePhp(domain, php)
    } catch (e: unknown) {
      toast(String(e), 'err', 4600)
    }
    await syncState()
  }
  return { switchPhp }
}
