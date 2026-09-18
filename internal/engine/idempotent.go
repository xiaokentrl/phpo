// 幂等三阶段编排：Pre-Clean → Execute → Post-Verify，失败全回滚（§5.13.1 原子性/可恢复性、§5.13.3）
// 与 health 的 Probe/Check 同构：编排为纯逻辑、各阶段以闭包注入，无 Docker 亦可测其收敛性。
package engine

import "context"

// Op 一次幂等操作的三阶段 + 回滚。任一阶段可为 nil（视为无操作）。
//   - PreClean：把目标恢复到「干净可执行」基线（清同名 / 检出冲突资源）
//   - Execute：真正落地本次操作
//   - PostVerify：校验 Docker 实际态 ≡ 期望态（配合 verify.go）
//   - Rollback：Execute 之后任一步失败时复原到操作前
//
// 幂等来自「PreClean 抹平差异 + Execute/Verify 对已达成态跳过」的约定：重复 Run 收敛到同一终态、零残留。
type Op struct {
	Name       string
	PreClean   func(ctx context.Context) error
	Execute    func(ctx context.Context) error
	PostVerify func(ctx context.Context) error
	Rollback   func(ctx context.Context) error
}

// Run 顺序执行三阶段。PreClean 失败时尚未改动状态，直接返回、不回滚；
// Execute / PostVerify 失败则调用 Rollback 尽力复原，并返回原始错误（回滚错误不覆盖它）。
func (o Op) Run(ctx context.Context) error {
	if o.PreClean != nil {
		if err := o.PreClean(ctx); err != nil {
			return err
		}
	}
	if o.Execute != nil {
		if err := o.Execute(ctx); err != nil {
			o.tryRollback(ctx)
			return err
		}
	}
	if o.PostVerify != nil {
		if err := o.PostVerify(ctx); err != nil {
			o.tryRollback(ctx)
			return err
		}
	}
	return nil
}

// tryRollback 回滚尽力而为；不因回滚失败掩盖原始错误。
func (o Op) tryRollback(ctx context.Context) {
	if o.Rollback != nil {
		_ = o.Rollback(ctx)
	}
}
