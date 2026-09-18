// 派生路径金样本测试：期望值逐一取自原型 475–590 行的输出
package config

import "testing"

func TestDerivePathsDefaults(t *testing.T) {
	e := DerivePaths("", "")
	want := map[string]string{
		"PHPO_HOME":        "~/phpo",
		"WWW_ROOT":         "~/www",
		"PHP_ROOT":         "~/phpo/php",
		"NGINX_ROOT":       "~/phpo/nginx",
		"NGINX_SITES_ROOT": "~/phpo/nginx/sites",
		"MYSQL_ROOT":       "~/phpo/mysql",
		"PGSQL_ROOT":       "~/phpo/pgsql",
		"REDIS_ROOT":       "~/phpo/redis",
		"BACKUP_ROOT":      "~/phpo/backups",
		"OFFLINE_ROOT":     "~/phpo/offline",
	}
	for k, w := range want {
		if got := e.Get(k); got != w {
			t.Errorf("Get(%q) = %q, 期望 %q", k, got, w)
		}
	}
}

func TestDerivePathsTrimsTrailingSlashesOnly(t *testing.T) {
	e := DerivePaths("/data/phpo///", "/srv/www/")
	if e.PHPOHome != "/data/phpo" || e.WWWRoot != "/srv/www" {
		t.Errorf("尾部斜杠未裁剪: %+v", e)
	}
	if e.NginxSitesRoot != "/data/phpo/nginx/sites" {
		t.Errorf("NGINX_SITES_ROOT = %q", e.NginxSitesRoot)
	}
}

func TestHostToContainerMappings(t *testing.T) {
	e := DerivePaths("~/phpo", "~/www")
	cases := []struct{ in, want string }{
		{"~/www/demo.test", "/var/www/demo.test"},
		{"~/www", "/var/www"},
		{"~/wwwx/a", "~/wwwx/a"}, // 前缀必须带 / 边界
		{"~/phpo/nginx/sites/a.conf", "/etc/nginx/sites/a.conf"},
		{"~/phpo/nginx/nginx.conf", "~/phpo/nginx/nginx.conf"}, // 非映射前缀原样
		{"", ""},
	}
	for _, c := range cases {
		if got := e.HostToContainer(c.in); got != c.want {
			t.Errorf("HostToContainer(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestContainerToHostRoundTrip(t *testing.T) {
	e := DerivePaths("~/phpo", "~/www")
	for _, p := range []string{"~/www/a/b", "~/phpo/nginx/sites/x.conf", "~/elsewhere"} {
		if got := e.ContainerToHost(e.HostToContainer(p)); got != p {
			t.Errorf("往返失败: %q → %q", p, got)
		}
	}
}

func TestVerRoot(t *testing.T) {
	e := DerivePaths("", "")
	if got := e.RootFor("php", "8.4"); got != "~/phpo/php/8.4" {
		t.Errorf("RootFor(php,8.4) = %q", got)
	}
}

func TestResolveMountsCountsAndPaths(t *testing.T) {
	e := DerivePaths("", "")
	counts := map[string]int{"php": 4, "nginx": 4, "mysql": 4, "pgsql": 5, "redis": 3}
	for kind, want := range counts {
		got := e.ResolveMounts(kind, "1.2")
		if len(got) != want {
			t.Fatalf("%s 挂载数 = %d, 期望 %d", kind, len(got), want)
		}
	}
	php := e.ResolveMounts("php", "8.4")
	if php[0].Host != "~/www" {
		t.Errorf("php[0].Host = %q, 期望 ~/www", php[0].Host)
	}
	if php[1].Host != "~/phpo/php/8.4/conf/php.ini" || php[1].To != "/usr/local/etc/php/conf.d/zz-phpo.ini" {
		t.Errorf("php.ini 挂载错误: %+v", php[1])
	}
	if len(e.ResolveMounts("unknown", "1")) != 0 {
		t.Error("未知 kind 应返回空")
	}
}

func TestVersionSubdirsLayout(t *testing.T) {
	if len(VersionSubdirs) != 5 {
		t.Fatalf("VERSION_SUBDIRS 服务数 = %d, 期望 5", len(VersionSubdirs))
	}
	if got := VersionSubdirs["mysql"]; len(got) != 4 || got[3] != "initdb" {
		t.Errorf("mysql 子目录 = %v", got)
	}
}
