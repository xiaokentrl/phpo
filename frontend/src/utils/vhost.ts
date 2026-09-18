// vhost 纯函数层：逐字迁移自原型 defaultVhost / parseVhost / replaceListen / replacePhpUpstream
// 硬红线 1：PHP 上游精确 php-{version}-fpm:9000，保留版本号原样。
// 生产版权威在 internal/vhost/；此处仅作前端即时预览与站点初始内容渲染。
import type { Env, Site } from '@/types'
import { hostToContainer } from './path'
import { REWRITE_PRESETS } from '@/constants/rewrite'

export function defaultVhost(
  env: Env,
  domain: string,
  port: number | string,
  php: string,
  hostRoot: string,
  rewriteKey: string,
  customRule?: string,
): string {
  const rw = REWRITE_PRESETS[rewriteKey] || REWRITE_PRESETS.none
  const rule = typeof customRule === 'string' && customRule.trim() ? customRule : rw.rule
  const phpUpstream = `php-${php}-fpm`
  const ruleLines = rule
    .split('\n')
    .map((l) => '    ' + l)
    .join('\n')
  const containerRoot = hostToContainer(env, hostRoot)
  return `server {
    listen ${port};
    server_name ${domain};
    root ${containerRoot};
    index index.php index.html;
${ruleLines}
    location ~ \\.php$ {
        resolver 127.0.0.11 valid=10s ipv6=off;
        set $php_upstream ${phpUpstream}:9000;
        fastcgi_pass $php_upstream;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }
}`
}

export interface ParsedVhost {
  port: number | null
  root: string | null
}

export function parseVhost(content: string): ParsedVhost {
  const result: ParsedVhost = { port: null, root: null }
  if (!content) return result
  const text = String(content)
  const listenMatch = text.match(/^\s*listen\s+(\d+)/m)
  if (listenMatch) {
    const p = parseInt(listenMatch[1], 10)
    if (!isNaN(p) && p >= 1 && p <= 65535) result.port = p
  }
  const rootMatch = text.match(/^\s*root\s+([^;]+);/m)
  if (rootMatch) result.root = rootMatch[1].trim()
  return result
}

export function replaceListen(content: string, port: number | string): string {
  if (!content) return content
  return String(content).replace(/^(\s*listen\s+)\d+(\s[^;]*)?;/m, `$1${port}$2;`)
}

export function replacePhpUpstream(content: string, php: string): string {
  if (!content) return content
  return String(content).replace(/set\s+\$php_upstream\s+[^;]+;/, () => `set $php_upstream php-${php}-fpm:9000;`)
}

// computeVhost：从站点对象派生 vhost 初始内容（原型 VHosts.compute）
export function computeVhost(env: Env, site: Site): string {
  if (!site) return ''
  const s = site as Site & { rewriteRule?: string }
  return defaultVhost(env, site.domain, site.port, site.php, site.root, site.rewrite, s.rewriteRule)
}
