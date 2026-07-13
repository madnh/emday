package source

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// --- cpu ---

type cpuSource struct{ name string }

func newCPUSource(name string) *cpuSource { return &cpuSource{name: name} }

func (s *cpuSource) Name() string { return s.name }

func (s *cpuSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	// percent since the previous call (first call primes the counter)
	percents, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return nil, nil, err
	}
	var samples []model.Sample
	if len(percents) > 0 {
		samples = append(samples, model.Sample{Metric: s.name + ".percent", Value: model.NumValue(round1(percents[0])), Time: now})
	}
	if runtime.GOOS != "windows" {
		if avg, err := load.AvgWithContext(ctx); err == nil {
			samples = append(samples,
				model.Sample{Metric: s.name + ".load1", Value: model.NumValue(avg.Load1), Time: now},
				model.Sample{Metric: s.name + ".load5", Value: model.NumValue(avg.Load5), Time: now},
				model.Sample{Metric: s.name + ".load15", Value: model.NumValue(avg.Load15), Time: now},
			)
		}
	}
	return samples, nil, nil
}

// --- memory ---

type memorySource struct{ name string }

func newMemorySource(name string) *memorySource { return &memorySource{name: name} }

func (s *memorySource) Name() string { return s.name }

func (s *memorySource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	samples := []model.Sample{
		{Metric: s.name + ".percent", Value: model.NumValue(round1(vm.UsedPercent)), Time: now},
		{Metric: s.name + ".used_mb", Value: model.NumValue(float64(vm.Used) / 1024 / 1024), Time: now},
		{Metric: s.name + ".total_mb", Value: model.NumValue(float64(vm.Total) / 1024 / 1024), Time: now},
	}
	if swap, err := mem.SwapMemoryWithContext(ctx); err == nil && swap.Total > 0 {
		samples = append(samples, model.Sample{Metric: s.name + ".swap_percent", Value: model.NumValue(round1(swap.UsedPercent)), Time: now})
	}
	return samples, nil, nil
}

// --- disk ---

type diskSource struct {
	name   string
	mounts map[string]string // alias -> path
}

func newDiskSource(name string, cfg *config.Source) *diskSource {
	mounts := cfg.Mounts
	if len(mounts) == 0 {
		if runtime.GOOS == "windows" {
			mounts = map[string]string{"c": `C:\`}
		} else {
			mounts = map[string]string{"root": "/"}
		}
	}
	return &diskSource{name: name, mounts: mounts}
}

func (s *diskSource) Name() string { return s.name }

func (s *diskSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	var samples []model.Sample
	var errs []string
	for alias, path := range s.mounts {
		usage, err := disk.UsageWithContext(ctx, path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s (%s): %v", alias, path, err))
			continue
		}
		prefix := s.name + "." + alias
		samples = append(samples,
			model.Sample{Metric: prefix + ".percent", Value: model.NumValue(round1(usage.UsedPercent)), Time: now},
			model.Sample{Metric: prefix + ".free_gb", Value: model.NumValue(round1(float64(usage.Free) / 1024 / 1024 / 1024)), Time: now},
		)
	}
	if len(samples) == 0 && len(errs) > 0 {
		return nil, nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return samples, nil, nil
}

// --- process ---

type processSource struct {
	name  string
	watch []string
}

func newProcessSource(name string, cfg *config.Source) *processSource {
	return &processSource{name: name, watch: cfg.Processes}
}

func (s *processSource) Name() string { return s.name }

func (s *processSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	counts := make(map[string]float64, len(s.watch))
	for _, p := range procs {
		pname, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		for _, w := range s.watch {
			if pname == w {
				counts[w]++
			}
		}
	}
	var samples []model.Sample
	for _, w := range s.watch {
		prefix := s.name + "." + w
		samples = append(samples,
			model.Sample{Metric: prefix + ".running", Value: model.BoolValue(counts[w] > 0), Time: now},
			model.Sample{Metric: prefix + ".count", Value: model.NumValue(counts[w]), Time: now},
		)
	}
	return samples, nil, nil
}
