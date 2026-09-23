package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite_CreatesDirAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "a.conf")
	if err := AtomicWrite(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("内容不符: %q err=%v", b, err)
	}
	// 覆盖写
	if err := AtomicWrite(path, []byte("world")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "world" {
		t.Fatalf("覆盖失败: %q", b)
	}
	// 目录内不应残留临时文件
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("应只剩目标文件，实得 %d 项", len(entries))
	}
}

// TestWriteFile_NormalizesPerm phpo 产出物一律 0777：umask（本机 0022）不得把权限位削成 0755
func TestWriteFile_NormalizesPerm(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sub", "deep")
	path := filepath.Join(dir, "file.txt")
	if err := WriteFile(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, filepath.Join(root, "sub"), DirPerm)
	assertPerm(t, dir, DirPerm)
	assertPerm(t, path, FilePerm)

	// 覆盖写同样归一
	if err := WriteFile(path, []byte("yy")); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, path, FilePerm)
}

// TestMkdirAll_NormalizesExistingLeaf 旧装机留下的 0755 目录要在下一次写入时归一
func TestMkdirAll_NormalizesExistingLeaf(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "old")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, dir, DirPerm)
}

// TestAtomicWrite_NormalizesPerm 原子写的最终模式取自临时文件，故临时文件必须归一后再 rename
func TestAtomicWrite_NormalizesPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "b.conf")
	if err := AtomicWrite(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, filepath.Dir(path), DirPerm)
	assertPerm(t, path, FilePerm)
}

func assertPerm(t *testing.T, p string, want os.FileMode) {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != want {
		t.Fatalf("%s 权限 = %o，期望 %o", p, got, want)
	}
}
