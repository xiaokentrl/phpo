// 主窗口建窗参数单测：勾选「启动后最小化」只多置一个 Hidden，其余参数一字不动
package ui

import "testing"

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
