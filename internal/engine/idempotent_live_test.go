// T305 真实 Docker 冒烟：仅验证 SDK 落地方法在守护进程可用时不报错，且 Pre-Clean 对空闲名幂等无副作用；无 Docker 自动跳过
package engine

import (
	"context"
	"testing"
)

func TestLiveIdempotentWhenDockerAvailable(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Skipf("无法构造 Docker 客户端: %v", err)
	}
	defer c.Close()

	if h := Check(context.Background(), c); !h.CanStart {
		t.Skipf("本机 Docker 不可用（%s），跳过真实冒烟", h.Status)
	}
	ctx := context.Background()

	// ManagedContainers：走真实 ContainerList + 标签过滤，不应报错
	if _, err := c.ManagedContainers(ctx); err != nil {
		t.Fatalf("ManagedContainers 报错: %v", err)
	}

	// PreCleanContainer：目标名空闲（不存在）→ 幂等返回 nil、无副作用
	if err := c.PreCleanContainer(ctx, "phpo-nonexistent-t305"); err != nil {
		t.Fatalf("对空闲名 Pre-Clean 应无操作、返回 nil，得 %v", err)
	}
}
