# Source: process

Whether named processes are running, and how many of each.

## Configure

    sources:
      services:
        type: process
        interval: 1m
        processes: [nginx, sshd]   # exact process names to watch

| field       | required | default             | meaning                          |
|-------------|----------|---------------------|----------------------------------|
| `processes` | yes      | —                   | list of process names to watch   |
| `interval`  | no       | `defaults.interval` | how often to scan                |

Names are matched **exactly** against each process's own name (the `comm`
value, e.g. `nginx`), not the full command line. The source name (`services`)
is the metric prefix.

## Metrics

Emitted once per watched name:

| metric                       | type   | unit  | notes                                |
|------------------------------|--------|-------|--------------------------------------|
| `<source>.<name>.running`    | bool   | 1 / 0 | 1 if at least one match is alive     |
| `<source>.<name>.count`      | number | count | number of matching processes         |

With the config above: `services.nginx.running`, `services.nginx.count`,
`services.sshd.running`, `services.sshd.count`.

## How it reads / what it needs

Enumerates `/proc/<pid>` and reads each process's name via gopsutil.
Under a **normal** `/proc` mount this needs no privilege — a non-root user
sees every process. **Read the gotcha below before running non-root.**

## Rules

    rules:
      - metric: services.nginx.running
        condition: "value == 0"     # bool renders as 1/0
        notify: [ops]
      - metric: services.sshd.count
        condition: "value == 0"
        notify: [ops]

## Gotchas

- **`hidepid` / `ProtectProc` hide other users' processes.** If `/proc` is
  mounted `hidepid=2`, or the unit sets systemd `ProtectProc=invisible`, a
  non-root emday cannot see processes owned by other users — a service running
  as root/`mysql`/etc. then reports `running = 0` **silently, while it is
  actually alive.** This is the one built-in source where non-root can be wrong
  without any error. Fixes: whitelist the emday user's group via
  `hidepid=...,gid=<group>`, add `SupplementaryGroups=<group>` to the unit, or
  do not set `ProtectProc`. `cpu`/`memory`/`disk`/`local-ip`/`public-ip` are
  unaffected.
- **Exact-name match only.** `nginx` matches the process named `nginx`, not a
  script invoked as `python /opt/app/nginx-helper.py`. Check the real name with
  `ps -e -o comm | sort -u`.
- A name with no matches yields `running = 0`, `count = 0` (not absent) — so a
  typo'd process name looks like a permanent outage. Verify names when adding.
