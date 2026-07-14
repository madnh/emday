# Deploying emday to a server / fleet

emday is a single static binary (CGO-free) plus one config directory. There
is nothing to install system-wide and no runtime dependency. This page covers
running it as a service in production — with a config-management tool
(Ansible/etc.) or by hand.

## Install the binary (pin the version)

For production, pin an exact release rather than tracking latest. Releases
publish per-platform archives and a `checksums.txt`:

    v=0.1.1  arch=amd64                          # or arch=arm64
    base=https://github.com/madnh/emday/releases/download/v$v
    curl -fsSLO $base/emday_${v}_linux_${arch}.tar.gz
    curl -fsSLO $base/checksums.txt
    sha256sum -c checksums.txt --ignore-missing  # must print: OK
    tar xzf emday_${v}_linux_${arch}.tar.gz
    install -m 0755 emday /usr/local/bin/emday

The archive name uses the version **without** the leading `v`
(`emday_0.1.1_linux_amd64.tar.gz`). There is no separate "stable" channel —
the latest `v*` tag is the release; pin that tag.

## The config directory

`/etc/emday` is the first default location probed on Linux, so a service with
no `--config-dir` finds it automatically. You do **not** need `emday init` on
a managed host: the marker is `emday.yaml` itself, so templating that one file
into `/etc/emday` is enough. emday creates `state.json`, `queue/` and `tmp/`
itself on first run.

    install -d -m 0700 /etc/emday
    # ...write your rendered emday.yaml to /etc/emday/emday.yaml, mode 0600...
    emday check-config --config-dir /etc/emday

The service user must be able to **write** the config directory (`state.json`,
`queue/`, `tmp/` all live inside it — state is derived from the config dir and
cannot be relocated).

## Run as a service

Two options:

**A. `emday install`** — generates a service unit (systemd/launchd/Windows)
via kardianos/service. It bakes an absolute `run --config-dir <dir>` into the
unit, so run it *after* the config dir exists:

    emday install --config-dir /etc/emday
    emday start
    emday status

The generated systemd unit runs as root, restarts on failure, and reads an
optional `EnvironmentFile=/etc/sysconfig/emday` — that file is where you put
secrets (see below).

**B. Template your own unit** — preferred for a fleet, because you control the
user and hardening. The command is exactly `emday run --config-dir /etc/emday`:

    [Unit]
    Description=emday
    After=network-online.target
    Wants=network-online.target

    [Service]
    ExecStart=/usr/local/bin/emday run --config-dir /etc/emday
    EnvironmentFile=/etc/emday/emday.env
    User=emday
    Restart=always
    RestartSec=5
    # hardening (all sources work unprivileged):
    NoNewPrivileges=true
    ProtectSystem=strict
    ReadWritePaths=/etc/emday
    ProtectHome=true

    [Install]
    WantedBy=multi-user.target

**Run unprivileged.** No built-in source needs root — `cpu`/`memory`/`disk`
read via syscalls, `local-ip` reads NIC addresses, `public-ip` makes outbound
HTTP. Create a system user and hand it the config dir:

    useradd -r -s /usr/sbin/nologin emday
    chown -R emday:emday /etc/emday

Do not use systemd `DynamicUser=yes`: its random UID cannot own the persistent
state under `/etc/emday`.

## Secrets: the service does not inherit your shell

`token_env` (telegram), `secret_env` (lark) and `url_env` (any webhook URL —
the URL is a secret, the token is in its path) name environment variables. A
service started by systemd sees **none** of your login shell's variables — you
must hand them over explicitly. Put them in an `EnvironmentFile`, readable only
by root/the service user:

    # /etc/emday/emday.env   (mode 0600, rendered from your secret store)
    EMDAY_TG_TOKEN=123456:ABC-...
    EMDAY_LARK_SECRET=xxxxxxxx
    EMDAY_SLACK_URL=https://hooks.slack.com/services/T/B/X

Reference it from your own unit with `EnvironmentFile=/etc/emday/emday.env`,
or — if you used `emday install` — from `/etc/sysconfig/emday` (the generated
unit already reads that path), or a drop-in:

    systemctl edit emday        # add [Service] EnvironmentFile=/etc/emday/emday.env

`emday doctor` reports any `*_env` variable that is unset where emday runs, and
the service refuses to start rather than send to an empty URL / unsigned Lark
message. Prefer `*_env` over inline secrets; if you must inline, the config
file has to be `root:0600`.

## Gate the deploy on `check-config`

Validate the rendered config before starting or restarting — a bad config
makes `emday run` exit non-zero, which becomes a restart loop under
`Restart=always`.

    emday check-config --config-dir /etc/emday          # exit 0 = ok, non-zero = problems
    emday check-config --config-dir /etc/emday --json    # same exit code, plus a JSON report

Both forms exit non-zero when the config has problems, so either works as a
CI/Ansible gate. Use `--json` when you also want to parse the findings
(`.ok`, `.problems[]`); use the plain form when you only care about pass/fail.
Run it after templating and before every (re)start.

## Upgrades

Replace the binary (same pinned-download steps) and restart the service. State
and queue are forward-compatible within a schema version; `state.json` carries
its own version and emday refuses to run against a newer-than-it-understands
state rather than corrupt it.
