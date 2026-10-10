// wipeTaskLedger：启动/退出两条路径共用的账本清空——账本存在即清空，不存在绝不借机建库。
package app

import (
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/model"
	"phpo/internal/store"
)

func storeWithRows(t *testing.T, dbPath string, rows int) *store.Store {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for i := 0; i < rows; i++ {
		if err := s.AppendOperation(model.Operation{Op: "rebuild", Status: "success", Label: "重建 mysql 8.4"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func countOps(t *testing.T, s *store.Store) int {
	t.Helper()
	ops, err := s.ListOperations(100)
	if err != nil {
		t.Fatal(err)
	}
	return len(ops)
}

func TestWipeTaskLedger_WipesExistingDb(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "phpo.db")
	st := storeWithRows(t, dbPath, 3)

	wipeTaskLedger(dbPath, st)

	if n := countOps(t, st); n != 0 {
		t.Fatalf("清空后账本应 0 条，剩 %d 条", n)
	}
}

func TestWipeTaskLedger_MissingDbCreatesNothing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "phpo.db") // 刻意不创建

	wipeTaskLedger(dbPath, nil) // 不触库，nil 即可

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("账本不存在时不得借机建库 (stat err=%v)", err)
	}
}

func TestWipeTaskLedger_MissingFileOnOpenDbKeepsNothingToWipe(t *testing.T) {
	// 账本文件被外部删掉但句柄仍开着：按文件存在性判定，不清（下次启动再清）——不应 panic
	dbPath := filepath.Join(t.TempDir(), "phpo.db")
	st := storeWithRows(t, dbPath, 1)
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	wipeTaskLedger(dbPath, st)
}
