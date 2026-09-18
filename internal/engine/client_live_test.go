// 真实 Docker 冒烟：仅在本机守护进程可用时验证 Probe 实现；无 Docker 自动跳过
package engine

import (
	"context"
	"testing"
)

func TestLiveProbeWhenDockerAvailable(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Skipf("无法构造 Docker 客户端: %v", err)
	}
	defer c.Close()

	h := Check(context.Background(), c)
	if h.Status == StatusNotInstalled || h.Status == StatusNotRunning {
		t.Skipf("本机 Docker 不可用（%s），跳过真实探测", h.Status)
	}
	if !h.CanStart {
		t.Fatalf("守护进程可用却判定不可启动: %+v", h)
	}
	t.Logf("Docker 版本=%s 状态=%s", h.Version, h.Status)
}
