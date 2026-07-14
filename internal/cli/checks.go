package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/madnh/emday/internal/appinfo"
	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/docs"
	"github.com/madnh/emday/internal/model"
	"github.com/madnh/emday/internal/notify"
)

func newCheckConfigCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "check-config",
		Short: "Validate the config and compile every rule condition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.MustResolve(flagConfigDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			probs := cfg.Validate()

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(map[string]any{
					"config_dir": dir,
					"ok":         len(probs) == 0,
					"sources":    len(cfg.Sources),
					"rules":      len(cfg.Rules),
					"notifiers":  len(cfg.Notifiers),
					"problems":   probs,
				}); err != nil {
					return err
				}
				// Still exit non-zero on problems so CI/Ansible can gate on the
				// exit code alone — the JSON already went to stdout, this error
				// only reaches stderr.
				if len(probs) > 0 {
					return fmt.Errorf("%d problem(s) found", len(probs))
				}
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "config dir: %s\n", dir)
			fmt.Fprintf(out, "%d source(s), %d rule(s), %d notifier(s)\n", len(cfg.Sources), len(cfg.Rules), len(cfg.Notifiers))
			if len(probs) == 0 {
				fmt.Fprintln(out, "ok")
				return nil
			}
			for _, p := range probs {
				fmt.Fprintf(out, "problem: %s\n", p.String())
			}
			return fmt.Errorf("%d problem(s) found", len(probs))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	return cmd
}

func newTestRuleCmd() *cobra.Command {
	var value string
	cmd := &cobra.Command{
		Use:   "test-rule <condition>",
		Short: "Evaluate a rule condition against a value (a syntax REPL)",
		Long: `Compiles a condition and evaluates it against --value, printing true or
false. Needs no config dir — try syntax before committing it to emday.yaml.
Cheat sheet: ` + appinfo.Name() + ` docs conditions`,
		Example: `  ` + appinfo.Name() + ` test-rule 'value > 90' --value 95
  ` + appinfo.Name() + ` test-rule 'value not in ["ok", "skipped"]' --value failed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("value") {
				return fmt.Errorf("pass the metric value to test with --value <v>")
			}
			prog, err := config.CompileCondition(args[0])
			if err != nil {
				return fmt.Errorf("condition does not compile: %w\nSee `%s docs conditions` for the syntax cheat sheet.", err, appinfo.Name())
			}
			result, err := config.EvalCondition(prog, model.ParseValue(value).Native())
			if err != nil {
				return fmt.Errorf("evaluation failed: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().StringVar(&value, "value", "", "metric value to evaluate against (number or string)")
	return cmd
}

func newTestNotifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test-notify <notifier>",
		Short: "Send a real test notification through a configured notifier",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.MustResolve(flagConfigDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			nc, ok := cfg.Notifiers[args[0]]
			if !ok {
				return fmt.Errorf("no notifier %q in config (defined: %s)", args[0], notifierNames(cfg))
			}
			n, err := notify.New(args[0], nc)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			err = n.Send(ctx, model.Event{
				Source:  "test",
				Level:   model.LevelInfo,
				Title:   "emday test notification",
				Message: fmt.Sprintf("If you can read this, notifier %q works.", args[0]),
				Time:    time.Now(),
			})
			if err != nil {
				return fmt.Errorf("send failed: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sent via %s — check the destination\n", args[0])
			return nil
		},
	}
}

func notifierNames(cfg *config.Config) string {
	names := ""
	for name := range cfg.Notifiers {
		if names != "" {
			names += ", "
		}
		names += name
	}
	if names == "" {
		return "none"
	}
	return names
}

func newDocsCmd() *cobra.Command {
	name := appinfo.Name()
	cmd := &cobra.Command{
		Use:     "docs [topic]",
		Aliases: []string{"skills"},
		Short:   "Read the built-in documentation (self-teaching, also for AI agents)",
		Long: `All ` + name + ` documentation ships inside this binary — humans and AI
agents learn ` + name + ` from ` + name + ` itself, no external docs needed.

Run with no argument for the topic list. `[1:] + "`" + name + ` docs agent` + "`" + ` prints a
compact operating guide an AI agent can load as context to configure,
extend, and diagnose ` + name + `.`,
		ValidArgs: docs.Topics(),
		Args:      cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := "index"
			if len(args) == 1 {
				topic = args[0]
			}
			content, err := docs.Topic(topic)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), content)
			return nil
		},
	}
	return cmd
}
