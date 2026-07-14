# Notifiers

Notifiers are where alerts go. Rules and exec sources pick targets by name
via their `notify: [...]` list. Every notifier has a persistent queue with
retry — if the network is down, alerts are delivered when it returns (that
is the point: "my IP changed" arrives once connectivity is back).

Test any notifier without waiting for an alert:

    emday test-notify my-telegram

## Keeping the URL out of the config file (`url_env`)

For slack, discord, lark, webhook and ntfy the webhook URL is itself a
secret — the token lives in the path. Any notifier that takes `url:` also
accepts `url_env:`, the NAME of an environment variable holding the URL, so
it never lands in `emday.yaml`:

    notifiers:
      my-slack:
        type: slack
        url_env: EMDAY_SLACK_URL      # NAME of the env var, not the URL

This mirrors `token_env` (telegram) and `secret_env` (lark). The service
does not inherit your shell — deliver the variable via systemd
`EnvironmentFile=` (see `emday docs deploy`). `emday doctor` flags a
`url_env` whose variable is unset, and the service refuses to start rather
than POST to an empty URL. If you keep the URL inline instead, the config
file must be `root:0600`.

## telegram

    notifiers:
      my-telegram:
        type: telegram
        token_env: EMDAY_TG_TOKEN    # env var holding the bot token
        chat_id: "-100123456"

Create a bot with @BotFather, add it to your chat/group, find the chat id
(e.g. via @userinfobot). Keep the token in an environment variable — the
service manager sets it (systemd: `Environment=`, launchd: plist
`EnvironmentVariables`). Messages use HTML formatting; script output is
escaped, so untrusted content cannot inject markup.

## ntfy

    notifiers:
      my-phone:
        type: ntfy
        url: https://ntfy.sh/your-secret-topic
        # priority: urgent           # optional; default maps from level

Zero-signup push notifications: subscribe to the topic in the ntfy app.
Levels map to priority (info→default, warn→high, error→urgent) and to a
tag emoji shown before the title.

## lark (Lark / Feishu custom bot)

    notifiers:
      my-lark:
        type: lark
        url: https://open.larksuite.com/open-apis/bot/v2/hook/...   # or open.feishu.cn
        secret_env: EMDAY_LARK_SECRET   # NAME of the env var holding the secret
        # — or put the secret itself inline (config file should be root:0600):
        # secret: "xxxxxxxxxxxx"

In the group: Settings → Bots → Add bot → Custom bot; copy the webhook URL.
If you enabled the signature option, put the secret in `secret_env` — emday
signs each message (HMAC-SHA256, as the bot API requires). Alerts render as
an interactive card: colored header by level (info=blue, warn=orange,
error=red, resolved=green), host/source fields, detail block.

Getting `lark code 19021: sign match fail or timestamp is not within one
hour`? Two causes: the server clock is off by more than an hour (check
`timedatectl`; fix with `timedatectl set-ntp true`), or the signing secret
is wrong / the env var is not set where emday runs (`emday doctor` flags
unset notifier env vars).

## slack (incoming webhook)

    notifiers:
      my-slack:
        type: slack
        url: https://hooks.slack.com/services/T000/B000/XXXX

Create an incoming webhook at api.slack.com/apps (or via a workflow).
Alerts render as a color-coded attachment.

## discord (channel webhook)

    notifiers:
      my-discord:
        type: discord
        url: https://discord.com/api/webhooks/...

Channel settings → Integrations → Webhooks → New. Alerts render as a
color-coded embed.

## webhook (generic — for everything else)

    notifiers:
      my-hook:
        type: webhook
        url: https://example.com/hook
        method: POST                  # default POST
        headers: {Authorization: "Bearer ..."}
        body_template: |
          {"text": {{json .Text}}}

`body_template` is a Go text/template. Available fields: `.Level` `.Title`
`.Message` `.Source` `.Time` `.Hostname` `.Resolved` `.Fields` and `.Text`
(the default full rendering). The `json` function safely quotes any value.
Omitting body_template sends a sensible JSON object with all fields.
