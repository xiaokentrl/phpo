// taskLabel：任务名的唯一渲染出口——有消息码就按当前语言现译（换语言即换名），
// 没有码（历史账本行、demo 通道）回落后端原样文本。抽屉标题条/队列行与托盘菜单共用，
// 不得再各写一份：托盘那处曾直接渲染 `label`，中文 fallback 在英文界面原样漏出（真机取证）。
import { t } from '@/composables/useI18n'

export function taskLabel(r: { label: string; labelCode?: string; labelParams?: Record<string, string> }): string {
  if (r.labelCode) return t(r.labelCode, r.labelParams || {})
  return r.label
}
