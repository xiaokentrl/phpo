// PHP 扩展全量目录（前端唯一清单）：安装弹窗与管理扩展弹窗共用同一份数据，不再各自裁剪。
// tool 必须与后端 config.ClassifyExt 一致，由 scripts/check-ext-catalog.go 在 task check 里对账。
export type ExtTool = 'builtin' | 'pecl'

export interface ExtDef {
  name: string
  tool: ExtTool
  group: string // 分组标签键：t(`ext.group.${group}`)
  common?: boolean // 常用项：安装 PHP 时默认勾选
  minVer?: string // 该扩展自此 PHP 版本起才存在（按 major.minor 比较）
}

// 分组顺序即渲染顺序；组内按字母序书写，便于人工核对完整性
export const EXT_GROUPS = ['basic', 'db', 'cache', 'media', 'net', 'async', 'framework', 'debug'] as const

export const EXT_CATALOG: ExtDef[] = [
  // ---- 基础 ----
  { name: 'bcmath', tool: 'builtin', group: 'basic', common: true },
  { name: 'bz2', tool: 'builtin', group: 'basic' },
  { name: 'calendar', tool: 'builtin', group: 'basic' },
  { name: 'ctype', tool: 'builtin', group: 'basic' },
  { name: 'curl', tool: 'builtin', group: 'basic', common: true },
  { name: 'dom', tool: 'builtin', group: 'basic' },
  { name: 'exif', tool: 'builtin', group: 'basic', common: true },
  { name: 'ffi', tool: 'builtin', group: 'basic', minVer: '7.4' },
  { name: 'fileinfo', tool: 'builtin', group: 'basic' },
  { name: 'filter', tool: 'builtin', group: 'basic' },
  { name: 'ftp', tool: 'builtin', group: 'basic' },
  { name: 'gettext', tool: 'builtin', group: 'basic' },
  { name: 'gmp', tool: 'builtin', group: 'basic' },
  { name: 'iconv', tool: 'builtin', group: 'basic' },
  { name: 'mbstring', tool: 'builtin', group: 'basic', common: true },
  { name: 'opcache', tool: 'builtin', group: 'basic', common: true },
  { name: 'openssl', tool: 'builtin', group: 'basic' },
  { name: 'pcntl', tool: 'builtin', group: 'basic', common: true },
  { name: 'pdo', tool: 'builtin', group: 'basic' },
  { name: 'posix', tool: 'builtin', group: 'basic' },
  { name: 'readline', tool: 'builtin', group: 'basic' },
  { name: 'session', tool: 'builtin', group: 'basic' },
  { name: 'shmop', tool: 'builtin', group: 'basic' },
  { name: 'sockets', tool: 'builtin', group: 'basic' },
  { name: 'sodium', tool: 'builtin', group: 'basic', minVer: '7.2' },
  { name: 'sysvmsg', tool: 'builtin', group: 'basic' },
  { name: 'sysvsem', tool: 'builtin', group: 'basic' },
  { name: 'sysvshm', tool: 'builtin', group: 'basic' },
  { name: 'tidy', tool: 'builtin', group: 'basic' },
  { name: 'tokenizer', tool: 'builtin', group: 'basic' },
  { name: 'xml', tool: 'builtin', group: 'basic' },
  { name: 'xmlreader', tool: 'builtin', group: 'basic' },
  { name: 'xmlwriter', tool: 'builtin', group: 'basic' },
  { name: 'xsl', tool: 'builtin', group: 'basic' },
  { name: 'zip', tool: 'builtin', group: 'basic' },
  { name: 'zlib', tool: 'builtin', group: 'basic' },
  { name: 'ds', tool: 'pecl', group: 'basic' },
  { name: 'uuid', tool: 'pecl', group: 'basic' },
  { name: 'yaml', tool: 'pecl', group: 'basic' },

  // ---- 数据库 ----
  { name: 'mysqli', tool: 'builtin', group: 'db', common: true },
  { name: 'pdo_dblib', tool: 'builtin', group: 'db' },
  { name: 'pdo_firebird', tool: 'builtin', group: 'db' },
  { name: 'pdo_mysql', tool: 'builtin', group: 'db', common: true },
  { name: 'pdo_odbc', tool: 'builtin', group: 'db' },
  { name: 'pdo_pgsql', tool: 'builtin', group: 'db' },
  { name: 'pdo_sqlite', tool: 'builtin', group: 'db' },
  { name: 'pgsql', tool: 'builtin', group: 'db' },
  { name: 'sqlite3', tool: 'builtin', group: 'db' },
  { name: 'mongodb', tool: 'pecl', group: 'db' },

  // ---- 缓存 / 序列化 ----
  { name: 'apcu', tool: 'pecl', group: 'cache', common: true },
  { name: 'igbinary', tool: 'pecl', group: 'cache', common: true },
  { name: 'memcached', tool: 'pecl', group: 'cache' },
  { name: 'msgpack', tool: 'pecl', group: 'cache' },
  { name: 'redis', tool: 'pecl', group: 'cache', common: true },

  // ---- 图像 / 文档 ----
  { name: 'gd', tool: 'builtin', group: 'media' },
  { name: 'imagick', tool: 'pecl', group: 'media' },
  { name: 'gmagick', tool: 'pecl', group: 'media' },
  { name: 'xlswriter', tool: 'pecl', group: 'media' },

  // ---- 网络 / 消息 ----
  { name: 'amqp', tool: 'pecl', group: 'net' },
  { name: 'grpc', tool: 'pecl', group: 'net' },
  { name: 'imap', tool: 'builtin', group: 'net' },
  { name: 'ldap', tool: 'builtin', group: 'net' },
  { name: 'protobuf', tool: 'pecl', group: 'net' },
  { name: 'rdkafka', tool: 'pecl', group: 'net' },
  { name: 'snmp', tool: 'builtin', group: 'net' },
  { name: 'ssh2', tool: 'pecl', group: 'net' },
  { name: 'zmq', tool: 'pecl', group: 'net' },

  // ---- 异步 / 高性能 ----
  { name: 'event', tool: 'pecl', group: 'async' },
  { name: 'swoole', tool: 'pecl', group: 'async' },
  { name: 'uv', tool: 'pecl', group: 'async' },

  // ---- 框架（C 实现） ----
  { name: 'phalcon', tool: 'pecl', group: 'framework' },
  { name: 'yaf', tool: 'pecl', group: 'framework' },

  // ---- 调试 / 开发 ----
  { name: 'xdebug', tool: 'pecl', group: 'debug' },
]

