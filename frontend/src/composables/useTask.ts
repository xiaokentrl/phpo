// useTask：runTask(args, label, meta) 的前端入口（T109）。
// 委托给 taskStore：构建日志行、忙锁、逐行回放、取消。状态落地仍留给后端事件（T110）。
import { useTaskStore } from '@/stores/taskStore'
import { hasBackend } from '@/api/site'
import { toast } from './useToast'

export type { TaskStatus, TaskMeta, TaskRecord, TaskLine, LineType } from '@/stores/taskStore'
export { TASK_BUSY } from '@/stores/taskStore'

export function runTask(args: string[], label?: string, meta?: import('@/stores/taskStore').TaskMeta): void {
  useTaskStore().start(args, label, meta)
}

// submitWrite：写操作统一入口。无宿主 → 本地回放，绝不触后端；有宿主 → 登记等效命令并展开任务抽屉，再发起后端调用。
// 状态一律由后端事件回流（硬红线 4），此处只把「后端当场拒绝」的原因显式反馈给用户。
export function submitWrite(
  args: string[],
  label: string,
  meta: import('@/stores/taskStore').TaskMeta,
  exec: () => Promise<unknown>,
): void {
  const store = useTaskStore()
  if (!hasBackend()) {
    store.start(args, label, meta)
    return
  }
  store.start(args, label, meta)
  exec().catch((e: unknown) => toast(String(e), 'err', 4600))
}

export function cancelTask(): void {
  useTaskStore().cancel()
}

export function useTask() {
  const store = useTaskStore()
  return { current: store.task, runTask, cancelTask, toggleDrawer: store.toggle }
}
