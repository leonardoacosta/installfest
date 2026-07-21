---
order: 0720d
---

# Proposal: wavetui-dispatch — Dispatcher interface, TmuxDispatcher, ClipboardDispatcher

## Change ID
`wavetui-dispatch`

## Summary
Add a `Dispatcher` interface to `apps/wavetui/` plus two implementations — `TmuxDispatcher`
(pastes into a live tmux pane) and `ClipboardDispatcher` (OSC52/pbcopy fallback) — and the
`QueuePane` UI actions that use them: single-item "Start", multi-select wave building with
file-overlap conflict warnings, and wave finalization.

## Context
- depends on: `wavetui-core`, `wavetui-sessions`
- **Depends on `wavetui-core`** (spec dir `openspec/changes/wavetui-core/`, capability epic
  `if-tkva`, feature bead `if-3g1c`) and `wavetui-sessions` (spec dir
  `openspec/changes/wavetui-sessions/`, feature bead `if-yufp`). Soft dependencies only — both
  already authored and gate-clean, both should land first in an apply wave since this proposal's
  target-selection logic reads `wavetui-core`'s `Item.FanOutScore`/`Item.TouchedFiles` and
  `wavetui-sessions`'s `Item.Session.PaneID`.
- **This is proposal 3 of 7 in the wavetui dependency spine**: `wavetui-core` ->
  {`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} ->
  {`wavetui-decision-lanes`, `wavetui-daemon`}. Resolves to the SAME capability epic (`if-tkva`,
  `[CAPABILITY] wavetui`) — verified at Phase 4 Gate 4.1 below, not re-created.
- **Reuse-not-rebuild (Reader Gate, non-negotiable)**: `openspec/specs/cc-tmux/spec.md`'s
  "Conductor dispatches tasks to panes (opt-in)" requirement (line 377) already ships
  `cc-tmux conductor dispatch --mode <switch|send-prompt|spawn-task|spawn-worktree> --target
  <target> [--prompt <text>] [--force]` plus `cc-tmux conductor list --json` for pane
  enumeration (`{id, session, window, state, project, branch, task, wait_reason, timestamp}`
  per row) — documented as "the single home of the dispatch CLI shape" by the plugin's own
  `cc-dispatch` skill (`apps/cc-tmux/skills/cc-dispatch/SKILL.md`). `TmuxDispatcher` shells out to
  this CLI for pane enumeration (`conductor list --json`) and for the no-paste `switch` mode.
  **One documented gap, one narrow fallback**: reading `apps/cc-tmux/src/cc_tmux/conductor.py`
  lines 378-401 (`_dispatch_send_prompt`), cc-tmux's own `send-prompt` mode pastes via
  `tmux send-keys -t <target> -l <prompt>` (a literal-keystroke send, not bracketed paste)
  followed by a separate `send-keys ... Enter` — it never checks `#{pane_in_mode}` before
  pasting. For wavetui's multi-line dispatch prompts this is unsafe (embedded newlines inside a
  `-l` literal string are typed character-by-character into whatever process owns the pane,
  risking premature submission or shell reinterpretation — exactly the injection hazard this
  proposal's Requirements below rule out) and offers no copy-mode guard. `TmuxDispatcher`
  therefore falls back to raw `tmux load-buffer`/`paste-buffer -p` (bracketed paste) + a
  separate `send-keys Enter`, and its own `#{pane_in_mode}` check, for the paste-and-submit
  primitive ONLY — `conductor list`/`conductor dispatch --mode switch` are still reused as-is.
  See `design.md` § TmuxDispatcher primitive choice for the full citation.
- **Clipboard fallback reuses this repo's own pbcopy/xclip/wl-copy fallback order**
  (`home/dot_zsh/rc/linux.zsh` lines 45-53 alias `pbcopy` to `xclip` -> `xsel` -> `wl-copy` on
  Linux; native on Darwin per `home/dot_zsh/rc/darwin.zsh` line 30) — but a Go binary invoked
  directly does not inherit zsh aliases, so `ClipboardDispatcher` probes the same fallback
  ORDER via `exec.LookPath` against the real binaries, not the aliased name. See `design.md`.
- Capability Preflight (Phase 1): not applicable, matching both siblings' precedent — local Go
  CLI, no hosting/deploy component. Skipped per explicit operator authorization.
- touches: `apps/wavetui/internal/dispatch/dispatcher.go`,
  `apps/wavetui/internal/dispatch/dispatcher_test.go`,
  `apps/wavetui/internal/dispatch/tmux.go`, `apps/wavetui/internal/dispatch/tmux_test.go`,
  `apps/wavetui/internal/dispatch/clipboard.go`, `apps/wavetui/internal/dispatch/clipboard_test.go`,
  `apps/wavetui/internal/wave/wave.go`, `apps/wavetui/internal/wave/wave_test.go`,
  `apps/wavetui/internal/ui/queuepane.go`, `apps/wavetui/internal/store/store.go` (additive
  field only — see Risks for the coordination note with `wavetui-core`/`wavetui-sessions`)

## Motivation
`wavetui-core` renders a live queue and `wavetui-sessions` shows which items already have a
linked Claude Code session, but neither can act — an operator still has to alt-tab to a terminal
and manually type or paste a prompt into the right pane. `wavetui-dispatch` closes that gap: one
keypress sends a selected item's prompt to its linked (or best-guess) pane, or to the clipboard
when no tmux target exists, and a multi-select mode lets the operator assemble a wave plan with
file-overlap conflicts flagged before committing.

## Requirements

### Requirement: Dispatcher interface abstracts prompt delivery behind Dispatch(item, promptText)
See `specs/wavetui/spec.md`.

### Requirement: TmuxDispatcher targets a linked or best-guess pane via bracketed paste, never a literal multi-line send-keys
See `specs/wavetui/spec.md`.

### Requirement: TmuxDispatcher refuses a pane whose linked session is mid-turn or in copy-mode
See `specs/wavetui/spec.md`.

### Requirement: ClipboardDispatcher is the fallback when no tmux target exists
See `specs/wavetui/spec.md`.

### Requirement: Dispatch-boundary values are validated against an id-shaped regex before crossing into a shell or tmux buffer
See `specs/wavetui/spec.md`.

### Requirement: Dispatch failures surface immediately with no automatic retry
See `specs/wavetui/spec.md`.

### Requirement: QueuePane Start dispatches the selected item with one keypress
See `specs/wavetui/spec.md`.

### Requirement: QueuePane select mode builds a wave plan with file-overlap conflict warnings
See `specs/wavetui/spec.md`.

## Scope
- **IN**: `Dispatcher` interface, `TmuxDispatcher`, `ClipboardDispatcher`, OSC52 feature
  detection + config override, `QueuePane` Start action, multi-select wave-builder mode,
  file-overlap conflict computation, wave finalization (format per the Open Question in
  `design.md` — flagged, not silently decided), id-shaped-regex validation at the dispatch
  boundary, no-auto-retry failure surfacing.
- **OUT**: `HeadlessDispatcher` (`claude -p` scheduler — `wavetui-daemon`'s concern; the
  interface shape here is designed so it can implement `Dispatcher` later with no signature
  change, see `design.md`); decision-lanes UI (`wavetui-decision-lanes`); rate-limit-aware retry
  logic (`wavetui-daemon`'s concern — this proposal's dispatch path never silently retries, but
  the smarter backoff/retry policy is out of scope here); memory-timeline pane
  (`wavetui-memory-timeline`); visual flair/theming (`wavetui-flair`).

## Done Means
- Operator can select one queue item and dispatch it to a live pane with one keypress, with the
  prompt actually landing in that pane.
- Operator can multi-select items into a wave, see file-overlap conflicts flagged before
  finalizing, and finalize into a wave file.
- Dispatching into a pane whose linked session is mid-turn (streaming) is refused or queued,
  never blind-pasted.
- With no tmux session detected, Start falls back to copying the dispatch prompt to the
  clipboard (OSC52 or pbcopy-equivalent) instead of failing silently.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/dispatch` (Dispatcher, TmuxDispatcher, ClipboardDispatcher) | `[4.1]` | `[4.4]` |
| `internal/wave` (conflict computation, finalization) | `[4.2]` | `[4.4]` |
| `internal/ui/queuepane.go` (Start, select mode) | `[4.3]` | `[4.4]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/dispatch/` | New package — `Dispatcher` interface + two implementations |
| `apps/wavetui/internal/wave/` | New package — wave-plan assembly + conflict detection |
| `apps/wavetui/internal/ui/queuepane.go` | Extended with Start + select-mode key handling |
| `apps/wavetui/internal/store/store.go` | Additive `Item.TouchedFiles []string` field only |
| Existing repo files | None modified — `apps/cc-tmux/` is read/shelled-out-to, never edited by this proposal |

## Risks
| Risk | Mitigation |
|------|-----------|
| `stack: t3` chosen (same non-ideal fit as both siblings) — no dedicated Go engineer agent exists yet | Same documented precedent (`add-daily-brief-tui`, `harden-ssh-mesh-1password-integration`, `wavetui-core`, `wavetui-sessions`); tracked, not silently absorbed. |
| Wave-file format (JSON vs Markdown) and whether a finalized wave is itself a bead is an **open cross-proposal question** — `wavetui-core`'s `Item`/`Snapshot` and `wavetui-sessions`'s `SessionLink` are both forward-compat-shaped for it but neither commits to a format | `design.md` § Open Question presents the tradeoff with a recommendation (JSON, not a bead) but this is flagged for Leo's explicit confirmation before implementation — three proposals' data models are downstream of this call, so it is not silently picked here. |
| `Item.TouchedFiles` is a NEW additive field on `wavetui-core`'s `Item` struct (third proposal to extend it, after `wavetui-sessions`'s `Session` field) | Purely additive — no existing field renamed/removed. Populated only by `OpenSpecSource` (from `- touches:`) today; `BeadsSource` items get an empty slice, not an error. |
| cc-tmux's `send-prompt` mode is bypassed for the actual paste (see Context) — a future cc-tmux fix to add bracketed paste there would make this proposal's raw-tmux fallback redundant | Documented gap + citation in `design.md`; if cc-tmux ever ships bracketed paste in `conductor dispatch --mode send-prompt`, `TmuxDispatcher` can be simplified to a pure shell-out in a follow-up — not blocking here. |
| Repo location (installfest vs. a standalone wavetui repo) is an open question shared with both siblings | Authored in `installfest` per explicit operator instruction for this run; flagged here for Leo's later call, not blocking. |
