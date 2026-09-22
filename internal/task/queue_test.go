// 任务队列单测：FIFO 串行排队、队列详情随广播可见、重复项拒绝、撤回排队项、嵌套防自锁、终态落账
package task

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"phpo/internal/model"
)

// gateStep 阻塞步骤：关闭 release 前不返回，用于制造「运行中」窗口
func gateStep(name string, release chan struct{}) *testStep {
	return &testStep{BaseStep: BaseStep{StepName: name}, rec: &recorder{},
		onRun: func(context.Context) { <-release }}
}

// waitFor 轮询等待条件成立；超时即失败（避免测试挂死）
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

// fakeRecorder 任务账本替身
type fakeRecorder struct {
	mu   sync.Mutex
	ops  []model.Operation
	fail error
}

func (f *fakeRecorder) AppendOperation(op model.Operation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, op)
	return f.fail
}

func (f *fakeRecorder) last(t *testing.T) model.Operation {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ops) == 0 {
		t.Fatal("账本没有任何记录")
	}
	return f.ops[len(f.ops)-1]
}

func (f *fakeRecorder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.ops)
}

// counter 广播计数器（回调并发安全）
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// —— 排队：运行中一项 + FIFO 若干项，按提交顺序串行接手 ——

func TestQueueFIFOAndBoard(t *testing.T) {
	em := &capturingEmitter{}
	m := NewManager(em)
	notify := &counter{}
	m.SetQueueWatcher(func() { notify.inc() })

	relA, relB, relC := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan struct{}, 3)
	go func() {
		m.Run(context.Background(), &Task{ID: "a", Label: "A", Steps: []Step{gateStep("a", relA)}})
		done <- struct{}{}
	}()
	waitFor(t, "A 进入运行", func() bool { return m.Running() })

	waitFor(t, "广播送达", func() bool { return notify.get() > 0 })
	if b := m.Board(); b.Running == nil || b.Running.ID != "a" || len(b.Pending) != 0 {
		t.Fatalf("运行中应只有 A: %+v", b)
	}

	// 逐个提交并等待入队：并发 goroutine 的到达顺序不确定，只有串行提交才能锁定 FIFO 次序
	submit := func(id string, rel chan struct{}) {
		t.Helper()
		go func() {
			_, _ = m.Run(context.Background(), &Task{ID: id, Label: strings.ToUpper(id), Steps: []Step{gateStep(id, rel)}})
			done <- struct{}{}
		}()
		waitFor(t, id+" 进入队列", func() bool {
			for _, p := range m.Board().Pending {
				if p.ID == id {
					return true
				}
			}
			return false
		})
	}
	submit("b", relB)
	submit("c", relC)
	pend := m.Board().Pending
	if pend[0].ID != "b" || pend[1].ID != "c" {
		t.Fatalf("队列必须 FIFO: %+v", pend)
	}

	close(relA)
	waitFor(t, "B 接手运行", func() bool {
		b := m.Board()
		return b.Running != nil && b.Running.ID == "b" && len(b.Pending) == 1
	})
	if !m.Running() {
		t.Fatal("接手期间仍应有运行中任务")
	}
	close(relB)
	waitFor(t, "C 接手运行", func() bool {
		b := m.Board()
		return b.Running != nil && b.Running.ID == "c"
	})
	close(relC)
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("任务未在期限内结束")
		}
	}
	if m.Running() || m.CurrentID() != "" {
		t.Errorf("队列清空后应空闲: running=%v id=%q", m.Running(), m.CurrentID())
	}
	if b := m.Board(); b.Running != nil || len(b.Pending) != 0 {
		t.Errorf("队列清空后面板应为空: %+v", b)
	}
}

// —— 进度：运行项带 step/total，供 UI 画进度条 ——

func TestBoardCarriesProgress(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	var got model.TaskBoard
	probe := &testStep{BaseStep: BaseStep{StepName: "second"}, rec: &recorder{},
		onRun: func(context.Context) { got = m.Board() }}
	if _, err := m.Run(context.Background(), &Task{ID: "p", Label: "P", Meta: model.TaskMeta{Type: "install"},
		Steps: []Step{NewNoopStep("first"), probe}}); err != nil {
		t.Fatal(err)
	}
	if got.Running == nil || got.Running.ID != "p" || got.Running.Type != "install" {
		t.Fatalf("运行项缺失: %+v", got.Running)
	}
	if got.Running.Step != 1 || got.Running.Total != 2 {
		t.Errorf("第二步执行时应已完成 1/2，实得 %d/%d", got.Running.Step, got.Running.Total)
	}
}

