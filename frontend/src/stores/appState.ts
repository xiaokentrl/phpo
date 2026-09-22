// appState：状态仓，形状对齐原型 DEFAULT_STATE（1209–1250 行）与后端 model.Snapshot
// 硬红线 4：本仓只由后端 state:changed / service:changed 事件落地（见 composables/useStateSync.ts），无本地乐观更新。
import { defineStore } from 'pinia'
import { computed, reactive } from 'vue'
import type { Backup, DockerStatus, Env, OfflineTree, ServiceKind, Site, StateSnapshot, TaskBoard } from '@/types'
import { derivePaths, DEFAULT_HOME, DEFAULT_WWW } from '@/utils/path'
import { hasBackend } from '@/api/site'

// 后端发的键：{KIND}_{VER}_PORT / {KIND}_{VER}_PASSWORD / {KIND}_{VER}_DATA_DIR + OFFLINE_ROOT / BACKUP_ROOT
// （internal/config/configstore.go 的 EnvKey* 与 FlatEnv）。
// 从不发 NGINX_PORT / NGINX_VERSION / *_ROOT_PASSWORD 这类原型遗留键——真宿主下种它们，
// 等于往快照 env 里永久留下无人覆盖的假值（applySnapshot 只清后端真发过的键）。
function defaultEnv(): Env {
  const env: Env = { ...derivePaths(DEFAULT_HOME, DEFAULT_WWW) } as Env
  if (hasBackend()) return env // 真宿主：路径键仅是向导未落地前的兜底，端口/密码一律等权威快照
  Object.assign(env, {
    MYSQL_84_PORT: '3384',
    MYSQL_84_PASSWORD: '123456',
    PGSQL_17_PORT: '5417',
    PGSQL_17_PASSWORD: '123456',
    REDIS_8_PORT: '6379',
    REDIS_8_PASSWORD: '123456',
    NGINX_ALPINE_PORT: '80',
  })
  return env
}

