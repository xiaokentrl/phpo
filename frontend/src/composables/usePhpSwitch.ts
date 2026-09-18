// usePhpSwitch：站点 PHP 版本切换的调用入口（T405）。
// 有后端（Wails 宿主）走 SiteService.SwitchPHP 三段式；纯 Vite 演示退化为 mock runTask。状态由后端事件回流。
import { runTask } from './useTask'
import { switchSitePhp, hasBackend } from '@/api/site'
import { toast } from './useToast'

export function usePhpSwitch() {
  function switchPhp(domain: string, php: string, original: string): void {
    if (php === original) return
    if (!hasBackend()) {
      runTask(['site', 'php', domain, php], `PHP ${domain} → ${php}`, { type: 'site-php', domain, php })
      return
    }
    switchSitePhp(domain, php).catch((e: unknown) => {
      toast(String(e), 'err', 4600)
    })
  }
  return { switchPhp }
}
