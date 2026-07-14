package source

// These are deliberately live-system tests. emday's core value — reading host
// metrics — is delegated to gopsutil, so if a gopsutil upgrade (or a refactor
// here) changes which metrics we emit, their value types, or their units, these
// tests must fail on CI rather than ship wrong numbers silently. They read real
// /proc, statfs and process state on the runner; nothing is mocked.

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// collectMap runs a source and returns its samples keyed by metric name.
func collectMap(t *testing.T, s Source) map[string]model.Value {
	t.Helper()
	samples, _, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("%s.Collect: %v", s.Name(), err)
	}
	m := make(map[string]model.Value, len(samples))
	for _, sm := range samples {
		m[sm.Metric] = sm.Value
	}
	return m
}

func mustNum(t *testing.T, m map[string]model.Value, key string) float64 {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing metric %q (have: %v)", key, keys(m))
	}
	if !v.IsNum {
		t.Fatalf("metric %q should be numeric, got string %q", key, v.Str)
	}
	return v.Num
}

func keys(m map[string]model.Value) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestCPUSourceContract(t *testing.T) {
	s := newCPUSource("cpu")
	// cpu.percent is measured since the previous call; prime, then sample.
	s.Collect(context.Background())
	time.Sleep(50 * time.Millisecond)
	m := collectMap(t, s)

	pct := mustNum(t, m, "cpu.percent")
	if pct < 0 || pct > 100 {
		t.Errorf("cpu.percent out of range: %v", pct)
	}
	if runtime.GOOS != "windows" {
		for _, k := range []string{"cpu.load1", "cpu.load5", "cpu.load15"} {
			if v := mustNum(t, m, k); v < 0 {
				t.Errorf("%s should be >= 0, got %v", k, v)
			}
		}
	}
}

func TestMemorySourceContract(t *testing.T) {
	m := collectMap(t, newMemorySource("memory"))

	pct := mustNum(t, m, "memory.percent")
	if pct < 0 || pct > 100 {
		t.Errorf("memory.percent out of range: %v", pct)
	}
	used := mustNum(t, m, "memory.used_mb")
	total := mustNum(t, m, "memory.total_mb")
	if used < 0 || total <= 0 || used > total {
		t.Errorf("expected 0 <= used_mb (%v) <= total_mb (%v)", used, total)
	}
	// Unit guard: total in MiB for any real machine sits well inside this band.
	// A bytes/KB unit drift in gopsutil (or our math) would blow past it.
	if total < 64 || total > 100_000_000 {
		t.Errorf("memory.total_mb %v is implausible for MiB — unit drift?", total)
	}
}

func TestDiskSourceContract(t *testing.T) {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	s := newDiskSource("disk", &config.Source{Type: "disk", Mounts: map[string]string{"root": root}})
	m := collectMap(t, s)

	// Exact metric-name contract for a single mount.
	if len(m) != 2 {
		t.Errorf("expected exactly 2 disk metrics, got %v", keys(m))
	}
	pct := mustNum(t, m, "disk.root.percent")
	if pct < 0 || pct > 100 {
		t.Errorf("disk.root.percent out of range: %v", pct)
	}
	if free := mustNum(t, m, "disk.root.free_gb"); free < 0 {
		t.Errorf("disk.root.free_gb should be >= 0, got %v", free)
	}
}

// A mount that cannot be stat'd is skipped, never reported as 0, and does not
// fail the whole source as long as another mount succeeds. Guards the
// partial-tolerance behaviour the docs promise (emday docs source-disk).
func TestDiskSourcePartialTolerance(t *testing.T) {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	s := newDiskSource("disk", &config.Source{Type: "disk", Mounts: map[string]string{
		"root":  root,
		"ghost": "/nonexistent/emday-test-mount",
	}})
	m := collectMap(t, s)
	if _, ok := m["disk.ghost.percent"]; ok {
		t.Errorf("a failed mount must be absent, not reported: %v", keys(m))
	}
	if _, ok := m["disk.root.percent"]; !ok {
		t.Errorf("the good mount must still be reported: %v", keys(m))
	}
}

func TestProcessSourceContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the `sleep` binary")
	}
	// A process we control: guarantees a deterministic running=1 / count>=1.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer cmd.Process.Kill()
	time.Sleep(150 * time.Millisecond) // let it appear in the process table

	s := newProcessSource("proc", &config.Source{
		Type:      "process",
		Processes: []string{"sleep", "emday-definitely-not-running"},
	})
	m := collectMap(t, s)

	if running := mustNum(t, m, "proc.sleep.running"); running != 1 {
		t.Errorf("proc.sleep.running = %v, want 1", running)
	}
	if count := mustNum(t, m, "proc.sleep.count"); count < 1 {
		t.Errorf("proc.sleep.count = %v, want >= 1", count)
	}
	// An absent process is 0/0, not a missing metric — a typo'd name must look
	// like a down service, deterministically.
	if v := mustNum(t, m, "proc.emday-definitely-not-running.running"); v != 0 {
		t.Errorf("absent process running = %v, want 0", v)
	}
	if v := mustNum(t, m, "proc.emday-definitely-not-running.count"); v != 0 {
		t.Errorf("absent process count = %v, want 0", v)
	}
}
