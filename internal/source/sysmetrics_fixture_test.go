package source

// Deterministic fixture tests. gopsutil supports pointing its /proc reads at a
// fake tree via the HOST_PROC env var (this is how gopsutil tests itself), so
// we can feed a known /proc/meminfo and /proc/loadavg and assert the EXACT
// numbers emday derives. Where the live-system tests catch gross breakage,
// these pin the units and arithmetic precisely: if a gopsutil upgrade changes
// how it parses a field or what unit it returns, the expected value shifts and
// the test fails on CI. Linux-only — HOST_PROC has no effect on the macOS/BSD
// (sysctl) code paths.

import (
	"runtime"
	"testing"
)

func TestMemoryFixtureExactUnits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HOST_PROC fixtures only drive the Linux /proc code path")
	}
	// meminfo fixture: MemTotal 8000000 kB, MemAvailable 4000000 kB, no swap.
	t.Setenv("HOST_PROC", "testdata/proc")
	m := collectMap(t, newMemorySource("memory"))

	// Total = 8000000 kB = 8192000000 B -> 7812.5 MiB
	if got := m["memory.total_mb"].Num; got != 7812.5 {
		t.Errorf("memory.total_mb = %v, want 7812.5 (unit drift?)", got)
	}
	// Used = Total - Available = 4000000 kB -> 3906.25 MiB
	if got := m["memory.used_mb"].Num; got != 3906.25 {
		t.Errorf("memory.used_mb = %v, want 3906.25", got)
	}
	// UsedPercent = 4000000 / 8000000 * 100 = 50
	if got := m["memory.percent"].Num; got != 50 {
		t.Errorf("memory.percent = %v, want 50", got)
	}
	// SwapTotal is 0 in the fixture -> swap_percent must be absent.
	if _, ok := m["memory.swap_percent"]; ok {
		t.Errorf("swap_percent must be absent when the host has no swap")
	}
}

func TestCPULoadFixtureExact(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HOST_PROC fixtures only drive the Linux /proc code path")
	}
	// loadavg fixture: "0.50 1.50 2.50 ..."
	t.Setenv("HOST_PROC", "testdata/proc")
	s := newCPUSource("cpu")
	m := collectMap(t, s)

	for k, want := range map[string]float64{
		"cpu.load1":  0.5,
		"cpu.load5":  1.5,
		"cpu.load15": 2.5,
	} {
		if got := m[k].Num; got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
	// cpu.percent is a delta between two /proc/stat reads, not deterministic
	// from a single static fixture, so it is exercised by TestCPUSourceContract
	// (range check) rather than pinned here.
}