// —— 目标定位：服务类任务把 kind/version 带进面板，服务卡片据此亮「执行中…」/「等待中」 ——

func TestBoardCarriesServiceTarget(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	rel := make(chan struct{})
	defer close(rel)
	run := func(id string) {
		if _, err := m.Run(context.Background(), &Task{ID: id, Label: "安装 phpo-mysql-8.4 " + id,
			Meta:  model.TaskMeta{Type: "install", Kind: "mysql", Version: "8.4"},
			Steps: []Step{gateStep(id, rel), NewNoopStep("创建容器")}}); err != nil {
			t.Errorf("%s 应入队成功: %v", id, err) // 在 goroutine 里跑，只能用 Errorf
		}
	}
	go run("a")
	waitFor(t, "A 运行", func() bool {
		b := m.Board()
		return b.Running != nil && b.Running.ID == "a"
	})
	go run("b")
	waitFor(t, "B 排队", func() bool { return len(m.Board().Pending) == 1 })

	if got := m.Board().Running; got.Kind != "mysql" || got.Version != "8.4" || got.Type != "install" {
		t.Errorf("运行项应带服务目标: %+v", got)
	}
	if got := m.Board().Pending[0]; got.Kind != "mysql" || got.Version != "8.4" {
		t.Errorf("排队项应带服务目标: %+v", got)
	}
}

// —— 站点任务把域名带进面板：站点列表行据此亮「执行中…/等待中」，不靠 label 反推 ——

func TestBoardCarriesSiteTarget(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	rel := make(chan struct{})
	defer close(rel)
	run := func(id, label string, meta model.TaskMeta) {
		if _, err := m.Run(context.Background(), &Task{ID: id, Label: label, Meta: meta,
			Steps: []Step{gateStep(id, rel), NewNoopStep("写 vhost")}}); err != nil {
			t.Errorf("%s 应入队成功: %v", id, err) // goroutine 内只能用 Errorf
		}
	}
	go run("a", "切换 demo.test 的 PHP", model.TaskMeta{Type: "php-switch", Domain: "demo.test"})
	waitFor(t, "A 运行", func() bool {
		b := m.Board()
		return b.Running != nil && b.Running.ID == "a"
	})
	go run("b", "删除站点 api.test", model.TaskMeta{Type: "site-remove", Domain: "api.test"})
	waitFor(t, "B 排队", func() bool { return len(m.Board().Pending) == 1 })

	if got := m.Board().Running; got.Domain != "demo.test" || got.Type != "php-switch" || got.Kind != "" {
		t.Errorf("运行项应带站点域名且不带服务种类: %+v", got)
	}
	if got := m.Board().Pending[0]; got.Domain != "api.test" {
		t.Errorf("排队项应带站点域名: %+v", got)
	}
}

// —— 去重：同一操作已在队列中不得叠加 ——

func TestQueueRejectsDuplicate(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	rel := make(chan struct{})
	go func() {
		_, _ = m.Run(context.Background(), &Task{ID: "a", Label: "安装 phpo-nginx-alpine", Steps: []Step{gateStep("a", rel)}})
	}()
	waitFor(t, "A 运行", func() bool { return m.Running() })

	// 首个同名排队项正常入队
	queued := make(chan error, 1)
	go func() {
		_, err := m.Run(context.Background(), &Task{ID: "b", Label: "安装 phpo-php-8.4", Steps: []Step{NewNoopStep("x")}})
		queued <- err
	}()
	waitFor(t, "B 入队", func() bool { return len(m.Board().Pending) == 1 })

	// 再次点同一操作：立即被拒（不等前序），队列仍只有一项
	if _, err := m.Run(context.Background(), &Task{ID: "c", Label: "安装 phpo-php-8.4", Steps: []Step{NewNoopStep("x")}}); !errors.Is(err, ErrQueued) {
		t.Fatalf("同名重复项应得 ErrQueued，实得 %v", err)
	}
	if len(m.Board().Pending) != 1 {
		t.Errorf("重复项不得叠加排队: %+v", m.Board().Pending)
	}
	// 与「运行中」任务同名的重复项同样被拒，不得二次排队
	if _, err := m.Run(context.Background(), &Task{ID: "d", Label: "安装 phpo-nginx-alpine", Steps: []Step{NewNoopStep("x")}}); !errors.Is(err, ErrQueued) {
		t.Fatalf("与运行中任务同名应被拒，实得 %v", err)
	}
	close(rel)
	if err := <-queued; err != nil {
		t.Errorf("首个排队项不应被拒: %v", err)
	}
}

