// 引擎种类现取口径：识别结论晚于装配落定（启动钩子不再阻塞拨号），所以「未识别」必须能自愈——
// 用到时现补一拨，而不是把空当成 Docker。拨号失败不得写缓存，否则一次抖动会把整台机器的方言定死。
package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

// versionDaemon 只回 /version：count 记被拨了几次，body 决定自报家门（Podman 或 Docker）；
// fail 为真时回 500，模拟「socket 在但守护进程不应答」。
type versionDaemon struct {
	count atomic.Int32
	body  map[string]any
	fail  bool
}

func (d *versionDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/_ping"):
		w.Header().Set("API-Version", "1.51")
		_, _ = w.Write([]byte("OK"))
	case strings.HasSuffix(r.URL.Path, "/version"):
		d.count.Add(1)
		if d.fail {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "boom"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newVersionClient(t *testing.T, d http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithHost(srv.URL), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return &Client{cli: cli}
}

func podmanBody() map[string]any {
	return map[string]any{
		"Version":    "5.7.0",
		"ApiVersion": "1.51",
		"Components": []map[string]any{{"Name": "Podman Engine", "Version": "5.7.0"}},
	}
}

func TestEngineKind_DialsOnceThenCaches(t *testing.T) {
	d := &versionDaemon{body: podmanBody()}
	c := newVersionClient(t, d)
	if got := c.EngineKind(context.Background()); got != EnginePodman {
		t.Fatalf("未识别时应当场补一拨并按 Components 认出血统，实得 %q", got)
	}
	if n := d.count.Load(); n != 1 {
		t.Fatalf("第一次调用应拨号一次，实得 %d 次", n)
	}
	if got := c.EngineKind(context.Background()); got != EnginePodman {
		t.Fatalf("缓存后结论要稳定，实得 %q", got)
	}
	if n := d.count.Load(); n != 1 {
		t.Fatalf("已识别不得再拨（每次镜像方言都要问一遍引擎），实得 %d 次", n)
	}
}

func TestEngineKind_FailureNotCached(t *testing.T) {
	d := &versionDaemon{body: podmanBody(), fail: true}
	c := newVersionClient(t, d)
	if got := c.EngineKind(context.Background()); got != EngineUnknown {
		t.Fatalf("拨号失败要返回未识别，实得 %q", got)
	}
	d.fail = false
	if got := c.EngineKind(context.Background()); got != EnginePodman {
		t.Fatalf("守护进程恢复后下次调用要自愈，实得 %q", got)
	}
	if n := d.count.Load(); n != 2 {
		t.Fatalf("失败不缓存，应重拨一次，实得 %d 次", n)
	}
}

func TestEngineKind_CtxTimeoutBoundsDial(t *testing.T) {
	d := &versionDaemon{body: podmanBody()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/version") {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
			}
		}
		d.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithHost(srv.URL), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	c := &Client{cli: cli}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)
	start := time.Now()
	if got := c.EngineKind(ctx); got != EngineUnknown {
		t.Fatalf("挂死的 /version 到期要给未识别，实得 %q", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("到期即返回，不得等守护进程自己醒：%v", elapsed)
	}
}
