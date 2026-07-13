package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/madnh/emday/internal/appinfo"
	"github.com/madnh/emday/internal/buildinfo"
	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/notify"
)

// doctorReport is the full diagnosis. Everything in it was gathered
// read-only: doctor never creates or mutates what it inspects.
type doctorReport struct {
	Binary     string            `json:"binary"`
	Version    string            `json:"version"`
	Cwd        string            `json:"cwd"`
	FlagDir    string            `json:"flag_config_dir,omitempty"`
	EnvDir     string            `json:"env_config_dir,omitempty"`
	ConfigDir  string            `json:"config_dir,omitempty"`
	DirSource  string            `json:"config_dir_source,omitempty"`
	Candidates []candidateReport `json:"candidates,omitempty"`
	ResolveErr string            `json:"resolve_error,omitempty"`

	ConfigProblems []config.Problem `json:"config_problems,omitempty"`
	ConfigLoadErr  string           `json:"config_load_error,omitempty"`
	Sources        []string         `json:"sources,omitempty"`
	Rules          int              `json:"rules"`
	Notifiers      []string         `json:"notifiers,omitempty"`
	EnvMissing     []string         `json:"env_missing,omitempty"` // notifier env vars unset in THIS shell

	StateExists  bool           `json:"state_exists"`
	StateSize    int64          `json:"state_size,omitempty"`
	StateModTime string         `json:"state_mod_time,omitempty"`
	QueuePending map[string]int `json:"queue_pending,omitempty"`

	Verdict string `json:"verdict,omitempty"`
}

type candidateReport struct {
	Path      string `json:"path"`
	HasMarker bool   `json:"has_marker"`
}

func newDoctorCmd() *cobra.Command {
	var jsonOut, verdict bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the deployment — strictly read-only",
		Long: `Answers "which config dir resolves, is my config valid, is state healthy,
are notifications stuck?" without creating or changing anything. Unlike
other commands it does not fail when no config dir resolves — an unresolved
dir is precisely what it reports.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := gatherDoctor()
			if verdict || jsonOut {
				rep.Verdict = verdictFor(rep)
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}
			printDoctor(cmd, rep, verdict)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&verdict, "verdict", false, "add a plain-language conclusion and next step")
	return cmd
}

func gatherDoctor() *doctorReport {
	rep := &doctorReport{
		Binary:  appinfo.Executable(),
		Version: buildinfo.String(),
	}
	rep.Cwd, _ = os.Getwd()

	res := config.Resolve(flagConfigDir)
	rep.FlagDir, rep.EnvDir = res.FlagValue, res.EnvValue
	for _, c := range res.Candidates {
		rep.Candidates = append(rep.Candidates, candidateReport{Path: c.Path, HasMarker: c.HasMarker})
	}
	if res.Err != nil {
		rep.ResolveErr = res.Err.Error()
		return rep
	}
	rep.ConfigDir, rep.DirSource = res.Dir, res.Source

	cfg, err := config.Load(res.Dir)
	if err != nil {
		rep.ConfigLoadErr = err.Error()
	} else {
		rep.ConfigProblems = cfg.Validate()
		for name := range cfg.Sources {
			rep.Sources = append(rep.Sources, name)
		}
		for name, n := range cfg.Notifiers {
			rep.Notifiers = append(rep.Notifiers, name)
			for _, env := range []string{n.TokenEnv, n.SecretEnv} {
				if env != "" && os.Getenv(env) == "" {
					rep.EnvMissing = append(rep.EnvMissing, fmt.Sprintf("notifiers.%s: $%s", name, env))
				}
			}
		}
		rep.Rules = len(cfg.Rules)
	}

	statePath := filepath.Join(res.Dir, "state.json")
	if fi, err := os.Stat(statePath); err == nil {
		rep.StateExists = true
		rep.StateSize = fi.Size()
		rep.StateModTime = fi.ModTime().Format(time.RFC3339)
	}
	rep.QueuePending = notify.Pending(filepath.Join(res.Dir, "queue"))
	return rep
}

// verdictFor walks failure modes outermost-in and states the next step.
func verdictFor(r *doctorReport) string {
	name := appinfo.Name()
	switch {
	case r.ResolveErr != "":
		return fmt.Sprintf("No config dir found. Run `%s init` to create one, or point at an existing one with --config-dir / %s.", name, config.EnvConfigDir)
	case r.ConfigLoadErr != "":
		return fmt.Sprintf("Config dir resolved (%s) but emday.yaml does not load: %s", r.ConfigDir, r.ConfigLoadErr)
	case len(r.ConfigProblems) > 0:
		return fmt.Sprintf("Config loads but has %d problem(s) — run `%s check-config` and fix them.", len(r.ConfigProblems), name)
	case !r.StateExists:
		return fmt.Sprintf("Config is valid but there is no state yet — has `%s run` (or the service) ever started? Check `%s status`.", name, name)
	default:
		for target, n := range r.QueuePending {
			if n > 0 {
				return fmt.Sprintf("Deliveries are stuck: %d event(s) queued for %q — the notifier is failing. Verify its credentials/URL, then `%s test-notify %s`.", n, target, name, target)
			}
		}
		stale := ""
		if t, err := time.Parse(time.RFC3339, r.StateModTime); err == nil && time.Since(t) > 15*time.Minute {
			stale = fmt.Sprintf(" Note: state was last written %s — if that seems old, the daemon may not be running (`%s status`).", r.StateModTime, name)
		}
		return "Everything looks healthy." + stale
	}
}

func printDoctor(cmd *cobra.Command, r *doctorReport, verdict bool) {
	out := cmd.OutOrStdout()
	p := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }

	p("▸ Resolution")
	p("  binary        %s", r.Binary)
	p("  version       %s", r.Version)
	p("  cwd           %s", r.Cwd)
	if r.FlagDir != "" {
		p("  --config-dir  %s", r.FlagDir)
	}
	if r.EnvDir != "" {
		p("  %s  %s", config.EnvConfigDir, r.EnvDir)
	}
	if r.ConfigDir != "" {
		p("  config dir    %s  (via %s)", r.ConfigDir, r.DirSource)
	} else {
		p("  config dir    UNRESOLVED")
		for _, c := range r.Candidates {
			marker := "no marker"
			if c.HasMarker {
				marker = "has marker"
			}
			p("    candidate   %s  (%s)", c.Path, marker)
		}
		p("  error         %s", r.ResolveErr)
	}

	if r.ConfigDir != "" {
		p("▸ Config")
		if r.ConfigLoadErr != "" {
			p("  load error    %s", r.ConfigLoadErr)
		} else {
			p("  sources       %d  rules %d  notifiers %d", len(r.Sources), r.Rules, len(r.Notifiers))
			if len(r.ConfigProblems) == 0 {
				p("  validation    ok")
			}
			for _, prob := range r.ConfigProblems {
				p("  problem       %s", prob.String())
			}
			for _, miss := range r.EnvMissing {
				p("  env ⚠         %s is not set in THIS shell (a service gets its env from the service manager, e.g. `systemctl edit emday`)", miss)
			}
		}
		p("▸ Runtime")
		if r.StateExists {
			p("  state.json    %d bytes, modified %s", r.StateSize, r.StateModTime)
		} else {
			p("  state.json    missing (daemon has not run yet)")
		}
		for target, n := range r.QueuePending {
			p("  queue %-12s %d pending", target, n)
		}
	}

	if verdict {
		p("▸ Verdict")
		p("  %s", r.Verdict)
	}
}
