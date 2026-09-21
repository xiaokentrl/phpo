// 托盘驻留裁决单测：关闭窗口语义的唯一判据（纯函数，无需 GUI 宿主）
package ui

import "testing"

// TestResideOnClose 只有「显示托盘 + 关闭时最小化」同时成立才隐藏驻留；
// 托盘隐藏后无唤回入口，驻留等于隐形僵尸进程，必须退出
func TestResideOnClose(t *testing.T) {
	tests := []struct {
		name        string
		trayVisible bool
		minimize    bool
		want        bool
	}{
		{"两项均开-隐藏驻留", true, true, true},
		{"托盘隐藏-退出", false, true, false},
		{"不最小化-退出", true, false, false},
		{"两项均关-退出", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resideOnClose(tt.trayVisible, tt.minimize); got != tt.want {
				t.Fatalf("resideOnClose(%v, %v) = %v，期望 %v", tt.trayVisible, tt.minimize, got, tt.want)
			}
		})
	}
}
