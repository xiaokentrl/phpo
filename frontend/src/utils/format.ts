// 通用格式化：cmpVer / siteUrl / suggestPortFor（原型 1083–1156 行直译）
import type { Site } from '@/types'

// cmpVer：版本号降序（原型语义：数字大者在前）
export function cmpVer(a: string, b: string): number {
  const pa = String(a).split('.').map(Number)
  const pb = String(b).split('.').map(Number)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] || 0
    const y = pb[i] || 0
    if (x !== y) return y - x
  }
  return 0
}

export function siteUrl(site: Site): string {
  if (!site || !site.domain) return ''
  const port = site.port
  if (port && port !== 80 && port !== 443) return `http://${site.domain}:${port}`
  return `http://${site.domain}`
}

export function suggestPortFor(kind: string, version: string): number {
  if (kind === 'mysql') {
    const digits = String(version).replace(/\./g, '').slice(0, 2)
    const n = parseInt(digits, 10)
    return isNaN(n) ? 3380 : 3300 + n
  }
  if (kind === 'pgsql') {
    const major = parseInt(String(version).split('.')[0], 10)
    return isNaN(major) ? 5432 : 5400 + major
  }
  if (kind === 'redis') return 6379
  if (kind === 'nginx') return 80
  return 0
}

export function needsPort(kind: string): boolean {
  return ['mysql', 'pgsql', 'redis', 'nginx'].includes(kind)
}

export function needsPassword(kind: string): boolean {
  return ['mysql', 'pgsql', 'redis'].includes(kind)
}
