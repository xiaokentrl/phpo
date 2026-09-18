// Docker 资源命名规范（§5.13.2）：统一 phpo- 前缀命名空间，供创建与校验脚本复用
package dockerutil

import (
	"regexp"
	"strings"
)

// NamespacePrefix 所有 phpo 托管资源的名称前缀（隔离性，硬红线相关）
const NamespacePrefix = "phpo-"

// NetworkName 共享 bridge 网络固定名
const NetworkName = "phpo-network"

// 名称字符集：字母数字起始，后续可含 . _ -（Docker 合法名）
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ContainerName phpo-{kind}-{version}
func ContainerName(kind, version string) string {
	return NamespacePrefix + kind + "-" + version
}

// VolumeName phpo-{kind}-{version}-{purpose}
func VolumeName(kind, version, purpose string) string {
	return NamespacePrefix + kind + "-" + version + "-" + purpose
}

// NetworkAlias 网络别名；PHP 特例带 -fpm 以匹配 fastcgi 上游 php-{version}-fpm（硬红线 1）
func NetworkAlias(kind, version string) string {
	if kind == "php" {
		return kind + "-" + version + "-fpm"
	}
	return kind + "-" + version
}

// IsPhpoResource 名称是否属于 phpo 命名空间（孤儿扫描/清理判定用）
func IsPhpoResource(name string) bool {
	return strings.HasPrefix(name, NamespacePrefix) || name == NetworkName
}

// ValidName 校验资源名是否符合 Docker 命名且带 phpo 前缀
func ValidName(name string) bool {
	return IsPhpoResource(name) && nameRe.MatchString(name)
}
