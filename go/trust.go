package config

import (
	"path/filepath"

	"github.com/openabstractions/abstraction-storage/go/layout"
)

// trusted refuses a machine-wide answer unless an administrator owns, and
// only administrators can write, path and the folder it is in (storage
// STO-L4, owned by layout). A file under ProgramData proves nothing about
// who wrote it: BUILTIN\Users may create there and the creator keeps full
// control of what it made, so ownership is what a planter cannot choose.
// This is OpenSSH's StrictModes.
func trusted(path string) error {
	return layout.TrustMachine(path, filepath.Dir(path))
}
