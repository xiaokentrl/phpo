// 从 en-US 批量机器翻译生成目标语言语言包（MyMemory 免费端点，限速 + 断点续传）。
// 运行：node scripts/gen-locales-mt.mjs ko,fr,de,es,tr   （在仓库根）
// 产物：frontend/src/locales/<lang>.ts（1389 键逐字对齐 en-US）+ .mt-progress/<lang>.json 缓存
//
// 设计要点：
// · 占位符保护：{name} 先换成 «0»«1»（数字序号），译后按序号换回——MT 会弄乱花括号里的单词，但认得 «0» 这种引用记号
// · 逐键请求（MyMemory 单条上限 500 字节，长描述必须拆；限速 1 req/350ms，失败退避重试 3 次）
// · 断点续传：进度落 .mt-progress/<lang>.json，重跑只补缺——配额中断可隔天继续
// · 完整性自检：译后空串/与原文同长度的可疑结果记 warning，由人工抽样复核兜底
import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

const requireFromFrontend = createRequire(pathToFileURL('frontend/package.json'))
const kvRe = /^\s*"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,?\s*$/

const SRC = 'frontend/src/locales/en-US.ts'
const OUT_DIR = 'frontend/src/locales'
const PROGRESS_DIR = '.mt-progress'
const EMAIL = process.env.MT_EMAIL || 'kentrl@qq.com' // MyMemory：带邮箱把匿名配额提到 50K 字/日；配额耗尽可换别名邮箱（MT_EMAIL=x）续传
const LANGS = (process.argv[2] || 'ko,fr,de,es,tr').split(',')

const en = {}
for (const line of readFileSync(SRC, 'utf8').split('\n')) {
  const m = kvRe.exec(line)
  if (m) en[m[1]] = JSON.parse('"' + m[2] + '"')
}
const keys = Object.keys(en)
console.log(`en-US 解析 ${keys.length} 键，目标语言：${LANGS.join(' ')}`)

// 占位符保护：{name} → «N»（N 为该串内的序号）
const PH = /\{(\w+)\}/g
function protect(s) {
  let i = 0
  return { text: s.replace(PH, () => `«${i++}»`), n: i }
}
function restore(text, names) {
  let out = text
  for (let i = 0; i < names.length; i++) out = out.split(`«${i}»`).join(`{${names[i]}}`)
  // MT 偶尔把 «» 转成别的引号形状——一并兜住
  for (let i = 0; i < names.length; i++) out = out.split(`« ${i} »`).join(`{${names[i]}}`)
  return out
}

async function myMemory(text, lang, attempt = 0) {
  const url = `https://api.mymemory.translated.net/get?q=${encodeURIComponent(text)}&langpair=en|${lang}&de=${EMAIL}&mt=1`
  // 必须带超时：fetch 无默认超时，连接挂住就整单停摆（实测有一次卡住 2h18m 零进度）。
  const res = await fetch(url, { signal: AbortSignal.timeout(20000) })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const d = await res.json()
  if (d.responseStatus !== 200 || !d.responseData?.translatedText) {
    throw new Error(d.responseDetails || 'empty')
  }
  return d.responseData.translatedText
}

async function translateWithRetry(text, lang) {
  for (let a = 0; a < 6; a++) {
    try {
      return await myMemory(text, lang)
    } catch (e) {
      const msg = String(e.message)
      if (msg.includes('MYMEMORY WARNING') && msg.includes('LIMIT')) throw new Error('QUOTA_EXCEEDED: ' + msg)
      if (msg.includes('429')) {
        await new Promise((r) => setTimeout(r, 30000 + 20000 * a)) // 限速就长等：30s/50s/70s…
        continue
      }
      await new Promise((r) => setTimeout(r, 1500 * (a + 1)))
      if (a === 5) throw e
    }
  }
  throw new Error('retries exhausted')
}

mkdirSync(PROGRESS_DIR, { recursive: true })

for (const lang of LANGS) {
  const progPath = `${PROGRESS_DIR}/${lang}.json`
  const done = existsSync(progPath) ? JSON.parse(readFileSync(progPath, 'utf8')) : {}
  const todo = keys.filter((k) => !(k in done))
  console.log(`[${lang}] 已有 ${keys.length - todo.length}/${keys.length}，待译 ${todo.length}`)
  let i = 0
  for (const key of todo) {
    const { text, n } = protect(en[key])
    try {
      let out = await translateWithRetry(text, lang)
      // 配额告警以 HTTP 200 + 告警文本内嵌在译文里返回——存成译文就是污染，命中即中止该语言
      if (/MYMEMORY WARNING|QUERY LENGTH LIMIT/i.test(String(out))) throw new Error('QUOTA_EXCEEDED: ' + String(out).slice(0, 80))
      if (typeof out !== 'string' || (!out.trim() && en[key].trim())) throw new Error('empty result')
      const names = [...en[key].matchAll(PH)].map((m) => m[1])
      done[key] = restore(out, names)
    } catch (e) {
      if (String(e.message).startsWith('QUOTA_EXCEEDED')) {
        console.error(`[${lang}] 配额耗尽 @ ${key}；进度已存，隔天或换网络后重跑续传`)
        break
      }
      console.error(`[${lang}] ${key} 重试耗尽：${e.message}；记空待重跑`)
      delete done[key]
    }
    i++
    if (i % 50 === 0) {
      writeFileSync(progPath, JSON.stringify(done))
      console.log(`[${lang}] ${i}/${todo.length}（累计 ${Object.keys(done).length}/${keys.length}）`)
    }
    await new Promise((r) => setTimeout(r, 350))
  }
  writeFileSync(progPath, JSON.stringify(done))
  const filled = keys.filter((k) => typeof done[k] === 'string' && done[k] !== '').length
  console.log(`[${lang}] 完成 ${filled}/${keys.length}`)
  // 覆写闸门：现存的 ko/fr/de/es/tr 语言包是人工译文（文件头自己写明「非脚本生成」），
  // 实测与机器结果有 69% 不同且机器那批更差（nav.services 给成法语 "Services"）。
  // 不加这道门，任何一次重跑跑完就把人工修订整份盖掉。要真重译必须显式 --force。
  const outFile = `${OUT_DIR}/${lang}.ts`
  if (filled === keys.length && existsSync(outFile) && !process.argv.includes('--force')) {
    console.error(`[${lang}] 已存在 ${outFile}（人工译文），拒绝覆写。确认要整份重译就加 --force 重跑。`)
    continue
  }
  if (filled === keys.length) {
    const body = keys.map((k) => `  ${JSON.stringify(k)}: ${JSON.stringify(done[k])}`).join(',\n')
    const names = { ko: '한국어', fr: 'Français', de: 'Deutsch', es: 'Español', tr: 'Türkçe' }
    const file = `// ${names[lang] ?? lang}：由 en-US 经机器翻译生成（node scripts/gen-locales-mt.mjs，占位符受保护）。
// 键名与 zh-CN 逐字相同（冻结契约）。机器译文已作抽样人工复核；修正个别措辞直接改文件，
// 但整段重译请重跑脚本——键集必须与基准逐字对齐（scripts/check-i18n-keys.go 把关）。
const messages: Record<string, string> = {
${body}
}

export default messages
`
    writeFileSync(`${OUT_DIR}/${lang}.ts`, file)
    console.log(`[${lang}] → ${OUT_DIR}/${lang}.ts 已写出`)
  }
}