// —— 撤回排队项：尚未执行，无需回滚，直接 cancelled ——

func TestCancelQueuedItem(t *testing.T) {
	em := &capturingEmitter{}
	m := NewManager(em)
	rel := make(chan struct{})
	go func() {
		_, _ = m.Run(context.Background(), &Task{ID: "a", Label: "A", Steps: []Step{gateStep("a", rel)}})
	}()
	waitFor(t, "A 运行", func() bool { return m.Running() })

	type result struct {
		st  model.TaskStatus
		err error
	}
	res := make(chan result, 1)
	go func() {
		st, err := m.Run(context.Background(), &Task{ID: "b", Label: "B", Steps: []Step{NewNoopStep("never")}})
		res <- result{st, err}
	}()
	waitFor(t, "B 入队", func() bool { return len(m.Board().Pending) == 1 })

	if m.CancelQueued("nope") {
		t.Error("撤回不存在的排队项应返回 false")
	}
	if !m.CancelQueued("b") {
		t.Fatal("撤回排队项应返回 true")
	}
	if len(m.Board().Pending) != 0 {
		t.Errorf("撤回后队列应为空: %+v", m.Board().Pending)
	}
	got := <-res
	if got.st != model.TaskCancelled || !errors.Is(got.err, context.Canceled) {
		t.Fatalf("撤回的排队项应得 cancelled/context.Canceled，实得 %v/%v", got.st, got.err)
	}
	// 从未执行的任务不发任何 task:* 事件：队列详情出队即是它的终点
	for _, e := range em.Capture() {
		switch ev := e.Payload.(type) {
		case model.TaskLogEvent:
			if ev.ID == "b" {
				t.Errorf("撤回的排队项不应发 task:log: %+v", ev)
			}
		case model.TaskDoneEvent:
			if ev.ID == "b" {
				t.Errorf("撤回的排队项不应发 task:done: %+v", ev)
			}
		}
	}
	close(rel)
	waitFor(t, "队列清空", func() bool { return !m.Running() })
}

// —— 排队期间前端请求上下文取消：同样撤出队列，不死等 ——

func TestParentCancelWhileQueued(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	rel := make(chan struct{})
	go func() {
		_, _ = m.Run(context.Background(), &Task{ID: "a", Label: "A", Steps: []Step{gateStep("a", rel)}})
	}()
	waitFor(t, "A 运行", func() bool { return m.Running() })

	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() {
		_, err := m.Run(ctx, &Task{ID: "b", Label: "B", Steps: []Step{NewNoopStep("never")}})
		res <- err
	}()
	waitFor(t, "B 入队", func() bool { return len(m.Board().Pending) == 1 })
	cancel()
	if err := <-res; !errors.Is(err, context.Canceled) {
		t.Fatalf("父 ctx 取消应上抛 context.Canceled，实得 %v", err)
	}
	if len(m.Board().Pending) != 0 {
		t.Errorf("父 ctx 取消后应出队: %+v", m.Board().Pending)
	}
	close(rel)
}

// —— 嵌套防自锁：任务内再提任务必须快速失败，不能排队等自己 ——

func TestNestedRunReturnsBusy(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	var innerErr error
	inner := &Task{ID: "inner", Label: "嵌套", Steps: []Step{NewNoopStep("noop")}}
	outer := &Task{ID: "outer", Label: "外层", Steps: []Step{&FuncStep{StepName: "嵌套提交", Exec: func(ctx context.Context, _ StepLog) error {
		_, err := m.Run(ctx, inner)
		innerErr = err
		return nil
	}}}}
	done := make(chan struct{})
	go func() {
		_, _ = m.Run(context.Background(), outer)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("嵌套提交自锁：应快速返回 ErrBusy 而不是排队等自己")
	}
	if !errors.Is(innerErr, ErrBusy) {
		t.Fatalf("嵌套提交应得 ErrBusy，实得 %v", innerErr)
	}
}

// —— 账本：每个任务终态落一行（含失败原因与日志原文）——

