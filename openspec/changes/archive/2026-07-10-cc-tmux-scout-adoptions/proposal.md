# Proposal: cc-tmux-scout-adoptions

## Why

The `qeesung/tmux-scout` recon (`docs/recon/qeesung-tmux-scout.md`, 2026-07-10) surfaced four
findings against the just-shipped `apps/cc-tmux/` plugin. Two are straight gaps (no fzf preview
pane, no environment diagnostics), two required a design decision that Leo has now made:

- **Watchdog daemon** → adopt as a **daemon-free on-demand reconcile** (Leo's call, 2026-07-10).
  cc-tmux's core invariant — tmux pane options as the single source of truth, no background
  process, no external state files, stdlib only — is preserved. tmux-scout's daemon/socket
  architecture is NOT adopted; only its *outcome* (state stays fresh without waiting for the
  next hook) is, achieved by running the existing self-heal scan on more entry points.
- **MRU visit-recency sort** → adopt as a **recency tiebreak within priority groups** (Leo's
  call, 2026-07-10). The `waiting > idle > active` urgency-first ordering stays; within each
  group, the most-recently-visited pane surfaces first instead of most-recent-state-change.
  Visit tracking uses tmux-scout's `pane-focus-in[9909]` fixed-index hook-slot idiom (idempotent,
  coexists with any user hook) but stores the visit stamp in a **pane option** (`@cc-visited`),
  not an external `access-history.json` — same no-external-state invariant.

## What Changes

1. **fzf `--preview` pane** in the inbox and picker popups: the highlighted row's tmux pane tail
   (last ~40 lines) renders live in a right-side preview panel via `tmux capture-pane -ep`.
2. **`cc-tmux doctor`** subcommand: PASS/FAIL environment checklist (tmux ≥ 3.2, fzf present,
   python ≥ 3.10, `$TMUX` set, plugin symlink resolves, Claude plugin registered, hooks wired,
   tracked-pane sanity). Exit 0 always (fail-open convention); the checklist itself is the signal.
3. **MRU recency tiebreak**: a `pane-focus-in[9909]` tmux hook records `@cc-visited` (epoch) on
   every pane focus; `priority.py` sorts within each state group by `visited desc, timestamp desc`.
   Opt-out via `@cc-track-focus off`.
4. **Daemon-free reconcile**: the self-heal pass (process-scan → clear stale `@cc-state`) that
   today runs only on inbox open is extracted into a shared `reconcile()` and invoked from
   `inbox`, `picker-data`, `cycle`, and `status` entry points, rate-limited by a
   `@cc-last-reconcile` global-option stamp (default ≥ 10s between scans) so the 2s status-bar
   render never pays a process-scan on every tick.

## Non-Goals

- No background daemon, no Unix socket bridge, no external state files (`~/.tmux-scout/`-style).
- No multi-agent (Codex/Gemini/...) support — cc-tmux stays first-party Claude Code.
- No pure-MRU sort mode or `@cc-sort` config switch — recency is a within-group tiebreak only.
- No status-format placeholder DSL.

## Context

- touches: `apps/cc-tmux/cc-tmux.tmux`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/parser.py`, `apps/cc-tmux/src/cc_tmux/priority.py`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/testing.py`, `apps/cc-tmux/README.md`, `docs/tmux-layout-keybindings.md`

Source verdicts and evidence: `docs/recon/qeesung-tmux-scout.md` (Adapt 1, Adapt 2, Monitor→adopted
with design decision, Skip 1→adopted daemon-free). No dependency on any in-flight spec:
`view-command` touches `scripts/`/viewer files only; `add-tmux-credential-status` is stale and its
domain (usage segment) was already absorbed by the archived `cc-tmux-plugin` change — neither
shares a touched path with this proposal.

## Testing

| Seam | Coverage |
| --- | --- |
| Priority sort with visit tiebreak (`priority.py`) | `cc-tmux self-test` pure-function cases: visited beats timestamp within a group, group order unchanged, missing `visited` falls back to timestamp — tasks 1.6 / 3.1 |
| Reconcile rate-limit + stale-clear (`cli.py`/`tmux.py`) | self-test case for the rate-limit stamp logic (pure part) + live verification: kill a Claude process, run `cc-tmux status`, assert stale state cleared — tasks 1.6 / 3.3 |
| fzf preview | Live verification: open inbox with ≥2 tracked panes, assert preview panel renders the highlighted pane's tail — task 3.2 |
| doctor | Live verification: run `cc-tmux doctor` on this machine (all PASS, exit 0) and with `$TMUX` unset (FAIL row shown, still exit 0) — task 3.4 |
| Focus hook idempotency | Live verification: source tmux.conf twice, assert exactly one `pane-focus-in[9909]` hook — task 3.5 |
