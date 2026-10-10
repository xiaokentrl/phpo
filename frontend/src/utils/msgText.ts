// 消息码 → 界面文本（v2.9.16 多语言 Phase 2/3）
//
// 这是干什么的：后端每一行日志都带两样东西——中文原文，和一个「这句是什么消息」的记号（消息码 + 参数）。
// 这里负责把记号翻成用户当前选的语言；认不出记号（新句式还没登记进表、语言包缺这条键，或这一行本来就是
// 容器输出、路径、命令这类数据）时照直显示中文原文，绝不显示成键名，也绝不空着。
//
// 参数的值以 @ 开头时，表示那一段本身又是一条消息（步骤名与任务标签就是这么嵌进来的），写法：
//   @消息码?参数=值&参数2=值2|中文原文
// 值用 encodeURIComponent 过；竖线后面那份中文原文就是这一段的回退显示。
import { t, te } from '@/composables/useI18n'

export type MsgParams = Record<string, string>

// safeDecode：后端只对参数值做了 encodeURIComponent，中文原文那一段是原样带的；
// 万一里面出现裸的 %（路径里合法）解码会抛，抛了就当没编码，原样用——不得因为解码失败而让整行日志空白。
function safeDecode(v: string): string {
  try {
    return decodeURIComponent(v)
  } catch {
    return v
  }
}

// reportMissing：后端给了消息码、当前语言却没有这条文案时，界面退回原文显示，
// 同时开发构建下报一次——「登记漏了」要当场看得见，而不是等用户发现某行不换语言（§5.15）。
// 只报「有码但没文案」；码缺席的那些行本来就是数据（容器输出、路径、argv），不该报。
function reportMissing(code: string): void {
  if (import.meta.env.DEV) console.warn(`[i18n] 消息码缺文案，退回原文显示：${code}`)
}

function nested(ref: string): string {
  const bar = ref.indexOf('|')
  const spec = bar < 0 ? ref : ref.slice(0, bar)
  const fallback = bar < 0 ? '' : safeDecode(ref.slice(bar + 1))
  const cut = spec.indexOf('?')
  const code = cut < 0 ? spec : spec.slice(0, cut)
  if (!te(code)) {
    reportMissing(code)
    return fallback
  }
  const params: MsgParams = {}
  for (const kv of (cut < 0 ? '' : spec.slice(cut + 1)).split('&')) {
    const i = kv.indexOf('=')
    if (i > 0) params[kv.slice(0, i)] = safeDecode(kv.slice(i + 1))
  }
  const out = t(code, params)
  return out === code ? fallback : out
}

function resolve(params?: MsgParams): MsgParams {
  const out: MsgParams = {}
  for (const [k, v] of Object.entries(params ?? {})) out[k] = v.startsWith('@') ? nested(v.slice(1)) : v
  return out
}

// msgText：按当前语言渲染一行文本。码缺席、或这条语言没有对应文案 → 回落原文。
export function msgText(text: string, code?: string, params?: MsgParams): string {
  if (!code) return text
  if (!te(code)) {
    reportMissing(code)
    return text
  }
  return t(code, resolve(params))
}
