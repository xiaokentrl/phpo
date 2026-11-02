//go:build linux

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWebViewDataMigrationMovesLegacyProfiles(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "config")
	oldDataHome := filepath.Join(home, "old-data")
	oldCacheHome := filepath.Join(home, "old-cache")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", oldDataHome)
	t.Setenv("XDG_CACHE_HOME", oldCacheHome)

	dataSource := filepath.Join(oldDataHome, "phpo")
	cacheSource := filepath.Join(oldCacheHome, "phpo")
	identityDataSource := filepath.Join(oldDataHome, "io.github.example.phpo")
	identityCacheSource := filepath.Join(oldCacheHome, "io.github.example.phpo")
	writeWebViewTestFile(t, filepath.Join(dataSource, "localstorage", "settings"), "saved settings")
	writeWebViewTestFile(t, filepath.Join(cacheSource, "WebKitCache", "entry"), "cached response")
	writeWebViewTestFile(t, filepath.Join(identityDataSource, "mediakeys", "v1", "salt"), "identity media key")
	writeWebViewTestFile(t, filepath.Join(identityCacheSource, "WebKitCache", "Version 17", "salt"), "identity cache salt")

	migration, err := PrepareWebViewDataMigration("io.github.example.phpo", "phpo")
	if err != nil {
		t.Fatal(err)
	}
	wantDataHome := filepath.Join(configHome, "phpo")
	wantCacheHome := filepath.Join(wantDataHome, "webview-cache")
	if got := os.Getenv("XDG_DATA_HOME"); got != wantDataHome {
		t.Fatalf("XDG_DATA_HOME = %q, want %q", got, wantDataHome)
	}
	if got := os.Getenv("XDG_CACHE_HOME"); got != wantCacheHome {
		t.Fatalf("XDG_CACHE_HOME = %q, want %q", got, wantCacheHome)
	}

	if err := migration.Migrate(); err != nil {
		t.Fatal(err)
	}
	assertWebViewTestFile(t, filepath.Join(wantDataHome, "io.github.example.phpo", "localstorage", "settings"), "saved settings")
	assertWebViewTestFile(t, filepath.Join(wantCacheHome, "io.github.example.phpo", "WebKitCache", "entry"), "cached response")
	assertWebViewTestFile(t, filepath.Join(wantDataHome, "webview-legacy", "data", "io.github.example.phpo", "mediakeys", "v1", "salt"), "identity media key")
	assertWebViewTestFile(t, filepath.Join(wantDataHome, "webview-legacy", "cache", "io.github.example.phpo", "WebKitCache", "Version 17", "salt"), "identity cache salt")
	for _, path := range []string{dataSource, cacheSource, identityDataSource, identityCacheSource} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("legacy profile still exists at %s (error: %v)", path, err)
		}
	}
}

func TestPrepareWebViewDataMigrationPreservesBothProfileIdentities(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "config")
	oldDataHome := filepath.Join(home, "old-data")
	oldCacheHome := filepath.Join(home, "old-cache")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", oldDataHome)
	t.Setenv("XDG_CACHE_HOME", oldCacheHome)

	writeWebViewTestFile(t, filepath.Join(oldDataHome, "phpo", "localstorage", "settings"), "legacy app settings")
	writeWebViewTestFile(t, filepath.Join(oldDataHome, "io.github.example.phpo", "mediakeys", "v1", "salt"), "identity media key")
	writeWebViewTestFile(t, filepath.Join(oldCacheHome, "phpo", "HSTS", "hsts-storage.sqlite"), "legacy HSTS")
	writeWebViewTestFile(t, filepath.Join(oldCacheHome, "io.github.example.phpo", "HSTS", "hsts-storage.sqlite"), "identity HSTS")

	migration, err := PrepareWebViewDataMigration("io.github.example.phpo", "phpo")
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Migrate(); err != nil {
		t.Fatal(err)
	}

	dataRoot := filepath.Join(configHome, "phpo")
	assertWebViewTestFile(t, filepath.Join(dataRoot, "io.github.example.phpo", "localstorage", "settings"), "legacy app settings")
	assertWebViewTestFile(t, filepath.Join(dataRoot, "webview-legacy", "data", "io.github.example.phpo", "mediakeys", "v1", "salt"), "identity media key")
	assertWebViewTestFile(t, filepath.Join(dataRoot, "webview-cache", "io.github.example.phpo", "HSTS", "hsts-storage.sqlite"), "legacy HSTS")
	assertWebViewTestFile(t, filepath.Join(dataRoot, "webview-legacy", "cache", "io.github.example.phpo", "HSTS", "hsts-storage.sqlite"), "identity HSTS")
}

func TestPrepareWebViewDataMigrationDoesNotOverwriteExistingProfile(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "config")
	oldDataHome := filepath.Join(home, "old-data")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", oldDataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "old-cache"))

	dataSource := filepath.Join(oldDataHome, "phpo")
	writeWebViewTestFile(t, filepath.Join(dataSource, "localstorage", "settings"), "legacy")
	target := filepath.Join(configHome, "phpo", "io.github.example.phpo")
	writeWebViewTestFile(t, filepath.Join(target, "localstorage", "settings"), "current")

	migration, err := PrepareWebViewDataMigration("io.github.example.phpo", "phpo")
	if err != nil {
		t.Fatal(err)
	}
	err = migration.Migrate()
	if err == nil || !strings.Contains(err.Error(), "would overwrite") {
		t.Fatalf("expected a collision error, got %v", err)
	}
	assertWebViewTestFile(t, filepath.Join(dataSource, "localstorage", "settings"), "legacy")
	assertWebViewTestFile(t, filepath.Join(target, "localstorage", "settings"), "current")
}

func TestPrepareWebViewDataMigrationUsesXDGDefaultsForRelativeHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", "relative-data")
	t.Setenv("XDG_CACHE_HOME", "relative-cache")

	migration, err := PrepareWebViewDataMigration("phpo")
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "share", "phpo"),
		filepath.Join(home, ".cache", "phpo"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("relative XDG home should fall back without creating %s (error: %v)", path, err)
		}
	}
}

func writeWebViewTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertWebViewTestFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
