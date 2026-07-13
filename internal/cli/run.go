package cli

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/engine"
	"github.com/madnh/emday/internal/notify"
	"github.com/madnh/emday/internal/state"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run in the foreground (Ctrl-C to stop)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return runDaemon(ctx)
		},
	}
}

// runDaemon is the shared core of `run` and the service entry point.
// All logging goes to stderr; the first line announces the resolved dir.
func runDaemon(ctx context.Context) error {
	dir, err := config.MustResolve(flagConfigDir)
	if err != nil {
		return err
	}
	log.Printf("using config dir %s", dir)

	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if probs := cfg.Validate(); len(probs) > 0 {
		var lines []string
		for _, p := range probs {
			lines = append(lines, "  "+p.String())
		}
		return fmt.Errorf("config has %d problem(s):\n%s\nFix them and re-run `%s check-config`.",
			len(probs), strings.Join(lines, "\n"), "emday")
	}

	st, err := state.Load(cfg.StatePath())
	if err != nil {
		return err
	}

	notifiers := map[string]notify.Notifier{}
	for name, nc := range cfg.Notifiers {
		n, err := notify.New(name, nc)
		if err != nil {
			return fmt.Errorf("notifier %s: %w", name, err)
		}
		notifiers[name] = n
	}
	queue, err := notify.NewQueue(cfg.QueueDir(), notifiers)
	if err != nil {
		return err
	}

	eng, err := engine.New(cfg, st, queue)
	if err != nil {
		return err
	}
	srcs := eng.Sources()
	sort.Strings(srcs)
	log.Printf("watching: %s", strings.Join(srcs, ", "))

	eng.Run(ctx)
	log.Printf("shut down cleanly")
	return nil
}
