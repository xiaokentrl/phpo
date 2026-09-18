// 从原型 SSOT 精确提取 MESSAGES → frontend/src/locales/{zh-CN,en-US}.ts
// 运行：node scripts/gen-locales.mjs
import { readFileSync, writeFileSync } from 'node:fs'

const SRC = '前端唯一界面来源.txt'
const OUT_DIR = 'frontend/src/locales'

const lines = readFileSync(SRC, 'utf8').split('\n')
const start = lines.findIndex((l) => l.startsWith('const MESSAGES = {'))
if (start < 0) throw new Error('未找到 MESSAGES 起点')
let end = -1
for (let i = start + 1; i < lines.length; i++) {
  if (lines[i] === '};') {
    end = i
    break
  }
}
if (end < 0) throw new Error('未找到 MESSAGES 终点行 `};`')

const snippet = lines.slice(start, end + 1).join('\n')
// 原型 MESSAGES 为纯字符串对象字面量；去掉声明前缀与尾部分号后转成可 eval 的表达式
const objText = snippet
  .replace(/^const MESSAGES =/, '')
  .replace(/;\s*$/, '')
// eslint-disable-next-line no-eval
const MESSAGES = eval('(' + objText + ')')

const zh = MESSAGES['zh-CN']
const en = MESSAGES['en-US']
if (!zh || !en) throw new Error('缺少 zh-CN 或 en-US 语言块')

const zhKeys = Object.keys(zh)
const enKeys = Object.keys(en)
const onlyZh = zhKeys.filter((k) => !(k in en))
const onlyEn = enKeys.filter((k) => !(k in zh))
if (onlyZh.length || onlyEn.length) {
  console.error('键集合不一致：')
  if (onlyZh.length) console.error('  仅 zh-CN:', onlyZh)
  if (onlyEn.length) console.error('  仅 en-US:', onlyEn)
  process.exit(1)
}

const dump = (name, dict) => {
  const body = Object.entries(dict)
    .map(([k, v]) => `  ${JSON.stringify(k)}: ${JSON.stringify(v)}`)
    .join(',\n')
  const file = `// 自动生成自原型 MESSAGES.${name}（node scripts/gen-locales.mjs）；键名为冻结契约，勿手改\nconst messages: Record<string, string> = {\n${body}\n}\n\nexport default messages\n`
  writeFileSync(`${OUT_DIR}/${name}.ts`, file)
}

dump('zh-CN', zh)
dump('en-US', en)
console.log(`✓ 已生成 zh-CN.ts / en-US.ts，各 ${zhKeys.length} 键，键集合相等`)
