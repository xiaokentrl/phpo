// T301：Docker 可用性探测三态 + 过旧版本警告（mock Probe，零 Docker 依赖）
package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeProbe struct {
	ver string
	err error
}

func (f fakeProbe) Detect(context.Context) (string, error) { return f.ver, f.err }

func TestCheckDockerStatusBranches(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		probe      Probe
		wantStatus Status
		wantCan    bool
		wantWarn   bool
	}{
		{"正常", fakeProbe{ver: "28.3.2"}, StatusOK, true, false},
		{"未安装", fakeProbe{err: ErrDockerNotInstalled}, StatusNotInstalled, false, false},
		{"未运行", fakeProbe{err: ErrDockerNotRunning}, StatusNotRunning, false, false},
		{"其他连接错误归为未运行", fakeProbe{err: errors.New("connection refused")}, StatusNotRunning, false, false},
		{"版本过旧仍可用但警告", fakeProbe{ver: "19.03.5"}, StatusOldVersion, true, true},
		{"临界 20.10.0 通过", fakeProbe{ver: "20.10.0"}, StatusOK, true, false},
		{"版本串无法解析不误判过旧", fakeProbe{ver: "24.0.0-beta"}, StatusOK, true, false},
	}
	for _, c := range cases {
		got := Check(ctx, c.probe)
		if got.Status != c.wantStatus {
			t.Errorf("%s: Status=%q 期望 %q (msg=%q)", c.name, got.Status, c.wantStatus, got.Message)
		}
		if got.CanStart != c.wantCan {
			t.Errorf("%s: CanStart=%v 期望 %v", c.name, got.CanStart, c.wantCan)
		}
		if got.Warning != c.wantWarn {
			t.Errorf("%s: Warning=%v 期望 %v", c.name, got.Warning, c.wantWarn)
		}
	}
}

func TestCheckMessagesAreHumanReadable(t *testing.T) {
	ctx := context.Background()
	notInst := Check(ctx, fakeProbe{err: ErrDockerNotInstalled})
	if notInst.Message == "" || notInst.Hint == "" {
		t.Errorf("未安装应给出人话消息+下载建议: %+v", notInst)
	}
	// 硬红线 7：未安装/未运行一律不得启动服务
	if notInst.CanStart {
		t.Errorf("未安装不得启动服务")
	}
	notRun := Check(ctx, fakeProbe{err: ErrDockerNotRunning})
	if notRun.CanStart {
		t.Errorf("未运行不得启动服务")
	}
}

// TestCheckFlattensRawError 底层探测错误可能是整段 HTML 或多行堆栈（代理/异常端点回吐），
// 首启横幅直接铺原文会撑满界面 —— Message 必须压成单行、限长，并保留人话前缀。
func TestCheckFlattensRawError(t *testing.T) {
	raw := errors.New("501 Unsupported method ('POST')\n<html><body><h1>Error response</h1>" +
		"<p>Error code: 501</p>" + strings.Repeat("<p>Server does not support this operation.</p>", 30) + "</body></html>")
	got := Check(context.Background(), fakeProbe{err: raw})
	if got.Status != StatusNotRunning || got.CanStart {
		t.Fatalf("其他连接错误应归为未运行且不可启动，got=%+v", got)
	}
	if strings.ContainsAny(got.Message, "\n\r\t") {
		t.Errorf("Message 不得含换行/制表符：%q", got.Message)
	}
	if n := len([]rune(got.Message)); n > errBriefMax+20 {
		t.Errorf("Message 长度 %d 超过上限 %d：%q", n, errBriefMax+20, got.Message)
	}
	if !strings.HasPrefix(got.Message, "无法连接 Docker：") {
		t.Errorf("Message 应保留人话前缀：%q", got.Message)
	}
}

// TestErrBrief 单行化与截断的边界：空白折叠、限长加省略号、短错误原样保留。
func TestErrBrief(t *testing.T) {
	if got := errBrief(errors.New("  a\n\tb   c  ")); got != "a b c" {
		t.Errorf("空白应折叠为单行: %q", got)
	}
	long := errors.New(strings.Repeat("错", 300))
	brief := errBrief(long)
	if len([]rune(brief)) != errBriefMax+1 {
		t.Errorf("超长应截到 errBriefMax 并补省略号, got=%d", len([]rune(brief)))
	}
	if !strings.HasSuffix(brief, "…") {
		t.Errorf("截断须以省略号收尾: %q", brief[len(brief)-6:])
	}
}
