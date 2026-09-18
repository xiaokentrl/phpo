// config 扩展判定测试：内置 vs pecl 归类、命令 argv 构造、非法名拒绝（不拼 shell，防注入）
package config

import (
	"reflect"
	"testing"
)

func TestClassifyExt(t *testing.T) {
	cases := map[string]ExtTool{
		"redis":   ExtToolPECL,
		"Redis":   ExtToolPECL, // 大小写不敏感
		" swoole": ExtToolPECL, // 容忍首尾空格
		"gd":      ExtToolBuiltin,
		"opcache": ExtToolBuiltin,
		"mysqlnd": ExtToolBuiltin,
	}
	for name, want := range cases {
		if got := ClassifyExt(name); got != want {
			t.Errorf("ClassifyExt(%q)=%s 期望 %s", name, got, want)
		}
	}
}

func TestExtInstallCmds(t *testing.T) {
	if got := ExtInstallCmds("redis"); !reflect.DeepEqual(got, [][]string{{"pecl", "install", "redis"}, {"docker-php-ext-enable", "redis"}}) {
		t.Fatalf("pecl 两步命令不符: %v", got)
	}
	if got := ExtInstallCmds("gd"); !reflect.DeepEqual(got, [][]string{{"docker-php-ext-install", "gd"}}) {
		t.Fatalf("内置一步命令不符: %v", got)
	}
	// 路径不安全名（含穿越/分隔）应被拒（返回 nil）
	for _, bad := range []string{"gd; rm -rf /", "../etc", "a/b", ""} {
		if ExtInstallCmds(bad) != nil {
			t.Fatalf("非法扩展名应返回 nil: %q", bad)
		}
	}
}
