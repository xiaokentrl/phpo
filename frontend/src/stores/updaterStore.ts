// updaterStore（T604 / §5.9）：应用升级状态仓，唯一真实来源是后端 update:* 事件（硬红线 4）。
// 前端不乐观更新：available 由 update:available 填，progress 由 update:progress 逐阶段刷新，done 收尾并驱动 UI 提示重启。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { UpdateAvailable, UpdateProgress, UpdateDone } from '@/types'

export const useUpdaterStore = defineStore('updater', () => {
  const available = ref<UpdateAvailable | null>(null)
  const progress = ref<UpdateProgress | null>(null)
  const done = ref<UpdateDone | null>(null)
  const checking = ref(false)

  function setAvailable(a: UpdateAvailable | null): void {
    available.value = a
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

  return { available, progress, done, checking, setAvailable, setProgress, setDone, reset }
})
