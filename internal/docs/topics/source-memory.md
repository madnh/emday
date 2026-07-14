# Source: memory

RAM (and swap) utilisation of the host.

## Configure

    sources:
      memory:
        type: memory
        interval: 1m         # optional; falls back to defaults.interval (1m)

| field      | required | default             | meaning              |
|------------|----------|---------------------|----------------------|
| `interval` | no       | `defaults.interval` | how often to sample  |

The source name (`memory`) is the metric prefix.

## Metrics

| metric                 | type   | unit    | range | notes                                   |
|------------------------|--------|---------|-------|-----------------------------------------|
| `memory.percent`       | number | percent | 0–100 | used / total (Linux: excludes reclaimable cache, matches `free -m`'s "available") |
| `memory.used_mb`       | number | MiB     | ≥ 0   | used memory                             |
| `memory.total_mb`      | number | MiB     | ≥ 0   | total physical memory                   |
| `memory.swap_percent`  | number | percent | 0–100 | **only emitted when swap exists** (total > 0) |

## How it reads / what it needs

Reads `/proc/meminfo` (and swap counters) via gopsutil — world-readable.
**Runs unprivileged; no root, no capabilities.**

## Rules

    rules:
      - metric: memory.percent
        condition: "value >= 90"
        for: 5m
        notify: [ops]
      - metric: memory.swap_percent
        condition: "value >= 50"   # swap in heavy use = memory pressure
        notify: [ops]

## Gotchas

- **`memory.swap_percent` is absent on swapless hosts.** A rule on it then
  never fires (no data) rather than reading 0 — expected on containers/cloud
  instances with no swap.
- `percent` uses "available" memory semantics on Linux, so cache/buffers do
  **not** count as used — it will read lower than `used / total` from `top`.
  That is intentional: it reflects real memory pressure.
