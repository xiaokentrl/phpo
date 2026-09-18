// 通用字符串/密码/剪贴板工具，逐字迁移自原型 esc / DEFAULT_PASSWORD / genPassword / copyText
export const DEFAULT_PASSWORD = '123456'

// HTML 转义：仅用于极少数需渲染富文本的场景；Vue 模板默认已转义，一般无需调用
export function esc(s: unknown): string {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] as string))
}

// ★ FIX（总纲 #2）：默认值 123456；此为「重新生成」按钮的随机来源
export function genPassword(): string {
  const arr = new Uint8Array(8)
  crypto.getRandomValues(arr)
  return [...arr].map((b) => b.toString(16).padStart(2, '0')).join('')
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    let ok = false
    try {
      ok = document.execCommand('copy')
    } catch {
      /* ignore */
    }
    ta.remove()
    return ok
  }
}
