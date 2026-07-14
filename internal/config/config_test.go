package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const minimalConfig = `
version: 1
sources:
  cpu: {type: cpu}
rules:
  - metric: cpu.percent
    condition: "value >= 90"
    notify: [hook]
notifiers:
  hook: {type: webhook, url: "https://example.com/x"}
`

func TestLoadValidMinimal(t *testing.T) {
	dir := writeConfig(t, minimalConfig)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if probs := cfg.Validate(); len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	// defaults applied
	if cfg.Defaults.Cooldown.Duration != 30*time.Minute {
		t.Errorf("default cooldown = %v", cfg.Defaults.Cooldown.Duration)
	}
	if cfg.Sources["cpu"].Interval.Duration != time.Minute {
		t.Errorf("default interval = %v", cfg.Sources["cpu"].Interval.Duration)
	}
	if cfg.Rules[0].Level != "warn" {
		t.Errorf("default level = %q", cfg.Rules[0].Level)
	}
	// derived paths stay inside the dir
	if cfg.StatePath() != filepath.Join(dir, "state.json") {
		t.Errorf("state path = %s", cfg.StatePath())
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	dir := writeConfig(t, "version: 99\n")
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "upgrade emday") {
		t.Fatalf("want newer-schema refusal, got %v", err)
	}
}

func TestLoadRejectsMissingVersion(t *testing.T) {
	dir := writeConfig(t, "sources: {}\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("config without version must be refused")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := writeConfig(t, "version: 1\nsurprise_field: true\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("unknown top-level fields must fail loudly, not be ignored")
	}
}

