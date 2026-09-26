// 扩展编译失败的点名能力：从容器内输出里认出 configure 说「找不到」的系统开发包，
// 再翻成两种基座各自该装哪个包。这里的每一条输入都是真机失败日志的原文形状，不是编的样例。
package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestExtMissingDepNames_RealLogShapes 认两种真机写法，且只认这两种。
// 真机四条失败（sodium / gd / gd / curl）的日志原文就是这个形状。
func TestExtMissingDepNames_RealLogShapes(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			"requirements 带版本约束：zlib >= 1.2.11 → 只取 zlib",
			"configure: error: Package requirements (zlib >= 1.2.11) were not met:",
			[]string{"zlib"},
		},
		{
			"not found 单引号写法",
			"Package 'zlib', required by 'virtual:world', not found",
			[]string{"zlib"},
		},
		{
			"requirements 一项多名：括号里按空格分段",
			"configure: error: Package requirements (libavif libwebp >= 1.0) were not met:",
			[]string{"libavif", "libwebp"},
		},
		{
			"大小写归一：显示名与模块名不同拼写也进同一条目",
			"Package requirements (ZLIB) were not met:",
			[]string{"zlib"},
		},
		{
			"带点与加号的名字（libxml-2.0 / glib-2.0 这一族）",
			"Package 'libxml-2.0', required by 'virtual:world', not found",
			[]string{"libxml-2.0"},
		},
		{
			"两种写法同在一行也只去重出一份",
			"Package requirements (zlib) were not met: Package 'zlib', required by 'virtual:world', not found",
			[]string{"zlib"},
		},
		{
			"版本太旧**不是**缺包：这句刻意不认，报成缺系统开发包就是假话",
			"configure: error: Requested 'libsodium >= 1.0.8' but version of Sodium is 1.0.16",
			nil,
		},
		{
			"正常编译输出不误认",
			"Build complete. Don't forget to enable your extensions",
			nil,
		},
		{
			"缺包之外的 configure 报错不误认",
			"configure: error: cannot find /usr/local/bin/phpize",
			nil,
		},
	}
	for _, c := range cases {
		got := ExtMissingDepNames(c.line)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n输入 %q\n期望 %v\n实得 %v", c.name, c.line, c.want, got)
		}
	}
}

// TestExtSysPkgHint 认识的名字给出两种基座的包名；不认识的原样回，不得编一个不存在的包。
func TestExtSysPkgHint(t *testing.T) {
	got := ExtSysPkgHint("zlib")
	want := "zlib（Debian: zlib1g-dev ／ Alpine: zlib-dev）"
	if got != want {
		t.Errorf("已知名字提示不符\n期望 %q\n实得 %q", want, got)
	}
	if got := ExtSysPkgHint(" LibFoo "); got != "libfoo" {
		t.Errorf("未知名字应原样回（只归一小写与空白），实得 %q", got)
	}
	if p, ok := ExtSysPkgFor("ZLIB"); !ok || p.Debian != "zlib1g-dev" {
		t.Errorf("查表应忽略大小写与空白，实得 %+v ok=%v", p, ok)
	}
	if _, ok := ExtSysPkgFor("libnope"); ok {
		t.Error("表外的名字不得命中")
	}
}

// TestExtSysPkgTable_Shape 表内每一项必须两侧都有包名，且键的形状就是提取器会产出的形状。
// 键写成 `ZLIB` 或带空格，等于这条映射永远查不到——只有命令实测能保证的事不靠印象维持。
func TestExtSysPkgTable_Shape(t *testing.T) {
	for name, p := range extSysPkgs {
		if p.Debian == "" || p.Alpine == "" {
			t.Errorf("%s 缺一种基座的包名: %+v", name, p)
		}
		if !depNameRe.MatchString(name) {
			t.Errorf("键 %q 不是 pkg-config 模块名的形状，提取器产出不了它", name)
		}
		if strings.ToLower(strings.TrimSpace(name)) != name {
			t.Errorf("键 %q 应已是小写无空白", name)
		}
		// 键本身也要能被自己的提示函数认回来，否则日志里那条提示指向一个查不到的名字
		if hint := ExtSysPkgHint(name); !strings.HasPrefix(hint, name+"（") {
			t.Errorf("键 %q 的提示形状不符: %q", name, hint)
		}
	}
}
