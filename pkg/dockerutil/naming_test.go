// dockerutil 命名规范：容器/卷/别名/前缀判定（check-docker-naming.go 语义基线）
package dockerutil

import "testing"

func TestResourceNaming(t *testing.T) {
	cases := []struct {
		kind, ver string
		container string
		alias     string
		volume    string
	}{
		{"php", "8.4", "phpo-php-8.4", "php-8.4-fpm", "phpo-php-8.4-conf"},
		{"nginx", "alpine", "phpo-nginx-alpine", "nginx-alpine", "phpo-nginx-alpine-logs"},
		{"mysql", "8.4", "phpo-mysql-8.4", "mysql-8.4", "phpo-mysql-8.4-data"},
		{"redis", "8", "phpo-redis-8", "redis-8", "phpo-redis-8-data"},
	}
	for _, c := range cases {
		if got := ContainerName(c.kind, c.ver); got != c.container {
			t.Errorf("ContainerName(%q,%q)=%q 期望 %q", c.kind, c.ver, got, c.container)
		}
		if got := NetworkAlias(c.kind, c.ver); got != c.alias {
			t.Errorf("NetworkAlias(%q,%q)=%q 期望 %q", c.kind, c.ver, got, c.alias)
		}
		purpose := c.volume[len("phpo-"+c.kind+"-"+c.ver)+1:]
		if got := VolumeName(c.kind, c.ver, purpose); got != c.volume {
			t.Errorf("VolumeName=%q 期望 %q", got, c.volume)
		}
		if !ValidName(ContainerName(c.kind, c.ver)) {
			t.Errorf("容器名应合法: %s", c.container)
		}
	}
}

func TestPHPAliasCarriesFpm(t *testing.T) {
	// 硬红线 1：vhost fastcgi 上游 php-{version}-fpm:9000 必须能经网络别名解析
	if got := NetworkAlias("php", "8.4"); got != "php-8.4-fpm" {
		t.Errorf("PHP 别名必须为 php-8.4-fpm, got %q", got)
	}
}

func TestIsPhpoResource(t *testing.T) {
	if !IsPhpoResource("phpo-php-8.4") || !IsPhpoResource(NetworkName) {
		t.Error("phpo 资源应识别")
	}
	if IsPhpoResource("wordpress_http_1") {
		t.Error("非 phpo 资源不得误判")
	}
	if ValidName("phpo-bad/name") {
		t.Error("含斜杠的非法名不得通过")
	}
}
