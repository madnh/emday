package cli

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madnh/emday/internal/appinfo"
	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/docs"
)

//go:embed example.yaml
var exampleConfig []byte

func newInitCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new config directory (the only command that does)",
		Long: `Creates and seeds a config directory: emday.yaml (a commented starter
config), config.md (the configuration guide), and empty state. Refuses to
overwrite an existing one. Every other command requires an initialized dir.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "accept the default directory without prompting")
	return cmd
}

func runInit(cmd *cobra.Command, yes bool) error {
	name := appinfo.Name()

	// Resolve the target for CREATION: explicit flag/env is the operator's
	// decision; otherwise propose a default and confirm with a human.
	target := flagConfigDir
	explicit := target != ""
	if !explicit {
		if env := os.Getenv(config.EnvConfigDir); env != "" {
			target, explicit = env, true
		}
	}
	if !explicit {
		target = config.DefaultCandidates()[defaultInitCandidate()]
		switch {
		case isInteractive():
			fmt.Fprintf(cmd.OutOrStdout(), "Initialize a new config dir at:\n  %s\nPress Enter to accept, or type a different path: ", target)
			line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil && line == "" {
				return fmt.Errorf("aborted")
			}
			if typed := strings.TrimSpace(line); typed != "" {
				target = typed
			}
		case yes:
			// accepted default, non-interactive
		default:
			return fmt.Errorf("no directory given and not running interactively — pass --config-dir <dir> or --yes to accept %s", target)
		}
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	marker := filepath.Join(abs, config.MarkerFile)
	if _, err := os.Stat(marker); err == nil {
		return fmt.Errorf("%s already contains %s — refusing to overwrite a live deployment; remove it first if you really mean to start over", abs, config.MarkerFile)
	}

	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	for _, sub := range []string{"queue", "tmp"} {
		if err := os.MkdirAll(filepath.Join(abs, sub), 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(marker, exampleConfig, 0o600); err != nil {
		return err
	}
	guide, err := docs.Topic("config")
	if err == nil {
		os.WriteFile(filepath.Join(abs, "config.md"), []byte(guide), 0o600)
	}
	if err := os.WriteFile(filepath.Join(abs, "state.json"), []byte("{}\n"), 0o600); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Initialized config dir: %s\n", abs)

	// If the dir is not auto-discoverable, say how to point commands at it —
	// otherwise the user inits a dir that `run` can't find.
	discoverable := false
	for _, cand := range config.DefaultCandidates() {
		if c, err := filepath.Abs(cand); err == nil && c == abs {
			discoverable = true
			break
		}
	}
	if !discoverable {
		fmt.Fprintf(out, "\nNote: this location is not auto-discovered. Point commands at it with:\n  --config-dir %s   or   %s=%s\n", abs, config.EnvConfigDir, abs)
	}
	fmt.Fprintf(out, "\nNext steps:\n  1. Edit %s (guide: %s docs config)\n  2. %s check-config\n  3. %s test-notify <notifier>\n  4. %s run   (foreground)  or  %s install (as a service)\n",
		marker, name, name, name, name, name)
	return nil
}

// defaultInitCandidate picks which DefaultCandidates entry to propose:
// the system location when running as root/admin, the user location otherwise.
func defaultInitCandidate() int {
	if os.Geteuid() == 0 {
		return 0
	}
	// candidates: [system, user, ./emday] — pick user when not root
	if len(config.DefaultCandidates()) > 1 {
		return 1
	}
	return 0
}
