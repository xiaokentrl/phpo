// 扩展编译失败的真正原因：configure 找不到它要用的**系统开发包**（不是扩展本身缺失）。
// 真机取证（<用户数据目录>/phpo.db 的 operations 表，四条失败任务）全部停在同一形状的两句：
//
//	configure: error: Package requirements (zlib >= 1.2.11) were not met:
//	Package 'zlib', required by 'virtual:world', not found
//
// 而界面上只看得到「扩展 gd 安装失败，本次扩展集未应用」——用户不知道该装什么。
// 本文件只做两件事：① 从编译输出里认出被缺的那个 pkg-config 名字；② 把它翻译成「该装哪个包」
// （Debian 与 Alpine 两种基座各一个名字）。phpo **不代装系统包**（§0.2 规则 36：只离线扩展包本体，
// 不为基座系统包造缓存槽位），所以这份表只用于把失败说清楚、并告诉用户点哪里回来。
package config

import (
	"regexp"
	"strings"
)

// ExtSysPkg 一种系统开发包在两种基座下的安装名
type ExtSysPkg struct {
	Debian string // Debian 基座（php:{ver}-fpm）：apt-get install -y <此名>
	Alpine string // Alpine 基座（php:{ver}-fpm-alpine）：apk add <此名>
}

// extSysPkgs 键 = configure 报缺失时用的 pkg-config 模块名（小写）。
// 两侧包名**全部现取命令验证过**，未凭印象补录：
//   - Debian 侧在真基座 php:8.4-fpm 内 `apt-get update` 后逐个 `apt-cache policy`，17 个都有 Candidate；
//   - Alpine 侧在 alpine:latest 内 `apk update` 后逐个 `apk info -a`，17 个都命中仓库；
//   - 键（模块名）取自「装齐这些开发包之后」容器内 `pkg-config --list-all` 的真实输出，
//     以及真机失败日志里出现过的原文（zlib / libcurl / libsodium）。
//
// 表里没有的名字仍会照原样报给用户（ExtSysPkgHint 只回名字），不得因为不认识就当没缺。
var extSysPkgs = map[string]ExtSysPkg{
	"zlib":           {"zlib1g-dev", "zlib-dev"},
	"libz":           {"zlib1g-dev", "zlib-dev"},
	"libpng":         {"libpng-dev", "libpng-dev"},
	"libpng16":       {"libpng-dev", "libpng-dev"},
	"libjpeg":        {"libjpeg-dev", "libjpeg-turbo-dev"},
	"jpeg":           {"libjpeg-dev", "libjpeg-turbo-dev"},
	"libwebp":        {"libwebp-dev", "libwebp-dev"},
	"libwebpdecoder": {"libwebp-dev", "libwebp-dev"},
	"libwebpdemux":   {"libwebp-dev", "libwebp-dev"},
	"libwebpmux":     {"libwebp-dev", "libwebp-dev"},
	"libavif":        {"libavif-dev", "libavif-dev"},
	"libxpm":         {"libxpm-dev", "libxpm-dev"},
	"freetype2":      {"libfreetype-dev", "freetype-dev"},
	"freetype":       {"libfreetype-dev", "freetype-dev"},
	"libzip":         {"libzip-dev", "libzip-dev"},
	"oniguruma":      {"libonig-dev", "oniguruma-dev"},
	"openssl":        {"libssl-dev", "openssl-dev"},
	"libcrypto":      {"libssl-dev", "openssl-dev"},
	"libcurl":        {"libcurl4-openssl-dev", "curl-dev"},
	"libsodium":      {"libsodium-dev", "libsodium-dev"},
	"sqlite3":        {"libsqlite3-dev", "sqlite-dev"},
	"libpq":          {"libpq-dev", "libpq-dev"},
	"libxml-2.0":     {"libxml2-dev", "libxml2-dev"},
	"libxml2":        {"libxml2-dev", "libxml2-dev"},
	"icu-i18n":       {"libicu-dev", "icu-dev"},
	"icu-uc":         {"libicu-dev", "icu-dev"},
	"icu-io":         {"libicu-dev", "icu-dev"},
	"ldap":           {"libldap-dev", "openldap-dev"},
}

// ExtSysPkgFor 按 pkg-config 名查两种基座下的包名；表里没有即 ok=false
func ExtSysPkgFor(name string) (ExtSysPkg, bool) {
	p, ok := extSysPkgs[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// ExtSysPkgHint 把「缺的名字」写成一句能照着装的话：`zlib（Debian: zlib1g-dev ／ Alpine: zlib-dev）`。
// 不认识的名字只回原样——宁可少说一种包名，也不得编一个不存在的包。
func ExtSysPkgHint(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	p, ok := ExtSysPkgFor(n)
	if !ok {
		return n
	}
	return n + "（Debian: " + p.Debian + " ／ Alpine: " + p.Alpine + "）"
}

// configure 报「找不到系统开发包」的两种写法（真机失败日志里逐字出现的就是这两句）：
//
//	Package requirements (zlib >= 1.2.11) were not met:
//	Package 'zlib', required by 'virtual:world', not found
//
// 另一族写法 `Requested 'libsodium >= 1.0.8' but version of Sodium is …` **刻意不认**：
// 那是「包装了但版本太旧」，报成「缺系统开发包」就是假话。
var (
	depRequirementsRe = regexp.MustCompile(`Package requirements \(([^)]*)\) were not met`)
	depNotFoundRe     = regexp.MustCompile(`Package '([^']+)', required by`)
	// depNameRe 只收 pkg-config 模块名的形状；括号里的依赖表可能带 `>=`、`&&`、`||` 之类，靠它滤掉
	depNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._+-]*$`)
)

// ExtMissingDepNames 从一行容器内编译输出里认出被报「找不到」的系统开发包名（已去版本约束、转小写、去重）。
// 一行可能有两个：`Package requirements (libavif libwebp)` 里就列了多项。
func ExtMissingDepNames(line string) []string {
	var exprs []string
	if m := depRequirementsRe.FindStringSubmatch(line); m != nil {
		exprs = append(exprs, m[1])
	}
	if m := depNotFoundRe.FindStringSubmatch(line); m != nil {
		exprs = append(exprs, m[1])
	}
	if len(exprs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range exprs {
		// `zlib >= 1.2.11` → zlib；`(a b)` 形式的多项依赖按空格分段取每段首词
		for _, tok := range strings.Fields(e) {
			n := strings.ToLower(tok)
			if !depNameRe.MatchString(n) || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
