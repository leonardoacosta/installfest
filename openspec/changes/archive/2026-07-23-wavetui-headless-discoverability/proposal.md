---
order: 0722e
---

# Proposal: wavetui-headless-discoverability — surface the "a" admission toggle and per-item dispatch mechanism

## Change ID
`wavetui-headless-discoverability`

## Summary
Add an always-visible admission-state hint ("a: headless dispatch (off|on)") to the persistent
strip, and a per-item dispatch-mechanism indicator in `QueuePane`'s Blocker/Stale column showing
which mechanism an "enter" press would actually use for that row. Closes a discoverability gap
found live during a `/explore` session: `HeadlessDispatcher`, the admission loop
(`Controller.OnSnapshot`/`admit`), and the `"a"` keybinding are all real, wired, and archived
(`wavetui-daemon`, `wavetui-headless-admission`) — but `HeadlessBar.View()` renders the empty
string in the default (admission-off) case by design, so the one keybinding that turns it on has
never been mentioned anywhere in the UI, including the current help line.

## Context
- depends on: `wavetui-daemon`, `wavetui-headless-admission` (both archived — this proposal is
  additive UI surface on top of already-shipped mechanism, not new dispatch logic),
  `wavetui-table-detail-polish` (soft, in-flight — this proposal's persistent-strip addition
  should land after that proposal's height-budget fix, not before; both touch `root.go`)
- touches: `apps/wavetui/internal/ui/headlessbar.go`, `apps/wavetui/internal/ui/root.go`,
  `apps/wavetui/internal/ui/queuepane.go`
- **Found live, not speculative**: from the same `/explore` session (2026-07-22) that produced
  `wavetui-table-detail-polish` (order 0722c) and `wavetui-item-description` (order 0722d) — a
  distinct proposal because it needed a real UX decision first (resolved via `AskUserQuestion`
  during this feature's own Phase 2: both an always-visible hint AND a per-item indicator).
  Prior instance of this same symptom class: bd `if-ugxa.2` ("nothing in the wired app ever
  calls HeadlessDispatcher.Dispatch"), closed by scaffolding `wavetui-headless-admission` — that
  proposal wired the MECHANISM but never its UI discoverability, making this the second-order
  instance of "shipped but invisible."
- **Reuse-not-rebuild (Reader Gate)**: `Controller.AdmissionEnabled()` already exists
  (`internal/daemon/daemon.go`) — the persistent-strip hint reads this directly, no new state.
  The per-item indicator does NOT add a new `Resolver` preview API (see Risks for why) — it
  reuses `item.Session.PaneID` (already on every `store.Item`, already what
  `TmuxDispatcher.resolveTarget` itself prefers first per that function's own doc comment) as an
  approximation of which mechanism `Resolver.Dispatch` would pick.
- Capability Preflight: not applicable — local dev tool, no hosting/deploy component, same
  precedent every prior wavetui proposal cites.

## Motivation
A real, tested, autonomous headless-dispatch mechanism exists in this codebase and has existed
since `wavetui-headless-admission` archived — but an operator who hasn't read the Go source has
no way to discover the `"a"` keybinding exists, whether it's currently on, or what dispatch
mechanism a given item's "enter" press will actually use. This is the exact "we've completely
lost the idea of headless dispatch" symptom reported live against the running TUI: the feature
was never lost, it was never visible.

## Requirements

### Requirement: An operator keybinding enables/disables headless admission
See `specs/wavetui/spec.md`.

### Requirement: QueuePane renders the live queue with blocker badges and fan-out score
See `specs/wavetui/spec.md`.

## Scope
- **IN**: an always-visible admission-state line ("a: headless dispatch (off)"/"(on)") appended
  to the persistent strip — visible regardless of `HeadlessBar`'s own empty-string common-case
  render (a separate, always-rendered element, not a change to `HeadlessBar.View()`'s existing
  "renders nothing when not paused" contract); a per-item dispatch-mechanism tag in
  `QueuePane`'s Blocker/Stale column ("tmux" when `item.Session != nil &&
  item.Session.PaneID != ""`, "clipboard" otherwise) rendered ahead of any existing badge
  precedence (dispatch badge > lane badge > blocker badge, unchanged) when none of those three
  already occupy that cell.
- **OUT**: a new `Resolver.PreviewTarget`-style API that exactly mirrors
  `TmuxDispatcher.resolveTarget`'s full best-guess-candidate scoring — the per-item indicator is
  a labeled approximation (see Risks), not a byte-exact preview; changing `HeadlessBar`'s own
  render contract (still renders nothing when not paused — the new admission hint is a
  SEPARATE, always-rendered line, not a change to that pane); any change to admission's actual
  eligibility/scheduling logic (`Controller.admit` is unmodified).

## Done Means
- Operator sees "a: headless dispatch (off)" or "(on)" somewhere in the persistent strip on
  every run, regardless of whether admission has ever been toggled
- Operator toggling "a" sees that hint's text flip immediately
- Operator sees a per-item tag in the queue indicating which dispatch mechanism ("tmux" vs
  "clipboard") an "enter" press on that row would use, when no other badge already occupies
  that column

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/ui/headlessbar.go` / `root.go` (admission-state hint) | N/A — no pure-function render logic beyond Go compile | `[3.1]` (pty runtime verification) |
| `internal/ui/queuepane.go` (per-item mechanism tag) | N/A | `[3.1]` |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/ui/headlessbar.go` | New method exposing a one-line admission-state hint string, independent of `View()`'s existing empty-common-case contract |
| `apps/wavetui/internal/ui/root.go` | `View()` appends the admission-state hint to the persistent strip |
| `apps/wavetui/internal/ui/queuepane.go` | `renderBlockerCell` gains a mechanism-tag fallback when no dispatch/lane/blocker badge already occupies that cell |
| `openspec/specs/wavetui/spec.md` | Two existing Requirements get `## MODIFIED Requirements` deltas |
| Existing repo files outside the three `- touches:` paths | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| The per-item mechanism tag is a heuristic (linked-pane presence), not a byte-exact preview of `TmuxDispatcher.resolveTarget`'s full scoring (which also considers same-window/same-session/project-affinity candidates when no pane is directly linked) | Labeled and scoped explicitly as an approximation in this proposal's Scope/OUT — a real preview API is a larger, separate lift (risk of the preview logic drifting from real dispatch logic over time is a maintenance cost this proposal deliberately avoids taking on) |
| Adding a persistent-strip line changes vertical space accounting `wavetui-table-detail-polish`'s height-budget fix (`extraPaneReservedRows`) also touches | Sequenced explicitly AFTER `wavetui-table-detail-polish` via the `## Context` soft dependency declared above |
