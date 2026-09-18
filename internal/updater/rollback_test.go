// T604b · rollback 单测：无标记 / 新构建接管确认 / 中断下次启动回滚 / 恢复失败保留标记 / Begin 幂等覆盖
package updater

import (
	"errors"
	"testing"
)

func TestRollback_NoMarker(t *testing.T) {
	r := NewRollback(t.TempDir())
	if _, ok, err := r.Load(); ok || err != nil {
		t.Fatal("初始应无标记")
	}
	rolled, err := r.RecoverOnStartup("0.1.0", nil)
	if rolled || err != nil {
		t.Fatalf("无标记不应回滚，rolled=%v err=%v", rolled, err)
	}
}

func TestRollback_ConfirmSuccess(t *testing.T) {
	dir := t.TempDir()
	r := NewRollback(dir)
	if err := r.Begin(PendingUpdate{Target: "0.2.0", Backup: "/old/phpo"}); err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	restored := false
	// 运行版本已等于目标：新构建接管 → 清标记、不触发恢复
	rolled, err := r.RecoverOnStartup("0.2.0", func(string) error { restored = true; return nil })
	if rolled || err != nil {
		t.Fatalf("确认成功不应判为回滚，rolled=%v err=%v", rolled, err)
	}
	if restored {
		t.Fatal("确认成功不应调用 restore")
	}
	if _, ok, _ := r.Load(); ok {
		t.Fatal("确认后标记应被清除")
	}
}

func TestRollback_InterruptedRollback(t *testing.T) {
	dir := t.TempDir()
	r := NewRollback(dir)
	if err := r.Begin(PendingUpdate{Target: "0.2.0", Backup: "/old/phpo"}); err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	var gotBackup string
	// 运行版本仍是旧版：判定上次中断 → 恢复备份 → 清标记
	rolled, err := r.RecoverOnStartup("0.1.0", func(b string) error { gotBackup = b; return nil })
	if !rolled || err != nil {
		t.Fatalf("应判为回滚，rolled=%v err=%v", rolled, err)
	}
	if gotBackup != "/old/phpo" {
		t.Fatalf("restore 应收到备份路径，实得 %q", gotBackup)
	}
	if _, ok, _ := r.Load(); ok {
		t.Fatal("回滚后标记应被清除")
	}
}

func TestRollback_RestoreFailureKeepsMarker(t *testing.T) {
	r := NewRollback(t.TempDir())
	_ = r.Begin(PendingUpdate{Target: "0.2.0"})
	rolled, err := r.RecoverOnStartup("0.1.0", func(string) error { return errors.New("restore failed") })
	if !rolled || err == nil {
		t.Fatalf("恢复失败应 rolled=true 且返回错误，rolled=%v err=%v", rolled, err)
	}
	if _, ok, _ := r.Load(); !ok {
		t.Fatal("恢复失败必须保留标记，供下次启动重试")
	}
}

func TestRollback_BeginIdempotent(t *testing.T) {
	dir := t.TempDir()
	r := NewRollback(dir)
	_ = r.Begin(PendingUpdate{Target: "0.2.0"})
	_ = r.Begin(PendingUpdate{Target: "0.3.0", Backup: "/b"}) // 覆盖
	p, ok, err := r.Load()
	if !ok || err != nil || p.Target != "0.3.0" {
		t.Fatalf("Begin 应幂等覆盖为最新，p=%+v ok=%v err=%v", p, ok, err)
	}
}
