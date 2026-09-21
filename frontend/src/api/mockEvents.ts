// mockEvents：M1 事件后端尚未真发射时的开发期驱动（仅 DEV 生效，构建产物不含定时器）
// 用途：向 api/events.ts 的进程内总线按 §5.6 全量发射 17 个事件，验证前端「只订阅、事件落地」闭环。
import { ALL_EVENTS, EVENT, emitLocal } from '@/api/events'
import type { StateSnapshot } from '@/types'

// 每事件一份代表性载荷；state:changed 携带与初始 mock 不同的快照，用以证明落地来自事件而非本地乐观更新。
const MOCK_SNAPSHOT: StateSnapshot = {
  installed: { php: ['8.5', '8.4', '8.3'], mysql: ['8.4'], pgsql: ['17'], redis: ['8'], nginx: ['alpine'] },
  running: { php: ['8.5', '8.4'], mysql: ['8.4'], pgsql: [], redis: ['8'], nginx: ['alpine'] },
  sites: [
    { domain: 'demo.test', port: 80, php: '8.5', root: '~/www/demo.test', hosts: true, health: 'up', rewrite: 'laravel' },
    { domain: 'shop.test', port: 8080, php: '8.4', root: '~/www/shop.test', hosts: false, health: 'warn', rewrite: 'wordpress' },
  ],
  env: {
    PHPO_HOME: '~/phpo', WWW_ROOT: '~/www', NGINX_SITES_ROOT: '~/phpo/nginx/sites', OFFLINE_ROOT: '~/phpo/offline',
    BACKUP_ROOT: '~/phpo/backups', NGINX_PORT: '80', NGINX_VERSION: 'alpine',
  },
  phpExtensions: { '8.5': ['gd', 'redis', 'opcache'], '8.4': ['gd', 'redis', 'pdo_mysql'] },
  dirReady: { PHPO_HOME: true, WWW_ROOT: true },
  tasks: { running: { id: 't-1', label: 'mock · php 8.5 安装', type: 'install', step: 2, total: 6, startedAt: '2026-09-21T10:00:00Z' }, pending: [] },
}

const MOCK_SAMPLES: Record<string, unknown> = {
  [EVENT.StateChanged]: { snapshot: MOCK_SNAPSHOT },
  [EVENT.ServiceChanged]: { kind: 'pgsql', version: '17', running: true },
  [EVENT.TaskLog]: { id: 't-1', level: 'ok', text: '  ✓ mock log line' },
  [EVENT.TaskProgress]: { id: 't-1', step: 3, total: 6 },
  [EVENT.TaskDone]: { id: 't-1', status: 'success', duration: 2.4 },
  [EVENT.UpdateAvailable]: { version: '9.9.9', changelog: 'mock release', size: 42_000_000 },
  [EVENT.UpdateProgress]: { stage: 'downloading', percent: 55, speed: 1_200_000 },
  [EVENT.UpdateDone]: { status: 'success', version: '9.9.9' },
  [EVENT.DockerCleanup]: { stage: 'prune', resource: 'phpo-php-8.3', action: 'removed' },
  [EVENT.DockerOrphanFound]: { resources: [{ name: 'phpo-redis-7', kind: 'container' }] },
  [EVENT.DockerStateDrift]: { expected: { running: ['8.4'] }, actual: { running: [] } },
  [EVENT.CacheHit]: { kind: 'php', version: '8.4', source: 'offline', size: 450_000_000 },
  [EVENT.CacheMiss]: { kind: 'php', version: '8.5', action: 'pull' },
  [EVENT.CachePromote]: { kind: 'php', version: '8.5', entries: [{ name: 'image.tar', sha256: 'def456' }] },
  [EVENT.CacheCorrupted]: { kind: 'mysql', version: '8.4', entry: { name: 'image.tar', sha256: 'mismatch' } },
  [EVENT.CacheCleanup]: { mode: 'standard', freed_bytes: 128_000_000 },
  [EVENT.CacheTempdirCleared]: { path: '~/phpo/php/8.5/ext', reason: 'compile_done' },
}

// devEmitAll：按协议顺序把 17 个事件各投递一次到进程内总线。
export function devEmitAll(): number {
  for (const name of ALL_EVENTS) emitLocal(name, MOCK_SAMPLES[name])
  return ALL_EVENTS.length
}

if (import.meta.env.DEV) {
  (globalThis as Record<string, unknown>).__phpoMock = { emitAll: devEmitAll, emit: emitLocal, events: ALL_EVENTS }
}
