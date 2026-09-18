package vhost

import (
	"strings"
	"testing"
)

func TestReplaceListen(t *testing.T) {
	in := "    listen 80;\n    server_name demo.test;"
	got := ReplaceListen(in, 81)
	want := "    listen 81;\n    server_name demo.test;"
	if got != want {
		t.Fatalf("改端口失败:\n got=%q\nwant=%q", got, want)
	}
}

// TestReplacePhpUpstream_RoundTrip 硬红线 1：8.3↔8.4 往返逐字符精确
func TestReplacePhpUpstream_RoundTrip(t *testing.T) {
	base := `        set $php_upstream php-8.3-fpm:9000;
        fastcgi_pass $php_upstream;`
	to84 := ReplacePhpUpstream(base, "8.4")
	if !strings.Contains(to84, "set $php_upstream php-8.4-fpm:9000;") {
		t.Fatalf("切 8.4 失败: %q", to84)
	}
	if strings.Contains(to84, "8.3") {
		t.Fatalf("残留 8.3: %q", to84)
	}
	back := ReplacePhpUpstream(to84, "8.3")
	if back != base {
		t.Fatalf("切回应逐字符相等:\n got=%q\nwant=%q", back, base)
	}
}

// TestReplacePhpUpstream_MultiLocation 仅首个 set 被替换（对齐非全局 replace）
func TestReplacePhpUpstream_MultiLocation(t *testing.T) {
	in := "set $php_upstream php-8.0-fpm:9000;\n# again set $php_upstream php-7.4-fpm:9000;"
	got := ReplacePhpUpstream(in, "8.2")
	if !strings.HasPrefix(got, "set $php_upstream php-8.2-fpm:9000;") {
		t.Fatalf("首个应替换: %q", got)
	}
	// 注释中的第二条保持原样（非全局语义）
	if !strings.Contains(got, "php-7.4-fpm:9000") {
		t.Fatalf("第二条不应被动: %q", got)
	}
}

func TestReplace_EmptyContentPassthrough(t *testing.T) {
	if ReplaceListen("", 8080) != "" {
		t.Error("空 listen 应原样")
	}
	if ReplacePhpUpstream("", "8.4") != "" {
		t.Error("空 upstream 应原样")
	}
}
