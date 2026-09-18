// 默认配置模板：逐字迁移自原型 DEFAULT_CONFIGS（§0.3：5 服务 7 文件）
// 每服务返回 { name, path, content }[]；path 相对 PHPO_HOME。
export interface ConfigFile {
  name: string
  path: string
  content: string
}

type Factory = (v: string) => ConfigFile[]

export const DEFAULT_CONFIGS: Record<string, Factory> = {
  php: (v) => [
    {
      name: 'php.ini',
      path: `php/${v}/conf/php.ini`,
      content: `; phpo · PHP ${v} php.ini
[PHP]
memory_limit = 256M
upload_max_filesize = 64M
post_max_size = 64M
max_execution_time = 60
max_input_time = 60
date.timezone = Asia/Shanghai

[opcache]
opcache.enable = 1
opcache.memory_consumption = 128
opcache.max_accelerated_files = 10000
opcache.validate_timestamps = 1
opcache.revalidate_freq = 2

[Session]
session.save_handler = redis
session.save_path = "tcp://phpo-redis-8:6379"

[mail]
; sendmail_path = /usr/sbin/sendmail -t -i`,
    },
    {
      name: 'php-fpm.conf',
      path: `php/${v}/conf/php-fpm.conf`,
      content: `; phpo · PHP-FPM ${v} pool
[www]
user = www-data
group = www-data
listen = 0.0.0.0:9000
listen.allowed_clients = any

pm = dynamic
pm.max_children = 20
pm.start_servers = 4
pm.min_spare_servers = 2
pm.max_spare_servers = 8
pm.max_requests = 500

catch_workers_output = yes
clear_env = no
access.log = /var/log/php-fpm/access.log
slowlog = /var/log/php-fpm/slow.log
request_slowlog_timeout = 5s`,
    },
  ],
  mysql: (v) => [
    {
      name: 'my.cnf',
      path: `mysql/${v}/conf/my.cnf`,
      content: `# phpo · MySQL ${v}
[mysqld]
port = 3306
bind-address = 0.0.0.0
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci
default-authentication-plugin = mysql_native_password

datadir = /var/lib/mysql

max_connections = 200
max_allowed_packet = 64M
innodb_buffer_pool_size = 512M
innodb_log_file_size = 128M
innodb_flush_log_at_trx_commit = 2

slow_query_log = 1
slow_query_log_file = /var/log/mysql/slow.log
long_query_time = 1
log_error = /var/log/mysql/error.log

[client]
default-character-set = utf8mb4`,
    },
  ],
  pgsql: (v) => [
    {
      name: 'postgresql.conf',
      path: `pgsql/${v}/conf/postgresql.conf`,
      content: `# phpo · PostgreSQL ${v}
listen_addresses = '*'
port = 5432

hba_file = '/etc/postgresql/pg_hba.conf'
data_directory = '/var/lib/postgresql/data'

max_connections = 100
shared_buffers = 256MB
effective_cache_size = 1GB
work_mem = 8MB
maintenance_work_mem = 64MB
wal_level = replica

logging_collector = on
log_directory = '/var/log/postgresql'
log_filename = 'postgresql-%Y-%m-%d.log'
log_timezone = 'Asia/Shanghai'
timezone = 'Asia/Shanghai'
log_min_duration_statement = 1000`,
    },
    {
      name: 'pg_hba.conf',
      path: `pgsql/${v}/conf/pg_hba.conf`,
      content: `# phpo · PostgreSQL ${v} host-based auth

# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             all                                     trust
host    all             all             127.0.0.1/32            trust
host    all             all             ::1/128                 trust
host    all             all             0.0.0.0/0               md5
host    all             all             ::/0                    md5`,
    },
  ],
  redis: (v) => [
    {
      name: 'redis.conf',
      path: `redis/${v}/conf/redis.conf`,
      content: `# phpo · Redis ${v}
bind 0.0.0.0
port 6379
protected-mode yes
requirepass \${REDIS_${String(v).replace(/\./g, '')}_ROOT_PASSWORD}

dir /data
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec

maxmemory 256mb
maxmemory-policy allkeys-lru
logfile /var/log/redis/redis.log`,
    },
  ],
  nginx: (v) => [
    {
      name: 'nginx.conf',
      path: `nginx/${v}/conf/nginx.conf`,
      content: `# phpo · Nginx ${v}
user  nginx;
worker_processes  auto;
error_log  /var/log/nginx/error.log warn;
pid        /var/run/nginx.pid;

events {
    worker_connections  1024;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;
    sendfile        on;
    keepalive_timeout  65;
    client_max_body_size 100m;
    server_tokens off;
    gzip on;
    include /etc/nginx/conf.d/*.conf;
    # phpo vhosts 目录（挂载自主机的 NGINX_SITES_ROOT）
    include /etc/nginx/sites/*.conf;
}`,
    },
  ],
}

export function getDefaultFiles(kind: string, version: string): ConfigFile[] {
  const fn = DEFAULT_CONFIGS[kind]
  return fn ? fn(version) : []
}
