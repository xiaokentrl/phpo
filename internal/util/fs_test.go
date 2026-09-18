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
