// vhost 解析：从配置正文回读 listen 端口与 root 路径（parseVhost 直译）
package vhost

import (
	"regexp"
	"strconv"
	"strings"
)

// Parsed 解析结果；Port=0 表示未解析到合法端口（对应原型 null）
type Parsed struct {
	Port int
	Root string
}

var (
	listenRe = regexp.MustCompile(`(?m)^\s*listen\s+(\d+)`)
	rootRe   = regexp.MustCompile(`(?m)^\s*root\s+([^;]+);`)
)

// Parse 正则回读 listen 端口（限 1–65535）与 root 指令；空内容返回零值
func Parse(content string) Parsed {
	var out Parsed
	if content == "" {
		return out
	}
	if m := listenRe.FindStringSubmatch(content); m != nil {
		if p, err := strconv.Atoi(m[1]); err == nil && p >= 1 && p <= 65535 {
			out.Port = p
		}
	}
	if m := rootRe.FindStringSubmatch(content); m != nil {
		out.Root = strings.TrimSpace(m[1])
	}
	return out
}
