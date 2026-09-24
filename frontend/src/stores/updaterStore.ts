// updaterStore（T604 / §5.9）：应用升级状态仓，唯一真实来源是后端 update:* 事件（硬红线 4）。
// 前端不乐观更新：available 由 update:available 填，progress 由 update:progress 逐阶段刷新，done 收尾并驱动 UI 提示重启。
// dismissed 是「忽略此版本」的 UI 偏好（§3.1 原则 5）：只落 localStorage，不落 config.yaml、不进快照、不改后端状态。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { UpdateAvailable, UpdateProgress, UpdateDone } from '@/types'

const DISMISS_KEY = 'phpo-update-dismissed'

export const useUpdaterStore = defineStore('updater', () => {
  const available = ref<UpdateAvailable | null>(null)
  const progress = ref<UpdateProgress | null>(null)
  const done = ref<UpdateDone | null>(null)
  const checking = ref(false)
  const dismissed = ref(localStorage.getItem(DISMISS_KEY) ?? '')

  // hasUpdate 未被忽略的可用更新——左上角徽标与 toast 的判据（后台下载关窗后 available 仍在，徽标改显示进度）
  const hasUpdate = computed(() => !!available.value && available.value.version !== dismissed.value)

  function setAvailable(a: UpdateAvailable | null): void {
    available.value = a
    if (a) done.value = null // 新的可用版本即新一轮，上一次升级的终态不得继续顶掉徽标的「N 项可用更新」
  }
  function setProgress(p: UpdateProgress | null): void {
    progress.value = p
    if (p) done.value = null // 新一轮进度覆盖旧结果
  }
  function setDone(d: UpdateDone | null): void {
    done.value = d
    if (d) progress.value = null
  }
  function reset(): void {
    progress.value = null
    done.value = null
  }
  function dismiss(v: string): void {
    if (!v) return
    dismissed.value = v
    localStorage.setItem(DISMISS_KEY, v)
  }
  function undismiss(): void {
    dismissed.value = ''
    localStorage.removeItem(DISMISS_KEY)
  }

  return {
    available, progress, done, checking, dismissed, hasUpdate,
    setAvailable, setProgress, setDone, reset, dismiss, undismiss,
  }
})
