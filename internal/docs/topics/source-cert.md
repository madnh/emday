# Source: cert

TLS certificate expiry, chain validity and issuer — for endpoints you reach
over the network, or PEM files on disk.

## Configure

    sources:
      cert:
        type: cert
        interval: 6h                 # default for this source type
        timeout: 10s                 # per probe
        endpoints:                   # alias -> host[:port], port defaults to 443
          wms: "wms.example.com"
          studio: "studio.example.com:8443"
        files:                       # optional: read a PEM instead of dialling
          internal: "/etc/ssl/internal.pem"

| field       | required | default | meaning                              |
|-------------|----------|---------|--------------------------------------|
| `endpoints` | one of   | —       | alias → `host[:port]` probed over TLS |
| `files`     | one of   | —       | alias → PEM file read from disk      |
| `timeout`   | no       | `10s`   | dial + handshake deadline per probe  |
| `interval`  | no       | `6h`    | how often to probe                   |

At least one of `endpoints` / `files` is required. Each `alias` appears
verbatim in the metric name, so pick short, stable names.

**`interval` does not inherit `defaults.interval`.** Certificates change on the
scale of days; the usual one-minute default would be 1440 outbound probes per
endpoint per day to learn the same number.

## Metrics

Emitted once per alias:

| metric                    | type   | notes                                        |
|---------------------------|--------|----------------------------------------------|
| `cert.<alias>.status`     | string | always emitted, probe succeeded or not       |
| `cert.<alias>.days_left`  | number | full days until `notAfter`; negative once expired; **absent** when no certificate was obtained |
| `cert.<alias>.issuer`     | string | issuer CN (or organisation); for `on_change` |

`status` values:

| value               | meaning                                            |
|---------------------|----------------------------------------------------|
| `ok`                | in date, hostname covered, chain verifies          |
| `expired`           | the certificate (or one in its chain) has expired  |
| `hostname-mismatch` | the certificate does not cover the host dialled    |
| `untrusted`         | the chain does not verify against the system roots |
| `dns-failure`       | the host did not resolve                           |
| `refused`           | the port refused the connection                    |
| `timeout`           | dial or handshake exceeded `timeout`               |
| `handshake-error`   | connected, but the TLS handshake failed            |
| `connect-error`     | any other connection failure                       |
| `unreadable`        | `files` only: missing, or no certificate in it     |

`days_left` floors, so `14` means at least fourteen full days remain, and a
certificate that expired an hour ago reads `-1`, never `0`.

## How it reads / what it needs

One TCP dial plus a TLS handshake per endpoint, then the chain is verified in
process against the system trust store. Nothing is sent over the connection —
no request is made, and the connection is closed as soon as the certificate is
in hand. No root, no external binaries, works on every platform emday supports.

**It needs a system trust store**, which is the one host requirement this
source has. Ordinary server installs have one; minimal container images often
do not. Measured on `ubuntu:24.04` with no `ca-certificates` package: every
endpoint reads `untrusted`, including ones with a perfectly good certificate.
`apt-get install ca-certificates` (or `apk add ca-certificates`) flips them
back to `ok`. Check with `ls /etc/ssl/certs/ca-certificates.crt` before
concluding a certificate is really untrusted.

The handshake itself does not verify: that is deliberate, and it is why this
source can say more than `openssl s_client | grep notAfter`. Skipping the
built-in check means the chain is available **even when it is invalid**, so
expiry and trust become two independent answers instead of one failure hiding
the other. Verification then runs separately, which is what produces the
`status` values above.

## Rules

    rules:
      - metric: cert.wms.days_left
        condition: "value <= 14"
        level: error
        notify: [ops]

      - metric: cert.wms.status
        condition: 'value != "ok"'
        for: 15m               # ride out a brief network failure
        notify: [ops]

      - metric: cert.wms.issuer
        on_change: true        # catches a substituted certificate
        notify: [ops]

The `issuer` rule is worth having: a certificate replaced by an interception
proxy verifies fine and has plenty of days left, so nothing else here notices
it. A changed issuer is the signal.

## Gotchas

- **`status` is emitted even when the probe fails**, on purpose. A metric that
  disappears stops being evaluated, and then "certificate about to expire" and
  "host unreachable" are equally silent. Alert on `status` as well as on
  `days_left`, or an unreachable host silences your expiry alert.
- **`days_left` always describes the leaf certificate.** An expired
  *intermediate* gives `status: expired` while `days_left` is still positive —
  accurate, and the two together say which certificate to fix.
- **`for:` is worth setting on a `status` rule.** A single failed probe on a
  flaky link would otherwise page someone.
- **`files` cannot judge a chain or a hostname**, so a file only ever reads
  `ok`, `expired` or `unreadable`. `ok` from a file means *in date*, nothing
  more: the same self-signed certificate reads `untrusted` when dialled and
  `ok` when read from disk. Use `endpoints` for anything actually served.
- **An outage does not resolve a firing rule.** While a probe fails there is
  no `days_left` to evaluate, so a firing expiry rule stays firing silently
  and sends its `resolved` event only when a good value is measured again. It
  never claims the problem cleared because the host went away.
- **`on_change` is silent on the first observation** (there is nothing to
  compare against), so a rollout produces no issuer cards. When a probe fails
  the issuer metric disappears, which *is* reported —
  `cert.<alias>.issuer disappeared` — and its return is silent for the same
  reason as the first observation.
- **Verification uses the system trust store.** A private CA that the host
  trusts verifies; one it does not reads `untrusted`.
- An IP address as the target works: it is matched against the certificate's
  IP SANs.
