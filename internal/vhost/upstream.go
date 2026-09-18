// vhost 内容级改写：listen 端口与 PHP 上游精确替换（replaceListen / replacePhpUpstream 直译）
// 硬红线 1：上游必须唯一且精确等于 php-{version}-fpm:9000（版本号原样保留点号）
package vhost

import (
	"fmt"
	"regexp"
)

var (
	// 原型 /^(\s*listen\s+)\d+(\s[^;]*)?;/m：非全局，仅首个 listen 行，保留缩进与分号前后缀
	listenLineRe = regexp.MustCompile(`(?m)^(\s*listen\s+)\d+(\s[^;]*)?;`)
	// 原型 /set\s+\$php_upstream\s+[^;]+;/（非全局）：仅首个 set 指令整条重写
	upstreamRe = regexp.MustCompile(`set\s+\$php_upstream\s+[^;]+;`)
)

// replaceFirst 只替换正则首个匹配（对齐 JS String.replace(regexp 无 g) 语义）
func replaceFirst(re *regexp.Regexp, content string, template string) string {
	loc := re.FindStringSubmatchIndex(content)
	if loc == nil {
		return content
	}
	out := re.ExpandString(nil, template, content, loc)
	return content[:loc[0]] + string(out) + content[loc[1]:]
}

// ReplaceListen 就地把首个 listen 端口改为 port；空内容原样返回
func ReplaceListen(content string, port int) string {
	if content == "" {
		return content
	}
	return replaceFirst(listenLineRe, content, fmt.Sprintf("${1}%d${2};", port))
}

// ReplacePhpUpstream 就地把首个 $php_upstream 改写为 php-{php}-fpm:9000（硬红线 1）
func ReplacePhpUpstream(content, php string) string {
	if content == "" {
		return content
	}
	// ExpandString 需转义 $，此处上游名不含 $，用 $$ 表示字面量 $
	return replaceFirst(upstreamRe, content, "set $$php_upstream php-"+php+"-fpm:9000;")
}
