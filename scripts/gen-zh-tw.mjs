// 从 zh-CN 生成繁体中文语言包 zh-TW.ts（OpenCC s2twp：简繁字形 + 台湾用语短语转换，
// 网络→網路、软件→軟體、内存→記憶體等自动到位）。zh-CN 是简体的唯一来源，zh-TW 永远派生自它——
// 两侧手工各改一份必漂移，所以繁体不出手写：改了简体就重跑本脚本。
// 运行：node scripts/gen-zh-tw.mjs（在仓库根；需 frontend devDependency opencc-js）
import { readFileSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

// opencc-js 装在 frontend 的 node_modules（pnpm workspace），仓库根解析不到——
// 从 frontend/package.json 建一个 require 来解析，脚本放哪跑都行。
const requireFromFrontend = createRequire(pathToFileURL('frontend/package.json'))
const OpenCC = requireFromFrontend('opencc-js')

const SRC = 'frontend/src/locales/zh-CN.ts'
const OUT = 'frontend/src/locales/zh-TW.ts'

// parse：zh-CN.ts 是扁平 "key": "value" 字面量（生成物、无嵌套），逐行正则提取即可，
// 不 eval——值里可能有任意文本，正则口径与 scripts/check-i18n-keys.go 一致。
const kvRe = /^\s*"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,?\s*$/
const zh = {}
for (const line of readFileSync(SRC, 'utf8').split('\n')) {
  const m = kvRe.exec(line)
  if (m) zh[m[1]] = m[2]
}
if (Object.keys(zh).length === 0) throw new Error('zh-CN.ts 未解析出任何键值对')

// s2twp：简→繁（台湾）+ 用语转换。Converter 保持实例一次建好，1389 键一次跑完。
const converter = OpenCC.Converter({ from: 'cn', to: 'twp' })
const body = Object.entries(zh)
  .map(([k, v]) => `  ${JSON.stringify(k)}: ${JSON.stringify(converter(v))}`)
  .join(',\n')

const file = `// 繁体中文（台湾用语）：由 zh-CN 经 OpenCC s2twp 自动生成（node scripts/gen-zh-tw.mjs）。
// 键名与 zh-CN 逐字相同（冻结契约）；值勿手改——改简体原文后重跑脚本，两侧才不会漂移。
const messages: Record<string, string> = {
${body}
}

export default messages
`
writeFileSync(OUT, file)
console.log(`zh-TW.ts 已生成：${Object.keys(zh).length} 键`)
