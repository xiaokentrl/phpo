// PHP 扩展安装方式判定（T601）+ 扩展包离线化（§5.14.3 / §5.16.3）：
// 常见第三方扩展走 pecl，其余走 docker-php-ext-install 内置；命令以 argv 传入容器 exec，不经 shell（防注入）。
// pecl 与 apk 都先把**包文件**取到容器暂存目录，再由宿主取回临时目录 → 提升到离线缓存，之后可零网络复用。
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

// 容器内扩展包暂存目录：宿主取回后即清空，且必须在 docker commit 之前删掉——
// 留在容器文件系统里就会被固化进 phpo/php:{version}（那是镜像层垃圾，不是缓存）。
const (
	ExtStagingRoot = "/tmp/phpo-ext"
	ExtStagingAPK  = ExtStagingRoot + "/apk"
	ExtStagingPECL = ExtStagingRoot + "/pecl"
)

// ExtStagingCleanupCmd 清空容器暂存目录（commit 前最后一步）
var ExtStagingCleanupCmd = []string{"rm", "-rf", ExtStagingRoot}

// PkgManager 基座镜像的包管理器族：决定「构建依赖包」这一类离线对象存不存在
type PkgManager string

const (
	PkgManagerAPK  PkgManager = "apk"  // Alpine：PHPIZE_DEPS 经 apk 下载
	PkgManagerDeb  PkgManager = "deb"  // Debian 系：docker-php-ext-install 不下系统包
	PkgManagerNone PkgManager = "none" // 认不出来
)

// ExtPkgProbeCmd 探测基座包管理器。Alpine 的判据用 /lib/apk/db/installed——
// 官方 docker-php-ext-install 自己也是看这一个文件决定要不要 apk add，跟着它才不会判成两套。
var ExtPkgProbeCmd = []string{"sh", "-c",
	"if [ -f /lib/apk/db/installed ]; then echo apk; " +
		"elif [ -f /etc/debian_version ]; then echo deb; " +
		"else echo none; fi"}

// ExtApkPrefetchCmd 把 PHPIZE_DEPS（phpize/autoconf/g++ 一类构建依赖）的 .apk 落到暂存目录。
// 关键是 --cache-dir 而非官方脚本的 --no-cache：后者用完即弃，永远拿不到可离线的包文件。
// 依赖顺带装上，下次 docker-php-ext-install 自检发现已在位即不再拨网络。
func ExtApkPrefetchCmd() []string {
	return []string{"sh", "-c", "mkdir -p " + ExtStagingAPK +
		" && apk add --cache-dir " + ExtStagingAPK + " --virtual .phpo-ext-deps $PHPIZE_DEPS"}
}

// ExtPeclDownloadCmd 只取包文件不编译：pecl download 把 {name}-{版本}.tgz 落在暂存目录，
// 宿主取回提升进缓存后，这份 .tgz 才是「零网络可复用」的对象。
// 扩展名已过 ValidateExt（^[a-zA-Z0-9._-]+$，无 shell 元字符），脚本字面量拼接不构成注入。
func ExtPeclDownloadCmd(name string) []string {
	n := strings.TrimSpace(name)
	if !ValidateExt(n) {
		return nil
	}
	return []string{"sh", "-c", "mkdir -p " + ExtStagingPECL + " && cd " + ExtStagingPECL + " && pecl download " + n}
}

// ExtInstallFromFileCmds 从已在容器暂存目录里的包文件装 pecl 扩展（命中缓存即走这条，零网络）
func ExtInstallFromFileCmds(name, stagedPkg string) [][]string {
	n := strings.TrimSpace(name)
	if !ValidateExt(n) || stagedPkg == "" {
		return nil
	}
	return [][]string{{"pecl", "install", stagedPkg}, {"docker-php-ext-enable", n}}
}
