// PHP 扩展前端 API（T601）：应用（启停/增删）的薄封装。
// 硬红线 4/5：真实链路只发起后端调用（后端走三段式：编译→固化→重建→重载），状态由 state:changed 回流；纯 Vite demo 无宿主时回落本地 mock。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'

// applyExtensions 提交最终启用集；后端 diff 后跑任务重建镜像（已启用列表由快照落到 store，勿在此读回）
export async function applyExtensions(version: string, enabled: string[]): Promise<void> {
  if (!hasBackend()) return
  await app.ExtApply(version, enabled)
}
