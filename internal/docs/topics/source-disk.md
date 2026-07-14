# Source: disk

Used-space and free-space of one or more filesystems.

## Configure

    sources:
      disk:
        type: disk
        interval: 5m
        mounts:                    # alias -> path
          root: "/"
          data: "/var/lib/mysql"

| field      | required | default                                   | meaning                    |
|------------|----------|-------------------------------------------|----------------------------|
| `mounts`   | no       | `{root: "/"}` (Windows: `{c: "C:\\"}`)    | alias → mount path to watch|
| `interval` | no       | `defaults.interval`                       | how often to sample        |

Each `alias` appears verbatim in the metric name, so pick short, stable names.

## Metrics

Emitted once per alias:

| metric                   | type   | unit          | range | notes            |
|--------------------------|--------|---------------|-------|------------------|
| `disk.<alias>.percent`   | number | percent used  | 0–100 | of usable space  |
| `disk.<alias>.free_gb`   | number | GiB free      | ≥ 0   | available to non-root |

`percent` is `used / (used + available)` — it counts the root-reserved blocks
as unavailable, so it matches what a normal user actually sees, not raw
`total`.

## How it reads / what it needs

One `statfs(path)` syscall per mount (via gopsutil). It does **not** open or
list the directory — nothing inside the mount is read. `statfs` needs only
**search (`x`) permission on the parent directories**, not read on the target,
so a **non-root** service measures even a locked-down mount correctly — e.g.
`/var/lib/mysql` at mode `0700 mysql:mysql` reports fine as long as `/`,
`/var`, `/var/lib` are traversable (`0755`). No root, no capabilities.

## Rules

    rules:
      - metric: disk.root.percent
        condition: "value >= 90"
        level: error
        notify: [ops]
      - metric: disk.data.free_gb
        condition: "value < 5"
        notify: [ops]

## Gotchas

- **Partial-tolerant.** If one mount's `statfs` fails (path missing/unmounted),
  only *that* alias's metrics are omitted — never reported as a fake `0`. A
  stderr log line is written only when **every** mount fails.
- **No `disk._ok`.** Health metrics (`<source>._ok`) exist only for `exec`
  sources. So a mount that disappears makes its threshold rule go silent (no
  data → no alert), not fire. To catch a vanished mount, alert on the
  *absence* of its metric, or watch it via a small `exec` source.
- `percent` can differ slightly from `df` because of the reserved-blocks
  accounting described above.
