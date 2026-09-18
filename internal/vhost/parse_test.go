package vhost

import "testing"

func TestParse_Basics(t *testing.T) {
	content := `server {
    listen 8080;
    server_name demo.test;
    root /var/www/demo.test;
}`
	p := Parse(content)
	if p.Port != 8080 {
		t.Errorf("port 应为 8080，实得 %d", p.Port)
	}
	if p.Root != "/var/www/demo.test" {
		t.Errorf("root 解析错误: %q", p.Root)
	}
}

func TestParse_EmptyAndOutOfRange(t *testing.T) {
	if got := Parse(""); got.Port != 0 || got.Root != "" {
		t.Errorf("空内容应零值，实得 %+v", got)
	}
	// 端口越界（>65535）不采纳
	p := Parse("listen 99999;\nroot /x;")
	if p.Port != 0 {
		t.Errorf("越界端口应为 0，实得 %d", p.Port)
	}
	if p.Root != "/x" {
		t.Errorf("root 仍应解析: %q", p.Root)
	}
}

func TestParse_MultiLocationFirstWins(t *testing.T) {
	content := `server {
    listen 80;
    location / {
        root /var/www/a;
    }
    location /admin {
        root /var/www/b;
    }
}`
	p := Parse(content)
	if p.Port != 80 {
		t.Errorf("port=%d", p.Port)
	}
	// 首个 root 生效
	if p.Root != "/var/www/a" {
		t.Errorf("应取首个 root，实得 %q", p.Root)
	}
}

func TestParse_IDNDomain(t *testing.T) {
	// server_name 不参与解析；仅确保含 unicode 的正文仍能取 port/root
	p := Parse("listen 443;\nserver_name 例.test;\nroot /var/www/idn;")
	if p.Port != 443 || p.Root != "/var/www/idn" {
		t.Errorf("IDN 正文解析错误: %+v", p)
	}
}
