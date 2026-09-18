// appState：M1 阶段 mock 状态仓，形状对齐原型 DEFAULT_STATE（1209–1250 行）
// 硬红线 4：生产版由后端事件驱动更新；M1 仅用静态 mock 渲染。
import { defineStore } from 'pinia'
import { computed, reactive } from 'vue'
import type { Backup, Env, OfflineTree, ServiceKind, Site, TrayPrefs } from '@/types'
import { derivePaths } from '@/utils/path'

function defaultEnv(): Env {
  return {
    ...derivePaths('~/phpo', '~/www'),
    NGINX_PORT: '80',
    NGINX_VERSION: 'alpine',
    MYSQL_84_PORT: '3384',
    MYSQL_84_ROOT_PASSWORD: '123456',
    PGSQL_17_PORT: '5417',
    PGSQL_17_ROOT_PASSWORD: '123456',
    REDIS_8_PORT: '6379',
    REDIS_8_ROOT_PASSWORD: '123456',
  }
}

export const useAppState = defineStore('appState', () => {
  const installed = reactive<Record<ServiceKind, string[]>>({
    php: ['8.4', '8.3', '8.0'],
    mysql: ['8.4'],
    pgsql: ['17'],
    redis: ['8'],
    nginx: ['alpine'],
  })
  const stopped = reactive<Record<ServiceKind, string[]>>({
    php: [],
    mysql: [],
    pgsql: [],
    redis: [],
    nginx: [],
  })
  const sites = reactive<Site[]>([
    { domain: 'demo.test', port: 80, php: '8.4', root: '~/www/demo.test', hosts: true, health: 'up', rewrite: 'laravel' },
    { domain: 'blog.test', port: 80, php: '8.3', root: '~/www/blog.test', hosts: true, health: 'up', rewrite: 'thinkphp' },
    { domain: 'api.test', port: 8080, php: '8.4', root: '~/www/api.test', hosts: false, health: 'warn', rewrite: 'none' },
    { domain: 'legacy.test', port: 8000, php: '8.0', root: '~/www/legacy.test', hosts: true, health: 'down', rewrite: 'ci' },
  ])
  const backups = reactive<Backup[]>([
    { file: 'backup-20260913-093015.tar.gz', size: '48.2 MB', at: '2026-09-13 09:30', items: 5 },
    { file: 'backup-20260910-220845.tar.gz', size: '46.7 MB', at: '2026-09-10 22:08', items: 5 },
    { file: 'backup-20260905-141200.tar.gz', size: '41.1 MB', at: '2026-09-05 14:12', items: 4 },
  ])
  const offline = reactive<{ total: string; trees: OfflineTree[] }>({
    total: '1.42 GB',
    trees: [
      { svc: 'php', ver: '8.4', size: '420 MB', items: 138, verified: '2026-09-12 18:03' },
      { svc: 'php', ver: '8.0', size: '385 MB', items: 126, verified: '2026-09-08 22:15' },
      { svc: 'mysql', ver: '8.4', size: '212 MB', items: 1, verified: '2026-09-11 09:40' },
      { svc: 'pgsql', ver: '17', size: '180 MB', items: 1, verified: '2026-09-10 15:20' },
      { svc: 'redis', ver: '8', size: '68 MB', items: 1, verified: '2026-09-06 11:22' },
      { svc: 'nginx', ver: 'alpine', size: '52 MB', items: 1, verified: '2026-09-05 08:11' },
    ],
  })
  const phpExtensions = reactive<Record<string, string[]>>({
    '8.4': ['gd', 'redis', 'pdo_mysql', 'mysqli', 'pgsql', 'pdo_pgsql', 'zip', 'bcmath', 'intl', 'opcache', 'exif', 'soap', 'sockets', 'imagick', 'xdebug'],
    '8.3': ['gd', 'redis', 'pdo_mysql', 'mysqli', 'zip', 'bcmath', 'opcache', 'exif', 'sockets'],
    '8.0': ['gd', 'redis', 'pdo_mysql', 'mysqli', 'zip', 'bcmath', 'opcache'],
  })
  const env = reactive<Env>(defaultEnv())
  const tray = reactive<TrayPrefs>({ enabled: true, minimizeOnClose: true })
  // 用户改过的配置正文，键 `${kind}:${version}:${fileName}`（原型 state.configs）
  const configs = reactive<Record<string, string>>({})
  // PHPO_HOME 就绪标志：M1 默认已装机，使全部模态可直接打开验证
  const dirReady = reactive<{ PHPO_HOME: boolean }>({ PHPO_HOME: true })

  function isServiceRunning(kind: ServiceKind, version: string): boolean {
    return !stopped[kind].includes(version)
  }

  const phpVersions = computed(() => installed.php)

  return { installed, stopped, sites, backups, offline, phpExtensions, env, tray, configs, dirReady, isServiceRunning, phpVersions }
})
