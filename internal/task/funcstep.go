// FuncStep 用闭包装配一个 Step，使服务层能把原子操作编排进 task.Manager 而无需反向依赖
// （task 层不得 import service；故由 service 传入 Execute/Rollback 闭包）
package task

import "context"

// FuncStep 由函数指针构成；RB/Clean 可为 nil，Cancel 标记是否可中途取消
type FuncStep struct {
	BaseStep
	Exec  func(ctx context.Context, log StepLog) error
	RB    func(ctx context.Context) error
	Clean func()
	Can   bool
}

func (s *FuncStep) Name() string { return s.StepName }

func (s *FuncStep) Execute(ctx context.Context, log StepLog) error {
	if s.Exec == nil {
		return nil
	}
	return s.Exec(ctx, log)
}

func (s *FuncStep) Rollback(ctx context.Context) error {
	if s.RB == nil {
		return nil
	}
	return s.RB(ctx)
}

func (s *FuncStep) Cleanup() {
	if s.Clean != nil {
		s.Clean()
	}
}

func (s *FuncStep) Cancelable() bool { return s.Can }
