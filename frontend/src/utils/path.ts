// 路径派生：derivePaths / verRoot / hostToContainer / containerToHost（原型 475–531 行直译）
import type { Env } from '@/types'

export const DEFAULT_HOME = '~/phpo'
export const DEFAULT_WWW = '~/www'
export const WWW_CONTAINER = '/var/www'
export const SITES_CONTAINER = '/etc/nginx/sites'

export function derivePaths(home?: string, wwwRoot?: string): Env {
  const h = String(home || DEFAULT_HOME).replace(/\/+$/, '')
  const w = String(wwwRoot || DEFAULT_WWW).replace(/\/+$/, '')
  return {
    PHPO_HOME: h,
    WWW_ROOT: w,
    PHP_ROOT: `${h}/php`,
    NGINX_ROOT: `${h}/nginx`,
    NGINX_SITES_ROOT: `${h}/nginx/sites`,
    MYSQL_ROOT: `${h}/mysql`,
    PGSQL_ROOT: `${h}/pgsql`,
    REDIS_ROOT: `${h}/redis`,
    BACKUP_ROOT: `${h}/backups`,
    OFFLINE_ROOT: `${h}/offline`,
  }
}

export function verRoot(env: Env, kind: string, version: string): string {
  return `${env[`${kind.toUpperCase()}_ROOT`]}/${version}`
}

export function hostToContainer(env: Env, p: string): string {
  if (!p) return p
  const w = env.WWW_ROOT
  if (w && (p === w || p.startsWith(w + '/'))) return WWW_CONTAINER + p.slice(w.length)
  const s = env.NGINX_SITES_ROOT
  if (s && (p === s || p.startsWith(s + '/'))) return SITES_CONTAINER + p.slice(s.length)
  return p
}

export function containerToHost(env: Env, p: string): string {
  if (!p) return p
  const w = env.WWW_ROOT
  if (w && (p === WWW_CONTAINER || p.startsWith(WWW_CONTAINER + '/'))) return w + p.slice(WWW_CONTAINER.length)
  const s = env.NGINX_SITES_ROOT
  if (s && (p === SITES_CONTAINER || p.startsWith(SITES_CONTAINER + '/'))) return s + p.slice(SITES_CONTAINER.length)
  return p
}
