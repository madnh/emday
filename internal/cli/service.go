package cli

import (
	"context"
	"fmt"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"

	"github.com/madnh/emday/internal/appinfo"
	"github.com/madnh/emday/internal/config"
)

// program adapts runDaemon to the service manager lifecycle.
type program struct {
	cancel context.CancelFunc
	done   chan error
}

func (p *program) Start(_ service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)
	go func() { p.done <- runDaemon(ctx) }()
	return nil
}

func (p *program) Stop(_ service.Service) error {
	p.cancel()
	return <-p.done
}

func buildService() (service.Service, error) {
	// The service always gets an explicit --config-dir so it never depends
	// on the working directory or the invoking user's environment.
	dir, err := config.MustResolve(flagConfigDir)
	if err != nil {
		return nil, err
	}
	svcCfg := &service.Config{
		Name:        appinfo.CanonicalName, // fixed identifier, not the binary name
		DisplayName: "emday",
		Description: "emday — self-contained server monitoring with notifications",
		Arguments:   []string{"run", "--config-dir", dir},
	}
	return service.New(&program{}, svcCfg)
}

// serviceCmds returns the top-level service management commands
// (install/uninstall/start/stop/restart/status).
func serviceCmds() []*cobra.Command {
	actions := []struct {
		name, short string
		run         func(service.Service) error
	}{
		{"install", "Install emday as a system service (systemd/launchd/Windows)", func(s service.Service) error { return s.Install() }},
		{"uninstall", "Remove the emday system service", func(s service.Service) error { return s.Uninstall() }},
		{"start", "Start the emday service", func(s service.Service) error { return s.Start() }},
		{"stop", "Stop the emday service", func(s service.Service) error { return s.Stop() }},
		{"restart", "Restart the emday service", func(s service.Service) error { return s.Restart() }},
	}
	var cmds []*cobra.Command
	for _, a := range actions {
		a := a
		cmds = append(cmds, &cobra.Command{
			Use:   a.name,
			Short: a.short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				svc, err := buildService()
				if err != nil {
					return err
				}
				if err := a.run(svc); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: ok\n", a.name)
				return nil
			},
		})
	}

	cmds = append(cmds, &cobra.Command{
		Use:   "status",
		Short: "Show the emday service status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := buildService()
			if err != nil {
				return err
			}
			st, err := svc.Status()
			if err != nil {
				return err
			}
			out := map[service.Status]string{
				service.StatusRunning: "running",
				service.StatusStopped: "stopped",
			}[st]
			if out == "" {
				out = "unknown"
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	})
	return cmds
}
