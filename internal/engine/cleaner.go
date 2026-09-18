// 资源清洁：操作前清同名 / 检出冲突（§5.13.3 Pre-Clean；§5.13.13 禁止「不检查冲突就动手」）
// 判定归属为纯逻辑，删除/报错为 SDK 落地；外部资源占用同名 → 拒绝覆盖用户数据。
package engine

import (
	"context"
	"errors"
	"fmt"
)

// ErrNameConflict 目标名被非 phpo 托管资源占用：不得静默覆盖用户资源。
var ErrNameConflict = errors.New("资源名称冲突：目标名被外部资源占用")

// ownerState 名字占用者三态（Pre-Clean 决策依据）
type ownerState int

const (
	ownerFree    ownerState = iota // 名字空闲，可直接创建
	ownerPhpo                      // phpo 托管，先删后建（幂等重建）
	ownerForeign                   // 外部资源占用 → 冲突，拒绝操作
)

// classifyOwner 纯判定：给定「名字是否存在」与「存在者是否 phpo 托管」得出占用者三态。
func classifyOwner(exists, managed bool) ownerState {
	switch {
	case !exists:
		return ownerFree
	case managed:
		return ownerPhpo
	default:
		return ownerForeign
	}
}

// isPhpoManaged 归属标签判定（与 OwnershipLabels 对应）。
func isPhpoManaged(labels map[string]string) bool { return labels["phpo.managed"] == "true" }

// PreCleanContainer 操作前清理同名容器：
//   - 名字空闲 → 直接返回
//   - 名字为 phpo 托管 → 删除，交由后续 Execute 重建（幂等）
//   - 名字为外部资源 → 返回 ErrNameConflict，拒绝覆盖
func (c *Client) PreCleanContainer(ctx context.Context, name string) error {
	insp, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		if isNotFound(err) {
			return nil // ownerFree
		}
		return err
	}
	var labels map[string]string
	if insp.Config != nil {
		labels = insp.Config.Labels
	}
	if classifyOwner(true, isPhpoManaged(labels)) == ownerForeign {
		return fmt.Errorf("%w: %s", ErrNameConflict, name)
	}
	return c.RemoveContainer(ctx, name)
}
