// Toast 原语：模块级响应式队列，忠实迁移原型 toast(msg, kind, ttl)
// 三种 kind：info / ok / err（对齐 base.css .toast.ok/.err/.info）
import { reactive } from 'vue'

export type ToastKind = 'info' | 'ok' | 'err'
export interface ToastItem {
  id: number
  msg: string
  kind: ToastKind
}

const toasts = reactive<ToastItem[]>([])
let seq = 0

export function toast(msg: string, kind: ToastKind = 'info', ttl = 3200): void {
  const id = ++seq
  toasts.push({ id, msg, kind })
  setTimeout(() => {
    const i = toasts.findIndex((x) => x.id === id)
    if (i >= 0) toasts.splice(i, 1)
  }, ttl)
}

export function useToast() {
  return { toasts, toast }
}
