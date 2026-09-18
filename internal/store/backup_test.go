package store

import (
	"path/filepath"
	"testing"
)

// TestBackupTo 验证 VACUUM INTO 快照可独立打开并读回已落库的密码/端口（备份→异机恢复的字节级前提）
func TestBackupTo(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "phpo.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.SetPassword("mysql", "8.4", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetServicePort("mysql", "8.4", 3307); err != nil {
		t.Fatal(err)
	}

	snap := filepath.Join(t.TempDir(), "snapshot.db") // VACUUM INTO 要求目标不存在
	if err := s.BackupTo(snap); err != nil {
		t.Fatalf("BackupTo 失败: %v", err)
	}

	cp, err := Open(snap)
	if err != nil {
		t.Fatalf("打开快照失败: %v", err)
	}
	defer cp.Close()

	if pw, ok, _ := cp.GetPassword("mysql", "8.4"); !ok || pw != "s3cret" {
		t.Fatalf("快照密码不符，实得 %q ok=%v", pw, ok)
	}
	if p, ok, _ := cp.GetServicePort("mysql", "8.4"); !ok || p != 3307 {
		t.Fatalf("快照端口不符，实得 %d ok=%v", p, ok)
	}
}
