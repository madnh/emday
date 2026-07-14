# Changelog

All notable changes to emday are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/) (pre-1.0: minor bumps may break).

## [Unreleased]

## [0.2.0] - 2026-07-14

### Added

- Notifiers that take `url:` (webhook, ntfy, lark, slack, discord) now also
  accept `url_env:` — the NAME of an environment variable holding the URL, so
  the webhook URL (itself a secret) can stay out of `emday.yaml`, exactly like
  `token_env`/`secret_env`. A `url_env` whose variable is unset is a hard
  startup error, and `emday doctor` reports it. `check-config` rejects a URL
  pasted into `url_env`.
- `emday docs deploy` — production/service guide: systemd unit (own-template
  and `emday install`), running non-root, delivering secrets via
  `EnvironmentFile=`, pinning a release, and gating a deploy on `check-config`.
- Per-source reference topics `emday docs source-<type>` (public-ip, local-ip,
  cpu, memory, disk, process): exact metrics with types/units, how each reads
  the system and what privilege it needs (non-root, `hidepid` caveat for
  `process`), rule examples, and failure modes (partial-tolerance, absent-vs-zero).

### Fixed

- `check-config --json` now exits non-zero when the config has problems
  (previously it always exited 0, so the JSON report's `ok:false` was easy to
  miss in a CI/Ansible gate). The report still prints to stdout unchanged.

## [0.1.1] - 2026-07-13

### Changed

- **BREAKING**: the `ip` source type was split into two focused types —
  `local-ip` (NIC addresses from the kernel, no network calls; requires
  `interfaces`) and `public-ip` (user-configured endpoints; `mode`,
  `endpoints_v4/v6`). Metric names follow the source name: a `public-ip`
  source named `wan` produces `wan.v4`; a `local-ip` source named `lan`
  produces `lan.eth0_v4`. Old `type: ip` configs get a directive
  migration message from `check-config`.
- Notification titles now carry only the kind of event
  (`lan.eth0_v4 changed`, `cpu.percent: value >= 90`, `X: resolved`);
  the values moved into fields (`from`/`to`/`value`) which every provider
  renders in the message body — Lark shows them as card fields.

### Added

- `install.sh` one-line installer served from the website
  (`curl -fsSL https://madnh.github.io/emday/install.sh | sh`): detects
  OS/arch, verifies the release checksum, installs the binary. Supports
  `EMDAY_VERSION` and `EMDAY_INSTALL_DIR` overrides.
- `check-config` catches a secret value pasted into `token_env`/`secret_env`
  (they take the NAME of an env var) and points at the inline
  `secret:`/`token:` alternative.
- `doctor` lists notifier env vars that are unset in the current shell.
- Lark sends fail early with the real cause when a configured signing
  secret resolves to empty, instead of bouncing off Lark as an opaque
  `code 19021`.
- Test coverage for the config (resolution, schema guards, validation,
  conditions), state (persistence, dedup), model, embedded docs, and CLI
  packages — including a guard that the `init` starter config always
  passes `check-config`.

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

[Unreleased]: https://github.com/madnh/emday/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/madnh/emday/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/madnh/emday/releases/tag/v0.1.0