func TestValidateFindsProblems(t *testing.T) {
	dir := writeConfig(t, `
version: 1
sources:
  bad: {type: quantum}
  runner: {type: exec}
rules:
  - metric: x
    on_change: true
    condition: "value > 1"
    notify: [ghost]
  - metric: y
    condition: "value >>> nonsense"
    notify: [hook]
notifiers:
  hook: {type: webhook}
  tg: {type: telegram}
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	probs := cfg.Validate()
	wantSubstrings := []string{
		"unknown type \"quantum\"",
		"exec source needs `command`",
		"on_change or condition, not both",
		"is not a defined notifier",
		"does not compile",
		"webhook needs `url`",
		"chat_id",
	}
	joined := ""
	for _, p := range probs {
		joined += p.String() + "\n"
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
}

// Born from a real incident: a pasted secret in secret_env silently sent
// unsigned Lark messages. check-config must catch it; real env names must pass.
func TestSecretValuePastedIntoEnvField(t *testing.T) {
	dir := writeConfig(t, `
version: 1
sources:
  cpu: {type: cpu}
notifiers:
  lark:
    type: lark
    url: "https://open.larksuite.com/x"
    secret_env: LB7KiBoqCvWfbE9c8Gvy8
  tg:
    type: telegram
    token_env: EMDAY_TG_TOKEN
    chat_id: "1"
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	probs := cfg.Validate()
	found := false
	for _, p := range probs {
		if strings.Contains(p.Msg, "looks like a secret value") {
			if !strings.Contains(p.Where, "lark") {
				t.Errorf("flagged the wrong notifier: %s", p.Where)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("pasted secret in secret_env not caught; problems: %v", probs)
	}
	for _, p := range probs {
		if strings.Contains(p.Where, "tg") && strings.Contains(p.Msg, "secret value") {
			t.Errorf("EMDAY_TG_TOKEN is a legit env name, must not be flagged: %v", p)
		}
	}
}

func TestURLEnvSatisfiesURLAndRejectsPastedURL(t *testing.T) {
	dir := writeConfig(t, `
version: 1
sources:
  cpu: {type: cpu}
notifiers:
  good: {type: slack, url_env: EMDAY_SLACK_URL}
  pasted: {type: slack, url_env: "https://hooks.slack.com/services/T/B/X"}
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	probs := cfg.Validate()
	for _, p := range probs {
		if strings.Contains(p.Where, "good") {
			t.Errorf("url_env should satisfy the URL requirement, but got: %v", p)
		}
	}
	found := false
	for _, p := range probs {
		if strings.Contains(p.Where, "pasted") && strings.Contains(p.Msg, "url_env must be the NAME") {
			found = true
		}
	}
	if !found {
		t.Errorf("a URL pasted into url_env was not flagged; problems: %v", probs)
	}
}

func TestConditionCompileAndEval(t *testing.T) {
	cases := []struct {
		cond  string
		value any
		want  bool
	}{
		{"value > 90", 95.0, true},
		{"value > 90", 50.0, false},
		{`value in ["ok", "skipped"]`, "ok", true},
		{`value not in ["ok"]`, "failed", true},
		{`value startsWith "eth"`, "eth0", true},
		{`value contains "err" or value == "dead"`, "dead", true},
	}
	for _, c := range cases {
		prog, err := CompileCondition(c.cond)
		if err != nil {
			t.Fatalf("compile %q: %v", c.cond, err)
		}
		got, err := EvalCondition(prog, c.value)
		if err != nil {
			t.Fatalf("eval %q on %v: %v", c.cond, c.value, err)
		}
		if got != c.want {
			t.Errorf("%q on %v = %v, want %v", c.cond, c.value, got, c.want)
		}
	}
}

func TestResolveExplicitRequiresInitialized(t *testing.T) {
	t.Setenv(EnvConfigDir, "")
	res := Resolve(t.TempDir()) // empty dir, no marker
	if res.Err == nil || !strings.Contains(res.Err.Error(), "init") {
		t.Fatalf("uninitialized explicit dir must error and point at init, got %v", res.Err)
	}
}

func TestResolveViaEnv(t *testing.T) {
	dir := writeConfig(t, minimalConfig)
	t.Setenv(EnvConfigDir, dir)
	res := Resolve("")
	if res.Err != nil || res.Dir != dir || res.Source != "env" {
		t.Fatalf("res = %+v", res)
	}
}

func TestResolveFlagWinsOverEnv(t *testing.T) {
	flagDir := writeConfig(t, minimalConfig)
	envDir := writeConfig(t, minimalConfig)
	t.Setenv(EnvConfigDir, envDir)
	res := Resolve(flagDir)
	if res.Dir != flagDir || res.Source != "flag" {
		t.Fatalf("flag must win: %+v", res)
	}
}

func TestRuleIDStability(t *testing.T) {
	a := &Rule{Metric: "cpu.percent", Condition: "value > 90"}
	b := &Rule{Metric: "cpu.percent", Condition: "value > 90"}
	if a.ID() != b.ID() {
		t.Error("identical rules must share an ID (state survives restart)")
	}
	c := &Rule{Metric: "cpu.percent", Condition: "value > 95"}
	if a.ID() == c.ID() {
		t.Error("changed condition must change the ID")
	}
}

func TestDurationParsing(t *testing.T) {
	dir := writeConfig(t, `
version: 1
defaults: {cooldown: bogus}
`)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "invalid duration") {
		t.Fatalf("bogus duration must fail with a clear error, got %v", err)
	}
}

// The `ip` type was split in v0.1.1 — old configs must get a directive
// migration message, and cross-type field misuse must be caught.
func TestIPSourceSplit(t *testing.T) {
	dir := writeConfig(t, `
version: 1
sources:
  old: {type: ip}
  lan: {type: local-ip, mode: [v4]}
  wan: {type: public-ip, interfaces: [eth0]}
notifiers: {}
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, p := range cfg.Validate() {
		joined += p.String() + "\n"
	}
	for _, want := range []string{
		"was split in v0.1.1",            // migration hint for type: ip
		"local-ip needs `interfaces`",    // lan lacks interfaces
		"belong to a `public-ip` source", // lan has mode
		"belongs to a `local-ip` source", // wan has interfaces
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}
