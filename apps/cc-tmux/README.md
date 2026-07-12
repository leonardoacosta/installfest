# cc-tmux

A first-party Claude Code + tmux plugin that makes parallel Claude Code sessions
visible and navigable inside tmux.

Every Claude pane's state (`waiting` / `idle` / `active`) is tracked in tmux pane
options — the single source of truth — via Claude Code hooks. On top of that it
provides priority-based pane cycling, jump-back, an fzf notification inbox, OS
notifications with terminal auto-focus, status-bar integration, a folded-in
multi-account usage segment, and an opt-in dispatch Conductor.

Clean-room implementation (architecture adapted from `unsafe9/claude-tmux-hop`,
all code original). Python 3.10+, stdlib only, no runtime dependencies.

## CLI

```
cc-tmux <command>
```

Run `cc-tmux --help` for the full subcommand list, and `cc-tmux self-test` to run
the built-in pure-function test suite.

### Diagnostics

- `cc-tmux doctor` — environment checklist (tmux ≥ 3.2, fzf, python ≥ 3.10, `$TMUX`,
  plugin symlink, Claude plugin registration, focus hook, tracked-pane count). Prints
  PASS/FAIL/WARN rows and **always exits 0** — the checklist itself is the signal
  (contrast `self-test`, which exits non-zero on failure). Start here when the inbox,
  cycling, or notifications misbehave.
- `cc-tmux focus <pane_id>` — stamps `@cc-visited` on a tracked pane. Invoked
  automatically by the `pane-focus-in` hook; rarely run by hand.

### Recency & freshness

- **Recency tiebreak** — panes sort by attention priority first (`waiting → idle →
  active`); within a group the most-recently-*focused* pane surfaces first (falling
  back to last state-change for never-visited panes). A `pane-focus-in[9909]` tmux hook
  records the visit in the `@cc-visited` pane option. Opt out with `@cc-track-focus off`
  (unsets the hook).
- **Daemon-free reconcile** — the self-heal scan (clear stale `@cc-state` for dead
  Claude processes) runs on the `inbox`, `picker-data`, `cycle`, and `status` entry
  points, rate-limited by the `@cc-last-reconcile` global option. Minimum interval is
  10s, overridable via `@cc-reconcile-interval` (seconds). No background process — the
  status bar acts as the de-facto heartbeat.

### Status-bar session glyph

The session-bar row (status row 2) leads with a per-project session-count
glyph: `◌` no tracked Claude pane in the active window's project, `◉` one,
`◉ N` for N (2+). Counting keys on `@cc-project` (git-toplevel basename), so
panes inside linked git worktrees (`.worktrees/<id>/`) resolve to the
worktree's own basename and are not counted toward the parent project —
a known limitation.

### fzf preview

The inbox and picker popups render the highlighted pane's live tail (`tmux capture-pane
-ep | tail -40`) in a right-side preview panel. The `display-menu` fallback (no fzf)
is unchanged.

## Layout

```
apps/cc-tmux/
  bin/cc-tmux            # PYTHONPATH shim -> python3 -m cc_tmux
  src/cc_tmux/
    tmux.py             # pane-option state store (the only state store)
    priority.py         # attention-priority sort + cycle selection
    paths.py            # tmux.conf + plugin-dir detection
    cli.py / parser.py  # argparse subcommands -> cmd_<name>() handlers
    testing.py          # cc-tmux self-test
```
