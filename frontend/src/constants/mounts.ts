// MOUNTS 挂载表：1:1 迁移原型 544–575；resolveMounts 解析宿主路径（577–590）
import type { Env, ServiceKind } from '@/types'

export interface MountDef {
  global?: boolean
  key?: string
  sub?: string
  from?: string
  to: string
  mode: string
  label: string
}
export interface Mount extends MountDef {
  host: string
}

export const MOUNTS: Record<ServiceKind, MountDef[]> = {
  php: [
    { global: true, key: 'WWW_ROOT', to: '/var/www', mode: 'rw', label: 'site sources' },
    { sub: 'conf', from: 'php.ini', to: '/usr/local/etc/php/conf.d/zz-phpo.ini', mode: 'ro', label: 'php.ini' },
    { sub: 'conf', from: 'php-fpm.conf', to: '/usr/local/etc/php-fpm.d/zz-phpo.conf', mode: 'ro', label: 'php-fpm pool' },
    { sub: 'logs', to: '/var/log/php-fpm', mode: 'rw', label: 'FPM logs' },
  ],
  nginx: [
    { global: true, key: 'WWW_ROOT', to: '/var/www', mode: 'ro', label: 'static files' },
    { global: true, key: 'NGINX_SITES_ROOT', to: '/etc/nginx/sites', mode: 'ro', label: 'vhosts' },
    { sub: 'conf', from: 'nginx.conf', to: '/etc/nginx/nginx.conf', mode: 'ro', label: 'nginx.conf' },
    { sub: 'logs', to: '/var/log/nginx', mode: 'rw', label: 'logs' },
  ],
  mysql: [
    { sub: 'conf', from: 'my.cnf', to: '/etc/mysql/conf.d/zz-phpo.cnf', mode: 'ro', label: 'my.cnf' },
    { sub: 'data', to: '/var/lib/mysql', mode: 'rw', label: 'data' },
    { sub: 'logs', to: '/var/log/mysql', mode: 'rw', label: 'logs' },
    { sub: 'initdb', to: '/docker-entrypoint-initdb.d', mode: 'ro', label: 'initdb' },
  ],
  pgsql: [
    { sub: 'conf', from: 'postgresql.conf', to: '/etc/postgresql/postgresql.conf', mode: 'ro', label: 'postgresql.conf' },
    { sub: 'conf', from: 'pg_hba.conf', to: '/etc/postgresql/pg_hba.conf', mode: 'ro', label: 'pg_hba.conf' },
    { sub: 'data', to: '/var/lib/postgresql/data', mode: 'rw', label: 'data' },
    { sub: 'logs', to: '/var/log/postgresql', mode: 'rw', label: 'logs' },
    { sub: 'initdb', to: '/docker-entrypoint-initdb.d', mode: 'ro', label: 'initdb' },
  ],
  redis: [
    { sub: 'conf', from: 'redis.conf', to: '/usr/local/etc/redis/redis.conf', mode: 'ro', label: 'redis.conf' },
    { sub: 'data', to: '/data', mode: 'rw', label: 'data' },
    { sub: 'logs', to: '/var/log/redis', mode: 'rw', label: 'logs' },
  ],
}

export function resolveMounts(env: Env, kind: ServiceKind, version: string): Mount[] {
  const list = MOUNTS[kind] || []
  const root = env[`${kind.toUpperCase()}_ROOT`] || ''
  return list.map((m) => {
    let host = ''
    if (m.global) host = env[m.key as string] || ''
    else if (m.sub) {
      host = `${root}/${version}/${m.sub}`
      if (m.from) host += `/${m.from}`
    }
    return { ...m, host }
  })
}