// 原型 EXT_LIB 的 15 项里 pthreads 未收录：它只支持 NTS 之外的 CLI 场景且止于 PHP 7，
// 在 php-*-fpm 镜像里必然编译失败，列出来只会误导用户，故按「路径安全以外不限制」的口径不纳入目录。
export const EXT_NAMES: string[] = EXT_CATALOG.map((e) => e.name)

// extMatchesVersion 按 major.minor 比较版本下限；版本号非数字（用户自定义标签）时不做限制，交后端路径安全裁决。
export function extMatchesVersion(def: ExtDef, version: string): boolean {
  if (!def.minVer) return true
  const cur = parseVer(version)
  const min = parseVer(def.minVer)
  if (!cur || !min) return true
  return cur[0] * 100 + cur[1] >= min[0] * 100 + min[1]
}

function parseVer(v: string): [number, number] | null {
  const m = /^(\d+)\.(\d+)/.exec(v.trim())
  return m ? [Number(m[1]), Number(m[2])] : null
}

// catalogFor 某 PHP 版本可见的全量目录（按 EXT_GROUPS 顺序、组内保持书写顺序）
export function catalogFor(version: string): ExtDef[] {
  const visible = EXT_CATALOG.filter((e) => extMatchesVersion(e, version))
  return EXT_GROUPS.flatMap((g) => visible.filter((e) => e.group === g))
}

// commonExts 安装该版本时默认勾选的常用扩展
export function commonExts(version: string): string[] {
  return catalogFor(version).filter((e) => e.common).map((e) => e.name)
}

// EXT_LIB 兼容旧引用：全量扩展名（原型 15 项的超集）
export const EXT_LIB: string[] = EXT_NAMES
