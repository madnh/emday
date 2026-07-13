// Package source implements the collectors: built-in system metrics and the
// exec extension mechanism.
package source

import (
	"context"
	"fmt"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// Source collects samples (metrics) and, for exec, direct events.
type Source interface {
	Name() string
	Collect(ctx context.Context) ([]model.Sample, []model.Event, error)
}

// New builds a source from its config. Returns an error for source types
// unsupported on this platform (callers disable them with a warning).
func New(name string, cfg *config.Source, tmpDir string) (Source, error) {
	switch cfg.Type {
	case "ip":
		return newIPSource(name, cfg), nil
	case "cpu":
		return newCPUSource(name), nil
	case "memory":
		return newMemorySource(name), nil
	case "disk":
		return newDiskSource(name, cfg), nil
	case "process":
		return newProcessSource(name, cfg), nil
	case "exec":
		return newExecSource(name, cfg, tmpDir), nil
	default:
		return nil, fmt.Errorf("unknown source type %q", cfg.Type)
	}
}
