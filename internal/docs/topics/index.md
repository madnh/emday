# emday documentation

emday is a self-contained monitoring daemon: it watches your server (IP
address, CPU, RAM, disk, processes, anything a script can measure) and sends
notifications when something changes or crosses a threshold.

All documentation ships inside this binary. Topics:

    emday docs config       config directory, emday.yaml format, resolution order
    emday docs conditions   rule condition syntax cheat sheet
    emday docs exec         extending emday with scripts ($EMDAY_OUTPUT, NOTIFY_*)
    emday docs notifiers    notification targets (webhook, telegram, ntfy)
    emday docs deploy       running as a service: systemd, non-root, secrets, version pinning
    emday docs agent        compact operating guide for AI agents

Per-source reference (metrics, permissions, rules, gotchas):

    emday docs source-public-ip   WAN IP via your endpoints
    emday docs source-local-ip    NIC addresses from the kernel
    emday docs source-cpu         CPU percent and load average
    emday docs source-memory      RAM and swap
    emday docs source-disk        filesystem used/free
    emday docs source-process     named processes up/down + count

Getting started:

    emday init              create a config directory (the only command that does)
    emday check-config      validate config, compile every rule condition
    emday run               run in the foreground
    emday install           install as a system service (systemd/launchd/Windows)
    emday doctor            diagnose problems — strictly read-only

Project home:

    source    https://github.com/madnh/emday
    website   https://madnh.github.io/emday
    releases  https://github.com/madnh/emday/releases

Note: examples use `emday`; if you renamed the binary, substitute your name.
