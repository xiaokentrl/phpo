// 语言包聚合：键名为冻结契约（点号扁平键，勿手改）
import zhCN from './zh-CN'
import zhTW from './zh-TW'
import enUS from './en-US'
import ko from './ko'
import fr from './fr'
import de from './de'
import es from './es'
import tr from './tr'

export type Locale = 'zh-CN' | 'zh-TW' | 'en-US' | 'ko' | 'fr' | 'de' | 'es' | 'tr'

export const messages: Record<Locale, Record<string, string>> = {
  'zh-CN': zhCN,
  'zh-TW': zhTW,
  'en-US': enUS,
  ko,
  fr,
  de,
  es,
  tr,
}
