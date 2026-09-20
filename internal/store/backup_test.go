package store

import (
	"path/filepath"
	"testing"

	"phpo/internal/model"
)

// TestBackupTo 验证 VACUUM INTO 快照可独立打开并读回已落库的运行态（备份→异机恢复的字节级前提）
// 配置真相（密码/端口）已在 config.yaml，不在 SQLite 快照内；此处只校验运行态物化。
func TestBackupTo(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "phpo.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.SetInstalled("mysql", "8.4", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSite(model.Site{Domain: "demo.test", Port: 8080, PHP: "8.4", Root: "~/www/demo.test", Rewrite: "laravel"}); err != nil {
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

	got, err := cp.BuildSnapshot()
	if err != nil {
		t.Fatalf("读回快照失败: %v", err)
	}
	if len(got.Installed["mysql"]) != 1 || got.Installed["mysql"][0] != "8.4" {
		t.Fatalf("快照 installed 不符，实得 %v", got.Installed["mysql"])
	}
	if len(got.Sites) != 1 || got.Sites[0].Domain != "demo.test" || got.Sites[0].Port != 8080 {
		t.Fatalf("快照 sites 不符，实得 %+v", got.Sites)
	}
}
