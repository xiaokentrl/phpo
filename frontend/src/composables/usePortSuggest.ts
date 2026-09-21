// usePortSuggest：站点端口顺延 UI（T404）。改端口前先跑 preflight('site-port')：
// 占用则后端建议顺延首个可用（adjusted.port），弹 DangerConfirm 展示「80→81 已调整」，确认后落库并回流 state:changed。
import { useModalStore } from '@/stores/modalStore'
import { useI18n } from './useI18n'
import { usePreflight } from './usePreflight'
import { toast } from './useToast'
import { runTask } from './useTask'
import { syncState } from '@/composables/useStateSync'
import { setSitePort, hasBackend } from '@/api/site'
import DangerConfirm from '@/components/business/DangerConfirm.vue'

export function usePortSuggest() {
  const modal = useModalStore()
  const { t } = useI18n()
  const { preflight } = usePreflight()

  function applyPort(domain: string, desired: number): void {
    const check = preflight('site-port', { domain, port: desired })
    if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
    const finalPort = (check.adjusted.port as number | undefined) ?? desired
    const changed = finalPort !== desired
    const submit = async (): Promise<void> => {
      if (!hasBackend()) {
        runTask(['site', 'port', domain, String(finalPort)], `${t('sites.col.port')} ${domain} → ${finalPort}`, { type: 'site-port', domain, port: finalPort })
        return
      }
      // 硬红线 4：写后（含失败——vhost 可能已改而 reload 失败）拉一次权威快照收口，不本地乐观更新
      try {
        await setSitePort(domain, finalPort)
      } catch (e: unknown) {
        toast(String(e), 'err', 4600)
      }
      await syncState()
    }
    // 端口顺延或其余警告：先经 DangerConfirm 展示（preflight 已生成「80→81 已调整」文案），确认后落库
    const warnings = check.warnings.map((w) => ({ text: w }))
    if (changed || warnings.length) {
      modal.open(DangerConfirm, { title: `${t('sites.col.port')} ${domain}`, warnings, confirmLabel: t('common.confirm'), onConfirm: submit })
      return
    }
    submit()
  }

  return { applyPort }
}
