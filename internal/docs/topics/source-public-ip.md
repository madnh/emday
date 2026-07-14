# Source: public-ip

The host's public (WAN) IP address, as seen from the internet. The original
emday use case: "em đây — my IP changed".

## Configure

    sources:
      wan:
        type: public-ip
        interval: 5m
        mode: [v4]                 # v4, v6, or both: [v4, v6]
        endpoints_v4:              # YOUR choice; tried in order
          - https://api.ipify.org
          - https://checkip.amazonaws.com
        endpoints_v6:              # required only if mode includes v6
          - https://api6.ipify.org

| field          | required          | default             | meaning                                   |
|----------------|-------------------|---------------------|-------------------------------------------|
| `mode`         | no                | `[v4]`              | which families to resolve                 |
| `endpoints_v4` | if mode has `v4`  | —                   | URLs that return a bare IPv4              |
| `endpoints_v6` | if mode has `v6`  | —                   | URLs that return a bare IPv6              |
| `interval`     | no                | `defaults.interval` | how often to check                        |

Each endpoint must return **only** the address in its body. The source name
(`wan`) is the metric prefix.

## Metrics

| metric      | type   | notes                                    |
|-------------|--------|------------------------------------------|
| `wan.v4`    | string | the resolved IPv4 (when `mode` has `v4`) |
| `wan.v6`    | string | the resolved IPv6 (when `mode` has `v6`) |

Values are **strings** (an address), so watch them with `on_change`, not a
numeric threshold.

## How it reads / what it needs

Outbound HTTPS `GET` to your endpoints — nothing else. Each family is fetched
over a connection of that family, so a `v4` result reflects real IPv4
reachability. **Runs unprivileged; no root, no capabilities.**

An endpoint's response is accepted only if the whole body is one valid address
of the requested family; anything else (HTML, an error page, the wrong family,
non-200, an oversized body) is rejected and the next endpoint is tried.

## Rules

    rules:
      - metric: wan.v4
        on_change: true            # notify whenever the IP changes
        notify: [ops]

## Gotchas

- **Endpoints are trust-sensitive.** Whoever answers these URLs decides what
  emday thinks your IP is. List two or three independent providers; the strict
  validation means a broken one is skipped, never mistaken for a change.
- **Per-family partial success.** If `v4` resolves but every `v6` endpoint
  fails, `wan.v6` is simply absent (its errors are logged) — the source does
  not fail as a whole. Collect fails (stderr log, no samples) only when *every*
  configured family fails.
- Not for LAN/interface addresses — use `emday docs source-local-ip`.
