// appState：状态仓，形状对齐原型 DEFAULT_STATE（1209–1250 行）与后端 model.Snapshot
// 硬红线 4：本仓只由后端 state:changed / service:changed 事件落地（见 composables/useStateSync.ts），无本地乐观更新。
import { defineStore } from 'pinia'
import { computed, reactive } from 'vue'
import type { Backup, DockerStatus, Env, OfflineTree, ServiceKind, Site, StateSnapshot, TaskBoard, TrayPrefs } from '@/types'
import { derivePaths } from '@/utils/path'
import { hasBackend } from '@/api/site'

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
  // 目录就绪标志（首启硬门禁）：真实宿主默认未就绪，待启动权威快照（api/state bootstrap）按 DB 落地；
  // 主目录 PHPO_HOME 与网站目录 WWW_ROOT 双就绪才放行写操作（preflight NEEDS_HOME 同源）。
  // 无宿主（纯 Vite demo）默认双就绪，保持 M1 占位数据可直接打开各模态验证。
  const dirReady = reactive<{ PHPO_HOME: boolean; WWW_ROOT: boolean }>({ PHPO_HOME: !hasBackend(), WWW_ROOT: !hasBackend() })
  // homeReady：两道目录先决条件是否都满足（App 启动门禁 / lazy gate / preflight 统一判定源）
  const homeReady = computed(() => dirReady.PHPO_HOME && dirReady.WWW_ROOT)

  // docker：Docker 可用性运行时状态（首启/轮询探测，硬红线 7）。非持久、不来自 Snapshot/事件，
  // 由 useDockerPreflight 经 setDocker 落地；checked=false 表示尚未探测（不拦截）。
  const docker = reactive<DockerStatus & { checked: boolean }>({ status: 'unknown', canStart: false, warning: false, checked: false })

  // tasks：任务队列详情（运行中一项 + 其后 FIFO 排队项），来自权威快照的 tasks 字段。
  // 排队态由「所在分区」表达，不新增第 5 个任务状态（§0.3）；日志明细在 taskStore。
  const tasks = reactive<TaskBoard>({ running: null, pending: [] })

  // applyTaskBoard：整体替换队列（硬红线 4：只由快照落地，前端不自造排队项）。
  // 快照缺省（老数据/mock 未带 tasks）时按空队列处理，避免残留脏队列。
  function applyTaskBoard(b?: TaskBoard | null): void {
    tasks.running = b?.running ?? null
    tasks.pending.splice(0, tasks.pending.length, ...(b?.pending ?? []))
  }

  function setDocker(s: DockerStatus): void {
    Object.assign(docker, s, { checked: true })
  }

  function isServiceRunning(kind: ServiceKind, version: string): boolean {
    return !stopped[kind].includes(version)
  }

  // applySnapshot：后端 state:changed 全量快照落地。运行态由 running 反向推导为 stopped（§6）。
  function applySnapshot(s: StateSnapshot): void {
    for (const kind of Object.keys(installed) as ServiceKind[]) {
      installed[kind].splice(0, installed[kind].length, ...(s.installed[kind] ?? []))
    }
    for (const kind of Object.keys(stopped) as ServiceKind[]) {
      const running = new Set(s.running[kind] ?? [])
      stopped[kind].splice(0, stopped[kind].length, ...installed[kind].filter((v) => !running.has(v)))
    }
    sites.splice(0, sites.length, ...s.sites)
    for (const k of Object.keys(env)) delete env[k]
    Object.assign(env, s.env)
    for (const k of Object.keys(phpExtensions)) delete phpExtensions[k]
    for (const [k, v] of Object.entries(s.phpExtensions)) phpExtensions[k] = [...v]
    Object.assign(dirReady, s.dirReady)
    applyTaskBoard(s.tasks)
  }

  // setServiceRunning：后端 service:changed 单服务增量落地。
  function setServiceRunning(kind: ServiceKind, version: string, running: boolean): void {
    const arr = stopped[kind]
    const idx = arr.indexOf(version)
    if (running && idx >= 0) arr.splice(idx, 1)
    else if (!running && idx < 0) arr.push(version)
  }

  // setBackups：备份列表不在 Snapshot 内，由后端 BackupList 单独权威拉取后整体替换（硬红线 4，无本地乐观更新）。
  function setBackups(list: Backup[]): void {
    backups.splice(0, backups.length, ...list)
  }

  const phpVersions = computed(() => installed.php)

  return { installed, stopped, sites, backups, offline, phpExtensions, env, tray, configs, dirReady, homeReady, docker, tasks, isServiceRunning, applySnapshot, applyTaskBoard, setServiceRunning, setBackups, setDocker, phpVersions }
})
