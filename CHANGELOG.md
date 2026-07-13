# Changelog

All notable changes to emday are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/) (pre-1.0: minor bumps may break).

## [Unreleased]

## [0.1.0] - 2026-07-13

First working release — the full pipeline described in DESIGN.md.

### Added

- **Sources**: `ip` (public IP via user-configured endpoints with strict
  IPv4/IPv6 validation and family-pinned HTTP, plus local NIC addresses),
  `cpu`, `memory`, `disk`, `process`, and `exec` — the script extension
  mechanism with the `$EMDAY_OUTPUT` file channel (`KEY=VALUE`, heredoc
  multi-line values, `NOTIFY`/`NOTIFY_WARN`/`NOTIFY_ERROR` directives) and
  an opt-in `parse: stdout` mode (metrics only).
- **Rules**: `on_change` change detection (including key-disappeared),
  expression conditions (expr-lang: comparisons, contains/startsWith/endsWith,
  in/not in, and/or/not, regex), `for` anti-flapping, `resolve_for`,
  resolved notifications, per-rule `cooldown`, all persisted across restarts.
- **Notifiers**: `telegram` (HTML mode, untrusted content escaped), `ntfy`
  (tags + priority mapping), `lark` (interactive card, optional HMAC
  signature, body-level error detection), `slack`, `discord`, and generic
  `webhook` with Go templates. Per-notifier persistent queue with retry —
  alerts survive network outages and restarts.
- **Config directory**: self-contained (`emday.yaml` + `config.md` +
  `state.json` + `queue/`), explicit resolution order (flag → env →
  platform defaults), `init` as the only command that creates state.
- **CLI**: `init`, `run`, `check-config`, `test-rule`, `test-notify`,
  `doctor` (strictly read-only, `--json`, `--verdict`), service management
  (`install`/`uninstall`/`start`/`stop`/`restart`/`status` via
  systemd/launchd/Windows Services), `docs` (embedded documentation,
  including `docs agent` for AI agents; alias: `skills`), `version`.
- **Docs**: everything ships inside the binary (`emday docs`); `init`
  writes the config guide next to the config.

[Unreleased]: https://github.com/madnh/emday/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/madnh/emday/releases/tag/v0.1.0
