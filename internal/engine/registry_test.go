// T302：服务种类 → 镜像引用映射
package engine

import "testing"

func TestImageRefFor(t *testing.T) {
	cases := []struct {
		kind, ver, want string
	}{
		{"php", "8.4", "php:8.4-fpm"},
		{"php", "7.4", "php:7.4-fpm"},
		{"mysql", "8.4", "mysql:8.4"},
		{"pgsql", "17", "postgres:17"},
		{"redis", "8", "redis:8"},
		{"nginx", "alpine", "nginx:alpine"},
	}
	for _, c := range cases {
		got, err := ImageRefFor(c.kind, c.ver)
		if err != nil {
			t.Errorf("ImageRefFor(%q,%q) 报错: %v", c.kind, c.ver, err)
			continue
		}
		if got != c.want {
			t.Errorf("ImageRefFor(%q,%q) = %q，期望 %q", c.kind, c.ver, got, c.want)
		}
	}
}

func TestImageRefForErrors(t *testing.T) {
	if _, err := ImageRefFor("mongo", "7"); err == nil {
		t.Errorf("未知服务应报错")
	}
	if _, err := ImageRefFor("php", ""); err == nil {
		t.Errorf("空版本应报错")
	}
	// 任意字符串版本（路径安全内）应直通为 tag
	if got, err := ImageRefFor("redis", "8-custom.1"); err != nil || got != "redis:8-custom.1" {
		t.Errorf("自由版本串应支持: got=%q err=%v", got, err)
	}
}
