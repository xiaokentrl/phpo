// T302 真实镜像冒烟：pull→save→remove→load 幂等（默认跳过；PHPO_DOCKER_INTEGRATION=1 时执行）
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
)

func TestImageSaveLoadRemoveIdempotent(t *testing.T) {
	if os.Getenv("PHPO_DOCKER_INTEGRATION") == "" {
		t.Skip("设置 PHPO_DOCKER_INTEGRATION=1 且具备 Docker 与网络时运行真实镜像用例")
	}
	c, err := New()
	if err != nil {
		t.Skipf("无法构造 Docker 客户端: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if h := Check(ctx, c); !h.CanStart {
		t.Skipf("Docker 不可用: %s", h.Status)
	}

	const ref = "alpine:3.20" // 极小镜像，用于幂等验证
	if err := c.ImagePull(ctx, ref, nil); err != nil {
		t.Fatalf("pull 失败: %v", err)
	}

	tarPath := filepath.Join(t.TempDir(), "alpine.tar")
	if err := c.ImageSave(ctx, ref, tarPath); err != nil {
		t.Fatalf("save 失败: %v", err)
	}
	if fi, err := os.Stat(tarPath); err != nil || fi.Size() == 0 {
		t.Fatalf("导出的 tar 为空: %v", err)
	}

	if err := c.ImageRemove(ctx, ref); err != nil {
		t.Fatalf("remove 失败: %v", err)
	}
	if _, err := c.cli.ImageInspect(ctx, ref); err == nil {
		t.Fatalf("remove 后镜像仍存在")
	}

	// 零网络重新载入，且重复载入结果一致（幂等）
	for i := 0; i < 2; i++ {
		if err := c.ImageLoad(ctx, tarPath); err != nil {
			t.Fatalf("第 %d 次 load 失败: %v", i+1, err)
		}
		if _, err := c.cli.ImageInspect(ctx, ref); err != nil {
			t.Fatalf("第 %d 次 load 后镜像未就绪: %v", i+1, err)
		}
	}

	// 清理，不留脏资源
	if _, err := c.cli.ImageRemove(ctx, ref, image.RemoveOptions{Force: true}); err != nil {
		t.Logf("测试清理镜像失败（可忽略）: %v", err)
	}
}
