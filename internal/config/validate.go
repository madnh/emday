package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

var sourceTypes = map[string]bool{
	"public-ip": true, "local-ip": true,
	"cpu": true, "memory": true, "disk": true, "process": true, "exec": true,
}

var notifierTypes = map[string]bool{
	"webhook": true, "telegram": true, "ntfy": true,
	"lark": true, "slack": true, "discord": true,
}

var levels = map[string]bool{"info": true, "warn": true, "error": true}

// Problem is one validation finding, addressable enough to fix.
type Problem struct {
	Where string `json:"where"` // "sources.backup", "rules[2]", "notifiers.tg"
	Msg   string `json:"msg"`
}

func (p Problem) String() string { return p.Where + ": " + p.Msg }

// Validate checks cross-references and compiles every rule condition.
// It returns all problems at once rather than stopping at the first.
func (c *Config) Validate() []Problem {
	var probs []Problem
	add := func(where, format string, args ...any) {
		probs = append(probs, Problem{Where: where, Msg: fmt.Sprintf(format, args...)})
	}

	if len(c.Sources) == 0 {
		add("sources", "no sources defined — emday would have nothing to watch")
	}
	for name, s := range c.Sources {
		where := "sources." + name
		if s.Type == "ip" {
			add(where, "type `ip` was split in v0.1.1: use `local-ip` (with interfaces) and/or `public-ip` (with mode/endpoints) — see `emday docs config`")
			continue
		}
		if !sourceTypes[s.Type] {
			add(where, "unknown type %q (see `emday docs config`)", s.Type)
			continue
		}
		if s.Type == "exec" {
			if s.Command == "" {
				add(where, "exec source needs `command`")
			}
			if s.Parse != "" && s.Parse != "stdout" {
				add(where, "parse must be omitted (output file) or \"stdout\", got %q", s.Parse)
			}
		}
		if s.Type == "public-ip" {
			for _, m := range s.Mode {
				if m != "v4" && m != "v6" {
					add(where, "mode entries must be \"v4\" or \"v6\", got %q", m)
				}
			}
			for _, u := range append(append([]string{}, s.EndpointsV4...), s.EndpointsV6...) {
				if parsed, err := url.Parse(u); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					add(where, "endpoint %q is not a valid http(s) URL", u)
				}
			}
			if len(s.Interfaces) > 0 {
				add(where, "`interfaces` belongs to a `local-ip` source, not public-ip")
			}
		}
		if s.Type == "local-ip" {
			if len(s.Interfaces) == 0 {
				add(where, "local-ip needs `interfaces` (e.g. [eth0])")
			}
			if len(s.Mode) > 0 || len(s.EndpointsV4) > 0 || len(s.EndpointsV6) > 0 {
				add(where, "`mode`/`endpoints_*` belong to a `public-ip` source, not local-ip")
			}
		}
		for _, target := range s.Notify {
			if _, ok := c.Notifiers[target]; !ok {
				add(where, "notify target %q is not a defined notifier", target)
			}
		}
	}

	for i, r := range c.Rules {
		where := fmt.Sprintf("rules[%d]", i)
		if r.Metric == "" {
			add(where, "missing `metric`")
		}
		if r.OnChange && r.Condition != "" {
			add(where, "use either on_change or condition, not both")
		}
		if !r.OnChange && r.Condition == "" {
			add(where, "needs on_change: true or a condition")
		}
		if r.Condition != "" {
			if _, err := CompileCondition(r.Condition); err != nil {
				add(where, "condition does not compile: %v (see `emday docs conditions`)", err)
			}
		}
		if !levels[r.Level] {
			add(where, "level must be info|warn|error, got %q", r.Level)
		}
		if len(r.Notify) == 0 {
			add(where, "no notify targets — this rule would alert nobody")
		}
		for _, target := range r.Notify {
			if _, ok := c.Notifiers[target]; !ok {
				add(where, "notify target %q is not a defined notifier", target)
			}
		}
	}

	for name, n := range c.Notifiers {
		where := "notifiers." + name
		for field, val := range map[string]string{"token_env": n.TokenEnv, "secret_env": n.SecretEnv} {
			if looksLikeSecretValue(val) {
				add(where, "%s must be the NAME of an environment variable (e.g. EMDAY_LARK_SECRET), but %q looks like a secret value itself — either export it under a name and reference that, or use `%s:` for an inline value",
					field, val, strings.TrimSuffix(field, "_env"))
			}
		}
		switch n.Type {
		case "webhook":
			if n.URL == "" {
				add(where, "webhook needs `url`")
			}
		case "telegram":
			if n.Token == "" && n.TokenEnv == "" {
				add(where, "telegram needs `token_env` (or `token`)")
			}
			if n.ChatID == "" {
				add(where, "telegram needs `chat_id`")
			}
		case "ntfy":
			if n.URL == "" {
				add(where, "ntfy needs `url` (e.g. https://ntfy.sh/your-topic)")
			}
		case "lark":
			if n.URL == "" {
				add(where, "lark needs `url` (the custom bot webhook)")
			}
		case "slack":
			if n.URL == "" {
				add(where, "slack needs `url` (an incoming webhook)")
			}
		case "discord":
			if n.URL == "" {
				add(where, "discord needs `url` (a channel webhook)")
			}
		default:
			add(where, "unknown type %q (supported: webhook, telegram, ntfy, lark, slack, discord)", n.Type)
		}
	}

	return probs
}

// looksLikeSecretValue guesses whether a *_env field holds a pasted secret
// instead of an env var name. Env var names are conventionally UPPER_SNAKE;
// tokens tend to be long, mixed-case, digit-bearing, and underscore-free.
// (Born from a real incident: `secret_env: LB7Ki...` → silent unsigned sends.)
func looksLikeSecretValue(v string) bool {
	if len(v) < 16 || strings.Contains(v, "_") {
		return false
	}
	var hasLower, hasUpper, hasDigit bool
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasLower && hasUpper && hasDigit
}

// CompileCondition compiles a rule expression. The only variable is `value`.
func CompileCondition(cond string) (*vm.Program, error) {
	return expr.Compile(cond, expr.AllowUndefinedVariables(), expr.AsBool())
}

// EvalCondition runs a compiled condition against a value.
func EvalCondition(prog *vm.Program, value any) (bool, error) {
	out, err := expr.Run(prog, map[string]any{"value": value})
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("condition returned %T, want bool", out)
	}
	return b, nil
}
