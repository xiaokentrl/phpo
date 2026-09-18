// 任务引擎单测：三段式（Pre-Clean→Execute→Post-Verify）、事件序列、回滚、取消、单飞
package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"phpo/internal/model"
)

// capturingEmitter 本包测试用的事件捕获器（不依赖上层 app 包，避免测试期 import 环）
type capturingEmitter struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	Name    string
	Payload any
}

func (c *capturingEmitter) Emit(name string, p any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedEvent{name, p})
}

func (c *capturingEmitter) Capture() []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedEvent, len(c.events))
	copy(out, c.events)
	return out
}

// —— 测试替身步骤 ——

type recorder struct {
	mu       sync.Mutex
	events   []string
	rollacks []string
	cleanups []string
}

func (r *recorder) add(list *[]string, s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*list = append(*list, s)
}

type testStep struct {
	BaseStep
	rec        *recorder
	err        error
	onRun      func(ctx context.Context)
	retCtxErr  bool // 模拟 pull 中途被取消：Execute 观测 ctx 取消即返回 ctx.Err()
	cancelable bool
}

func (s *testStep) Execute(ctx context.Context, _ StepLog) error {
	if s.onRun != nil {
		s.onRun(ctx)
	}
	s.rec.add(&s.rec.events, s.StepName)
	if s.retCtxErr {
		return ctx.Err()
	}
	return s.err
}

func (s *testStep) Rollback(context.Context) error {
	s.rec.add(&s.rec.rollacks, s.StepName)
	return nil
}

func (s *testStep) Cleanup()         { s.rec.add(&s.rec.cleanups, s.StepName) }
func (s *testStep) Cancelable() bool { return s.cancelable }

func newStep(rec *recorder, name string) *testStep {
	return &testStep{BaseStep: BaseStep{StepName: name}, rec: rec}
}

// —— 事件辅助：从捕获事件流取 task:done 载荷 ——

func doneOf(t *testing.T, cap *capturingEmitter) model.TaskDoneEvent {
	t.Helper()
	var last model.TaskDoneEvent
	found := false
	for _, e := range cap.Capture() {
		if e.Name == "task:done" {
			last = e.Payload.(model.TaskDoneEvent)
			found = true
		}
	}
	if !found {
		t.Fatal("未捕获到 task:done 事件")
	}
	return last
}

// —— 成功路径 ——

func TestRunSuccess(t *testing.T) {
	cap := &capturingEmitter{}
	m := NewManager(cap)
	rec := &recorder{}
	applyCalled := 0
	task := &Task{
		ID:    "t1",
		Steps: []Step{newStep(rec, "a"), newStep(rec, "b")},
		Apply: func() error { applyCalled++; return nil },
	}
	status, err := m.Run(context.Background(), task)
	if err != nil || status != model.TaskSuccess {
		t.Fatalf("期望 success，得 status=%v err=%v", status, err)
	}
	if applyCalled != 1 {
		t.Fatalf("Apply 应调用一次，得 %d", applyCalled)
	}
	// 两个步骤均执行且均清理
	if len(rec.events) != 2 || len(rec.cleanups) != 2 {
		t.Fatalf("执行/清理计数不符: %v / %v", rec.events, rec.cleanups)
	}
	// progress 发射两次（1/2, 2/2）
	prog := 0
	for _, e := range cap.Capture() {
		if e.Name == "task:progress" {
			prog++
		}
	}
	if prog != 2 {
		t.Fatalf("应有 2 次 progress，得 %d", prog)
	}
	if doneOf(t, cap).Status != model.TaskSuccess {
		t.Fatal("done 事件应为 success")
	}
}

// —— 失败回滚（逆序，含失败步）——

func TestRunFailureRollsBackReverse(t *testing.T) {
	cap := &capturingEmitter{}
	m := NewManager(cap)
	rec := &recorder{}
	boom := errors.New("boom")
	s1, s2 := newStep(rec, "a"), newStep(rec, "b")
	s2.err = boom
	applyCalled := 0
	task := &Task{
		ID:    "t2",
		Steps: []Step{s1, s2},
		Apply: func() error { applyCalled++; return nil },
	}
	status, err := m.Run(context.Background(), task)
	if status != model.TaskFailed || !errors.Is(err, boom) {
		t.Fatalf("期望 failed+boom，得 %v / %v", status, err)
	}
	if applyCalled != 0 {
		t.Fatal("失败时 Apply 不应执行")
	}
	// 回滚逆序：先 b 再 a
	if len(rec.rollacks) != 2 || rec.rollacks[0] != "b" || rec.rollacks[1] != "a" {
		t.Fatalf("回滚顺序应为 [b a]，得 %v", rec.rollacks)
	}
	// 无论失败，Cleanup 对两步都执行
	if len(rec.cleanups) != 2 {
		t.Fatalf("失败也应清理两步，得 %v", rec.cleanups)
	}
	if doneOf(t, cap).Status != model.TaskFailed {
		t.Fatal("done 事件应为 failed")
	}
}

