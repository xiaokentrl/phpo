package vhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
)

type fakeValidator struct{ err error }

func (f fakeValidator) Validate(context.Context, string, string) error { return f.err }

func tmpEnv(t *testing.T) (Manager, string) {
	t.Helper()
	dir := t.TempDir()
	env := config.DerivePaths(dir, filepath.Join(dir, "www"))
	return *New(env), env.NginxSitesRoot
}

func TestSave_WritesFile(t *testing.T) {
	m, sitesRoot := tmpEnv(t)
	content := "server { listen 80; }"
	if err := m.Save(context.Background(), fakeValidator{}, "demo.test", content); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(sitesRoot, "demo.test.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != content {
		t.Fatalf("落盘内容不符: %q", b)
	}
}

// TestSave_BlockedByValidator 硬红线 2：校验失败不得落盘
func TestSave_BlockedByValidator(t *testing.T) {
	m, sitesRoot := tmpEnv(t)
	boom := errors.New("nginx: configuration test failed")
	err := m.Save(context.Background(), fakeValidator{err: boom}, "demo.test", "bad config")
	if err == nil {
		t.Fatal("校验失败应报错")
	}
	if _, statErr := os.Stat(filepath.Join(sitesRoot, "demo.test.conf")); !os.IsNotExist(statErr) {
		t.Fatal("校验失败不应落盘")
	}
}

func TestDeleteFile(t *testing.T) {
	m, sitesRoot := tmpEnv(t)
	if err := m.Save(context.Background(), fakeValidator{}, "demo.test", "x"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sitesRoot, "demo.test.conf")
	if err := m.DeleteFile("demo.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("文件应被删除")
	}
	// 二次删除（不存在）应视为成功
	if err := m.DeleteFile("demo.test"); err != nil {
		t.Fatalf("删除不存在文件应成功: %v", err)
	}
}

// fileValidator 需 conf 落盘才能核验；通过 err 控制 nginx -t 成败
type fileValidator struct {
	err  error
	seen string // 记录被校验时磁盘上的正文，验证确已先写盘
}

func (f *fileValidator) Validate(context.Context, string, string) error { return nil }
func (f *fileValidator) ValidateFile(_ context.Context, path string) error {
	if b, err := os.ReadFile(path); err == nil {
		f.seen = string(b)
	}
	return f.err
}

// TestSave_FileValidator_Creates 新站点：先写盘→校验通过→保留文件
func TestSave_FileValidator_Creates(t *testing.T) {
	m, sitesRoot := tmpEnv(t)
	fv := &fileValidator{}
	content := "server { listen 8080; }"
	if err := m.Save(context.Background(), fv, "demo.test", content); err != nil {
		t.Fatal(err)
	}
	if fv.seen != content {
		t.Fatalf("校验时应已见新正文，实得 %q", fv.seen)
	}
	b, _ := os.ReadFile(filepath.Join(sitesRoot, "demo.test.conf"))
	if string(b) != content {
		t.Fatalf("落盘内容不符: %q", b)
	}
}

// TestSave_FileValidator_BlockedNewFile 新站点校验失败：写盘后回滚为「无文件」
func TestSave_FileValidator_BlockedNewFile(t *testing.T) {
	m, sitesRoot := tmpEnv(t)
	fv := &fileValidator{err: errors.New("unexpected }")}
	if err := m.Save(context.Background(), fv, "demo.test", "bad"); err == nil {
		t.Fatal("校验失败应报错")
	}
	if _, e := os.Stat(filepath.Join(sitesRoot, "demo.test.conf")); !os.IsNotExist(e) {
		t.Fatal("新站点校验失败应删除回滚")
	}
}

// TestSave_FileValidator_RestorePrev 已存在站点改配校验失败：回滚为原内容，不丢旧配置
func TestSave_FileValidator_RestorePrev(t *testing.T) {
	m, sitesRoot := tmpEnv(t)
	path := filepath.Join(sitesRoot, "demo.test.conf")
	if err := m.Save(context.Background(), &fileValidator{}, "demo.test", "OLD"); err != nil {
		t.Fatal(err)
	}
	fv := &fileValidator{err: errors.New("bad upstream")}
	if err := m.Save(context.Background(), fv, "demo.test", "NEW"); err == nil {
		t.Fatal("校验失败应报错")
	}
	if fv.seen != "NEW" {
		t.Fatalf("校验时应见新正文 NEW，实得 %q", fv.seen)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "OLD" {
		t.Fatalf("校验失败应回滚为原内容 OLD，实得 %q", b)
	}
}

// TestExecValidator_ReportsNginxError T406：nginx -t 失败时错误含命令原始输出
func TestExecValidator_ReportsNginxError(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("nginx: [emerg] unknown directive \"foo\" in /etc/nginx/sites/demo.test.conf:3"), errors.New("exit status 1")
	}
	ev := NewExecValidator(run, "docker", "exec", "phpo-nginx-alpine", "nginx", "-t")
	err := ev.ValidateFile(context.Background(), "/ignored")
	if err == nil {
		t.Fatal("nginx -t 失败应报错")
	}
	if !strings.Contains(err.Error(), "unknown directive") {
		t.Fatalf("错误应含 nginx 原文，实得: %v", err)
	}
}

func TestExecValidator_OK(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, arg ...string) ([]byte, error) {
		gotName, gotArgs = name, arg
		return nil, nil
	}
	ev := NewExecValidator(run, "docker", "exec", "phpo-nginx-alpine", "nginx", "-t")
	if err := ev.ValidateFile(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if gotName != "docker" || len(gotArgs) != 4 || gotArgs[0] != "exec" || gotArgs[2] != "nginx" || gotArgs[3] != "-t" {
		t.Fatalf("命令拼装错误: %s %v", gotName, gotArgs)
	}
}
