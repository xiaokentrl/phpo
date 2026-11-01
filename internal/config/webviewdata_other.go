//go:build !linux

package config

func PrepareWebViewDataMigration(string, ...string) (WebViewDataMigration, error) {
	return WebViewDataMigration{}, nil
}
