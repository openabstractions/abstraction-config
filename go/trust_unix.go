//go:build !windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// A machine-wide answer is read only when root owns the file and the directory
// it is in and nobody else can write either. This is OpenSSH's StrictModes.
func trusted(path string) error {
	for _, p := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New(p + ": cannot tell who owns it")
		}
		if st.Uid != 0 {
			return fmt.Errorf("%s is owned by uid %d, not root, so no administrator wrote it", p, st.Uid)
		}
		if perm := info.Mode().Perm(); perm&0o022 != 0 {
			return fmt.Errorf("%s is writable by others (%v), so anyone could have written it", p, perm)
		}
	}
	return nil
}
