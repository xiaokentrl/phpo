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

// TestNormalizeExtName 真机 php -m 打的是显示名（PDO / SQLite3 / Zend OPcache），而扩展目录用的是小写名。
// 归一必须只在这一处发生：前端再判一次就会出现「两边口径不一致」，界面显示 off 而后端认为 on。
func TestNormalizeExtName(t *testing.T) {
	cases := map[string]string{
		"bcmath":       "bcmath",
		"PDO":          "pdo",
		"SQLite3":      "sqlite3",
		"Zend OPcache": "opcache",
		"OPcache":      "opcache",
		"  mysqli  ":   "mysqli",
		"zlib":         "zlib",
	}
	for line, want := range cases {
		got, ok := NormalizeExtName(line)
		if !ok || got != want {
			t.Errorf("NormalizeExtName(%q)=%q,%v 期望 %q,true", line, got, ok, want)
		}
	}
	// 段标题与空行不是扩展名：当作名字收进集合就会凭空多出一颗开关
	for _, junk := range []string{"", "   ", "[PHP Modules]", "[Zend Modules]", "a/b", "../etc", "x; rm -rf /"} {
		if got, ok := NormalizeExtName(junk); ok {
			t.Errorf("NormalizeExtName(%q)=%q 应被拒", junk, got)
		}
	}
}

// TestExtProbeCmds 两条探针都必须是 argv（不经 shell，防注入），且只读：
// 打开弹窗这件事不该改动容器状态。
func TestExtProbeCmds(t *testing.T) {
	if got := ExtLoadedProbeCmd; len(got) != 2 || got[0] != "php" || got[1] != "-m" {
		t.Fatalf("启用态探针应为 php -m，实得 %v", got)
	}
	if got := ExtIniListCmd; !reflect.DeepEqual(got, []string{"ls", "-1", ExtConfDir}) {
		t.Fatalf("ini 清单探针应为 ls -1 目录，实得 %v", got)
	}
	if got := ExtIniFile("redis"); got != "docker-php-ext-redis.ini" {
		t.Fatalf("ini 文件名不符: %s", got)
	}
}
