// 这一份只锁 runDeletes 的循环口径：不中断、取消优先、回执等长同序、失败项不计体积。
// 「具体哪一种对象怎么删」依赖 Docker 守护进程，由 test/integration 的真环境用例覆盖；
// 这里把 del 抽成参数，因此在没有 Docker 的机器上也能把上面四条钉死。
package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"phpo/internal/model"
)

// stubDelete 记下被调用过哪些项，并按 refsFail 里的 Ref 决定这颗成不成。
type stubDelete struct {
	called   []string
	refsFail map[string]bool
}

func (s *stubDelete) del(ctx context.Context, op DeleteOp) (int64, string, error) {
	s.called = append(s.called, op.Ref)
	if s.refsFail[op.Ref] {
		return 7, "", errors.New("占用中，删不掉")
	}
	return op.Size, "", nil
}

func opsOf(refs ...string) []DeleteOp {
	ops := make([]DeleteOp, 0, len(refs))
	for i, r := range refs {
		ops = append(ops, DeleteOp{Row: "row." + r, Kind: DelContainer, Ref: r, Size: int64(100 + i)})
	}
	return ops
}

// TestRunDeletes_KeepsGoingAfterOneFails 是这层存在的理由：勾了 5 项，第 3 项失败，
// 剩下 2 项照样要删完，界面逐行点名——不是整单落空。
func TestRunDeletes_KeepsGoingAfterOneFails(t *testing.T) {
	s := &stubDelete{refsFail: map[string]bool{"c": true}}
	ops := opsOf("a", "b", "c", "d", "e")

	res := runDeletes(context.Background(), ops, nil, s.del)

	if len(res) != len(ops) {
		t.Fatalf("回执数应等于指令数（失败的也要有一条），实得 %d", len(res))
	}
	if strings.Join(s.called, ",") != "a,b,c,d,e" {
		t.Fatalf("一颗失败不得挡住后面的，实调序列 %s", strings.Join(s.called, ","))
	}
	if res[2].Err == nil {
		t.Fatal("第 3 项应带失败原因")
	}
	// 失败项的体积不计进「已释放」——否则等于虚报磁盘空间。
	if res[2].Freed != 0 {
		t.Fatalf("失败项 Freed 必须为 0，实得 %d", res[2].Freed)
	}
	for _, want := range []int{0, 1, 3, 4} {
		if res[want].Err != nil || res[want].Freed != ops[want].Size {
			t.Fatalf("第 %d 项应删成并按扫描体积记账，得 Freed=%d Err=%v", want, res[want].Freed, res[want].Err)
		}
	}
	// 回执与指令一一对应、同序：服务层按 Row 逐行铺日志，错一位就点名错对象。
	for i := range ops {
		if res[i].Row != ops[i].Row || res[i].Ref != ops[i].Ref || res[i].Kind != ops[i].Kind {
			t.Fatalf("第 %d 条回执与指令不对应：%+v vs %+v", i, res[i], ops[i])
		}
	}
}

// TestRunDeletes_CancelStopsAndReceiptsRemainder 取消优先：已删的不回滚，没删的必须各有一条
// ctx.Err() 的回执——它们没被动过，凭空消失等于界面少报几行。
func TestRunDeletes_CancelStopsAndReceiptsRemainder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &stubDelete{}
	ops := opsOf("a", "b", "c")

	// 删完第一项即取消，模拟用户在任务抽屉点了「撤回」。
	res := runDeletes(ctx, ops, func(DeleteResult) { cancel() }, s.del)

	if len(s.called) != 1 {
		t.Fatalf("取消后不得再发起新的删除，实调 %v", s.called)
	}
	if len(res) != len(ops) {
		t.Fatalf("剩余项也要各有一条回执，实得 %d", len(res))
	}
	if res[0].Err != nil {
		t.Fatalf("取消前那一项已完成，不该被改成失败：%v", res[0].Err)
	}
	for _, r := range res[1:] {
		if !errors.Is(r.Err, context.Canceled) {
			t.Fatalf("未删除的项应带 context.Canceled，实得 %v", r.Err)
		}
		if r.Freed != 0 {
			t.Fatalf("没删掉的项 Freed 必须为 0，实得 %d", r.Freed)
		}
	}
}

// TestRunDeletes_PerItemCallback 删除有时长（构建缓存清扫可能几十秒），界面要边删边看到行：
// 回调次数与顺序必须和回执一致，且每次回调拿到的就是刚做完那一项。
func TestRunDeletes_PerItemCallback(t *testing.T) {
	ops := opsOf("a", "b", "c")
	var seen []string
	s := &stubDelete{refsFail: map[string]bool{"b": true}}

	runDeletes(context.Background(), ops, func(r DeleteResult) {
		seen = append(seen, r.Ref)
	}, s.del)

	if strings.Join(seen, ",") != "a,b,c" {
		t.Fatalf("每项删完就该回调一次，实得 %s", strings.Join(seen, ","))
	}
}

// TestRunDeletes_EmptyAndNilCallback 空清单与不关心逐行回执的调用方都不该炸。
func TestRunDeletes_EmptyAndNilCallback(t *testing.T) {
	s := &stubDelete{}
	if got := runDeletes(context.Background(), nil, nil, s.del); len(got) != 0 {
		t.Fatalf("空清单应得空回执，实得 %d 条", len(got))
	}
	if len(s.called) != 0 {
		t.Fatal("空清单不该发起任何删除")
	}
	if got := runDeletes(context.Background(), opsOf("a"), nil, s.del); len(got) != 1 {
		t.Fatalf("nil 回调仍要返回回执，实得 %d 条", len(got))
	}
}

// TestRunDeletes_TrashPathOnlyOnSuccess 「先进回收站再删」的那几项要把落点交给服务层登记 7 天到期。
// 口径只有一条：删成才记落点。没删成的那项留空——说一件还在原处的东西「已经进回收站」是假账。
func TestRunDeletes_TrashPathOnlyOnSuccess(t *testing.T) {
	ops := opsOf("a", "b")
	del := func(_ context.Context, op DeleteOp) (int64, string, error) {
		if op.Ref == "b" {
			return 0, "/trash/b", errors.New("提权被拒")
		}
		return op.Size, "/trash/a", nil
	}

	res := runDeletes(context.Background(), ops, nil, del)
	if res[0].TrashPath != "/trash/a" {
		t.Fatalf("删成的项要带回回收站落点，实得 %q", res[0].TrashPath)
	}
	if res[1].TrashPath != "" {
		t.Fatalf("失败项不得声称已进回收站，实得 %q", res[1].TrashPath)
	}
}

// TestDeleteOpKindsMatchModel 两处词表必须一字不差：服务层把界面行翻成删除指令、又把回执翻回
// 界面行，靠的就是这串字符串对上；model 侧改了这里不改，那一项就会掉进「不认识要删的类型」。
func TestDeleteOpKindsMatchModel(t *testing.T) {
	for kind, want := range map[string]string{
		DelContainer:    string(model.ResContainer),
		DelImage:        string(model.ResImage),
		DelVolume:       string(model.ResVolume),
		DelNetwork:      string(model.ResNetwork),
		DelPlugin:       string(model.ResPlugin),
		DelSwarmService: string(model.ResSwarmService),
		DelSwarmConfig:  string(model.ResSwarmConfig),
		DelSwarmSecret:  string(model.ResSwarmSecret),
	} {
		if kind != want {
			t.Fatalf("%s 的值漂移成了 %q（界面对不上账）", want, kind)
		}
	}
}
