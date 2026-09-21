// 任务实时通道的后端封装（Q5）：取消运行中任务 / 撤回排队项 / 任务账本历史。
// 队列详情与进度不在此拉取——它们随权威快照 state:changed 实时推送（§5.6 事件名冻结，不新增第 18 个）。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import { listOperations } from '@/api/cleanup'
import type { Operation } from '@/types'

// cancelRunning 请求取消当前运行中任务；终态仍等后端 task:done
export async function cancelRunning(): Promise<void> {
  if (!hasBackend()) return
  await app.Cancel()
}

// withdrawQueued 撤回排队项（尚未执行，无需回滚）。返回后端是否命中：
// false 表示已被队首移交执行或记录不存在，调用方不得据此改本地状态。
export async function withdrawQueued(id: string): Promise<boolean> {
  if (!hasBackend()) return false
  return Boolean(await app.CancelQueued(id))
}

// listTaskHistory 任务账本（跨重启的历史任务记录）：只取带 taskId 的行，
// 含终态、耗时、失败原因与日志原文（operations 表由任务引擎在终态写入）。
export async function listTaskHistory(limit = 80): Promise<Operation[]> {
  const rows = await listOperations(limit)
  return (rows ?? []).filter((r) => !!r.taskId)
}
