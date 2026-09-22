// 任务管理器：串行执行队列（同一时刻仅一个任务在跑，后来的按 FIFO 排队），
// 持有发射器、取消句柄、队列详情与终态账本回调
package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"phpo/internal/model"
)

// ErrBusy 任务内嵌套提交（排队等于等自己 → 自锁），必须快速失败
var ErrBusy = errors.New("任务内不得嵌套提交任务")

// ErrQueued 同一操作已在队列中（运行中或排队中），拒绝重复叠加
var ErrQueued = errors.New("同一操作已在任务队列中")

type taskIDKey struct{}

// handoff 排队项的了结结果：ctx 非空表示移交执行权，err 非空表示出队原因
type handoff struct {
	ctx context.Context
	err error
}

// ticket 排队项；ch 缓冲 1，只由 Manager 在锁内 settleLocked 投递一次
type ticket struct {
	brief   model.TaskBrief
	ch      chan handoff
	settled bool
}

// Manager 任务引擎入口
type Manager struct {
	mu      sync.Mutex
	em      Emitter
	cur     *model.TaskBrief
	cancel  context.CancelFunc
	pending []*ticket
	rec     Recorder
	onQueue func() // 队列变化通知（装配层注入：重建权威快照并发 state:changed）
	onDone  func() // 任务终态通知（装配层注入：§5.13.9「每次任务后校准」，不分成败）
}

func NewManager(em Emitter) *Manager {
	if em == nil {
		em = NopEmitter{}
	}
	return &Manager{em: em}
}

// SetRecorder 注入任务账本（store.Store.AppendOperation 满足）；未注入则不落账
func (m *Manager) SetRecorder(r Recorder) { m.rec = r }

// SetQueueWatcher 注入队列变化回调。回调在 Manager 锁外调用，可安全回读 Board()。
func (m *Manager) SetQueueWatcher(fn func()) { m.onQueue = fn }

// SetDoneWatcher 注入任务终态回调（成功/失败/取消各一次）。回调在执行权移交后、锁外调用，
// 可安全访问 Docker 与快照；未获执行权的任务不触发。
func (m *Manager) SetDoneWatcher(fn func()) { m.onDone = fn }

// Running 当前是否有任务在跑
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur != nil
}

// CurrentID 当前运行任务 ID（无任务时空串）
func (m *Manager) CurrentID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return ""
	}
	return m.cur.ID
}

// Board 任务队列详情（快照 tasks 字段的唯一来源）
func (m *Manager) Board() model.TaskBoard {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := model.TaskBoard{Pending: make([]model.TaskBrief, 0, len(m.pending))}
	if m.cur != nil {
		cur := *m.cur
		b.Running = &cur
	}
	for _, tk := range m.pending {
		b.Pending = append(b.Pending, tk.brief)
	}
	return b
}

// Cancel 请求取消当前运行中任务
func (m *Manager) Cancel() {
	m.mu.Lock()
	c := m.cancel
	m.mu.Unlock()
	if c != nil {
		c()
	}
}

// CancelQueued 撤回一个尚未执行的排队项（未执行即无需回滚）。返回是否命中。
func (m *Manager) CancelQueued(id string) bool {
	m.mu.Lock()
	for i, tk := range m.pending {
		if tk.brief.ID != id {
			continue
		}
		m.pending = append(m.pending[:i], m.pending[i+1:]...)
		m.settleLocked(tk, handoff{err: context.Canceled})
		m.mu.Unlock()
		m.notify()
		return true
	}
	m.mu.Unlock()
	return false
}

// notify 队列变化通知（锁外调用，回调内可回读 Board）
func (m *Manager) notify() {
	if m.onQueue != nil {
		m.onQueue()
	}
}

// notifyDone 任务终态通知（锁外调用；调用方须先移交执行权，回调读到的 Board 已不含本任务）
func (m *Manager) notifyDone() {
	if m.onDone != nil {
		m.onDone()
	}
}

