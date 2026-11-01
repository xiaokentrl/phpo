//go:build linux && !android && !server

package ui

import "testing"

func TestLinuxTraySupport(t *testing.T) {
	tests := []struct {
		name             string
		watcherAvailable bool
		wantAvailable    bool
		wantCanToggle    bool
	}{
		{"托盘宿主可用", true, true, false},
		{"托盘宿主不可用", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linuxTraySupport(tt.watcherAvailable)
			if got.Available != tt.wantAvailable || got.CanToggleVisibility != tt.wantCanToggle {
				t.Fatalf("linuxTraySupport(%v) = %+v", tt.watcherAvailable, got)
			}
		})
	}
}
