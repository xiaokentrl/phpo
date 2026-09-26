// 镜像源测速：并发向每个源问一次「你是 registry v2 服务吗」，记下往返耗时与不通的原因。
//
// 为什么问 /v2/：这是 Docker 拉镜像前必经的那一次握手，它同时回答「这台源现在活着没有」和「离本机多远」。
// 它**不是下载带宽**——真下载还取决于层大小与并发段数，所以界面把这一格叫「延迟 ms」而不是「速度」，
// 数字只用来决定「先试谁」。握手 401/403 也算通（要登录的注册表照样在），能不能拉到某个镜像由真正 pull 那一步裁决。
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"phpo/internal/model"
)

// MirrorProbeTimeout 单个镜像源的握手超时；超时即判该源这次不通（并发探测，一个卡住不拖住其余）。
// 声明为 var 供单测收紧到毫秒级（与 engine/container.go 的 startWait 同口径）。
var MirrorProbeTimeout = 5 * time.Second

// mirrorClient 共享一个 Transport 即可（探测是短连接小请求）；不跟随重定向：
// 注册表服务的 /v2/ 不跳转，跳到登录页的那种地址不是镜像源，跟随后拿到的 200 属于假阳性。
var mirrorClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// ProbeSources 并发探测全部镜像源，返回顺序与入参一致（界面按这份顺序列表；「最快」由调用方挑）。
// host 已由 config.ValidateRegistryHost 收口为「主机[:端口]」，这里不再做任何拼接前的校验。
func ProbeSources(ctx context.Context, hosts []string) []model.MirrorSource {
	out := make([]model.MirrorSource, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, model.MirrorSource{Host: h})
	}
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			ms := probeSource(ctx, host)
			out[i] = ms
		}(i, h)
	}
	wg.Wait()
	return out
}

// probeSource 探一个源：先按 HTTPS 问，若失败的原因像是「对面其实是明文 HTTP」再补问一次 HTTP。
// 补这一刀是为了不撒谎：本机跑的明文 registry（127.0.0.1:5000 这类）在只试 HTTPS 时永远显示不通，
// 而 Docker 守护进程配了 insecure-registries 之后又真能拉——检测结论与拉取行为对不上更难排查。
func probeSource(ctx context.Context, host string) model.MirrorSource {
	res := model.MirrorSource{Host: host}
	lat, err := handshake(ctx, "https", host)
	if err == nil {
		res.LatencyMs, res.OK = lat.Milliseconds(), true
		return res
	}
	if !looksLikePlaintextHTTP(err) {
		res.Error = err.Error()
		return res
	}
	lat, err2 := handshake(ctx, "http", host)
	if err2 == nil {
		res.LatencyMs, res.OK = lat.Milliseconds(), true
		return res
	}
	res.Error = fmt.Sprintf("%v（该地址不以 HTTPS 应答，改用 HTTP 也不通：%v）", err, err2)
	return res
}

// handshake 向 scheme://host/v2/ 发一次 GET 并计时；返回往返耗时。
// 200（匿名可读的注册表）与 401/403（要凭据的注册表）都算「在、连得上」；其余状态码原样写进结论。
func handshake(ctx context.Context, scheme, host string) (time.Duration, error) {
	cctx, cancel := context.WithTimeout(ctx, MirrorProbeTimeout)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, scheme+"://"+host+"/v2/", nil)
	if err != nil {
		return 0, err
	}
	resp, err := mirrorClient.Do(req)
	if err != nil {
		return 0, cleanHTTPErr(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	lat := time.Since(start)

	switch {
	case resp.StatusCode == http.StatusOK:
		return lat, nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		// 要登录的注册表：握手成功即算可用，能否拉到某个镜像由真正 pull 的那一步裁决
		return lat, nil
	case resp.StatusCode >= 500:
		return lat, fmt.Errorf("服务端错误 HTTP %d", resp.StatusCode)
	default:
		return lat, fmt.Errorf("HTTP %d（该地址可能不是 Docker 镜像源）", resp.StatusCode)
	}
}

// cleanHTTPErr 把 Go 的 http 错误包成一句人话：去掉「Get "https://…"」这类前缀，只留原因本身。
func cleanHTTPErr(err error) error {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && strings.HasPrefix(msg, "Get ") {
		msg = msg[i+2:]
	}
	return errors.New(msg)
}

// looksLikePlaintextHTTP 判断失败原因是否指向「对面是明文 HTTP，却收到了 TLS 客户端的请求」。
// 真机形状有两类：客户端侧的 http: server gave HTTP response to HTTPS client，
// 与服务端侧的 tls: first record does not look like a TLS handshake。其余失败（DNS 解析不到、证书不受信）
// 换 scheme 也不会通，退回明文只会把「证书有问题」说成「两边都不通」。
func looksLikePlaintextHTTP(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return looksLikePlaintextHTTP(urlErr.Err)
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http response to https") ||
		strings.Contains(msg, "first record does not look like a tls handshake")
}