// demo：无宿主（纯 Vite 起前端）时用原型占位数据直接打开各视图做 UI 验证；
// 真宿主一律从空开始，等 GetState / state:changed 落地（硬红线 4：前端不编造后端事实）。
// 宿主判据可能晚于 store 建立（window._wails 在 WindowLoadFinished 才注入），故 App.vue 确认宿主后
// 还要调 enterRealHost() 收掉这里可能已经铺下的占位数据。
export const useAppState = defineStore('appState', () => {
  const demo = !hasBackend()
  const installed = reactive<Record<ServiceKind, string[]>>(
    demo
      ? { php: ['8.4', '8.3', '8.0'], mysql: ['8.4'], pgsql: ['17'], redis: ['8'], nginx: ['alpine'] }
      : { php: [], mysql: [], pgsql: [], redis: [], nginx: [] },
  )
  const stopped = reactive<Record<ServiceKind, string[]>>({
    php: [],
    mysql: [],
    pgsql: [],
    redis: [],
    nginx: [],
  })
  const sites = reactive<Site[]>(
    demo
      ? [
          { domain: 'demo.test', port: 80, php: '8.4', root: '~/www/demo.test', hosts: true, health: 'up', rewrite: 'laravel' },
          { domain: 'blog.test', port: 80, php: '8.3', root: '~/www/blog.test', hosts: true, health: 'up', rewrite: 'thinkphp' },
          { domain: 'api.test', port: 8080, php: '8.4', root: '~/www/api.test', hosts: false, health: 'warn', rewrite: 'none' },
          { domain: 'legacy.test', port: 8000, php: '8.0', root: '~/www/legacy.test', hosts: true, health: 'down', rewrite: 'ci' },
        ]
      : [],
  )
  const backups = reactive<Backup[]>(
    demo
      ? [
          { file: 'backup-20260913-093015.tar.gz', size: '48.2 MB', at: '2026-09-13 09:30', items: 5 },
          { file: 'backup-20260910-220845.tar.gz', size: '46.7 MB', at: '2026-09-10 22:08', items: 5 },
          { file: 'backup-20260905-141200.tar.gz', size: '41.1 MB', at: '2026-09-05 14:12', items: 4 },
        ]
      : [],
  )
  const offline = reactive<{ total: string; trees: OfflineTree[] }>(
    demo
      ? {
          total: '1.42 GB',
          trees: [
            { svc: 'php', ver: '8.4', size: '420 MB', items: 138, verified: '2026-09-12 18:03' },
            { svc: 'php', ver: '8.0', size: '385 MB', items: 126, verified: '2026-09-08 22:15' },
            { svc: 'mysql', ver: '8.4', size: '212 MB', items: 1, verified: '2026-09-11 09:40' },
            { svc: 'pgsql', ver: '17', size: '180 MB', items: 1, verified: '2026-09-10 15:20' },
            { svc: 'redis', ver: '8', size: '68 MB', items: 1, verified: '2026-09-06 11:22' },
            { svc: 'nginx', ver: 'alpine', size: '52 MB', items: 1, verified: '2026-09-05 08:11' },
          ],
        }
      : { total: '', trees: [] },
  )
  const phpExtensions = reactive<Record<string, string[]>>(
    demo
      ? {
          '8.4': ['gd', 'redis', 'pdo_mysql', 'mysqli', 'pgsql', 'pdo_pgsql', 'zip', 'bcmath', 'intl', 'opcache', 'exif', 'soap', 'sockets', 'imagick', 'xdebug'],
          '8.3': ['gd', 'redis', 'pdo_mysql', 'mysqli', 'zip', 'bcmath', 'opcache', 'exif', 'sockets'],
          '8.0': ['gd', 'redis', 'pdo_mysql', 'mysqli', 'zip', 'bcmath', 'opcache'],
        }
      : {},
  )
  const env = reactive<Env>(defaultEnv())
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
    // 集合字段逐处兜空：本函数是 state:changed 落地链的第一环，此处抛错即整链中断——
    // 后面的 applyTaskBoard 永不执行，队列只能退化成任务 ID（后端 BuildSnapshot 同守此契约）。
    sites.splice(0, sites.length, ...(s.sites ?? []))
    for (const k of Object.keys(env)) delete env[k]
    Object.assign(env, s.env)
    for (const k of Object.keys(phpExtensions)) delete phpExtensions[k]
    for (const [k, v] of Object.entries(s.phpExtensions ?? {})) phpExtensions[k] = [...v]
    Object.assign(dirReady, s.dirReady)
    applyTaskBoard(s.tasks)
  }

  // enterRealHost：App.vue 确认宿主（waitForBackend 命中）后一次性收掉 M1 占位数据，此后界面只认后端事实。
  // 不能只靠 applySnapshot 覆盖：backups 不在快照内、offline 的权威在 cacheStore，
  // 而快照可能压根拉不到（后端报错/未装机）——那种情况下假站点、假备份、假缓存仍会挂在界面上被当成真。
  function enterRealHost(): void {
    for (const kind of Object.keys(installed) as ServiceKind[]) installed[kind].splice(0, installed[kind].length)
    for (const kind of Object.keys(stopped) as ServiceKind[]) stopped[kind].splice(0, stopped[kind].length)
    sites.splice(0, sites.length)
    backups.splice(0, backups.length)
    offline.total = ''
    offline.trees.splice(0, offline.trees.length)
    for (const k of Object.keys(phpExtensions)) delete phpExtensions[k]
    for (const k of Object.keys(env)) delete env[k]
    Object.assign(env, derivePaths(DEFAULT_HOME, DEFAULT_WWW))
    // dirReady 同理：宿主未确认时的 demo 值是「已就绪」，真宿主首帧必须回落到未就绪，由权威快照判定
    dirReady.PHPO_HOME = false
    dirReady.WWW_ROOT = false
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

  return { installed, stopped, sites, backups, offline, phpExtensions, env, configs, dirReady, homeReady, docker, tasks, isServiceRunning, applySnapshot, applyTaskBoard, enterRealHost, setServiceRunning, setBackups, setDocker, phpVersions }
})
