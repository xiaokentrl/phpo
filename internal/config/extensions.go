// PHP 扩展安装方式判定（T601，用户裁决「只走内置编译工具」）：
// 常见第三方扩展走 pecl，其余走 docker-php-ext-install 内置；命令以 argv 传入容器 exec，不经 shell（防注入）。
package config

import "strings"

// ExtTool 容器内安装扩展所用的工具族
type ExtTool string

const (
	ExtToolBuiltin ExtTool = "builtin" // docker-php-ext-install（内置扩展：gd/opcache/mysqli/pdo_* 等）
	ExtToolPECL    ExtTool = "pecl"    // pecl install + docker-php-ext-enable（第三方扩展：redis/swoole 等）
)

// peclExts 需经 PECL 分发的常见第三方扩展名（小写）；不在此表者按内置处理。
// 这份表必须与前端唯一清单 frontend/src/constants/ext.ts 的 tool: 'pecl' 项一致，
// 由 scripts/check-ext-catalog.go（task check）对账——分类错了就是「内置命令装第三方扩展」，必然编译失败。
var peclExts = map[string]bool{
	"redis": true, "swoole": true, "xdebug": true, "imagick": true, "gmagick": true, "mongodb": true,
	"apcu": true, "memcached": true, "msgpack": true, "igbinary": true, "yaf": true,
	"phalcon": true, "ssh2": true, "protobuf": true, "rdkafka": true, "zmq": true,
	"uuid": true, "ds": true, "yaml": true, "amqp": true, "grpc": true, "uv": true,
	"event": true, "xlswriter": true,
}

// ClassifyExt 判定扩展安装方式（大小写不敏感）
func ClassifyExt(name string) ExtTool {
	if peclExts[strings.ToLower(strings.TrimSpace(name))] {
		return ExtToolPECL
	}
	return ExtToolBuiltin
}

// ExtInstallCmds 返回在 php 容器内按序执行的 argv 列表；无效扩展名（未过路径安全校验）返回 nil。
// pecl 需两步：先 pecl install，再 docker-php-ext-enable 写出 ini。
func ExtInstallCmds(name string) [][]string {
	n := strings.TrimSpace(name)
	if !ValidateExt(n) {
		return nil
	}
	if ClassifyExt(n) == ExtToolPECL {
		return [][]string{{"pecl", "install", n}, {"docker-php-ext-enable", n}}
	}
	return [][]string{{"docker-php-ext-install", n}}
}
