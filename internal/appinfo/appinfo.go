// Package appinfo derives the command name from the running executable so
// help texts and hints stay honest when the binary is renamed.
package appinfo

import (
	"os"
	"path/filepath"
	"strings"
)

// CanonicalName is the fixed product name, used for data formats (config
// filename, service name) — never derived from the binary.
const CanonicalName = "emday"

// Name returns the name of the running executable, falling back to the
// canonical name. Use it wherever a message says "run `X ...`".
func Name() string {
	exe, err := os.Executable()
	if err != nil {
		return CanonicalName
	}
	base := filepath.Base(exe)
	base = strings.TrimSuffix(base, ".exe")
	if base == "" || base == "." {
		return CanonicalName
	}
	return base
}

// Executable returns the resolved absolute path of the running binary.
func Executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}
