// 旧版 postgresql.conf 的日志段自愈门禁：崩溃循环的 pgsql 必须靠「启用」就能救回来，
// 且只允许改那三行默认原文——用户自己编辑过的配置一个字都不动。
package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

type capLog struct{ lines []string }

func (c *capLog) Log(level, text string) { c.lines = append(c.lines, level+" "+text) }

func (c *capLog) text() string { return strings.Join(c.lines, "\n") }

// pgConf 写出某内容的 postgresql.conf，返回其宿主路径
func pgConf(t *testing.T, env config.Env, version, content string) string {
	t.Helper()
	path := filepath.Join(env.RootFor(string(model.KindPgsql), version), "conf", "postgresql.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const oldPgConf = "# phpo · PostgreSQL 17\nshared_buffers = 256MB\n" + oldPgLogBlock + "log_timezone = 'Asia/Shanghai'"

func TestHealPgLogging_RewritesOldDefault(t *testing.T) {
	env := config.DerivePaths(t.TempDir(), t.TempDir())
	log := &capLog{}
	path := pgConf(t, env, "17", oldPgConf)
	healPgLogging(env, model.KindPgsql, "17", log)
	got := read(t, path)
	if strings.Contains(got, "logging_collector = on") {
		t.Fatalf("旧默认日志段应被改写: %q", got)
	}
	if !strings.Contains(got, "logging_collector = off") || !strings.Contains(got, "log_destination = 'stderr'") {
		t.Fatalf("应换成 stderr + collector off: %q", got)
	}
	// 用户改过的其它行必须原样保留
	for _, keep := range []string{"shared_buffers = 256MB", "log_timezone = 'Asia/Shanghai'", "# phpo · PostgreSQL 17"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("原有内容 %q 被改掉: %q", keep, got)
		}
	}
	if !strings.Contains(log.text(), "stderr") {
		t.Fatalf("改写了配置必须报一行日志: %q", log.text())
	}
}

func TestHealPgLogging_KeepsUserEditedBlock(t *testing.T) {
	env := config.DerivePaths(t.TempDir(), t.TempDir())
	log := &capLog{}
	content := "logging_collector = on\nlog_directory = '/data/pglog'\n"
	path := pgConf(t, env, "17", content)
	healPgLogging(env, model.KindPgsql, "17", log)
	if got := read(t, path); got != content {
		t.Fatalf("非默认写法不得改写: %q", got)
	}
	if !strings.Contains(log.text(), "不自动改写") {
		t.Fatalf("应提醒用户该项未自动改写: %q", log.text())
	}
}

func TestHealPgLogging_IdempotentAndTolerant(t *testing.T) {
	env := config.DerivePaths(t.TempDir(), t.TempDir())
	// 已是新内容：不再产生日志
	healed := "log_destination = 'stderr'\nlogging_collector = off\n"
	path := pgConf(t, env, "17", healed)
	log := &capLog{}
	healPgLogging(env, model.KindPgsql, "17", log)
	if read(t, path) != healed || log.lines != nil {
		t.Fatalf("已修复的配置应原样跳过，实得 %q / 日志 %q", read(t, path), log.text())
	}
	// 文件不存在（尚未落盘）：静默跳过，不影响启动
	healPgLogging(env, model.KindPgsql, "99", log)
	// 非 pgsql：不碰文件
	healPgLogging(env, model.KindRedis, "17", log)
	if read(t, path) != healed {
		t.Fatalf("其它服务不应改 pgsql 配置")
	}
	if len(log.lines) != 0 {
		t.Fatalf("后三种情形都应静默，实得日志 %q", log.text())
	}
}

// prepareService 对全新安装直接给出正确内容（不留旧写法的残留，也不报修复日志）
func TestPrepareService_FreshPgsqlHasNoCollector(t *testing.T) {
	home := t.TempDir()
	env := config.DerivePaths(home, filepath.Join(home, "www"))
	log := &capLog{}
	if err := prepareService(env, model.KindPgsql, "17", log); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(env.RootFor("pgsql", "17"), "conf", "postgresql.conf"))
	if strings.Contains(got, oldPgLogBlock) {
		t.Fatalf("新装即应是无 logging_collector 的配置: %q", got)
	}
	if strings.Contains(log.text(), "改回 stderr") {
		t.Fatalf("新装不该报修复日志: %q", log.text())
	}
}
