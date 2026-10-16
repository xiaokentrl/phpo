// 语言包聚合：键名为冻结契约（点号扁平键，勿手改）
import zhCN from './zh-CN'
import zhTW from './zh-TW'
import enUS from './en-US'

export type Locale = 'zh-CN' | 'zh-TW' | 'en-US'

export const messages: Record<Locale, Record<string, string>> = {
  'zh-CN': zhCN,
  'zh-TW': zhTW,
  'en-US': enUS,
}
