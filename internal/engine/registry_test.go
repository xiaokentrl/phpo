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

// TestMirrorRef 锁死「从某个镜像源拉」这条改写：单段镜像名必须补 library/，
// 空源即原样返回（直连官方）。改写只影响拉取用的名字，拉回后仍会 tag 回原始引用。
func TestMirrorRef(t *testing.T) {
	cases := []struct {
		host, ref, want string
	}{
		// 官方镜像都在 Docker Hub 的 library/ 命名空间下，源转发按这段路径找，不补即 404
		{"docker.m.daocloud.io", "php:8.4-fpm", "docker.m.daocloud.io/library/php:8.4-fpm"},
		{"docker.m.daocloud.io", "mysql:8.4", "docker.m.daocloud.io/library/mysql:8.4"},
		// 多段仓库名（phpo 自有镜像、第三方命名空间）原样接在源后面，不得再插 library/
		{"docker.m.daocloud.io", "phpo/php:8.4", "docker.m.daocloud.io/phpo/php:8.4"},
		{"mirror.local", "bitnami/redis:8", "mirror.local/bitnami/redis:8"},
		// 带端口的源：ref 里第一个冒号是端口，找 tag 只在最后一个斜杠之后找
		{"127.0.0.1:5000", "redis:8", "127.0.0.1:5000/library/redis:8"},
		{"127.0.0.1:5000", "phpo/app:1.2", "127.0.0.1:5000/phpo/app:1.2"},
		// 无 tag 补 latest，与 docker pull 的默认一致
		{"mirror.local", "redis", "mirror.local/library/redis:latest"},
		{"mirror.local", "phpo/app", "mirror.local/phpo/app:latest"},
		// 空源 = 不改写，直连官方
		{"", "php:8.4-fpm", "php:8.4-fpm"},
		{"", "redis", "redis"},
	}
	for _, c := range cases {
		if got := MirrorRef(c.host, c.ref); got != c.want {
			t.Errorf("MirrorRef(%q,%q) = %q，期望 %q", c.host, c.ref, got, c.want)
		}
	}
}

// TestSplitRef 拆名与 tag：带端口的源里第一个冒号不是 tag 分隔符。
func TestSplitRef(t *testing.T) {
	cases := []struct {
		ref       string
		name, tag string
	}{
		{"php:8.4-fpm", "php", "8.4-fpm"},
		{"redis", "redis", "latest"},
		{"127.0.0.1:5000/app:1.2", "127.0.0.1:5000/app", "1.2"},
		{"127.0.0.1:5000/app", "127.0.0.1:5000/app", "latest"},
	}
	for _, c := range cases {
		n, tg := splitRef(c.ref)
		if n != c.name || tg != c.tag {
			t.Errorf("splitRef(%q) = (%q,%q)，期望 (%q,%q)", c.ref, n, tg, c.name, c.tag)
		}
	}
}