func TestLedgerWritten(t *testing.T) {
	rec := &fakeRecorder{}
	m := NewManager(&capturingEmitter{})
	m.SetRecorder(rec)

	if _, err := m.Run(context.Background(), &Task{ID: "ok-1", Label: "安装 phpo-redis-8",
		Meta: model.TaskMeta{Type: "install"}, Steps: []Step{NewNoopStep("拉取镜像"), NewNoopStep("创建容器")}}); err != nil {
		t.Fatal(err)
	}
	op := rec.last(t)
	if op.TaskID != "ok-1" || op.Label != "安装 phpo-redis-8" || op.Op != "install" || op.Status != string(model.TaskSuccess) {
		t.Errorf("成功任务落账字段不全: %+v", op)
	}
	if op.Error != "" || op.DurationMs < 0 || !strings.Contains(op.Logs, "▶ 安装 phpo-redis-8") {
		t.Errorf("成功任务不应带 error 且应存日志原文: %+v", op)
	}

	boom := errors.New("nginx -t 失败：配置语法错误")
	_, err := m.Run(context.Background(), &Task{ID: "bad-1", Label: "切换 PHP 8.1", Meta: model.TaskMeta{Type: "php-switch"},
		Steps: []Step{NewNoopStep("写 vhost"), &testStep{BaseStep: BaseStep{StepName: "校验"}, rec: &recorder{}, err: boom}}})
	if !errors.Is(err, boom) {
		t.Fatalf("失败任务应上抛原错误，实得 %v", err)
	}
	op = rec.last(t)
	if op.Status != string(model.TaskFailed) || !strings.Contains(op.Error, "nginx -t") {
		t.Errorf("失败原因必须落账: %+v", op)
	}
	if !strings.Contains(op.Logs, "校验 失败") || !strings.Contains(op.Logs, "写 vhost 完成") {
		t.Errorf("日志原文应含失败步骤: %q", op.Logs)
	}
}

// —— 账本写失败不得改变任务终态 ——

func TestLedgerFailureDoesNotAffectTask(t *testing.T) {
	rec := &fakeRecorder{fail: errors.New("disk full")}
	m := NewManager(&capturingEmitter{})
	m.SetRecorder(rec)
	st, err := m.Run(context.Background(), &Task{ID: "x", Label: "X", Steps: []Step{NewNoopStep("only")}})
	if st != model.TaskSuccess || err != nil {
		t.Fatalf("落账失败不得污染任务结果: %v %v", st, err)
	}
	if rec.count() != 1 {
		t.Errorf("仍应尝试落账: %d", rec.count())
	}
}

// —— 日志采集上限：超长任务只留尾部，避免账本无界增长 ——

func TestLedgerLogsCapped(t *testing.T) {
	rec := &fakeRecorder{}
	m := NewManager(&capturingEmitter{})
	m.SetRecorder(rec)
	const total = maxLedgerLines + 200
	steps := make([]Step, 0, total)
	for i := 0; i < total; i++ {
		steps = append(steps, NewNoopStep("step"))
	}
	if _, err := m.Run(context.Background(), &Task{ID: "big", Label: "BIG", Steps: steps}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(rec.last(t).Logs, "\n")
	if len(lines) > maxLedgerLines {
		t.Fatalf("日志原文应截尾到 %d 行，实得 %d", maxLedgerLines, len(lines))
	}
	if lines[len(lines)-1] != "step 完成" {
		t.Errorf("截断必须保尾（最后一步的结果在尾部），实得 %q", lines[len(lines)-1])
	}
}

// —— 广播：入队与终态各通知一次，装配层据此发 state:changed ——

func TestBroadcasterFiresOutsideLock(t *testing.T) {
	m := NewManager(&capturingEmitter{})
	var mu sync.Mutex
	var calls []string
	m.SetQueueWatcher(func() {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, "notify")
		// 广播回调里读队列详情：若广播持锁，这里会死锁并使测试超时
		_ = m.Board()
	})
	rel := make(chan struct{})
	go func() {
		_, _ = m.Run(context.Background(), &Task{ID: "a", Label: "A", Steps: []Step{gateStep("a", rel)}})
	}()
	waitFor(t, "A 运行并广播", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) >= 1
	})
	go func() {
		_, _ = m.Run(context.Background(), &Task{ID: "b", Label: "B", Steps: []Step{NewNoopStep("x")}})
	}()
	waitFor(t, "B 入队并广播", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) >= 2
	})
	close(rel)
	waitFor(t, "A 终态并广播", func() bool { return !m.Running() })
	mu.Lock()
	n := len(calls)
	mu.Unlock()
	if n < 3 {
		t.Errorf("每次队列变化都应广播，实得 %d 次", n)
	}
}
