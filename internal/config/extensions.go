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

// peclExts 需经 PECL 分发的常见第三方扩展名（小写）；不在此表者按内置处理
var peclExts = map[string]bool{
	"redis": true, "swoole": true, "xdebug": true, "imagick": true, "mongodb": true,
	"apcu": true, "memcached": true, "msgpack": true, "igbinary": true, "yaf": true,
	"phalcon": true, "ssh2": true, "protobuf": true, "rdkafka": true, "zmq": true,
	"uuid": true, "ds": true,
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
