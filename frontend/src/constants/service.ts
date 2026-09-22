// 服务元数据常量：SVC_META / NAV / DIR_ROWS / 配置文件数（原型 1371–1393、2660–2666 行直译）
import type { ServiceKind, SvcMeta } from '@/types'

// defaultPort 与后端 internal/service/registry.go 的 specs[].HostPort 同值（改一处必改两处）：
// 端口键未落库时，容器实际发布的就是这个默认端口。前端不得再用 suggestPortFor 猜
// （那是原型给安装弹窗预填的算法，mysql 8.4 会算成 3384，而真容器发布 3306）。
export const SVC_META: Record<ServiceKind, SvcMeta> = {
  php: { titleKey: 'php.title', icon: '🐘', subtitleKey: 'php.subtitle', hintKey: 'php.empty.desc', emptyTitleKey: 'php.empty.title', suggested: ['8.4', '8.3', '8.2', '8.1', '8.0', '7.4'], defaultPort: 0 },
  mysql: { titleKey: 'mysql.title', icon: '🐬', subtitleKey: 'mysql.subtitle', hintKey: 'mysql.empty.desc', emptyTitleKey: 'mysql.empty.title', suggested: ['9.1', '8.4', '8.0', '5.7'], defaultPort: 3306 },
  pgsql: { titleKey: 'pgsql.title', icon: '🐘', subtitleKey: 'pgsql.subtitle', hintKey: 'pgsql.empty.desc', emptyTitleKey: 'pgsql.empty.title', suggested: ['17', '16', '15', '14'], defaultPort: 5432 },
  redis: { titleKey: 'redis.title', icon: '⚡', subtitleKey: 'redis.subtitle', hintKey: 'redis.empty.desc', emptyTitleKey: 'redis.empty.title', suggested: ['8', '7'], defaultPort: 6379 },
  nginx: { titleKey: 'nginx.title', icon: '🌐', subtitleKey: 'nginx.subtitle', hintKey: 'nginx.empty.desc', emptyTitleKey: 'nginx.empty.title', suggested: ['alpine', '1.25'], single: true, defaultPort: 80 },
}

export const NAV: Array<{ section: string } | { id: ServiceKind | 'sites' | 'backup' | 'offline' | 'settings' | 'overview'; labelKey: string; icon: string }> = [
  { section: 'nav.business' },
  { id: 'sites', labelKey: 'nav.sites', icon: '🔗' },
  { section: 'nav.services' },
  { id: 'php', labelKey: 'nav.php', icon: '🐘' },
  { id: 'mysql', labelKey: 'nav.mysql', icon: '🐬' },
  { id: 'pgsql', labelKey: 'nav.pgsql', icon: '🐘' },
  { id: 'redis', labelKey: 'nav.redis', icon: '⚡' },
  { id: 'nginx', labelKey: 'nav.nginx', icon: '🌐' },
  { section: 'nav.ops' },
  { id: 'backup', labelKey: 'nav.backup', icon: '📦' },
  { id: 'offline', labelKey: 'nav.offline', icon: '🗄️' },
  { id: 'settings', labelKey: 'nav.settings', icon: '⚙️' },
  { id: 'overview', labelKey: 'nav.overview', icon: '◈' },
]

// versionCard 目录行：[子目录, i18n 标签键]
export const DIR_ROWS: Record<ServiceKind, Array<[string, string]>> = {
  php: [['conf', 'svc.confDir'], ['logs', 'svc.logDir']],
  nginx: [['conf', 'svc.confDir'], ['logs', 'svc.logDir']],
  mysql: [['conf', 'svc.confDir'], ['data', 'svc.dataDir'], ['logs', 'svc.logDir'], ['initdb', 'svc.initdbDir']],
  pgsql: [['conf', 'svc.confDir'], ['data', 'svc.dataDir'], ['logs', 'svc.logDir'], ['initdb', 'svc.initdbDir']],
  redis: [['conf', 'svc.confDir'], ['data', 'svc.dataDir'], ['logs', 'svc.logDir']],
}

// VERSION_SUBDIRS：各服务版本目录子项（原型 497–503 行直译，供任务日志使用）
export const VERSION_SUBDIRS: Record<ServiceKind, string[]> = {
  php: ['conf', 'logs'],
  nginx: ['conf', 'logs'],
  mysql: ['conf', 'data', 'logs', 'initdb'],
  pgsql: ['conf', 'data', 'logs', 'initdb'],
  redis: ['conf', 'data', 'logs'],
}

// DEFAULT_CONFIGS 每服务文件数（§0.3：5 服务 7 文件）
export const DEFAULT_FILE_COUNT: Record<ServiceKind, number> = {
  php: 2,
  mysql: 1,
  pgsql: 2,
  redis: 1,
  nginx: 1,
}

export const SVC_ICON: Record<string, string> = { php: '🐘', mysql: '🐬', pgsql: '🐘', redis: '⚡', nginx: '🌐' }
