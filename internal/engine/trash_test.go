// T403 回收站：移入/恢复/幂等 + 永久清理
package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func mkSite(t *testing.T, www, domain string) string {
	t.Helper()
	dir := filepath.Join(www, domain)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 1;"), 0o644)
	return dir
}

func TestTrash_MoveRestore(t *testing.T) {
	base := t.TempDir()
	www := filepath.Join(base, "www")
	tr := NewTrash(filepath.Join(base, "trash"))
	siteDir := mkSite(t, www, "demo.test")

	dest, err := tr.Move(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(siteDir); !os.IsNotExist(e) {
		t.Fatal("移动后原目录应消失")
	}
	if _, e := os.Stat(filepath.Join(dest, "index.php")); e != nil {
		t.Fatalf("回收站应含源码: %v", e)
	}
	// 恢复到原位
	if err := tr.Restore(dest, siteDir); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(filepath.Join(siteDir, "index.php")); e != nil {
		t.Fatal("恢复后源码应回到原位")
	}
}

func TestTrash_MoveIdempotent(t *testing.T) {
	base := t.TempDir()
	www := filepath.Join(base, "www")
	tr := NewTrash(filepath.Join(base, "trash"))
	siteDir := mkSite(t, www, "demo.test")

	first, _ := tr.Move(siteDir)
	// 二次移动同一原目录（已不存在）：应幂等返回目标、不报错
	second, err := tr.Move(siteDir)
	if err != nil {
		t.Fatalf("重复移动应幂等: %v", err)
	}
	if second != first {
		t.Fatalf("目标路径应稳定: %s vs %s", second, first)
	}
}

func TestTrash_Purge(t *testing.T) {
	base := t.TempDir()
	tr := NewTrash(filepath.Join(base, "trash"))
	siteDir := mkSite(t, filepath.Join(base, "www"), "demo.test")
	dest, _ := tr.Move(siteDir)
	if err := tr.Purge(dest); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal("Purge 后条目应消失")
	}
}
