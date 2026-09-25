// PHP 扩展前端 API（T601）：应用（启停/增删）的薄封装 + 打开弹窗时的一次现查。
// 硬红线 4/5：真实链路只发起后端调用（后端走三段式：编译→固化→重建→重载），状态由 state:changed 回流；纯 Vite demo 无宿主时回落本地 mock。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'

// applyExtensions 提交最终启用集；后端 diff 后跑任务重建镜像（已启用列表由快照落到 store，勿在此读回）
export async function applyExtensions(version: string, enabled: string[]): Promise<void> {
  if (!hasBackend()) return
  await app.ExtApply(version, enabled)
}

// extStatus 打开「管理扩展」时让后端到容器里现查一次「此刻哪些扩展开着」。
// 后端在返回前已把实测结果写回权威库并推快照，所以这里只取两句说明：
// live = 这次查到的是容器里的实时值还是库里存的旧值（查不到时界面要写明，并禁止提交）；
// builtIn = 实测集里那几颗「已经开着但删不掉」的（静态编进 PHP 本体，没有 ini 可删）。
// 「开着哪些」本身只读权威快照 phpExtensions，不回抄这次响应（硬红线 4）。
export async function extStatus(version: string): Promise<{ live: boolean; builtIn: string[] }> {
  // 无宿主（纯 Vite demo）：没有容器可查，开关全部可编辑，免得本地回放被一颗禁用键卡死
  if (!hasBackend()) return { live: true, builtIn: [] }
  const r = await app.ExtStatus(version)
  return { live: !!r.live, builtIn: r.builtIn ?? [] }
}
