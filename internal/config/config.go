// Package config defines the emday.yaml schema, the config-directory
// resolution rules, and validation (including compiling rule conditions).
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.yaml.in/yaml/v3"
)

// SchemaVersion is the newest emday.yaml schema this binary understands.
// A file with a greater version is refused (upgrade the binary instead of
// silently misreading it).
const SchemaVersion = 1

// MarkerFile is the fixed config filename whose presence marks a config dir.
// It names a data format, so it never changes with the binary name.
const MarkerFile = "emday.yaml"

// Duration wraps time.Duration for YAML ("5m", "30s").
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	Version   int                  `yaml:"version"`
	Defaults  Defaults             `yaml:"defaults"`
	Sources   map[string]*Source   `yaml:"sources"`
	Rules     []*Rule              `yaml:"rules"`
	Notifiers map[string]*Notifier `yaml:"notifiers"`

	// Dir is the resolved config directory (not stored in the file).
	Dir string `yaml:"-"`
}

type Defaults struct {
	Cooldown Duration `yaml:"cooldown"` // min interval between identical alerts; default 30m
	Interval Duration `yaml:"interval"` // default source interval; default 1m
}

// Source is one collector instance. Type-specific fields are flattened;
// only the ones matching Type are consulted.
type Source struct {
	Type     string   `yaml:"type"` // public-ip | local-ip | cpu | memory | disk | process | exec
	Interval Duration `yaml:"interval"`
	Notify   []string `yaml:"notify"` // targets for NOTIFY_* directives (exec)

	// exec
	Command string   `yaml:"command"`
	Timeout Duration `yaml:"timeout"`
	Parse   string   `yaml:"parse"` // "" (output-file, default) | "stdout"

	// ip
	Mode        []string `yaml:"mode"`         // subset of "v4", "v6"; default ["v4"]
	EndpointsV4 []string `yaml:"endpoints_v4"` // URLs returning a bare IPv4
	EndpointsV6 []string `yaml:"endpoints_v6"` // URLs returning a bare IPv6
	Interfaces  []string `yaml:"interfaces"`   // local NICs to report addresses for

	// disk
	Mounts map[string]string `yaml:"mounts"` // alias -> mount path

	// process
	Processes []string `yaml:"processes"` // process names to watch
}

type Rule struct {
	Metric     string    `yaml:"metric"`
	OnChange   bool      `yaml:"on_change"`
	Condition  string    `yaml:"condition"`
	For        Duration  `yaml:"for"`
	ResolveFor Duration  `yaml:"resolve_for"`
	Cooldown   *Duration `yaml:"cooldown"` // overrides defaults.cooldown
	Level      string    `yaml:"level"`    // info | warn | error; default warn
	Notify     []string  `yaml:"notify"`
}

// ID returns a stable identity for persisted rule state: it survives
// restarts and reordering, and changes when the rule meaningfully changes.
func (r *Rule) ID() string {
	kind := r.Condition
	if r.OnChange {
		kind = "on_change"
	}
	return r.Metric + "|" + kind + "|" + r.For.String()
}

type Notifier struct {
	Type string `yaml:"type"` // webhook | telegram | ntfy | lark | slack

	// shared
	URL string `yaml:"url"`
	// The webhook URL is often itself a secret (the token lives in the path),
	// so it can be resolved from an env var instead — keeping it out of the
	// config file, exactly like token_env/secret_env.
	URLEnv string `yaml:"url_env"` // env var holding the target URL

	// webhook
	Method       string            `yaml:"method"`
	Headers      map[string]string `yaml:"headers"`
	BodyTemplate string            `yaml:"body_template"`

	// telegram
	Token    string `yaml:"token"`     // discouraged; prefer token_env
	TokenEnv string `yaml:"token_env"` // env var holding the secret
	ChatID   string `yaml:"chat_id"`

	// ntfy
	Priority string `yaml:"priority"`

	// lark (custom bot signature, optional)
	Secret    string `yaml:"secret"`     // discouraged; prefer secret_env
	SecretEnv string `yaml:"secret_env"` // env var holding the signing secret
}

// StatePath and QueueDir are derived from the config dir — never configured —
// so the directory stays self-contained.
func (c *Config) StatePath() string { return filepath.Join(c.Dir, "state.json") }
func (c *Config) QueueDir() string  { return filepath.Join(c.Dir, "queue") }
func (c *Config) TmpDir() string    { return filepath.Join(c.Dir, "tmp") }

// Load reads and validates <dir>/emday.yaml. It does not create anything.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, MarkerFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Version > SchemaVersion {
		return nil, fmt.Errorf("%s declares schema version %d, but this binary only understands up to %d — upgrade emday", path, cfg.Version, SchemaVersion)
	}
	if cfg.Version < 1 {
		return nil, fmt.Errorf("%s: missing or invalid `version` (expected 1)", path)
	}
	cfg.Dir = dir
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Defaults.Cooldown.Duration == 0 {
		c.Defaults.Cooldown.Duration = 30 * time.Minute
	}
	if c.Defaults.Interval.Duration == 0 {
		c.Defaults.Interval.Duration = time.Minute
	}
	for _, s := range c.Sources {
		if s.Interval.Duration == 0 {
			s.Interval = c.Defaults.Interval
		}
		if s.Type == "exec" && s.Timeout.Duration == 0 {
			s.Timeout.Duration = 30 * time.Second
		}
	}
	for _, r := range c.Rules {
		if r.Level == "" {
			r.Level = "warn"
		}
	}
}
