// T301：Docker 可用性探测三态 + 过旧版本警告（mock Probe，零 Docker 依赖）
package engine

import (
	"context"
	"errors"
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
