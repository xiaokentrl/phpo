// useUpdater（T604 / §5.9）：应用升级的前端接线层——订阅 update:* 事件落地 updaterStore，
// 并提供「检查更新 / 立即升级」两个动作。硬红线 4：状态只来自后端事件，前端不乐观更新。
import { computed } from 'vue'
import { EVENT, onEvent, type UpdateAvailablePayload } from '@/api/events'
import { useUpdaterStore } from '@/stores/updaterStore'
import { checkUpdate, applyUpdate } from '@/api/updater'
import { toast } from '@/composables/useToast'
import { t } from '@/composables/useI18n'
import type { UpdateProgress, UpdateDone } from '@/types'

let subscribed = false
const offs: Array<() => void> = []

// subscribeUpdater：注册 update:available/progress/done 订阅（幂等，重复调用不叠加监听）。
// update:available 是唯一 toast 来源（后端 Checker 每次发现新版本必发此事件），故手动检查不重复弹提示。
export function subscribeUpdater(): void {
  if (subscribed) return
  subscribed = true
  const s = useUpdaterStore()
  offs.push(
    onEvent(EVENT.UpdateAvailable, (p) => {
      const a = p as UpdateAvailablePayload
      s.setAvailable(a)
      toast(t('update.found', { version: a.version }), 'info', 3600)
    }),
    onEvent(EVENT.UpdateProgress, (p) => s.setProgress(p as UpdateProgress)),
    onEvent(EVENT.UpdateDone, (p) => s.setDone(p as UpdateDone)),
  )
}

export function unsubscribeUpdater(): void {
  offs.forEach((off) => off())
  offs.length = 0
  subscribed = false
}

export function useUpdater() {
  const s = useUpdaterStore()

  const percent = computed(() => s.progress?.percent ?? 0)
  const stage = computed(() => s.progress?.stage ?? '')
  const busy = computed(() => s.progress !== null)

  // check 主动检查更新：有更新时提示由 update:available 事件统一发出；无更新提示已最新
  async function check(): Promise<void> {
    if (s.checking) return
    s.checking = true
    try {
      const info = await checkUpdate()
      if (!info) toast(t('update.latestShort'), 'ok', 2600)
    } catch (e) {
      toast(String(e), 'err', 4600)
    } finally {
      s.checking = false
    }
  }

  // apply 立即升级：交给后端三段式任务；进度/结果经 update:* 事件回流（失败自动回滚，硬红线 6）
  async function apply(): Promise<void> {
    s.reset()
    try {
      await applyUpdate()
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  return { store: s, percent, stage, busy, check, apply }
}
