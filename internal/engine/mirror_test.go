// 镜像源测速单测：用真 HTTP 服务（httptest）跑真实 socket，锁死「什么算可用」这条判定。
//
// 界面这一格叫「延迟 ms」而不是「速度」：握手只回答「这台源现在活着没有、离本机多远」，
// 能不能拉到某个镜像由后面真正 pull 那一步裁决——所以 401/403（要登录的注册表）也算可用。
package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// hostOf 从 httptest 的 URL 取出「主机:端口」（探测入参就是这一种形状）
func hostOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	return strings.TrimPrefix(srv.URL, "http://")
}

// TestProbeSourcesStatusVerdicts 逐个状态码问一遍「这算不算一个可用的镜像源」。
//
// httptest 起的是明文 HTTP 服务，客户端先按 HTTPS 问必然失败，因此这批用例同时锁死
// 「失败原因指向对面是明文时补问一次 HTTP」那条回落——回落不生效的话全部行都会判不可用。
func TestProbeSourcesStatusVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantOK  bool
		wantErr string // 不可时期望结论里出现的字样
	}{
		{"匿名可读 200", http.StatusOK, true, ""},
		{"要登录 401", http.StatusUnauthorized, true, ""},
		{"禁止 403", http.StatusForbidden, true, ""},
		{"服务端 503", http.StatusServiceUnavailable, false, "服务端错误 HTTP 503"},
		{"不是 registry 404", http.StatusNotFound, false, "可能不是 Docker 镜像源"},
		// 注册表的 /v2/ 不跳转；跳到登录页的地址不是镜像源，跟随后拿到的 200 属于假阳性
		{"跳转 302 不算通", http.StatusFound, false, "HTTP 302"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(c.status) })
			srv := httptest.NewServer(mux)
			defer srv.Close()

			host := hostOf(t, srv)
			res := ProbeSources(context.Background(), []string{host})
			if len(res) != 1 {
				t.Fatalf("返回行数应与入参一致，得 %d", len(res))
			}
			got := res[0]
			if got.Host != host {
				t.Errorf("Host 应原样回填，得 %q", got.Host)
			}
			if got.OK != c.wantOK {
				t.Fatalf("可用判定 = %v，期望 %v（结论 %q）", got.OK, c.wantOK, got.Error)
			}
			if c.wantOK {
				if got.Error != "" {
					t.Errorf("可用时不该带结论文本，得 %q", got.Error)
				}
				return
			}
			if !strings.Contains(got.Error, c.wantErr) {
				t.Errorf("结论 %q 应含 %q", got.Error, c.wantErr)
			}
		})
	}
}

// 明文 HTTP 源不得永远显示不通：dockerd 配了 insecure-registries 后真能拉，
// 检测结论与拉取行为对不上会更难排查——故 HTTPS 失败原因指向「对面是明文」时补问一次 HTTP。
func TestProbeSourcesFallsBackToPlaintext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := ProbeSources(context.Background(), []string{hostOf(t, srv)})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("明文 registry 应经 HTTP 回落判为可用，得 %#v", res)
	}
}

// 证书不受信的源不得被「改用明文再问一次」洗成可用：那等于把「证书有问题」说成「两边都不通」。
// 这里只锁死回落的判据本身（looksLikePlaintextHTTP），因为它决定要不要多拨那一次。
func TestLooksLikePlaintextHTTPOnlyMatchesThatShape(t *testing.T) {
	for _, s := range []string{
		`Get "https://127.0.0.1:5000/v2/": server gave HTTP response to HTTPS client`,
		"tls: first record does not look like a TLS handshake",
	} {
		if !looksLikePlaintextHTTP(errors.New(s)) {
			t.Errorf("该失败应触发明文回落: %q", s)
		}
	}
	for _, s := range []string{
		"dial tcp: lookup nope.invalid: no such host",
		"tls: failed to verify certificate: x509: certificate signed by unknown authority",
		"HTTP 404（该地址可能不是 Docker 镜像源）",
	} {
		if looksLikePlaintextHTTP(errors.New(s)) {
			t.Errorf("该失败不该触发明文回落: %q", s)
		}
	}
}

// 端口没人监听：结论要能读（连接被拒），且不得把「不通」说成「可用」
func TestProbeSourcesRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host := hostOf(t, srv)
	srv.Close() // 关掉即该端口无人应答

	res := ProbeSources(context.Background(), []string{host})
	if res[0].OK {
		t.Fatal("端口已关闭却判可用")
	}
	if res[0].Error == "" {
		t.Error("不可用时必须给出原因")
	}
	if strings.HasPrefix(res[0].Error, "Get ") {
		t.Errorf("结论不该带 Go 的 %q 前缀，得 %q", "Get ", res[0].Error)
	}
}

// 超时：一个源卡住不得拖住其余（并发 + 单源超时）
func TestProbeSourcesTimeoutIsPerSource(t *testing.T) {
	old := MirrorProbeTimeout
	MirrorProbeTimeout = 120 * time.Millisecond
	defer func() { MirrorProbeTimeout = old }()

	release := make(chan struct{})
	defer close(release)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fast.Close()

	start := time.Now()
	res := ProbeSources(context.Background(), []string{hostOf(t, slow), hostOf(t, fast), hostOf(t, fast)})
	used := time.Since(start)
	if used > 2*time.Second {
		t.Errorf("并发探测不该串行等待，用了 %v", used)
	}
	if res[0].OK {
		t.Error("卡住的源应判不可用")
	}
	if !res[1].OK || res[1].Error != "" {
		t.Errorf("其余源不该受牵连，得 %#v", res[1])
	}
	if res[2].Host != hostOf(t, fast) {
		t.Errorf("返回顺序必须与入参逐行对应，得 %#v", res)
	}
}

func TestProbeSourcesEmptyAndOrder(t *testing.T) {
	if got := ProbeSources(context.Background(), nil); len(got) != 0 {
		t.Errorf("无源应返回空，得 %#v", got)
	}
	// 混合：两行指向同一个活源、中间一行端口已关——顺序与行数是界面表格的对齐依据
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer alive.Close()
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadHost := hostOf(t, gone)
	gone.Close()

	want := []string{hostOf(t, alive), deadHost, hostOf(t, alive)}
	res := ProbeSources(context.Background(), want)
	for i, h := range want {
		if res[i].Host != h {
			t.Errorf("第 %d 行 Host = %q，期望 %q", i, res[i].Host, h)
		}
	}
	if !res[0].OK || res[1].OK || !res[2].OK {
		t.Errorf("可用判定错位：%#v", res)
	}
}

// 取消要立刻收口：不得让界面在用户已关掉卡片后还挂着请求
func TestProbeSourcesHonorsContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := ProbeSources(ctx, []string{hostOf(t, srv)})
	if res[0].OK {
		t.Error("上下文已取消却判可用")
	}
	if res[0].Error == "" {
		t.Error("取消也必须给出原因，否则界面那一行是空白")
	}
}
