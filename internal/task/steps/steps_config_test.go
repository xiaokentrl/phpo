// T505 验收：SaveConfigStep 原子写入 + 失败回滚
// 用「目标文件的某级父路径是普通文件」制造与权限无关的写入失败（root 亦失败），验证已写文件逆序还原
package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", p, err)
	}
	return string(b)
}

func TestSaveConfigStep_WritesAllFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a", "one.conf")
	one := NewSaveConfigStep("save", []ConfigFile{{Host: good, Content: "NEW"}})
	if err := one.Execute(context.Background(), nopLog{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, good); got != "NEW" {
		t.Fatalf("应写入 NEW，实得 %q", got)
	}
}

func TestSaveConfigStep_RestoresOriginalsOnFailure(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "one.conf")
	if err := os.WriteFile(good, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	// bad 的父路径阻塞.conf 先建成普通文件 → MkdirAll 必然失败（非权限相关）
	blocker := filepath.Join(dir, "阻塞.conf")
	bad := filepath.Join(blocker, "two.conf")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewSaveConfigStep("save", []ConfigFile{
		{Host: good, Content: "NEW"},
		{Host: bad, Content: "NEW"},
	})
	if err := s.Execute(context.Background(), nopLog{}); err == nil {
		t.Fatal("第二文件应写入失败")
	}
	if err := s.Rollback(context.Background()); err != nil {
		t.Fatalf("回滚不应报错: %v", err)
	}
	if got := readFile(t, good); got != "OLD" {
		t.Fatalf("回滚应还原 one.conf=OLD，实得 %q", got)
	}
}

func TestSaveConfigStep_AbortsBeforeWriteOnBadBackup(t *testing.T) {
	dir := t.TempDir()
	// 首个 Host 的父是普通文件 → 备份趟读失败（非 NotExist）应在写任何文件前中止
	blocker := filepath.Join(dir, "block.conf")
	bad := filepath.Join(blocker, "x.conf")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "keep.conf")
	s := NewSaveConfigStep("save", []ConfigFile{
		{Host: bad, Content: "NEW"},
		{Host: good, Content: "NEW"},
	})
	if err := s.Execute(context.Background(), nopLog{}); err == nil {
		t.Fatal("备份失败应中止")
	}
	if len(s.wrote) != 0 {
		t.Fatalf("备份中止后不得写入任何文件，wrote=%v", s.wrote)
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Fatalf("不得产生半写文件，keep.conf stat=%v", err)
	}
}

// nopLog 空日志接收器
type nopLog struct{}

func (nopLog) Log(string, string) {}
