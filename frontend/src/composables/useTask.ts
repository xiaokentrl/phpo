// useTask：runTask(args, label, meta) 的前端入口（T109）。
// 委托给 taskStore：构建日志行、忙锁、逐行回放、取消。状态落地仍留给后端事件（T110）。
import { useTaskStore } from '@/stores/taskStore'

export type { TaskStatus, TaskMeta, TaskRecord, TaskLine, LineType } from '@/stores/taskStore'
export { TASK_BUSY } from '@/stores/taskStore'

export function runTask(args: string[], label?: string, meta?: import('@/stores/taskStore').TaskMeta): void {
  useTaskStore().start(args, label, meta)
}

export function cancelTask(): void {
  useTaskStore().cancel()
}

export function useTask() {
  const store = useTaskStore()
  return { current: store.task, runTask, cancelTask, toggleDrawer: store.toggle }
}
