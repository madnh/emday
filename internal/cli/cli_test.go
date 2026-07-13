package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madnh/emday/internal/config"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestInitCreatesAndRefusesOverwrite(t *testing.T) {
	t.Setenv(config.EnvConfigDir, "")
	t.Setenv(EnvNonInteractive, "1")
	dir := filepath.Join(t.TempDir(), "emday")

	out, err := runCLI(t, "init", "--config-dir", dir)
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// The seeded dir must be complete: marker, guide, state.
	for _, f := range []string{config.MarkerFile, "config.md", "state.json"} {
		if !fileExists(filepath.Join(dir, f)) {
			t.Errorf("init did not create %s", f)
		}
	}
	// doctor must run cleanly (and read-only) on the fresh dir
	if out, err := runCLI(t, "doctor", "--config-dir", dir); err != nil {
		t.Fatalf("doctor on fresh dir: %v\n%s", err, out)
	}

	// Second init must refuse, not clobber.
	_, err = runCLI(t, "init", "--config-dir", dir)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("re-init must refuse to overwrite, got %v", err)
	}
}

// The starter config written by `init` must always pass check-config —
// this pins example.yaml to the real schema so they cannot drift apart.
func TestStarterConfigIsValid(t *testing.T) {
	t.Setenv(config.EnvConfigDir, "")
	t.Setenv(EnvNonInteractive, "1")
	dir := filepath.Join(t.TempDir(), "emday")

	if out, err := runCLI(t, "init", "--config-dir", dir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	out, err := runCLI(t, "check-config", "--config-dir", dir)
	if err != nil {
		t.Fatalf("starter config fails check-config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected ok, got:\n%s", out)
	}
}

func TestCommandsRequireInitializedDir(t *testing.T) {
	t.Setenv(config.EnvConfigDir, "")
	t.Setenv(EnvNonInteractive, "1")
	empty := t.TempDir() // exists but has no marker

	for _, cmd := range []string{"run", "check-config"} {
		_, err := runCLI(t, cmd, "--config-dir", empty)
		if err == nil || !strings.Contains(err.Error(), "init") {
			t.Errorf("%s on uninitialized dir must error and point at init, got %v", cmd, err)
		}
	}
}

func TestTestRuleEvaluates(t *testing.T) {
	out, err := runCLI(t, "test-rule", "value > 90", "--value", "95")
	if err != nil || strings.TrimSpace(out) != "true" {
		t.Fatalf("test-rule = %q, %v", out, err)
	}
	out, err = runCLI(t, "test-rule", `value not in ["ok"]`, "--value", "ok")
	if err != nil || strings.TrimSpace(out) != "false" {
		t.Fatalf("test-rule = %q, %v", out, err)
	}
	_, err = runCLI(t, "test-rule", "value >>> broken", "--value", "1")
	if err == nil || !strings.Contains(err.Error(), "docs conditions") {
		t.Fatalf("bad syntax must point at the cheat sheet, got %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
