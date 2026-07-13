# Extending emday with scripts (exec sources)

An exec source runs your command on an interval. Your script reports data by
appending lines to the file named by the `$EMDAY_OUTPUT` environment variable
(the same model as GitHub Actions' `$GITHUB_OUTPUT`). stdout/stderr are NOT
parsed — they are captured as debug logs, so your script may print freely.

    sources:
      backup-status:
        type: exec
        command: /opt/scripts/check-backup.sh
        interval: 5m
        timeout: 30s
        notify: [my-telegram]   # targets for NOTIFY_* directives (below)

## Metrics: KEY=VALUE

    echo "BACKUP_STATUS=failed"  >> "$EMDAY_OUTPUT"
    echo "BACKUP_SIZE_GB=42.5"   >> "$EMDAY_OUTPUT"

Each key becomes a metric named `<source>.<KEY>` (here:
`backup-status.BACKUP_STATUS`) that rules can watch — see `emday docs
conditions`. Values that parse as numbers are numeric; anything else is a
string. Keys must match `[A-Za-z_][A-Za-z0-9_]*`.

Multi-line values use a heredoc marker:

    cat >> "$EMDAY_OUTPUT" <<'EOF'
    DETAIL<<END
    line 1
    line 2
    END
    EOF

## Direct notifications: NOTIFY_*

When only your script understands the logic (multi-condition business rules),
it may send a notification directly — no rule needed:

    echo 'NOTIFY=informational message'   >> "$EMDAY_OUTPUT"
    echo 'NOTIFY_WARN=warning message'    >> "$EMDAY_OUTPUT"
    echo 'NOTIFY_ERROR=error message'     >> "$EMDAY_OUTPUT"

The first line is the title; further lines (via heredoc) become the body.
These events go to the source's `notify:` targets. emday dedups identical
messages per source within the cooldown window (default 30m), so a script
that repeats itself every interval will not spam you.

Prefer metrics + rules when a threshold can express the logic — rules get
`for` (anti-flapping) and "resolved" notifications for free. Use NOTIFY_*
only for what rules cannot express.

## Health metrics (automatic)

Every exec source also reports:

    <source>._ok            1 when the last run exited 0, else 0
    <source>._exit_code     exit code (-1 when the process failed to run)
    <source>._duration_ms   run duration

A rule like `metric: backup-status._ok, condition: "value == 0"` catches a
silently broken script.

A key that disappears between runs counts as a change for `on_change` rules.

## One-liners: parse: stdout

For trivial commands, parse stdout directly instead of the output file:

    sources:
      quick:
        type: exec
        command: 'echo "VALUE=$(some-command)"'
        parse: stdout

stdout mode accepts metrics only — NOTIFY_* directives are ignored there
(anything the command's tools print could look like a directive; the file
channel is the deliberate one).
