// 根 Service 的守卫用例：重启只允许一次，宿主未就绪时拒绝
package main

import (
	"errors"
	"strings"
	"testing"
)

// TestRestartBlockedByMarker 已带重启标记仍被请求重启 → 拒绝且不改状态，防无限重启循环
func TestRestartBlockedByMarker(t *testing.T) {
	t.Setenv(restartMarker, "1")
	a := &App{}
	err := a.Restart()
	if err == nil {
		t.Fatal("带重启标记时应拒绝再次重启")
	}
	if !strings.Contains(err.Error(), "重启") {
		t.Fatalf("错误文案应含「重启」，实际：%v", err)
	}
	if a.restartPending {
		t.Fatal("被拒绝时不应置 restartPending")
	}
}

// TestRestartWithoutWails 宿主未注入时返回启动守卫错误，不误置重启标记
func TestRestartWithoutWails(t *testing.T) {
	a := &App{}
	if err := a.Restart(); !errors.Is(err, errNotReady) {
		t.Fatalf("期望 errNotReady，实际：%v", err)
	}
	if a.restartPending {
		t.Fatal("宿主未就绪时不应置 restartPending")
	}
}
