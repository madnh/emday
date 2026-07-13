// Package cli implements the emday command-line interface.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/madnh/emday/internal/appinfo"
	"github.com/madnh/emday/internal/buildinfo"
)

var (
	flagConfigDir      string
	flagNonInteractive bool
)

// EnvNonInteractive force-disables prompts regardless of TTY.
const EnvNonInteractive = "EMDAY_NONINTERACTIVE"

// isInteractive reports whether prompting a human is appropriate.
// Explicit overrides win over TTY detection; there is deliberately no way
// to force interactive on.
func isInteractive() bool {
	if flagNonInteractive {
		return false
	}
	if v := os.Getenv(EnvNonInteractive); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// NewRoot builds the root command tree.
func NewRoot() *cobra.Command {
	name := appinfo.Name()
	root := &cobra.Command{
		Use:   name,
		Short: "Self-contained server monitoring with notifications",
		Long: name + ` watches this machine — public IP, CPU, RAM, disk, processes,
and anything a script can measure — and notifies you (Telegram, ntfy,
webhooks) when something changes or crosses a threshold.

All documentation ships inside this binary:

  ` + name + ` docs            list documentation topics
  ` + name + ` docs agent      compact operating guide for AI agents —
                        an agent can configure, extend, and diagnose
                        ` + name + ` from that one page`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	}
	root.Version = buildinfo.String()
	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().StringVar(&flagConfigDir, "config-dir", "", "config directory (default: $EMDAY_CONFIG_DIR, then platform locations)")
	root.PersistentFlags().BoolVar(&flagNonInteractive, "non-interactive", false, "never prompt; fail instead (also: "+EnvNonInteractive+"=1)")

	root.AddCommand(
		newInitCmd(),
		newRunCmd(),
		newDoctorCmd(),
		newCheckConfigCmd(),
		newTestRuleCmd(),
		newTestNotifyCmd(),
		newDocsCmd(),
		newVersionCmd(),
	)
	root.AddCommand(serviceCmds()...)
	return root
}

// Execute runs the CLI.
func Execute() {
	if err := NewRoot().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appinfo.Name(), err)
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
		},
	}
}