// —— Pre-Clean 失败：步骤不执行 ——

func TestPreCleanFailureSkipsSteps(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	rec := &recorder{}
	preErr := errors.New("pre")
	task := &Task{
		ID:       "t3",
		PreClean: func(context.Context) error { return preErr },
		Steps:    []Step{newStep(rec, "a")},
	}
	status, err := m.Run(context.Background(), task)
	if status != model.TaskFailed || !errors.Is(err, preErr) {
		t.Fatalf("Pre-Clean 失败应 failed，得 %v/%v", status, err)
	}
	if len(rec.events) != 0 {
		t.Fatalf("Pre-Clean 失败时步骤不应执行，得 %v", rec.events)
	}
}

// —— Post-Verify 失败触发回滚 ——

func TestVerifyFailureRollsBack(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	rec := &recorder{}
	verr := errors.New("verify")
	task := &Task{
		ID:     "t4",
		Steps:  []Step{newStep(rec, "a")},
		Verify: func(context.Context) error { return verr },
	}
	status, _ := m.Run(context.Background(), task)
	if status != model.TaskFailed {
		t.Fatalf("Verify 失败应 failed，得 %v", status)
	}
	if len(rec.rollacks) != 1 || rec.rollacks[0] != "a" {
		t.Fatalf("Verify 失败应回滚已完成步骤，得 %v", rec.rollacks)
	}
}

// —— 取消：可取消步骤执行中请求取消 ——

func TestCancelDuringRun(t *testing.T) {
	cap := &capturingEmitter{}
	m := NewManager(cap)
	rec := &recorder{}
	s1 := newStep(rec, "a")
	s2 := newStep(rec, "b")
	// 第一步执行时请求取消
	s1.onRun = func(context.Context) { m.Cancel() }
	s2.cancelable = true // 本步会因 ctx.Done 在循环入口被拦下
	task := &Task{ID: "t5", Steps: []Step{s1, s2}}
	status, _ := m.Run(context.Background(), task)
	if status != model.TaskCancelled {
		t.Fatalf("取消应得 cancelled，得 %v", status)
	}
	// b 未执行
	if len(rec.events) != 1 {
		t.Fatalf("取消后不应继续执行后续步骤，得 %v", rec.events)
	}
	if doneOf(t, cap).Status != model.TaskCancelled {
		t.Fatal("done 事件应为 cancelled")
	}
}

// 步骤执行中途被取消（如 pull 阻塞时收到取消）：应判为 cancelled 而非 failed，
// 且回滚包含当前步（状态回原点），Cleanup 仍执行（清空临时目录）。
func TestCancelMidStepSurfacesCancelled(t *testing.T) {
	cap := &capturingEmitter{}
	m := NewManager(cap)
	rec := &recorder{}
	s1 := newStep(rec, "a")
	s2 := newStep(rec, "pull")
	s2.cancelable = true
	// pull 执行中请求取消，并像真实 pull 那样返回 ctx.Err()
	s2.onRun = func(context.Context) { m.Cancel() }
	s2.retCtxErr = true
	applyCalled := 0
	task := &Task{
		ID:    "t6",
		Steps: []Step{s1, s2},
		Apply: func() error { applyCalled++; return nil },
	}
	status, err := m.Run(context.Background(), task)
	if status != model.TaskCancelled {
		t.Fatalf("中途取消应得 cancelled，得 %v (err=%v)", status, err)
	}
	if applyCalled != 0 {
		t.Fatal("取消时 Apply 不应落地（状态回原点）")
	}
	// 回滚应包含已完成 + 当前步，逆序：[pull a]
	if len(rec.rollacks) != 2 || rec.rollacks[0] != "pull" || rec.rollacks[1] != "a" {
		t.Fatalf("取消应逆序回滚 [pull a]，得 %v", rec.rollacks)
	}
	// 无论取消，两步 Cleanup 均执行
	if len(rec.cleanups) != 2 {
		t.Fatalf("取消也应清理两步，得 %v", rec.cleanups)
	}
	if doneOf(t, cap).Status != model.TaskCancelled {
		t.Fatal("done 事件应为 cancelled")
	}
}

// —— 单飞：并发提交第二个返回 ErrBusy ——

func TestSingleFlightBusy(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	release := make(chan struct{})
	blocker := &testStep{BaseStep: BaseStep{StepName: "blk"}, rec: &recorder{},
		onRun: func(context.Context) { <-release }}
	go func() { m.Run(context.Background(), &Task{ID: "long", Steps: []Step{blocker}}) }()
	// 等运行标志置起
	for !m.Running() {
		time.Sleep(time.Millisecond)
	}
	_, err := m.Run(context.Background(), &Task{ID: "second"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("并发第二个任务应得 ErrBusy，得 %v", err)
	}
	close(release)
	for m.Running() {
		time.Sleep(time.Millisecond)
	}
}
