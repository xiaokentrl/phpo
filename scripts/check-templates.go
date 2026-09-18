//go:build ignore

// check-templates.go —— 模板渲染快照与原配置一致性校验（TX03，T307 验收）
// 目的：证明 internal/template 渲染出的 5 服务 7 文件逐字等于原型 DEFAULT_CONFIGS，diff 为空。
// 运行：go run scripts/check-templates.go
package main

import (
	"fmt"
	"os"
	"strings"

	"phpo/internal/template"
)

// golden 为原型 前端唯一界面来源.txt 中 DEFAULT_CONFIGS 各服务在代表版本下的渲染结果，逐字转录。
var golden = map[string]map[string]string{
	"php|8.4": {
		"php.ini": `; phpo · PHP 8.4 php.ini
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
		"php-fpm.conf": `; phpo · PHP-FPM 8.4 pool
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
	"mysql|8.4": {
		"my.cnf": `# phpo · MySQL 8.4
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
	"pgsql|17": {
		"postgresql.conf": `# phpo · PostgreSQL 17
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
		"pg_hba.conf": `# phpo · PostgreSQL 17 host-based auth

# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             all                                     trust
host    all             all             127.0.0.1/32            trust
host    all             all             ::1/128                 trust
host    all             all             0.0.0.0/0               md5
host    all             all             ::/0                    md5`,
	},
	"redis|8": {
		"redis.conf": `# phpo · Redis 8
bind 0.0.0.0
port 6379
protected-mode yes
requirepass ${REDIS_8_ROOT_PASSWORD}

dir /data
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec

maxmemory 256mb
maxmemory-policy allkeys-lru
logfile /var/log/redis/redis.log`,
	},
	"nginx|alpine": {
		"nginx.conf": `# phpo · Nginx alpine
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
}

func main() {
	var diffs []string
	checked := 0
	for key, files := range golden {
		kind, version := splitKey(key)
		rendered, err := template.FilesFor(kind, version)
		if err != nil {
			diffs = append(diffs, fmt.Sprintf("%s: 渲染报错 %v", key, err))
			continue
		}
		if len(rendered) != len(files) {
			diffs = append(diffs, fmt.Sprintf("%s: 文件数 %d ≠ 金样本 %d", key, len(rendered), len(files)))
		}
		for _, f := range rendered {
			want, ok := files[f.Name]
			if !ok {
				diffs = append(diffs, fmt.Sprintf("%s: 多出文件 %s", key, f.Name))
				continue
			}
			checked++
			if d := lineDiff(want, f.Content); d != "" {
				diffs = append(diffs, fmt.Sprintf("%s/%s: 内容不一致\n%s", key, f.Name, d))
			}
		}
	}

	// vhost 抽样校验（defaultVhost 直译）
	vh, err := template.RenderVhost(template.VhostInput{
		Domain: "demo.test", Port: 80, ContainerRoot: "/var/www/demo.test",
		Upstream: "php-8.4-fpm",
		Rule:     "# Laravel 5+ / Lumen\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}",
	})
	if err != nil {
		diffs = append(diffs, fmt.Sprintf("vhost: 渲染报错 %v", err))
	} else if !strings.Contains(vh, "set $php_upstream php-8.4-fpm:9000;") {
		diffs = append(diffs, "vhost: 未含精确 PHP 上游 php-8.4-fpm:9000")
	} else {
		checked++
	}

	if len(diffs) > 0 {
		fmt.Printf("✗ 模板渲染快照与原配置存在 %d 处差异：\n", len(diffs))
		for _, d := range diffs {
			fmt.Printf("  %s\n", d)
		}
		os.Exit(1)
	}
	fmt.Printf("✓ 模板一致性校验通过：%d 个服务配置 + vhost 抽样，diff 为空\n", len(golden))
}

func splitKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func lineDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var b strings.Builder
	n := len(wl)
	if len(gl) > n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			fmt.Fprintf(&b, "    行 %d:\n      期望 %q\n      实际 %q\n", i+1, w, g)
		}
	}
	return b.String()
}