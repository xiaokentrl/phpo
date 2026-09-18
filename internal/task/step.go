// Step 五方法接口（§5.13.3）：Name / Execute / Rollback / Cleanup / Cancelable
// 每个 Step 负责自身可回滚；Cleanup 无论成败必须执行（如清空临时目录）
package task

import "context"

// StepLog 让 Step 向内推送日志行
type StepLog interface {
	Log(level, text string)
}

type Step interface {
	Name() string
	Execute(ctx context.Context, log StepLog) error
	Rollback(ctx context.Context) error
	Cleanup()
	Cancelable() bool
}

// BaseStep 提供默认实现：不可取消、回滚/清理为空，具体步骤按需覆盖
type BaseStep struct {
	StepName string
}

func (b BaseStep) Name() string                   { return b.StepName }
func (b BaseStep) Rollback(context.Context) error { return nil }
func (b BaseStep) Cleanup()                       {}
func (b BaseStep) Cancelable() bool               { return false }

// NoopStep 空步骤：三段式骨架与测试占位
type NoopStep struct{ BaseStep }

func NewNoopStep(name string) *NoopStep { return &NoopStep{BaseStep{StepName: name}} }

func (NoopStep) Execute(context.Context, StepLog) error { return nil }
