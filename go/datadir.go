package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DataDirFor is OA's per-user data folder: %LOCALAPPDATA%\openabstractions on
// Windows, ~/Library/Application Support/openabstractions on macOS and
// $XDG_DATA_HOME/openabstractions (default ~/.local/share) on Linux. The
// runtime's state (runtime-v1) and the file job store live under it. It never
// resolves to the home folder itself, which backup tools such as File History
// copy whole.
func DataDirFor(platform string, getenv func(string) string, home func() (string, error)) (string, error) {
	var base string
	switch platform {
	case "windows":
		base = getenv("LOCALAPPDATA")
	case "linux":
		base = getenv("XDG_DATA_HOME")
		if base == "" {
			h, err := home()
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(h) {
				return "", errors.New("config: absolute home required")
			}
			base = filepath.Join(h, ".local", "share")
		}
	case "darwin":
		h, err := home()
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(h) {
			return "", errors.New("config: absolute home required")
		}
		base = filepath.Join(h, "Library", "Application Support")
	default:
		return "", fmt.Errorf("config: unsupported data platform %q", platform)
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("config: absolute user data directory required")
	}
	return filepath.Join(base, "openabstractions"), nil
}

// DataDir is DataDirFor this process's platform and environment.
func DataDir() (string, error) {
	return DataDirFor(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

// LegacyJobStore is where the local file job store lives when nothing names
// one: job-store under DataDir.
func LegacyJobStore() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "job-store"), nil
}

// LegacyHomeStoreNotice is one line for the release after the job store left
// the home folder root: when store holds no jobs yet and ~/.abstraction or
// ~/.modelget does, it names that folder and ABSTRACTION_STORE. Otherwise "".
func LegacyHomeStoreNotice(store, home string) string {
	if hasJobs(store) || home == "" {
		return ""
	}
	for _, name := range []string{".abstraction", ".modelget"} {
		if legacy := filepath.Join(home, name); hasJobs(legacy) {
			return fmt.Sprintf("jobs from an earlier release are in %s; set ABSTRACTION_STORE=%s to keep using them", legacy, legacy)
		}
	}
	return ""
}

func hasJobs(store string) bool {
	entries, err := os.ReadDir(filepath.Join(store, "jobs"))
	return err == nil && len(entries) > 0
}
