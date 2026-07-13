package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/madnh/emday/internal/appinfo"
)

// EnvConfigDir is the env var pointing at the config directory.
const EnvConfigDir = "EMDAY_CONFIG_DIR"

// Candidate is one probed location during resolution, kept for doctor output.
type Candidate struct {
	Path      string
	HasMarker bool
}

// Resolution records how the config dir was (or was not) found.
type Resolution struct {
	Dir        string // resolved dir, "" when unresolved
	Source     string // "flag" | "env" | "inferred" | ""
	FlagValue  string
	EnvValue   string
	Candidates []Candidate // probed default locations (inference)
	Err        error
}

// DefaultCandidates returns the platform default locations probed during
// inference, in priority order. All are only *accepted* when they contain
// the marker file.
func DefaultCandidates() []string {
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		if pd := os.Getenv("ProgramData"); pd != "" {
			dirs = append(dirs, filepath.Join(pd, "emday"))
		}
		if ad := os.Getenv("APPDATA"); ad != "" {
			dirs = append(dirs, filepath.Join(ad, "emday"))
		}
	case "darwin":
		dirs = append(dirs, "/usr/local/etc/emday")
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".config", "emday"))
		}
	default: // linux, bsd…
		dirs = append(dirs, "/etc/emday")
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dirs = append(dirs, filepath.Join(xdg, "emday"))
		} else if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".config", "emday"))
		}
	}
	// portable: next to the current working directory
	dirs = append(dirs, "emday")
	return dirs
}

func hasMarker(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, MarkerFile))
	return err == nil && fi.Mode().IsRegular()
}

// Resolve finds an *initialized* config dir: flag → env → inference.
// It never creates anything; explicit sources must already be initialized.
func Resolve(flagDir string) Resolution {
	res := Resolution{FlagValue: flagDir, EnvValue: os.Getenv(EnvConfigDir)}

	explicit := ""
	switch {
	case flagDir != "":
		explicit, res.Source = flagDir, "flag"
	case res.EnvValue != "":
		explicit, res.Source = res.EnvValue, "env"
	}
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			res.Err = err
			return res
		}
		if !hasMarker(abs) {
			res.Err = fmt.Errorf("config dir %s (from %s) is not initialized: no %s found — run `%s init --config-dir %s` first",
				abs, res.Source, MarkerFile, appinfo.Name(), abs)
			return res
		}
		res.Dir = abs
		return res
	}

	for _, cand := range DefaultCandidates() {
		abs, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		ok := hasMarker(abs)
		res.Candidates = append(res.Candidates, Candidate{Path: abs, HasMarker: ok})
		if ok {
			res.Dir = abs
			res.Source = "inferred"
			return res
		}
	}

	var probed []string
	for _, c := range res.Candidates {
		probed = append(probed, "  "+c.Path)
	}
	res.Err = fmt.Errorf(
		"could not find an emday config dir.\nProbed default locations (no %s in any):\n%s\nPoint at one with --config-dir <dir> or %s=<dir>, or create one with `%s init`.",
		MarkerFile, strings.Join(probed, "\n"), EnvConfigDir, appinfo.Name())
	return res
}

// MustResolve is Resolve for commands that require an initialized dir.
func MustResolve(flagDir string) (string, error) {
	res := Resolve(flagDir)
	return res.Dir, res.Err
}
