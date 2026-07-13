# emday documentation

emday is a self-contained monitoring daemon: it watches your server (IP
address, CPU, RAM, disk, processes, anything a script can measure) and sends
notifications when something changes or crosses a threshold.

All documentation ships inside this binary. Topics:

    emday docs config       config directory, emday.yaml format, resolution order
    emday docs conditions   rule condition syntax cheat sheet
    emday docs exec         extending emday with scripts ($EMDAY_OUTPUT, NOTIFY_*)
    emday docs notifiers    notification targets (webhook, telegram, ntfy)
    emday docs agent        compact operating guide for AI agents

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
