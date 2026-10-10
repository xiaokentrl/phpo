// taskLabel：任务名的唯一渲染出口——有消息码就按当前语言现译（换语言即换名），
// 没有码（历史账本行、demo 通道）回落后端原样文本。抽屉标题条/队列行与托盘菜单共用，
// 不得再各写一份：托盘那处曾直接渲染 `label`，中文 fallback 在英文界面原样漏出（真机取证）。
// 走 msgText 而非裸 t()：语言包缺这条键时回退中文原文显示（dev 下告警一次），
// 绝不把「task.xxx」键名直接亮给用户——裸 t() 的缺口由 Go 侧用例锁死，这里再兜一层。
import { msgText } from '@/utils/msgText'

export function taskLabel(r: { label: string; labelCode?: string; labelParams?: Record<string, string> }): string {
  if (r.labelCode) return msgText(r.label, r.labelCode, r.labelParams)
  return r.label
}
