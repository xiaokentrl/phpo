//go:build linux

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"phpo/internal/util"
)

type webViewProfileMove struct {
	source string
	target string
}

// PrepareWebViewDataMigration redirects WebKit's XDG paths and defers migration until Wails checks single-instance ownership.
func PrepareWebViewDataMigration(profileName string, legacyNames ...string) (WebViewDataMigration, error) {
	if !validProfileName(profileName) {
		return WebViewDataMigration{}, fmt.Errorf("invalid WebView profile name %q", profileName)
	}
	names := make([]string, 0, len(legacyNames)+1)
	seenNames := make(map[string]struct{}, len(legacyNames)+1)
	for _, name := range legacyNames {
		if !validProfileName(name) {
			return WebViewDataMigration{}, fmt.Errorf("invalid legacy WebView profile name %q", name)
		}
		if _, exists := seenNames[name]; exists {
			continue
		}
		seenNames[name] = struct{}{}
		names = append(names, name)
	}
	if _, exists := seenNames[profileName]; !exists {
		names = append(names, profileName)
	}

	dataDir, err := UserDataDir()
	if err != nil {
		return WebViewDataMigration{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return WebViewDataMigration{}, err
	}

	dataHome := xdgHome("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	cacheHome := xdgHome("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	cacheDataHome := filepath.Join(dataDir, "webview-cache")

	oldDataHome, oldDataHomeSet := os.LookupEnv("XDG_DATA_HOME")
	if err := os.Setenv("XDG_DATA_HOME", dataDir); err != nil {
		return WebViewDataMigration{}, err
	}
	if err := os.Setenv("XDG_CACHE_HOME", cacheDataHome); err != nil {
		restoreEnvironment("XDG_DATA_HOME", oldDataHome, oldDataHomeSet)
		return WebViewDataMigration{}, err
	}

	moves := append(
		legacyWebViewProfileMoves(dataHome, filepath.Join(dataDir, profileName), filepath.Join(dataDir, "webview-legacy", "data"), names),
		legacyWebViewProfileMoves(cacheHome, filepath.Join(cacheDataHome, profileName), filepath.Join(dataDir, "webview-legacy", "cache"), names)...,
	)

	return WebViewDataMigration{migrate: func() error {
		return migrateWebViewProfiles(moves)
	}}, nil
}

func legacyWebViewProfileMoves(sourceHome, activeTarget, legacyRoot string, names []string) []webViewProfileMove {
	existing := make([]string, 0, len(names))
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(sourceHome, name)); err == nil {
			existing = append(existing, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			existing = append(existing, name)
		}
	}
	if len(existing) == 0 {
		return nil
	}

	moves := make([]webViewProfileMove, 0, len(existing))
	for index, name := range existing {
		target := filepath.Join(legacyRoot, name)
		if index == 0 {
			target = activeTarget
		}
		moves = append(moves, webViewProfileMove{
			source: filepath.Join(sourceHome, name),
			target: target,
		})
	}
	return moves
}

func validProfileName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00")
}

func xdgHome(variable, fallback string) string {
	if value := os.Getenv(variable); filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return fallback
}

func restoreEnvironment(key, value string, wasSet bool) {
	if wasSet {
		_ = os.Setenv(key, value)
	} else {
		_ = os.Unsetenv(key)
	}
}

func migrateWebViewProfiles(moves []webViewProfileMove) error {
	uniqueMoves := make([]webViewProfileMove, 0, len(moves))
	seenSources := make(map[string]struct{}, len(moves))

	for _, move := range moves {
		source := filepath.Clean(move.source)
		target := filepath.Clean(move.target)
		if source == target {
			continue
		}
		if _, exists := seenSources[source]; exists {
			continue
		}
		seenSources[source] = struct{}{}

		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect legacy WebView profile %s: %w", source, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy WebView profile is not a regular directory: %s", source)
		}

		targetInfo, err := os.Lstat(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect WebView profile destination %s: %w", target, err)
		}
		if err == nil && (!targetInfo.IsDir() || targetInfo.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("WebView profile destination is not a regular directory: %s", target)
		}

		entries, err := os.ReadDir(source)
		if err != nil {
			return fmt.Errorf("read legacy WebView profile %s: %w", source, err)
		}
		for _, entry := range entries {
			targetEntry := filepath.Join(target, entry.Name())
			if _, err := os.Lstat(targetEntry); err == nil {
				return fmt.Errorf("WebView migration would overwrite %s; legacy data remains at %s", targetEntry, source)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect WebView migration destination %s: %w", targetEntry, err)
			}
		}
		uniqueMoves = append(uniqueMoves, webViewProfileMove{source: source, target: target})
	}

	for _, move := range uniqueMoves {
		entries, err := os.ReadDir(move.source)
		if err != nil {
			return fmt.Errorf("read legacy WebView profile %s: %w", move.source, err)
		}
		if len(entries) == 0 {
			if err := os.Remove(move.source); err != nil {
				return fmt.Errorf("remove empty legacy WebView profile %s: %w", move.source, err)
			}
			continue
		}
		if err := util.MkdirAll(move.target); err != nil {
			return fmt.Errorf("create WebView profile directory %s: %w", move.target, err)
		}
		for _, entry := range entries {
			source := filepath.Join(move.source, entry.Name())
			target := filepath.Join(move.target, entry.Name())
			if err := moveWebViewPath(source, target); err != nil {
				return fmt.Errorf("move WebView data from %s to %s: %w", source, target, err)
			}
		}
		if err := os.Remove(move.source); err != nil {
			return fmt.Errorf("remove empty legacy WebView profile %s: %w", move.source, err)
		}
	}
	return nil
}

func moveWebViewPath(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	if err := copyWebViewPath(source, target); err != nil {
		return err
	}
	if err := os.RemoveAll(source); err != nil {
		return err
	}
	return nil
}

func copyWebViewPath(source, target string) (result error) {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	created := false
	defer func() {
		if result != nil && created {
			_ = os.RemoveAll(target)
		}
	}()

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.Symlink(link, target); err != nil {
			return err
		}
		created = true
	case info.IsDir():
		if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
			return err
		}
		created = true
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyWebViewPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
	case info.Mode().IsRegular():
		input, err := os.Open(source)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		created = true
		_, copyErr := io.Copy(output, input)
		syncErr := output.Sync()
		outputCloseErr := output.Close()
		inputCloseErr := input.Close()
		for _, err := range []error{copyErr, syncErr, outputCloseErr, inputCloseErr} {
			if err != nil {
				return err
			}
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported file type %s: %s", info.Mode(), source)
	}
	return nil
}
