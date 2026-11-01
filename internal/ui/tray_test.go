// 托盘驻留裁决单测：关闭窗口语义的唯一判据（纯函数，无需 GUI 宿主）
package ui

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestResideOnClose(t *testing.T) {
	tests := []struct {
		name        string
		trayVisible bool
		minimize    bool
		want        bool
	}{
		{"两项均开-驻留", true, true, true},
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

func TestResideOnCloseRequiresAvailableTray(t *testing.T) {
	if resideOnCloseWithSupport(true, true, false) {
		t.Fatal("托盘不可用时不得驻留")
	}
	if !resideOnCloseWithSupport(true, true, true) {
		t.Fatal("托盘可用且两个偏好开启时应驻留")
	}
}

func TestNormalizeTrayPrefs(t *testing.T) {
	tests := []struct {
		name        string
		support     TraySupport
		enabled     bool
		minimize    bool
		wantEnabled bool
		wantMin     bool
	}{
		{"可切换且支持托盘", TraySupport{Available: true, CanToggleVisibility: true}, false, true, false, false},
		{"关闭图标同步关闭最小化", TraySupport{Available: true, CanToggleVisibility: true}, true, false, true, false},
		{"Linux 托盘图标固定显示", TraySupport{Available: true}, false, true, true, true},
		{"Linux 无托盘宿主", TraySupport{}, false, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, minimize := NormalizeTrayPrefs(tt.support, tt.enabled, tt.minimize)
			if enabled != tt.wantEnabled || minimize != tt.wantMin {
				t.Fatalf("NormalizeTrayPrefs() = (%v, %v)，期望 (%v, %v)", enabled, minimize, tt.wantEnabled, tt.wantMin)
			}
		})
	}
}

func TestShouldMinimiseOnClose(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		sessionType string
		backend     string
		wayland     string
		x11         string
		want        bool
	}{
		{"Wayland 默认后端", "linux", "wayland", "", "wayland-0", ":0", true},
		{"Wayland 显式后端", "linux", "x11", "wayland,x11", "wayland-0", ":0", true},
		{"X11 显式优先", "linux", "wayland", "x11,wayland", "wayland-0", ":0", false},
		{"X11 会话", "linux", "x11", "", "", ":0", false},
		{"没有可用显示", "linux", "wayland", "wayland,x11", "", "", false},
		{"非 Linux", "darwin", "wayland", "", "wayland-0", ":0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldMinimiseOnClose(tt.goos, tt.sessionType, tt.backend, tt.wayland, tt.x11); got != tt.want {
				t.Fatalf("shouldMinimiseOnClose(...) = %v，期望 %v", got, tt.want)
			}
		})
	}
}

func TestToggleTrayWindowRestoresMinimisedWindow(t *testing.T) {
	toggled, shown := false, false
	toggleTrayWindow(true, func() { toggled = true }, func() { shown = true })
	if toggled || !shown {
		t.Fatalf("最小化窗口点击托盘后：toggled=%v，shown=%v", toggled, shown)
	}
}

func TestToggleTrayWindowTogglesVisibleWindow(t *testing.T) {
	toggled, shown := false, false
	toggleTrayWindow(false, func() { toggled = true }, func() { shown = true })
	if !toggled || shown {
		t.Fatalf("普通窗口点击托盘后：toggled=%v，shown=%v", toggled, shown)
	}
}

func TestLinuxTrayActivationCoalescesDoubleClick(t *testing.T) {
	var lastActivation atomic.Int64
	start := time.Unix(1, 0)
	shows := 0

	if !handleLinuxTrayActivation(&lastActivation, start, func() { shows++ }) {
		t.Fatal("首次托盘激活应显示主界面")
	}
	if handleLinuxTrayActivation(&lastActivation, start.Add(100*time.Millisecond), func() { shows++ }) {
		t.Fatal("双击产生的重复激活不应再次显示主界面")
	}
	if !handleLinuxTrayActivation(&lastActivation, start.Add(linuxTrayActivationDebounce), func() { shows++ }) {
		t.Fatal("防抖窗口结束后的新激活应显示主界面")
	}
	if shows != 2 {
		t.Fatalf("显示主界面次数=%d，期望 2", shows)
	}
}

func TestNonLinuxTrayClickKeepsToggleBehavior(t *testing.T) {
	toggled, shown := false, false
	handleTrayClick(func() bool { return false },
		func() { toggled = true },
		func() { shown = true },
	)
	if !toggled || shown {
		t.Fatalf("非 Linux 托盘点击行为改变：toggled=%v，shown=%v", toggled, shown)
	}
}
