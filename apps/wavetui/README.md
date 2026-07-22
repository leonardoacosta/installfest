# wavetui

A `bubbletea`-based terminal dashboard for this dotfiles fleet: it aggregates
beads, OpenSpec, and other project sources into one queue view, ranked and
navigable from a single TUI. Go, stdlib-leaning (see `internal/config`'s own
header on why the config parser is a hand-rolled TOML subset, not a
dependency).

Full behavior contract lives in `openspec/specs/wavetui/spec.md` (event bus,
Store-as-single-writer, per-source re-query-not-diff semantics, etc.) — this
file only covers how to build, run, and test it.

## Build & Run

```bash
cd apps/wavetui
go build ./cmd/wavetui   # produces ./wavetui
go run ./cmd/wavetui     # run directly against the current project root
```

## Test

```bash
cd apps/wavetui
go test ./...
```

Also wired into the repo-wide gate as `section_apps_go` in `scripts/check.sh`
(skipped with a warning if `go` isn't installed).

## Config

wavetui reads an optional `.wavetui.toml` from the project root it's run
against (`internal/config/config.go`). Missing file = all-defaults-off. Known
keys today: `show_plans`, `show_advisor_plans` (surface `plans/` /
`advisor-plans/` items), `flair_enabled` / `flair_calm_mode` (opt-in
animation, see `openspec/changes/wavetui-flair/design.md`), `force_osc52`
(clipboard escape-sequence override), and `headless_concurrency_cap`
(`wavetui-daemon`'s headless-dispatch concurrency, default 2).

## Layout

```
apps/wavetui/
  cmd/wavetui/       # main package — entrypoint, wires bus/Store/config/UI
  internal/          # bus, config, daemon, dispatch, flair, sources, store, ui, wave, ...
```
