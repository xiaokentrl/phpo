// T305 验收：三阶段编排语义（失败全回滚 / PreClean 失败不回滚）+
// install/uninstall/start/stop 各重复执行 100 次结果一致、零残留（§5.13.1 幂等/原子/可恢复）
package engine

import (
	"context"
	"errors"
	"testing"
)

// ---- 编排语义：用调用记录验证阶段顺序与回滚触发 ----

type recorder struct{ calls []string }

func (r *recorder) op(mark string, errs map[string]error) Op {
	fail := func(phase string) error { return errs[phase] }
	return Op{
		Name: mark,
		PreClean: func(context.Context) error {
			r.calls = append(r.calls, "pre")
			return fail("pre")
		},
		Execute: func(context.Context) error {
			r.calls = append(r.calls, "exec")
			return fail("exec")
		},
		PostVerify: func(context.Context) error {
			r.calls = append(r.calls, "verify")
			return fail("verify")
		},
		Rollback: func(context.Context) error {
			r.calls = append(r.calls, "rollback")
			return nil
		},
	}
}

func TestOp_HappyPathOrder(t *testing.T) {
	var r recorder
	if err := r.op("x", nil).Run(context.Background()); err != nil {
		t.Fatalf("无错应返回 nil，得 %v", err)
	}
	want := []string{"pre", "exec", "verify"}
	if !eq(r.calls, want) {
		t.Fatalf("阶段顺序 = %v，期望 %v", r.calls, want)
	}
}

func TestOp_PreCleanFailureNoRollback(t *testing.T) {
	var r recorder
	preErr := errors.New("pre failed")
	err := r.op("x", map[string]error{"pre": preErr}).Run(context.Background())
	if !errors.Is(err, preErr) {
		t.Fatalf("应透传 PreClean 错误，得 %v", err)
	}
	for _, c := range r.calls {
		if c == "exec" || c == "rollback" {
			t.Fatalf("PreClean 失败后不应执行 Execute/Rollback，calls=%v", r.calls)
		}
	}
}

func TestOp_ExecuteFailureRollsBack(t *testing.T) {
	var r recorder
	execErr := errors.New("exec failed")
	err := r.op("x", map[string]error{"exec": execErr}).Run(context.Background())
	if !errors.Is(err, execErr) {
		t.Fatalf("应返回 Execute 原始错误（不被回滚掩盖），得 %v", err)
	}
	if !contains(r.calls, "rollback") || contains(r.calls, "verify") {
		t.Fatalf("Execute 失败应回滚且跳过 Verify，calls=%v", r.calls)
	}
}

func TestOp_VerifyFailureRollsBack(t *testing.T) {
	var r recorder
	verr := errors.New("verify failed")
	err := r.op("x", map[string]error{"verify": verr}).Run(context.Background())
	if !errors.Is(err, verr) {
		t.Fatalf("应返回 PostVerify 错误，得 %v", err)
	}
	if !contains(r.calls, "rollback") {
		t.Fatalf("Verify 失败应回滚，calls=%v", r.calls)
	}
}

func TestOp_NilPhasesAreNoop(t *testing.T) {
	if err := (Op{Name: "empty"}).Run(context.Background()); err != nil {
		t.Fatalf("空 Op 应无操作、返回 nil，得 %v", err)
	}
}

// ---- 幂等收敛：内存假世界模拟 install/uninstall/start/stop ----

// fakeWorld 极简容器世界：name -> 是否运行
type fakeWorld struct{ containers map[string]bool }

func newWorld() *fakeWorld { return &fakeWorld{containers: map[string]bool{}} }

func (w *fakeWorld) snapshot() map[string]bool {
	cp := make(map[string]bool, len(w.containers))
	for k, v := range w.containers {
		cp[k] = v
	}
	return cp
}

func (w *fakeWorld) installOp(name string) Op {
	return Op{
		Name:       "install " + name,
		PreClean:   func(context.Context) error { delete(w.containers, name); return nil }, // 清同名
		Execute:    func(context.Context) error { w.containers[name] = true; return nil },  // 建并运行
		PostVerify: requireState(w, name, true, true),
		Rollback:   func(context.Context) error { delete(w.containers, name); return nil },
	}
}

