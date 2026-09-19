// cleanupStore（T605 / §5.13.6-7）：清理面板状态仓。数据只来自后端读接口与 docker:* 事件（硬红线 4），
// 前端不乐观更新——清理完成后重新 scan/list 拉权威结果。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { OrphanReport, TrashEntry, Operation } from '@/types'

const emptyOrphans: OrphanReport = { containers: [], volumes: [], networks: [], images: [] }

export const useCleanupStore = defineStore('cleanup', () => {
  const orphans = ref<OrphanReport>({ ...emptyOrphans })
  const trash = ref<TrashEntry[]>([])
  const operations = ref<Operation[]>([])
  const scanning = ref(false)
  const cleaning = ref(false)

  function setOrphans(o: OrphanReport): void {
    orphans.value = o
  }
  function setTrash(t: TrashEntry[]): void {
    trash.value = t
  }
  function setOperations(o: Operation[]): void {
    operations.value = o
  }

  return { orphans, trash, operations, scanning, cleaning, setOrphans, setTrash, setOperations }
})
