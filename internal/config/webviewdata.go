package config

// WebViewDataMigration defers moving old WebKit profiles until the app owns the single-instance lock.
type WebViewDataMigration struct {
	migrate func() error
}

// Migrate moves legacy WebKit data and cache into the configured user-data directory.
func (m WebViewDataMigration) Migrate() error {
	if m.migrate == nil {
		return nil
	}
	return m.migrate()
}
