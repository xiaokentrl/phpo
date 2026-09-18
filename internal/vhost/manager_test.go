package vhost

import (
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

func newMgr() *Manager {
	env := config.DerivePaths("~/phpo", "~/www")
	m := New(env)
	m.Sync([]model.Site{{
		Domain: "demo.test", Port: 80, PHP: "8.4",
		Root: "~/www/demo.test", Rewrite: "laravel",
	}})
	return m
}

func TestManager_Compute_Golden(t *testing.T) {
	m := newMgr()
	got := m.Get("demo.test")
	// root 应映射为容器路径，上游精确 php-8.4-fpm:9000，listen 80
	for _, want := range []string{
		"listen 80;",
		"root /var/www/demo.test;",
		"set $php_upstream php-8.4-fpm:9000;",
		"# Laravel 5+ / Lumen",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("vhost 应含 %q，实得:\n%s", want, got)
		}
	}
}

func TestManager_ApplyPhp_Precise(t *testing.T) {
	m := newMgr()
	// 非手改态：ApplyPhp 走重算
	got := m.ApplyPhp("demo.test", "8.3")
	if !strings.Contains(got, "php-8.3-fpm:9000;") || strings.Contains(got, "8.4") {
		t.Fatalf("重算切换失败:\n%s", got)
	}
}

func TestManager_ApplyPort_Idempotent(t *testing.T) {
	m := newMgr()
	first := m.ApplyPort("demo.test", 8080)
	second := m.ApplyPort("demo.test", 8080)
	if first != second {
		t.Fatalf("重复改同端口应幂等:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(first, "listen 8080;") {
		t.Fatalf("端口未生效:\n%s", first)
	}
}

// TestManager_Customized_Preserved 手改后 applyPort/applyPhp 走就地替换而非重算，保留手改结构
func TestManager_Customized_Preserved(t *testing.T) {
	m := newMgr()
	manual := "server {\n    listen 80;\n    set $php_upstream php-8.4-fpm:9000;\n    # MY CUSTOM COMMENT\n}"
	m.SetContent("demo.test", manual)
	site, _ := m.Site("demo.test")
	if !site.VhostCustomized {
		t.Fatal("手改后应标记 customized")
	}
	// applyPort 应保留自定义注释，仅改 listen
	after := m.ApplyPort("demo.test", 81)
	if !strings.Contains(after, "# MY CUSTOM COMMENT") {
		t.Fatalf("手改结构应保留:\n%s", after)
	}
	if !strings.Contains(after, "listen 81;") {
		t.Fatalf("端口应改:\n%s", after)
	}
	// applyPhp 保留自定义，仅换上游
	after2 := m.ApplyPhp("demo.test", "8.2")
	if !strings.Contains(after2, "# MY CUSTOM COMMENT") || !strings.Contains(after2, "php-8.2-fpm:9000;") {
		t.Fatalf("手改态切 PHP 失败:\n%s", after2)
	}
	// Regenerate 丢弃手改，重算回标准
	regen := m.Regenerate("demo.test")
	if strings.Contains(regen, "# MY CUSTOM COMMENT") {
		t.Fatalf("Regenerate 应丢弃手改:\n%s", regen)
	}
}

func TestManager_RebuildAll_PrunesOrphans(t *testing.T) {
	m := newMgr()
	_ = m.Get("demo.test") // 建立缓存
	m.Sync([]model.Site{}) // 移除站点
	m.RebuildAll()
	if m.Get("demo.test") != "" {
		t.Fatal("站点移除后 vhost 应被清理")
	}
}

func TestManager_UnknownDomain(t *testing.T) {
	m := newMgr()
	if m.Get("nope.test") != "" {
		t.Fatal("未知域名 Get 应返回空")
	}
	if m.ApplyPhp("nope.test", "8.4") != "" {
		t.Fatal("未知域名 ApplyPhp 应返回空")
	}
}
