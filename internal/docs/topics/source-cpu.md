# Source: cpu

CPU utilisation and load average of the host.

## Configure

    sources:
      cpu:
        type: cpu
        interval: 30s        # optional; falls back to defaults.interval (1m)

| field      | required | default            | meaning                          |
|------------|----------|--------------------|----------------------------------|
| `interval` | no       | `defaults.interval`| how often to sample              |

The source name (`cpu` above) is the metric prefix — name it whatever you
like; `type: cpu` is what selects this collector.

## Metrics

| metric        | type   | unit          | range   | notes                              |
|---------------|--------|---------------|---------|------------------------------------|
| `cpu.percent` | number | percent busy  | 0–100   | whole-host average across all CPUs |
| `cpu.load1`   | number | load average  | ≥ 0     | 1-minute; **not** on Windows       |
| `cpu.load5`   | number | load average  | ≥ 0     | 5-minute; not on Windows           |
| `cpu.load15`  | number | load average  | ≥ 0     | 15-minute; not on Windows          |

Load average is a run-queue length, not a percentage: `load1 >= number of
cores` means the CPU is saturated. `cpu.percent` is the busy percentage since
the previous sample.

## How it reads / what it needs

Reads `/proc/stat` (CPU busy time) and `/proc/loadavg` via gopsutil — both
world-readable. **Runs unprivileged; no root, no capabilities.**

## Rules

    rules:
      - metric: cpu.percent
        condition: "value >= 90"
        for: 5m                 # sustained, not a momentary spike
        notify: [ops]
      - metric: cpu.load5
        condition: "value >= 8" # e.g. an 8-core box
        notify: [ops]

## Gotchas

- **First sample primes the counter.** `cpu.percent` is measured *since the
  previous collect*, so the very first sample after start is based on the
  interval since boot and can read low/odd. Pair thresholds with `for:` so one
  reading never fires an alert.
- **Load metrics are absent on Windows** — only `cpu.percent` is emitted there.
- Whole-host only: there is no per-core breakdown.