// settleLocked 一次性了结排队项（重复调用无效）；调用方必须持锁
func (m *Manager) settleLocked(tk *ticket, h handoff) {
	if tk.settled {
		return
	}
	tk.settled = true
	tk.ch <- h
}

// acquire 取得执行权：空闲立即开始；否则 FIFO 排队等前序任务移交。
func (m *Manager) acquire(parent context.Context, t *Task) (context.Context, func(), error) {
	m.mu.Lock()
	// 嵌套提交：parent 派生自本 Manager 的运行中任务，排队等于等自己 → 快速失败
	if m.cur != nil && parent.Value(taskIDKey{}) == m.cur.ID {
		m.mu.Unlock()
		return nil, func() {}, ErrBusy
	}
	brief := briefOf(t)
	if m.duplicateLocked(brief.Label) {
		m.mu.Unlock()
		return nil, func() {}, fmt.Errorf("%w：%s", ErrQueued, brief.Label)
	}
	if m.cur == nil {
		ctx := m.beginLocked(brief)
		m.mu.Unlock()
		m.notify()
		return ctx, m.release, nil
	}
	tk := &ticket{brief: brief, ch: make(chan handoff, 1)}
	m.pending = append(m.pending, tk)
	m.mu.Unlock()
	m.notify()

	select {
	case h := <-tk.ch:
		if h.err != nil {
			return nil, func() {}, h.err
		}
		return h.ctx, m.release, nil

	case <-parent.Done():
		// 排队期间请求上下文取消：尽力出队；若移交已先到达则照常执行（本项目不因请求取消而改写已提交的写操作）
		m.mu.Lock()
		for i, p := range m.pending {
			if p == tk {
				m.pending = append(m.pending[:i], m.pending[i+1:]...)
				break
			}
		}
		m.settleLocked(tk, handoff{err: parent.Err()})
		m.mu.Unlock()
		m.notify()
		h := <-tk.ch
		if h.ctx != nil {
			return h.ctx, m.release, nil
		}
		return nil, func() {}, h.err
	}
}

// beginLocked 置为运行中并派生可取消 ctx；调用方必须持锁
func (m *Manager) beginLocked(brief model.TaskBrief) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, taskIDKey{}, brief.ID)
	brief.StartedAt = time.Now()
	m.cur = &brief
	m.cancel = cancel
	return ctx
}

// release 归还执行权并移交队首；无队首则置空
func (m *Manager) release() {
	m.mu.Lock()
	m.cur = nil
	m.cancel = nil
	if len(m.pending) > 0 {
		tk := m.pending[0]
		m.pending = m.pending[1:]
		m.settleLocked(tk, handoff{ctx: m.beginLocked(tk.brief)})
	}
	m.mu.Unlock()
	m.notify()
}

// duplicateLocked 同 label 操作是否已在运行或排队；空 label 不参与去重
func (m *Manager) duplicateLocked(label string) bool {
	if label == "" {
		return false
	}
	if m.cur != nil && m.cur.Label == label {
		return true
	}
	for _, tk := range m.pending {
		if tk.brief.Label == label {
			return true
		}
	}
	return false
}

// setProgress 记录运行中任务的步骤进度并广播队列变化（快照 tasks 与 task:progress 同步推进）
func (m *Manager) setProgress(step, total int) {
	m.mu.Lock()
	if m.cur == nil {
		m.mu.Unlock()
		return
	}
	m.cur.Step, m.cur.Total = step, total
	m.mu.Unlock()
	m.notify()
}

// briefOf 面板摘要。Kind/Version 只有服务类任务才有、Domain 只有站点类任务才有：
// 服务卡片与站点行据此把「运行中/排队中」精确标到被操作的那一项上，不必靠 label 文案反推。
func briefOf(t *Task) model.TaskBrief {
	return model.TaskBrief{
		ID:      t.ID,
		Label:   label(t),
		Type:    t.Meta.Type,
		Kind:    t.Meta.Kind,
		Version: t.Meta.Version,
		Domain:  t.Meta.Domain,
		Total:   len(t.Steps),
	}
}
