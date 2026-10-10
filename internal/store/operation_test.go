// ClearOperations：覆盖安装清账本——operations 全清，其余表一张不碰
package store

import (
	"testing"

	"phpo/internal/model"
)

func TestClearOperations_WipesLedgerOnly(t *testing.T) {
	s := openStore(t)
	for i := 0; i < 3; i++ {
		if err := s.AppendOperation(model.Operation{Op: "rebuild", Status: "success", Label: "重建 mysql 8.4"}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM operations`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("预备 3 行，实际 %d (err=%v)", n, err)
	}
	if err := s.ClearOperations(); err != nil {
		t.Fatalf("清空账本: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM operations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("清空后 operations 应 0 行，实际 %d (err=%v)", n, err)
	}
	// 清空可重复（幂等）：再清一次不报错
	if err := s.ClearOperations(); err != nil {
		t.Fatalf("重复清空应安全: %v", err)
	}
}
