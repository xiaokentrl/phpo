// 站点步骤的回滚对账（T403）：AddHosts / RemoveHosts 各自必须可回滚（§5.13.1 可恢复性），
// 且只回滚本步真正改动过的条目——「已存在跳过」与提权被拒都不算改动，反向删改会破坏用户手工写的行
package steps

import (
	"context"
	"errors"
	"testing"

	"phpo/internal/vhost/hosts"
)

// logFake 收集步骤日志行（StepLog 最小实现）
type logFake struct{ lines []string }

func (l *logFake) Log(level, text string) { l.lines = append(l.lines, level+" "+text) }

// hostsFake 记录 Add/Remove 的调用与返回；一次调用一个结果
type hostsFake struct {
	addRes, rmRes hosts.Result
	addErr, rmErr error
	added, gone   []string
}

func (f *hostsFake) Add(domain string) (hosts.Result, error) {
	f.added = append(f.added, domain)
	return f.addRes, f.addErr
}

func (f *hostsFake) Remove(domain string) (hosts.Result, error) {
	f.gone = append(f.gone, domain)
	return f.rmRes, f.rmErr
}

func (f *hostsFake) addCalls() int { return len(f.added) }
func (f *hostsFake) rmCalls() int  { return len(f.gone) }

func TestAddHostsRollbackRemovesWhatItAdded(t *testing.T) {
	h := &hostsFake{addRes: hosts.Result{Changed: true}}
	s := NewAddHosts("写入 hosts", h, "demo.test")
	if err := s.Execute(context.Background(), &logFake{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := s.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if h.rmCalls() != 1 || h.gone[0] != "demo.test" {
		t.Fatalf("本步已写入条目，回滚必须撤下，实际调用 %v", h.gone)
	}
}

func TestAddHostsRollbackSkipsWhenNothingWritten(t *testing.T) {
	cases := []struct {
		name string
		h    *hostsFake
	}{
		{"幂等跳过（条目已在）", &hostsFake{addRes: hosts.Result{Changed: false}}},
		{"提权被拒（仅告警）", &hostsFake{addRes: hosts.Result{Warning: "无法修改 hosts（/etc/hosts）。请以管理员身份运行。"}}},
		{"写入报错", &hostsFake{addErr: errors.New("permission denied")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewAddHosts("写入 hosts", tc.h, "demo.test")
			if err := s.Execute(context.Background(), &logFake{}); err != nil {
				t.Fatalf("hosts 失败只告警、不阻断建站，Execute 应返回 nil: %v", err)
			}
			if err := s.Rollback(context.Background()); err != nil {
				t.Fatalf("Rollback: %v", err)
			}
			if tc.h.rmCalls() != 0 {
				t.Fatalf("本步未写入任何条目，回滚不得删 hosts，实际调用 %v", tc.h.gone)
			}
		})
	}
}

func TestAddHostsRollbackSurfacesRemoveError(t *testing.T) {
	h := &hostsFake{addRes: hosts.Result{Changed: true}, rmErr: errors.New("polkit 拒绝")}
	s := NewAddHosts("写入 hosts", h, "demo.test")
	if err := s.Execute(context.Background(), &logFake{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := s.Rollback(context.Background()); err == nil {
		t.Fatal("回滚失败必须上抛，由 task 层聚合记账，不得静默")
	}
}

func TestRemoveHostsRollbackReAddsWhatItRemoved(t *testing.T) {
	h := &hostsFake{rmRes: hosts.Result{Changed: true}}
	s := NewRemoveHosts("回收 hosts", h, "demo.test")
	if err := s.Execute(context.Background(), &logFake{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := s.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if h.addCalls() != 1 || h.added[0] != "demo.test" {
		t.Fatalf("删站任务失败即站点没删，域名解析不该丢，实际 %v", h.added)
	}
}

func TestRemoveHostsRollbackSkipsWhenNothingRemoved(t *testing.T) {
	cases := []struct {
		name string
		h    *hostsFake
	}{
		{"无该条目", &hostsFake{rmRes: hosts.Result{Changed: false}}},
		{"提权被拒（仅告警）", &hostsFake{rmRes: hosts.Result{Warning: "无法修改 hosts（/etc/hosts）。请以管理员身份运行。"}}},
		{"回收报错", &hostsFake{rmErr: errors.New("permission denied")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewRemoveHosts("回收 hosts", tc.h, "demo.test")
			if err := s.Execute(context.Background(), &logFake{}); err != nil {
				t.Fatalf("hosts 回收失败只告警、不阻断删站，Execute 应返回 nil: %v", err)
			}
			if err := s.Rollback(context.Background()); err != nil {
				t.Fatalf("Rollback: %v", err)
			}
			if tc.h.addCalls() != 0 {
				t.Fatalf("本步未删除任何条目，回滚不得往 hosts 里加行，实际 %v", tc.h.added)
			}
		})
	}
}
