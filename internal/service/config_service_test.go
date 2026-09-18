// T505 验收：ConfigService 读回显（宿主优先/回落模板）、原子保存、未知文件名拒绝（防穿越）
package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
)

func newConfigSvc(t *testing.T) (*ConfigService, config.Env, *fakeEmitter) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	return NewConfigService(env, task.NewManager(em)), env, em
}

func TestConfigService_GetFiles_FallsBackToTemplate(t *testing.T) {
	s, _, _ := newConfigSvc(t)
	files, err := s.GetFiles(model.KindMySQL, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "my.cnf" {
		t.Fatalf("mysql 应返回单文件 my.cnf，实得 %+v", files)
	}
	if !strings.Contains(files[0].Path, "mysql/8.4/conf/my.cnf") {
		t.Fatalf("路径应为模板权威路径，实得 %q", files[0].Path)
	}
	if files[0].Content == "" {
		t.Fatal("缺失宿主文件时应回落模板默认内容")
	}
}

func TestConfigService_GetFiles_ReadsHost(t *testing.T) {
	s, env, _ := newConfigSvc(t)
	host := filepath.Join(env.PHPOHome, "mysql/8.4/conf/my.cnf")
	if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte("CUSTOM=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := s.GetFiles(model.KindMySQL, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Content != "CUSTOM=1" {
		t.Fatalf("应回显宿主内容，实得 %q", files[0].Content)
	}
}

func TestConfigService_Save_WritesAndEmits(t *testing.T) {
	s, env, em := newConfigSvc(t)
	if err := s.Save(context.Background(), model.KindMySQL, "8.4", []ConfigFile{{Name: "my.cnf", Content: "NEW=1"}}); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(env.PHPOHome, "mysql/8.4/conf/my.cnf")
	if got := mustRead(t, host); got != "NEW=1" {
		t.Fatalf("保存应落盘 NEW=1，实得 %q", got)
	}
	if !em.has("task:done") {
		t.Fatalf("保存应经 task.Manager 发 task:done，实得 %v", em.events)
	}
}

func TestConfigService_Save_RejectsUnknownName(t *testing.T) {
	s, env, _ := newConfigSvc(t)
	err := s.Save(context.Background(), model.KindMySQL, "8.4", []ConfigFile{{Name: "../evil.cnf", Content: "x"}})
	if err == nil {
		t.Fatal("未知/穿越文件名应被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(env.PHPOHome, "evil.cnf")); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝后不得写出任何文件，stat=%v", statErr)
	}
}

func TestConfigService_Save_EmptyIsNoop(t *testing.T) {
	s, _, em := newConfigSvc(t)
	if err := s.Save(context.Background(), model.KindMySQL, "8.4", nil); err != nil {
		t.Fatal(err)
	}
	if em.has("task:done") {
		t.Fatal("空改动不应触发任务")
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", p, err)
	}
	return string(b)
}
