<img src="docs/logo.png" alt="emday logo — a friendly server waving hello" width="140" align="right">

# emday

Self-contained server monitoring with notifications, in one binary.

**Website**: https://madnh.github.io/emday · [Hướng dẫn tiếng Việt](https://madnh.github.io/emday/huong-dan.html) · [Changelog](CHANGELOG.md)

emday runs quietly on your machine and tells you when something changes or
crosses a threshold: public IP changed, CPU/RAM/disk too high, a process
died, a backup failed — delivered to Telegram, ntfy, or any webhook
(Slack, Lark, ...). No agent phoning home, no UI, no dependencies.

The name: *"em đây"* is Vietnamese for *"it's me!"* — when your server's IP
changes, emday pings you: *"em đây, IP này nè"* (it's me, here's my new IP).

## Install

Download the binary for your platform from the
[latest release](https://github.com/madnh/emday/releases/latest)
(`linux_amd64`, `linux_arm64`, `darwin_arm64`, ... — verify with
`sha256sum -c checksums.txt --ignore-missing`), or build from source with
`make build-release`.

## Quick start

```console
$ emday init                      # create a config dir (guided)
$ vi ~/.config/emday/emday.yaml   # add your notifier, tweak rules
$ emday check-config              # validates + compiles every rule
$ emday test-notify my-telegram   # make sure alerts actually arrive
$ emday run                       # foreground; or:
$ sudo emday install && sudo emday start   # as a system service
```

Runs on Linux (systemd), macOS (launchd), and Windows (Services) from a
single static binary per platform.

## Everything is documented inside the binary

```console
$ emday docs              # topic list
$ emday docs config       # config format & resolution
$ emday docs conditions   # rule syntax cheat sheet
$ emday docs exec         # extend emday with your own scripts
$ emday docs notifiers    # telegram / ntfy / webhook setup
$ emday docs agent        # operating guide for AI agents
```

`emday docs agent` prints a compact guide an AI agent can load as context —
the agent (or you) can configure, extend, and diagnose emday without any
external documentation. `emday doctor --json` and `emday check-config --json`
close the loop with machine-readable diagnostics.

## Extending with scripts

Any script becomes a source: append `KEY=VALUE` metrics — or `NOTIFY_ERROR=...`
direct notifications — to the file `$EMDAY_OUTPUT` (the GitHub Actions model):

```bash
#!/bin/sh
echo "BACKUP_AGE_HOURS=$age"                       >> "$EMDAY_OUTPUT"
[ "$age" -gt 26 ] && echo "NOTIFY_ERROR=backup overdue" >> "$EMDAY_OUTPUT"
```

Then watch it with a rule: `condition: "value > 24"`, `for: 5m`, and emday
handles anti-flapping, resolved notifications, dedup, and offline queueing.

## Building

```console
$ make build-dev       # local build with debug symbols
$ make build-release   # stripped, -trimpath — the only shape to distribute
$ make test
```

## Design

See [DESIGN.md](DESIGN.md) (Vietnamese) for the full architecture and the
reasoning behind the decisions: pull-only model, config-directory layout,
expression-based rules, and the self-teaching CLI.
