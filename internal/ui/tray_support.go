package ui

// TraySupport reports system tray availability and visibility-toggle support.
type TraySupport struct {
	Available           bool `json:"available"`
	CanToggleVisibility bool `json:"canToggleVisibility"`
}

// NormalizeTrayPrefs enforces the dependency between tray visibility and minimize-on-close.
func NormalizeTrayPrefs(support TraySupport, enabled, minimizeOnClose bool) (bool, bool) {
	if !support.CanToggleVisibility {
		enabled = true
	}
	if !support.Available || !enabled {
		minimizeOnClose = false
	}
	return enabled, minimizeOnClose
}
