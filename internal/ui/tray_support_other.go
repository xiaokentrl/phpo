//go:build !linux || android || server

package ui

// DetectTraySupport reports tray capabilities provided by non-Linux desktop platforms.
func DetectTraySupport() (TraySupport, error) {
	return TraySupport{
		Available:           true,
		CanToggleVisibility: true,
	}, nil
}
