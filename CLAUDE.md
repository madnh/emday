# emday — notes for maintainers

Go daemon, single binary, no UI. Architecture and decisions: DESIGN.md
(Vietnamese). Layout: `cmd/emday` (main), `internal/{config,model,state,
source,notify,engine,cli,docs,appinfo,buildinfo}`.

## Hard rules

- **Docs travel with the code**: whenever you change the config schema, the
  exec output contract (`$EMDAY_OUTPUT`, `NOTIFY_*`), rule syntax, CLI
  commands/flags, or resolution order — update the embedded docs
  (`internal/docs/topics/*.md`), the starter config
  (`internal/cli/example.yaml`), AND the website (`docs/index.html`,
  `docs/huong-dan.html` — GitHub Pages serves `/docs` on main) in the SAME
  change. Bump `config.SchemaVersion` when `emday.yaml` changes
  incompatibly. The binary is the documentation (`emday docs`); stale docs
  are lies.
- Releases: tag `v*` → `.github/workflows/release.yml` runs GoReleaser
  (config `.goreleaser.yaml`, same flags as `make build-release`). Add a
  CHANGELOG.md entry (Keep a Changelog format) before tagging.
- **GitHub Actions are pinned to exact commit SHAs** (`uses: owner/repo@<sha>
  # vX.Y.Z`) — never a mutable tag. When bumping, resolve the new tag to its
  commit and update the comment too (Dependabot does this automatically).
- **Only `init` creates a config dir.** Every other command must error when
  none resolves — never "create it here". `doctor` is strictly read-only:
  stat before open, never create what it inspects.
- **stdout is the command's result; all logs/warnings go to stderr.**
- **Never execute discovered files.** Exec sources run exactly the
  `command` the user configured, nothing emday finds on its own.
- State/queue paths are derived from the config dir — never add a config
  field pointing them elsewhere.
- Distribute only `make build-release` output (stripped, -trimpath).

## Verify

`go test ./...` covers the exec parser, rule state machine, dedup, and
queue-outage recovery. For end-to-end: `make build-dev`, `emday init
--config-dir /tmp/e2e`, point a webhook notifier at a local listener,
`emday run` briefly, and confirm deliveries.
