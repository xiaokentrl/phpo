// backendMsg：后端直接甩给界面那句中文错误，按当前界面语言说同一句话。
//
// 这是干什么的：后端到前端的错误只剩一句字符串（preflight 的最终裁决会把几条错误用换行拼成
// 一句 Go error 再返回，任务失败也是 error 的原文）。这句话里塞不下消息码，也不该为此去改 DTO
// （改了就得重新生成绑定）。所以按「形状」认：认得出就按当前语言渲染，认不出（新句子没登记、
// 或这一行本来就是路径／容器输出这类数据）就照直显示后端原文，绝不空着、绝不显示成键名。
//
// 形状表与匹配逻辑在 ./errShapes；参数值里嵌着的已知句子（任务框架日志 reason 里那句错误原文）
// 由 msgText.resolve 现译；任务标签的反查（错误串里的「重建 phpo-mysql-5.7」要跟语言走）由
// taskStore 经 registerParamResolver 注册进来。
import { msgText, type MsgParams } from './msgText'
import { matchShape, HAN } from './errShapes'

type ParamResolver = (code: string, params: MsgParams) => MsgParams

let paramResolver: ParamResolver | null = null

export function registerParamResolver(fn: ParamResolver): void {
  paramResolver = fn
}

// resolveParams：给反查钩子一个口子——查得到的参数换成已渲染的当前语言文本，查不到原样保留。
function resolveParams(code: string, params: MsgParams): MsgParams {
  if (!paramResolver) return params
  try {
    return { ...params, ...paramResolver(code, params) }
  } catch {
    return params
  }
}

function matchLine(line: string): string {
  if (!HAN.test(line)) return line
  const hit = matchShape(line)
  if (!hit) return line
  return msgText(line, hit.code, resolveParams(hit.code, hit.params))
}

// 后端那句中文 → 当前界面语言的那句话。没接后端的通道（demo）与本来就不含中文的句子原样返回。
export function backendMsg(raw: string): string {
  if (!raw || !HAN.test(raw)) return raw
  return raw
    .split('\n')
    .map((line) => {
      const hit = matchLine(line.trim())
      return hit === line.trim() ? line : hit
    })
    .join('\n')
}
