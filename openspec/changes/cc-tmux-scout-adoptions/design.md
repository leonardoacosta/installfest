# Design: cc-tmux-scout-adoptions

## Decision 1: Daemon-free reconcile (Leo, 2026-07-10)

tmux-scout keeps state fresh with a tmux-owned Node watchdog (2s/30s/60s hybrid loop) plus a
single-writer Unix socket. cc-tmux's spec invariant is the opposite: pane options are the sole
state store, no background process, no external files, stdlib only.

**Chosen shape**: extract the existing inbox-open self-heal (`process scan → clear stale
`@cc-state``) into a shared `reconcile()` in `tmux.py`, called from the four read entry points
(`inbox`, `picker-data`, `cycle`, `status`). Rate limiting lives in a tmux **global** option
`@cc-last-reconcile` (epoch stamp, min interval 10s) — the stamp itself follows the
no-external-state rule and dies with the server.

Why entry-point-driven beats a daemon here: every consumer of state freshness IS one of those
entry points. A daemon buys freshness for state nobody is currently reading — pure cost. The
status bar (rendered every few seconds by tmux) becomes the de-facto heartbeat, at one process
scan per ≥10s instead of per render.

Trade-off accepted: with the status segment absent from a user's status-right, staleness can
persist until the next inbox/cycle. That is the same window today (inbox-only self-heal) or
better, and acceptable for a single-user plugin.

## Decision 2: MRU as within-group tiebreak (Leo, 2026-07-10)

tmux-scout sorts purely by visit recency (MRU floats to top). cc-tmux sorts by attention
priority (`waiting > idle > active`), newest-state-change-first within groups. Leo chose the
hybrid: **priority groups unchanged, recency replaces the timestamp as the primary within-group
key** (timestamp remains the fallback for never-visited panes).

**Storage**: `@cc-visited` epoch stamp as a *pane option* — not tmux-scout's
`access-history.json`. Pane options auto-delete with the pane, need no cap/GC, and keep the
single-source-of-truth invariant. The hop-pane single read (`get_hop_panes()`) picks it up in
the same `list-panes -F` format string — zero extra tmux round-trips.

**Recording**: adopt tmux-scout's fixed-index hook-slot idiom verbatim —
`set-hook -g 'pane-focus-in[9909]' "run-shell -b 'cc-tmux focus #{pane_id}'"`. The numeric slot
overwrites itself on every reload (idempotent) and coexists with any user-owned bare
`pane-focus-in` hook. `@cc-track-focus off` unsets the slot (`set-hook -gu`).

`priority.py` change is minimal: `_timestamp_of` gains a sibling `_visited_of`; group sort key
becomes `(-visited, -timestamp)`. Pure-function — covered by `self-test`.

## Decision 3: fzf preview transport

Inbox rows are already `label\tpane_id`. The popup gains
`--delimiter '\t' --with-nth 1 --preview 'tmux capture-pane -ep -t {2} | tail -40'` — pane_id
stays machine-readable in field 2, hidden from display, and drives both `enter` (switch) and the
preview. No new subcommand; `cmd_inbox`/`cmd_picker_data` output format is unchanged.

## Decision 4: doctor is diagnostics, not tests

`self-test` (pure functions, asserts, non-zero on failure) and `doctor` (environment I/O,
PASS/FAIL prose, always exit 0) stay separate subcommands with opposite exit contracts.
doctor's checks: tmux ≥ 3.2 (`tmux -V` parse), fzf on PATH, python ≥ 3.10, `$TMUX` set,
`~/.tmux/plugins/cc-tmux` resolves, `claude plugin list` contains cc-tmux (skipped with WARN if
`claude` absent), `pane-focus-in[9909]` hook present when tracking on, and a tracked-pane count.
