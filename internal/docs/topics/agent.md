# emday — operating guide for AI agents

You are working with emday, a single-binary monitoring daemon. This page is
the complete mental model; the other topics (`emday docs config`,
`conditions`, `exec`, `notifiers`) hold the details. You never need
documentation outside this binary.

## Concept model

    sources ──samples──▶ rules ──events──▶ notifiers
    (collect metrics)    (decide)          (deliver, queued+retried)

- A **source** collects metrics on an interval: built-ins `public-ip` (via
  user-configured endpoints, strictly validated IPv4/IPv6), `local-ip` (NIC
  addresses from the kernel), `cpu`, `memory`, `disk`, `process`, and `exec`
  (any script).
- A **rule** watches one metric: `on_change: true` fires on any change;
  `condition: "value >= 90"` (expression over `value`) with optional `for: 5m`
  (must hold continuously) fires an alert and later a "resolved" event.
- A **notifier** delivers events: `telegram`, `ntfy`, `lark`, `slack`,
  `discord`, `webhook` (generic, templated). Each has a persistent retry queue.
- **exec scripts** append `KEY=VALUE` metrics and `NOTIFY_ERROR=...` direct
  notifications to the file `$EMDAY_OUTPUT` (GitHub Actions model).

## State & layout

Everything lives in one config directory (resolution: `--config-dir` flag →
`EMDAY_CONFIG_DIR` env → platform defaults containing `emday.yaml`):
`emday.yaml` (the only file you edit), `config.md` (generated guide),
`state.json`, `queue/`, `tmp/` (all managed — do not edit).

## Commands you will use

    emday init [--config-dir D] [--yes]     create config dir (ONLY command that creates one)
    emday check-config [--json]             validate + compile all conditions; run after EVERY config edit
    emday test-rule '<expr>' --value <v>    evaluate a condition instantly (no config needed)
    emday test-notify <name>                send a test event through a notifier
    emday doctor [--json] [--verdict]       diagnose; strictly read-only, safe to run anytime
    emday run                               foreground run (Ctrl-C to stop)
    emday install|start|stop|status|uninstall   manage the system service
    emday docs [topic]                      this documentation
    emday version                           build info

## Workflows

**Configure monitoring**: edit `emday.yaml` → `emday check-config` → fix
reported problems (each names its location, e.g. `rules[2]`) → `emday
test-notify <target>` → restart the service (`emday stop && emday start`)
or `emday run` for foreground.

**Write an extension**: create a script appending `KEY=VALUE` to
`$EMDAY_OUTPUT` → add an exec source with `command`, `interval`, `notify` →
add rules on `<source>.<KEY>` → `emday check-config`. Health comes free:
`<source>._ok` is 0 when the script fails.

**Diagnose "no notifications"**: `emday doctor --json` — check, in order:
config dir resolved? config valid? state.json healthy? queue backlog per
notifier (backlog = delivery failing; check the notifier's credentials/URL,
then `emday test-notify <name>`). Service running? `emday status`.

**Try condition syntax**: `emday test-rule 'value not in ["ok"]' --value failed`
— treat it as a REPL; do not guess syntax into config files.

## Project home

Source and issues: https://github.com/madnh/emday — but prefer the built-in
docs and `--json` diagnostics over fetching anything remote.

## Contracts to respect

- `check-config` and `doctor` and `test-rule` never mutate anything.
- `test-notify` sends a real message to the target.
- Only `init` creates a config dir; it refuses to overwrite an existing one.
- Machine-readable output: pass `--json` to check-config/doctor.
- Exit codes: 0 success; non-zero = failure with a directive error on stderr.
