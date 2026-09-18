package template

import (
	"strings"
	"testing"
)

func TestKindFileCount(t *testing.T) {
	if got := len(Kinds()); got != 5 {
		t.Fatalf("服务种类数 = %d，期望 5", got)
	}
	total := 0
	for _, k := range Kinds() {
		total += len(kindFiles[k])
	}
	if total != 7 {
		t.Fatalf("默认配置文件总数 = %d，期望 7（DEFAULT_CONFIGS 5 服务 7 文件）", total)
	}
}

func TestFilesForNamesAndPaths(t *testing.T) {
	cases := map[string]struct {
		names []string
		paths []string
	}{
		"php":   {[]string{"php.ini", "php-fpm.conf"}, []string{"php/8.4/conf/php.ini", "php/8.4/conf/php-fpm.conf"}},
		"mysql": {[]string{"my.cnf"}, []string{"mysql/8.4/conf/my.cnf"}},
		"pgsql": {[]string{"postgresql.conf", "pg_hba.conf"}, []string{"pgsql/17/conf/postgresql.conf", "pgsql/17/conf/pg_hba.conf"}},
		"redis": {[]string{"redis.conf"}, []string{"redis/8/conf/redis.conf"}},
		"nginx": {[]string{"nginx.conf"}, []string{"nginx/alpine/conf/nginx.conf"}},
	}
	for kind, want := range cases {
		files, err := FilesFor(kind, versionOf(kind))
		if err != nil {
			t.Fatalf("%s: FilesFor 报错: %v", kind, err)
		}
		if len(files) != len(want.names) {
			t.Fatalf("%s: 文件数 = %d，期望 %d", kind, len(files), len(want.names))
		}
		for i, f := range files {
			if f.Name != want.names[i] {
				t.Errorf("%s[%d]: name = %q，期望 %q", kind, i, f.Name, want.names[i])
			}
			if f.Path != want.paths[i] {
				t.Errorf("%s[%d]: path = %q，期望 %q", kind, i, f.Path, want.paths[i])
			}
			if f.Content == "" {
				t.Errorf("%s[%d]: content 为空", kind, i)
			}
		}
	}
}

func TestUnknownKindIsEmpty(t *testing.T) {
	files, err := FilesFor("mongo", "7")
	if err != nil {
		t.Fatalf("未知种类不应报错: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("未知种类应返回空，得 %d 项", len(files))
	}
}

// redis requirepass 用去点版本号拼环境变量名；其余模板用原样版本
func TestVersionSubstitution(t *testing.T) {
	files, err := FilesFor("redis", "7.2")
	if err != nil {
		t.Fatal(err)
	}
	c := files[0].Content
	if !strings.Contains(c, "# phpo · Redis 7.2") {
		t.Errorf("redis.conf 头未含原样版本 7.2:\n%s", c)
	}
	if !strings.Contains(c, "requirepass ${REDIS_72_ROOT_PASSWORD}") {
		t.Errorf("redis.conf requirepass 未使用去点版本 REDIS_72:\n%s", c)
	}
}

// 含非法标识符字符的版本应规整为合法环境变量名（去点 + 非 [A-Za-z0-9_] → _）
func TestEnvVarNameSanitized(t *testing.T) {
	files, err := FilesFor("redis", "8-custom.1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files[0].Content, "requirepass ${REDIS_8_custom1_ROOT_PASSWORD}") {
		t.Errorf("requirepass 未规整非法字符:\n%s", files[0].Content)
	}
}

func TestRenderVhost(t *testing.T) {
	got, err := RenderVhost(VhostInput{
		Domain:        "demo.test",
		Port:          8080,
		ContainerRoot: "/var/www/demo.test",
		Upstream:      "php-8.4-fpm",
		Rule:          "location / {\n    try_files $uri $uri/ /index.php?$query_string;\n}",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `server {
    listen 8080;
    server_name demo.test;
    root /var/www/demo.test;
    index index.php index.html;
    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
    location ~ \.php$ {
        resolver 127.0.0.11 valid=10s ipv6=off;
        set $php_upstream php-8.4-fpm:9000;
        fastcgi_pass $php_upstream;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }
}`
	if got != want {
		t.Errorf("vhost 渲染不符，实际:\n%s\n期望:\n%s", got, want)
	}
}

func versionOf(kind string) string {
	switch kind {
	case "pgsql":
		return "17"
	case "redis":
		return "8"
	case "nginx":
		return "alpine"
	default:
		return "8.4"
	}
}
