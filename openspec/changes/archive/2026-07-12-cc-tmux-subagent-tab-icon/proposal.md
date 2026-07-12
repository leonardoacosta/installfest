---
status: draft
---

# Proposal: cc-tmux-subagent-tab-icon

## Why

User ask: the animated tab icon should stay solid ONLY when a session has no sub-agents running;
when it does, show one of four glyphs (`◎`/`◉`/`◎`/`●` — refined during clarification below) to
distinguish different sub-agent activity statuses.

**Critical mechanism finding, verified against this exact fleet's own already-shipped work
(`~/dev/cc/rules/TOOLING.md` § hook-liveness row, `ratchet-liveness-and-cost-rates`):
`SubagentStop` never fires on this Claude Code version** — confirmed dead via telemetry (zero
fires across 22.9K session transcripts) and already worked around elsewhere in this fleet by
gating at the NEXT `SubagentStart` instead. Building this feature on `SubagentStart`/`SubagentStop`
as a naive start/stop pair would ship the exact same class of bug: the "running" count would only
ever increase, never decrease, since the stop signal never arrives.

**The mechanism this proposal uses instead** (verified live against `~/dev/cc/settings.json`,
which already runs a `PostToolUse` hook matched on `"Task"` for telemetry — not a guess): a
sub-agent dispatch's own tool call (`Task`, the underlying tool name Claude Code's hook payloads
use regardless of what the fleet's dispatch wrapper is called) fires `PreToolUse` when dispatched
and `PostToolUse` when it returns — and `PostToolUse` reliably fires on real tool-call completion
(it's the same event cc-tmux's own `hooks.json` already uses to re-assert the `active` state).
For a **foreground** (blocking) dispatch, `PostToolUse` firing means the sub-agent is genuinely
done — a clean, already-proven-live start/stop pair.

For a **background** dispatch (`run_in_background: true`), the dispatching tool call returns
immediately once the background task is *launched*, not when it *finishes* — so `PostToolUse`
fires almost immediately regardless of how long the background agent actually runs, and there is
no hook event anywhere in Claude Code that signals "a background agent finished" (the harness
notifies the model directly, not via a hook). Background sub-agent activity is therefore
**tracked as a time-boxed heuristic, not an exact signal** — a background dispatch counts as
"active" for a configurable window after launch, then ages out. This is flagged explicitly, not
hidden, because pretending otherwise would repeat the exact "assumed a hook fires when it
doesn't" mistake this fleet already paid for once.

## What Changes

- **`apps/cc-tmux/hooks/hooks.json`**: add a `PreToolUse` matcher on `"Task"` (increment) and rely
  on the existing unmatched `PostToolUse` hook — extended to special-case a `"Task"` match
  (decrement foreground count) before falling through to its existing unconditional `active`
  re-assert.
- **`apps/cc-tmux/src/cc_tmux/tmux.py`**: two new pane options, `@cc-subagent-fg` (int, current
  foreground sub-agent count) and `@cc-subagent-bg` (list of launch timestamps, for the
  time-boxed background heuristic) — added to `_ALL_OPTS` so they die with the pane
  (invariant 1).
- **`apps/cc-tmux/src/cc_tmux/cli.py`**: `cmd_register` handles the new `Task`-matched
  `PreToolUse`/`PostToolUse` events to increment/decrement `@cc-subagent-fg`, and detects a
  background dispatch (from the `Task` tool's own input parameters, if the hook payload exposes
  `run_in_background`; if it does not, this task becomes a `[user]`-flagged gap — see tasks.md)
  to append to `@cc-subagent-bg`. A background entry ages out of the active count after a
  configurable `@cc-subagent-bg-timeout` (default a few minutes), pruned on read.
- **`apps/cc-tmux/src/cc_tmux/render.py`** (`animated_icon`): when `@cc-subagent-fg` /
  unexpired-`@cc-subagent-bg` counts are both zero, render exactly as today (existing
  solid/animated icon by `@cc-state`). Otherwise, render one of four glyphs by the resolved
  mapping below.
- **Glyph mapping** (clarified via `/openspec:explore` follow-up questions): foreground count
  takes precedence when both are nonzero (foreground is the known-exact signal; background is
  heuristic).

  | Condition | Glyph |
  | --- | --- |
  | 1 foreground sub-agent running | `◎` |
  | 2+ foreground sub-agents running | `◉` |
  | 0 foreground, 1 background sub-agent (unexpired) | `◎` shares the "one" glyph* |
  | 0 foreground, 2+ background sub-agents (unexpired) | `◉` shares the "multiple" glyph* |

  *Open design question carried into tasks.md task 1.1 rather than guessed: the clarification
  round established "background replaces failed" as the 4th state's *meaning*, but did not
  resolve whether background activity gets its OWN two glyphs (a 6-way total: fg-1/fg-2+/bg-1/
  bg-2+, needing 2 more distinct marks beyond the 4 named) or reuses `◎`/`◉` for both fg and bg
  counts (a true 4-way: count-1/count-2+ × unspecified-whether-fg-or-bg-distinguishable). Task
  1.1 resolves this with the user before the render logic is implemented — see tasks.md.

## Non-Goals

- No exact background-completion detection — no hook signals it; the timeout heuristic is
  explicitly approximate and documented as such in the spec.
- No sub-agent identity/name display on the tab (just a count-derived glyph) — a richer per-agent
  breakdown belongs in the existing `cc-status`/`cc-dispatch` skills or the inbox, not the
  space-constrained tab icon.
- No change to the underlying `@cc-state` (waiting/idle/active) machinery — this is an orthogonal
  overlay signal, not a replacement for it.

## Context

- touches: `apps/cc-tmux/hooks/hooks.json`, `apps/cc-tmux/src/cc_tmux/tmux.py`,
  `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `openspec/specs/cc-tmux/spec.md`
- Related: extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`). Shares `render.py`'s
  `animated_icon` with the existing "Animated tab icon" requirement — this proposal ADDS a
  precondition (fg/bg counts) to when that function's existing `@cc-state`-driven output applies,
  rather than replacing it.
- Origin: `/openspec:explore` session, 2026-07-12, refined via that session's clarifying
  questions (mechanism risk flagged before scaffolding; "background replaces failed" as the 4th
  state's semantic, per user's direct edit to the proposed mapping).

## Testing

| Seam | Coverage |
| --- | --- |
| `PreToolUse`/`PostToolUse` `Task`-matcher increment/decrement | `cc-tmux self-test` case: a mocked dispatch/return pair moves `@cc-subagent-fg` 0->1->0; two concurrent dispatches move it 0->1->2->1->0 — task 2.1 |
| Background timeout aging | `cc-tmux self-test` case: a `@cc-subagent-bg` entry older than the timeout is pruned on read and does not count toward the active total — task 2.2 |
| `animated_icon` glyph selection | `cc-tmux self-test` cases covering all resolved states from task 1.1's design decision — task 2.3 |
| End-to-end, foreground | Live verification: dispatch a real foreground sub-agent, observe the tab icon change to the "one running" glyph while it's in flight and revert to normal once it returns — paste observed output — task 3.1 |
| End-to-end, background | Live verification: dispatch a real background agent, observe the tab icon reflect it during the timeout window and age out afterward — paste observed output, noting this is a heuristic (may not match the agent's true completion time) — task 3.2 |
