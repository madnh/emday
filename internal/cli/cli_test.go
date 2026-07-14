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

// check-config --json must still exit non-zero on an invalid config, so CI
// and Ansible can gate on the exit code alone — while emitting the report as
// JSON on stdout. (Regression: the --json branch used to always return nil.)
func TestCheckConfigJSONExitsNonZeroOnProblems(t *testing.T) {
	t.Setenv(config.EnvConfigDir, "")
	t.Setenv(EnvNonInteractive, "1")
	dir := t.TempDir()
	bad := "version: 1\n" +
		"sources:\n  cpu:\n    type: cpu\n" +
		"rules:\n  - metric: cpu.percent\n    condition: \"value >= 90\"\n    notify: [ghost]\n"
	if err := os.WriteFile(filepath.Join(dir, config.MarkerFile), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "check-config", "--json", "--config-dir", dir)
	if err == nil {
		t.Fatalf("check-config --json on invalid config must return an error (non-zero exit)\n%s", out)
	}
	if !strings.Contains(out, `"ok": false`) {
		t.Fatalf("expected JSON report with ok:false on stdout, got:\n%s", out)
	}

	// A valid config must exit zero with ok:true.
	good := t.TempDir()
	if out, err := runCLI(t, "init", "--config-dir", good); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	out, err = runCLI(t, "check-config", "--json", "--config-dir", good)
	if err != nil {
		t.Fatalf("check-config --json on valid config must exit zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Fatalf("expected ok:true, got:\n%s", out)
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