func (w *fakeWorld) uninstallOp(name string) Op {
	return Op{
		Name:       "uninstall " + name,
		Execute:    func(context.Context) error { delete(w.containers, name); return nil },
		PostVerify: requireState(w, name, false, false),
	}
}

func (w *fakeWorld) startOp(name string) Op {
	return Op{
		Name: "start " + name,
		Execute: func(context.Context) error {
			if _, ok := w.containers[name]; !ok {
				return errors.New("容器不存在，无法启动")
			}
			w.containers[name] = true
			return nil
		},
		PostVerify: requireState(w, name, true, true),
	}
}

func (w *fakeWorld) stopOp(name string) Op {
	return Op{
		Name: "stop " + name,
		Execute: func(context.Context) error {
			if _, ok := w.containers[name]; !ok {
				return errors.New("容器不存在，无法停止")
			}
			w.containers[name] = false
			return nil
		},
		PostVerify: requireState(w, name, true, false),
	}
}

// requireState 生成一个校验容器存在/运行态的 PostVerify 闭包
func requireState(w *fakeWorld, name string, wantExists, wantRunning bool) func(context.Context) error {
	return func(context.Context) error {
		got, exists := w.containers[name]
		if exists != wantExists {
			return errors.New("存在性漂移")
		}
		if wantExists && got != wantRunning {
			return errors.New("运行态漂移")
		}
		return nil
	}
}

// TestOp_Idempotent100x 对四种操作各重复执行 100 次，断言每次都成功、终态与单次一致、零残留。
func TestOp_Idempotent100x(t *testing.T) {
	name := "phpo-php-8.4"

	t.Run("install", func(t *testing.T) {
		// 起始为一个脏的同名残留（已停止）——PreClean 应抹平差异
		w := newWorld()
		w.containers[name] = false
		first := w.installOp(name)
		if err := first.Run(context.Background()); err != nil {
			t.Fatalf("首次 install 失败: %v", err)
		}
		golden := w.snapshot()
		for i := 0; i < 100; i++ {
			if err := w.installOp(name).Run(context.Background()); err != nil {
				t.Fatalf("第 %d 次 install 失败: %v", i, err)
			}
		}
		assertWorld(t, w.snapshot(), map[string]bool{name: true})
		if !sameWorld(golden, w.snapshot()) {
			t.Fatalf("100 次后终态与单次不一致")
		}
	})

	t.Run("start", func(t *testing.T) {
		w := newWorld()
		w.containers[name] = true // 已安装且在跑
		for i := 0; i < 100; i++ {
			if err := w.startOp(name).Run(context.Background()); err != nil {
				t.Fatalf("第 %d 次 start 失败: %v", i, err)
			}
		}
		assertWorld(t, w.snapshot(), map[string]bool{name: true})
	})

	t.Run("stop", func(t *testing.T) {
		w := newWorld()
		w.containers[name] = true
		for i := 0; i < 100; i++ {
			if err := w.stopOp(name).Run(context.Background()); err != nil {
				t.Fatalf("第 %d 次 stop 失败: %v", i, err)
			}
		}
		assertWorld(t, w.snapshot(), map[string]bool{name: false})
	})

	t.Run("uninstall", func(t *testing.T) {
		w := newWorld()
		w.containers[name] = true
		for i := 0; i < 100; i++ {
			if err := w.uninstallOp(name).Run(context.Background()); err != nil {
				t.Fatalf("第 %d 次 uninstall 失败: %v", i, err)
			}
		}
		if len(w.containers) != 0 {
			t.Fatalf("卸载后应零残留，实得 %v", w.containers)
		}
	})
}

// ---- 断言小工具 ----

func assertWorld(t *testing.T, got, want map[string]bool) {
	t.Helper()
	if !sameWorld(got, want) {
		t.Fatalf("世界状态 = %v，期望 %v", got, want)
	}
}

func sameWorld(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}
