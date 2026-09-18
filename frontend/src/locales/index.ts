// 语言包聚合：键名为冻结契约（点号扁平键，勿手改）
import zhCN from './zh-CN'
import enUS from './en-US'

export type Locale = 'zh-CN' | 'en-US'

export const messages: Record<Locale, Record<string, string>> = {
  'zh-CN': zhCN,
  'en-US': enUS,
}
