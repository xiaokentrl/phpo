// 轻量 i18n：扁平点号键 + 响应式 locale（语言切换即时重渲染）
// 说明：原型 MESSAGES 存在 settings.layout 既是叶子又是父级的键，
// 无法用 vue-i18n 的点号路径嵌套解析，故用扁平字典直查。
import { ref, type Ref } from 'vue'
import { messages, type Locale } from '@/locales'

const LS_KEY = 'phpo-locale'

function initialLocale(): Locale {
  try {
    const v = localStorage.getItem(LS_KEY)
    return v === 'en-US' ? 'en-US' : 'zh-CN'
  } catch {
    return 'zh-CN'
  }
}

const locale: Ref<Locale> = ref(initialLocale())

export function setLocale(l: Locale): void {
  locale.value = l
  try {
    localStorage.setItem(LS_KEY, l)
  } catch {
    /* 隐私模式等忽略 */
  }
}

export type TParams = Record<string, string | number>

// t：按键直查，缺键回落到键名本身；支持 {token} 占位插值
export function t(key: string, params?: TParams): string {
  let s = messages[locale.value][key] ?? key
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      s = s.split('{' + k + '}').join(String(v))
    }
  }
  return s
}

export function useI18n() {
  return { locale, t, setLocale }
}
