// §0.2 规则 20 门禁：删除容器只删容器本体——不得带 v=1（删卷），否则卸载/重装会连数据一起抹
package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/client"
)

// fakeDaemon 记录最后一次 DELETE /containers/{name} 的查询参数，用于断言 SDK 实际下发到守护进程的选项
type fakeDaemon struct {
	deleteQuery  string
	inspectState map[string]bool
	t            *testing.T
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/_ping"):
		w.Header().Set("API-Version", "1.51")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json"):
		name := strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/containers/")+len("/containers/"):], "/json")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":    "abc123",
			"State": map[string]any{"Status": "running", "Running": f.inspectState[name]},
		})
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/"):
		f.deleteQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Fatalf("未预期的守护进程请求: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
	}
}

func newFakeClient(t *testing.T, daemon http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(daemon)
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithHost(srv.URL), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return &Client{cli: cli}
}

func TestRemoveContainer_KeepsVolumes(t *testing.T) {
	d := &fakeDaemon{t: t, inspectState: map[string]bool{"phpo-mysql-8.4": true}}
	c := newFakeClient(t, d)
	if err := c.RemoveContainer(context.Background(), "phpo-mysql-8.4"); err != nil {
		t.Fatalf("删除容器失败: %v", err)
	}
	q := d.deleteQuery
	if !strings.Contains(q, "force=1") {
		t.Fatalf("删除应带 force（停删合一）: %q", q)
	}
	// RemoveVolumes 会让 SDK 下发 v=1，Docker 随即删除该容器的匿名卷——卸载默认保留数据，不得下发
	if strings.Contains(q, "v=1") {
		t.Fatalf("删除容器不得请求删卷，实得 query=%q", q)
	}
}

func TestRemoveContainer_MissingIsNoop(t *testing.T) {
	d := &fakeDaemon{t: t, inspectState: map[string]bool{}}
	c := newFakeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			d.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "No such container"})
			return
		}
		d.ServeHTTP(w, r)
	}))
	if err := c.RemoveContainer(context.Background(), "phpo-mysql-9.9"); err != nil {
		t.Fatalf("不存在应视为已删除: %v", err)
	}
	if d.deleteQuery != "" {
		t.Fatalf("不存在不应下发 DELETE: %q", d.deleteQuery)
	}
}
