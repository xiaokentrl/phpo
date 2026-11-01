// 主窗口建窗参数单测：勾选「启动后最小化」只多置一个 Hidden，其余参数一字不动
package ui

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type windowPresenterProbe struct {
	calls     []string
	visible   bool
	minimised bool
}

func (w *windowPresenterProbe) IsMinimised() bool { return w.minimised }
func (w *windowPresenterProbe) IsVisible() bool   { return w.visible }
func (w *windowPresenterProbe) Restore()          { w.calls = append(w.calls, "restore") }
func (w *windowPresenterProbe) Show() application.Window {
	w.calls = append(w.calls, "show")
	return nil
}
func (w *windowPresenterProbe) UnMinimise() { w.calls = append(w.calls, "unminimise") }
func (w *windowPresenterProbe) Focus()      { w.calls = append(w.calls, "focus") }

func TestShowLinuxMainWindowUnminimisesWithoutPresentingAgain(t *testing.T) {
	window := &windowPresenterProbe{visible: true, minimised: true}
	showLinuxMainWindow(window)
	if got, want := len(window.calls), 1; got != want {
		t.Fatalf("调用次数=%d，期望 %d：%v", got, want, window.calls)
	}
	if got := window.calls[0]; got != "unminimise" {
		t.Fatalf("调用=%q，期望只恢复最小化窗口", got)
	}
}

func TestShowLinuxMainWindowShowsHiddenWindowOnce(t *testing.T) {
	window := &windowPresenterProbe{}
	showLinuxMainWindow(window)
	if got, want := len(window.calls), 1; got != want {
		t.Fatalf("调用次数=%d，期望 %d：%v", got, want, window.calls)
	}
	if got := window.calls[0]; got != "show" {
		t.Fatalf("调用=%q，期望只显示隐藏窗口", got)
	}
}

func TestShowLinuxMainWindowOnlyFocusesVisibleWindow(t *testing.T) {
	window := &windowPresenterProbe{visible: true}
	showLinuxMainWindow(window)
	if got, want := len(window.calls), 1; got != want {
		t.Fatalf("调用次数=%d，期望 %d：%v", got, want, window.calls)
	}
	if got := window.calls[0]; got != "focus" {
		t.Fatalf("已显示窗口的恢复操作=%q，期望只聚焦以保留尺寸和位置", got)
	}
}

func TestShowMainWindowPreservesOtherDisplaySequence(t *testing.T) {
	window := &windowPresenterProbe{visible: true, minimised: true}
	showMainWindow(window, false)
	if got, want := len(window.calls), 3; got != want {
		t.Fatalf("原有显示流程调用次数=%d，期望 %d：%v", got, want, window.calls)
	}
	for i, want := range []string{"restore", "show", "focus"} {
		if got := window.calls[i]; got != want {
			t.Errorf("第 %d 步=%q，期望 %q", i+1, got, want)
		}
	}
}

func TestMainWindowOptions_StartHiddenOnlyTogglesHidden(t *testing.T) {
	for _, startHidden := range []bool{false, true} {
		o := MainWindowOptions(startHidden)
		if o.Hidden != startHidden {
			t.Errorf("startHidden=%v 时 Hidden=%v，期望逐字相同", startHidden, o.Hidden)
		}
		if o.Title != "phpo" || o.Width != 1280 || o.Height != 800 {
			t.Errorf("窗口身份参数被改动: title=%q %dx%d", o.Title, o.Width, o.Height)
		}
		if c := o.BackgroundColour; c.Red != 22 || c.Green != 24 || c.Blue != 28 {
			t.Errorf("背景色被改动: %+v", c)
		}
	}
}
