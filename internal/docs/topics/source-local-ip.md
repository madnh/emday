# Source: local-ip

The LAN/interface addresses of the host, read straight from the kernel — no
network calls.

## Configure

    sources:
      lan:
        type: local-ip
        interval: 30s
        interfaces: [eth0]         # NICs to report addresses for

| field        | required | default             | meaning                       |
|--------------|----------|---------------------|-------------------------------|
| `interfaces` | yes      | —                   | list of interface names       |
| `interval`   | no       | `defaults.interval` | how often to read             |

The source name (`lan`) is the metric prefix; the interface name becomes part
of the metric.

## Metrics

Emitted per interface, per family that has a global-unicast address:

| metric                  | type   | notes                                  |
|-------------------------|--------|----------------------------------------|
| `<source>.<iface>_v4`   | string | first global-unicast IPv4 on the NIC   |
| `<source>.<iface>_v6`   | string | first global-unicast IPv6 on the NIC   |

With the config above: `lan.eth0_v4` (and `lan.eth0_v6` if present). Values are
**strings**, so watch them with `on_change`.

## How it reads / what it needs

`net.InterfaceByName` + `Addrs()` — on Linux this is a netlink `RTM_GETADDR`
query, a read-only kernel call. **A non-root user reads every interface's
addresses; no root, no `CAP_NET_ADMIN`.**

Only **global-unicast** addresses are reported, so loopback, link-local
(`169.254.*`, `fe80::*`), multicast and unspecified addresses are skipped.
Private LAN ranges (`10.*`, `192.168.*`, …) *are* global-unicast and reported.
If a NIC has several addresses of a family, the **first** one wins.

## Rules

    rules:
      - metric: lan.eth0_v4
        on_change: true
        notify: [ops]

## Gotchas

- **A missing or down interface is an error, not a `0`.** A typo'd or absent
  NIC name yields no sample for it (logged to stderr); its `on_change` rule
  simply never has data. Confirm names with `ip -o link`.
- An interface with only a link-local address emits **nothing** for that family
  — link-local is filtered out by design.
- First-address-wins: on a NIC carrying multiple global IPs, only one per
  family is reported; emday does not enumerate all of them.
