// T505 验收：ConfigService 读回显（宿主优先/回落模板）、原子保存、未知文件名拒绝（防穿越）
// 追加：保存后的「生效步」——php 重启容器、nginx 重载、数据服务只说明不重启（追加需求）
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
)

// fakeRestarter 生效步替身：记录重启调用；running=false 时 Restart 不应被调
type fakeRestarter struct {
	running  bool
	fail     bool
	restarts []string
}

func (f *fakeRestarter) ContainerRunning(context.Context, string) (bool, error) {
	return f.running, nil
}

func (f *fakeRestarter) RestartContainer(_ context.Context, name string) error {
	if f.fail {
		return errors.New("restart boom")
	}
	f.restarts = append(f.restarts, name)
	return nil
}

// fakeReloader 复用 extension_service_test.go 的同名替身（calls 计数 / err 注入），此处不再重复声明

func newConfigSvc(t *testing.T) (*ConfigService, config.Env, *fakeEmitter) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	return NewConfigService(env, task.NewManager(em), &fakeRestarter{running: true}, &fakeReloader{}), env, em
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

// —— 保存后的「生效步」门禁（追加需求）：谁的脸变了动谁，生效失败不判死保存 ——

func TestConfigService_Save_PhpRestartsContainer(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	fr := &fakeRestarter{running: true}
	s := NewConfigService(env, task.NewManager(em), fr, &fakeReloader{})
	if err := s.Save(context.Background(), model.KindPHP, "8.4", []ConfigFile{{Name: "php.ini", Content: "NEW=1"}}); err != nil {
		t.Fatal(err)
	}
	if len(fr.restarts) != 1 || fr.restarts[0] != "phpo-php-8.4" {
		t.Fatalf("php 配置保存后应重启 phpo-php-8.4，实得 %v", fr.restarts)
	}
}

func TestConfigService_Save_PhpStoppedNoRestart(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	fr := &fakeRestarter{running: false}
	s := NewConfigService(env, task.NewManager(em), fr, &fakeReloader{})
	if err := s.Save(context.Background(), model.KindPHP, "8.4", []ConfigFile{{Name: "php.ini", Content: "NEW=1"}}); err != nil {
		t.Fatal(err)
	}
	if len(fr.restarts) != 0 {
		t.Fatalf("未运行的容器不应被拉起，实得 %v", fr.restarts)
	}
	if got := mustRead(t, filepath.Join(env.PHPOHome, "php/8.4/conf/php.ini")); got != "NEW=1" {
		t.Fatalf("配置仍应落盘: %q", got)
	}
}

func TestConfigService_Save_PhpRestartFailStillSaves(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	s := NewConfigService(env, task.NewManager(em), &fakeRestarter{running: true, fail: true}, &fakeReloader{})
	if err := s.Save(context.Background(), model.KindPHP, "8.4", []ConfigFile{{Name: "php.ini", Content: "NEW=1"}}); err != nil {
		t.Fatalf("重启失败不判死保存（文件已落盘，回滚等于把保存变成失败）: %v", err)
	}
	if got := mustRead(t, filepath.Join(env.PHPOHome, "php/8.4/conf/php.ini")); got != "NEW=1" {
		t.Fatalf("配置应已落盘: %q", got)
	}
}

func TestConfigService_Save_NginxReloads(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	fr := &fakeReloader{}
	s := NewConfigService(env, task.NewManager(em), &fakeRestarter{running: true}, fr)
	if err := s.Save(context.Background(), model.KindNginx, "alpine", []ConfigFile{{Name: "nginx.conf", Content: "NEW=1"}}); err != nil {
		t.Fatal(err)
	}
	if fr.calls != 1 {
		t.Fatalf("nginx 配置保存后应重载 1 次，实得 %d", fr.calls)
	}
}

func TestConfigService_Save_NginxReloadFailStillSaves(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	s := NewConfigService(env, task.NewManager(em), &fakeRestarter{running: true}, &fakeReloader{err: errors.New("reload boom")})
	if err := s.Save(context.Background(), model.KindNginx, "alpine", []ConfigFile{{Name: "nginx.conf", Content: "NEW=1"}}); err != nil {
		t.Fatalf("重载失败不判死保存: %v", err)
	}
	if got := mustRead(t, filepath.Join(env.PHPOHome, "nginx/alpine/conf/nginx.conf")); got != "NEW=1" {
		t.Fatalf("配置应已落盘: %q", got)
	}
}

func TestConfigService_Save_DbNoRestartNoReload(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	em := &fakeEmitter{}
	fr := &fakeRestarter{running: true}
	fl := &fakeReloader{}
	s := NewConfigService(env, task.NewManager(em), fr, fl)
	if err := s.Save(context.Background(), model.KindMySQL, "8.4", []ConfigFile{{Name: "my.cnf", Content: "NEW=1"}}); err != nil {
		t.Fatal(err)
	}
	if len(fr.restarts) != 0 || fl.calls != 0 {
		t.Fatalf("数据服务不自动重启/重载，实得 %v / %d", fr.restarts, fl.calls)
	}
}
