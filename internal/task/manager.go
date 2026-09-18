// 任务管理器：单飞调度（同一时刻仅一个运行中任务），持有发射器与取消句柄
package task

import (
	"context"
	"errors"
	"sync"
)

// ErrBusy 已有一任务在跑（后端权威兜底，前端 preflight 已先行拦截）
var ErrBusy = errors.New("已有任务运行中")

// Manager 任务引擎入口
type Manager struct {
	mu      sync.Mutex
	em      Emitter
	running bool
	cancel  context.CancelFunc
	id      string
}

func NewManager(em Emitter) *Manager {
	if em == nil {
		em = NopEmitter{}
	}
	return &Manager{em: em}
}

// Running 当前是否有任务在跑
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// CurrentID 当前运行任务 ID（无任务时空串）
func (m *Manager) CurrentID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.id
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

// acquire 单飞获取运行许可；返回可取消 ctx 与 release
func (m *Manager) acquire(id string) (context.Context, func(), error) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil, func() {}, ErrBusy
	}
	m.running = true
	m.id = id
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()
	return ctx, func() {
		m.mu.Lock()
		m.running = false
		m.cancel = nil
		m.mu.Unlock()
		cancel()
	}, nil
}
