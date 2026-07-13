# Configuration

## The config directory

One directory holds everything emday needs — config, state, queue, docs.
Move the directory and everything moves with it:

    emday/
    ├── emday.yaml     config (this directory's marker; has `version: 1`)
    ├── config.md      this guide, written by `emday init`
    ├── state.json     engine memory (managed by emday — do not edit)
    ├── queue/         notifications not yet delivered (managed by emday)
    └── tmp/           scratch space for exec sources (managed by emday)

State and queue paths are derived from the directory — they are never
configured, so they cannot drift somewhere else.

## How emday finds the directory

Every command resolves the config dir the same way, in order:

1. `--config-dir <dir>` flag
2. `EMDAY_CONFIG_DIR` environment variable
3. Default locations that already contain `emday.yaml`:
   Linux: `/etc/emday`, then `~/.config/emday`
   macOS: `/usr/local/etc/emday`, then `~/.config/emday`
   Windows: `%ProgramData%\emday`, then `%APPDATA%\emday`
   All platforms: `./emday` (portable, next to where you run it)

`emday init` is the ONLY command that creates a config dir. Everything else
errors when none exists — a command run in the wrong place fails loudly
instead of silently creating a stray data store. `emday doctor` shows exactly
which directory resolved and why.

## emday.yaml

    version: 1                  # schema version; required

    defaults:
      cooldown: 30m             # min interval between identical alerts
      interval: 1m              # default source interval

    sources:
      ip:
        type: ip
        interval: 1m
        mode: [v4]              # v4, v6, or both
        endpoints_v4:           # where to ask "what is my IP" — YOUR choice,
          - https://api.ipify.org        # tried in order until one returns
          - https://checkip.amazonaws.com # a valid bare IPv4 address
        # endpoints_v6: [...]   # same for IPv6 (used when mode includes v6)
        # interfaces: [eth0]    # also report local NIC addresses
      cpu:
        type: cpu               # metrics: cpu.percent, cpu.load1/5/15
        interval: 30s
      memory:
        type: memory            # memory.percent, memory.used_mb, memory.swap_percent
      disk:
        type: disk              # disk.<alias>.percent, disk.<alias>.free_gb
        mounts: {root: "/", data: "/data"}
      services:
        type: process           # services.<name>.running (1/0), .count
        processes: [nginx, sshd]
      backup:
        type: exec              # see `emday docs exec`
        command: /opt/scripts/check-backup.sh
        timeout: 30s
        notify: [my-telegram]

    rules:                      # see `emday docs conditions`
      - metric: ip.public_v4
        on_change: true
        notify: [my-telegram]
      - metric: cpu.percent
        condition: "value >= 90"
        for: 5m
        notify: [my-telegram]

    notifiers:                  # see `emday docs notifiers`
      my-telegram:
        type: telegram
        token_env: EMDAY_TG_TOKEN
        chat_id: "-100123456"

The ip source accepts responses ONLY when the entire body is one valid
address of the requested family — an endpoint returning HTML or an error
page is skipped, never mistaken for an IP change.

Validate after editing:

    emday check-config

This file is documentation only — settings live in emday.yaml. Examples use
`emday`; if you renamed the binary, substitute your name.
