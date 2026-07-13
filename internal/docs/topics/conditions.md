# Rule conditions

A rule condition is an expression over one variable, `value` — the current
value of the rule's metric. Numbers and strings are supported; the value's
type comes from the metric (numeric output parses as number, anything else
is a string).

| Goal                       | Write                                                    |
|----------------------------|----------------------------------------------------------|
| Compare numbers            | `value > 90`, `value <= 10`, `value == 0`                |
| Compare strings            | `value == "failed"`, `value != "ok"`                     |
| Contains / prefix / suffix | `value contains "err"`, `value startsWith "eth"`, `value endsWith ".vn"` |
| In a list                  | `value in ["a", "b"]`, `value not in ["ok"]`             |
| Combine                    | `... and ...`, `... or ...`, `not (...)`                 |
| Regex (advanced)           | `value matches "^10\\."`                                 |

Try a condition instantly, without touching config:

    emday test-rule 'value > 90' --value 95        # prints: true
    emday test-rule 'value in ["ok"]' --value ok   # prints: true

Rule fields around the condition:

    rules:
      - metric: cpu.percent
        condition: "value >= 90"
        for: 5m            # must hold continuously this long before alerting
        resolve_for: 2m    # optional: must be false this long before "resolved"
        cooldown: 30m      # optional: min interval between identical alerts
        level: warn        # info | warn | error (default warn)
        notify: [my-telegram]

      - metric: wan.v4
        on_change: true    # alert whenever the value changes (no condition)
        notify: [my-telegram]

`for` prevents flapping: the condition must stay true across collects for the
whole duration. When a firing rule's condition turns false (for `resolve_for`,
default 0), emday sends a "resolved" notification.

Full expression language reference: https://expr-lang.org — but the table
above is all you normally need.
