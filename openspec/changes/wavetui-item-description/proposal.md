---
order: 0722d
---

# Proposal: wavetui-item-description — plumb a Description field into DetailPane

## Change ID
`wavetui-item-description`

## Summary
Add an additive `Description string` field to `store.Item`, populate it from `bd list --json`'s
existing `description` key (beads) and a new `## Summary` section extraction from `proposal.md`
(openspec proposals), and render it in `DetailPane`. Closes an observability gap found live
during a `/explore` session: an operator selecting an item in the queue currently sees title,
ID, kind, blocker, task progress, and fan-out score — but never the actual description text
that explains *what the item is*, even though that data is already available at the source for
beads and readily extractable for proposals.

## Context
- depends on: none
- touches: `apps/wavetui/internal/store/store.go`, `apps/wavetui/internal/sources/beads.go`,
  `apps/wavetui/internal/sources/beads_test.go`, `apps/wavetui/internal/sources/openspec.go`,
  `apps/wavetui/internal/sources/openspec_test.go`, `apps/wavetui/internal/ui/detailpane.go`
- **Found live, not speculative**: from the same `/explore` session (2026-07-22) that produced
  `wavetui-table-detail-polish` (order 0722c) — one of that exploration's 10 findings, split
  into its own proposal because it is a data-plumbing change (new field, two source parsers),
  not a rendering tweak.
- **Reuse-not-rebuild (Reader Gate)**: `bd list --json`/`bd ready --json` (the only bd
  subcommands `BeadsSource` calls — see `beads.go`'s own doc comment: "It never parses
  `.beads/*.db` itself... `bd list`/`bd ready --json` are the documented stable interface")
  ALREADY return a `description` key in their JSON output (confirmed live:
  `bd list -n 2 --json` → field set includes `description`) — no new bd subcommand, no N+1
  per-item query. `openspec.go`'s `parseOneProposal` already reads `proposal.md`'s full content
  for `titleRe`/`parseProposalBlocker`/`parseProposalTouches` — a `## Summary` section extractor
  is one more regex against content already in memory, not a new file read.
- Capability Preflight: not applicable — local dev tool, no hosting/deploy component, same
  precedent every prior wavetui proposal cites.

## Motivation
Every other queue-adjacent surface in this codebase (bd itself, an openspec proposal.md) treats
a description/summary as first-class content. `DetailPane` — the one place an operator goes to
understand a selected item in depth — has no way to show it today, forcing a context-switch to
`bd show <id>` or opening `proposal.md` directly to answer "what is this item, actually."

## Requirements

### Requirement: DetailPane renders full detail for the selected queue row
See `specs/wavetui/spec.md`.

### Requirement: Store derives normalized queue state by re-querying CLIs, never by inferring from which file changed
See `specs/wavetui/spec.md`.

## Scope
- **IN**: additive `Description string` field on `store.Item`; populating it in `beads.go`'s
  `toItem` from `beadRecord.Description` (new field on that struct, mapped from bd's existing
  `description` JSON key); populating it in `openspec.go`'s `parseOneProposal` via a new
  `summaryRe` regex extracting the `## Summary` section body (mirrors `titleRe`'s existing
  single-purpose-regex convention); rendering it in `DetailPane.View()`.
- **OUT**: any change to `bd`'s own CLI or output shape; markdown rendering/formatting of the
  description text (plain text, wrapped to `detailWidth`, same as every other `DetailPane`
  field today); a description for a bead with no `notes`/`description` set (renders nothing,
  same "absence is the signal" convention `Blocker == nil` already establishes) — see
  `wavetui-table-detail-polish`'s companion change to that same convention.

## Done Means
- Operator selecting a bead in the queue sees that bead's `bd`-recorded description in the
  detail pane
- Operator selecting an openspec proposal sees that proposal's `## Summary` section body in the
  detail pane
- Operator selecting an item with no description/summary sees no extra blank section — absence
  renders as nothing, not an empty label

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/store/store.go` additive `Description` field | `[1.1]` | `[4.1]` |
| `internal/sources/beads.go` (`toItem` threads `beadRecord.Description` through) | `[2.1]` | `[4.1]` |
| `internal/sources/openspec.go` (`parseOneProposal` extracts `## Summary`) | `[2.2]` | `[4.1]` |
| `internal/ui/detailpane.go` (`View()` renders `Description`) | N/A — no pure-function render logic beyond Go compile | `[4.1]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/store/store.go` | Additive `Item.Description string` field only |
| `apps/wavetui/internal/sources/beads.go` | Additive `beadRecord.Description` field; one-line addition to `toItem` |
| `apps/wavetui/internal/sources/openspec.go` | New `summaryRe` regex + extraction call in `parseOneProposal` |
| `apps/wavetui/internal/ui/detailpane.go` | `View()` gains a conditional description block |
| `openspec/specs/wavetui/spec.md` | Two existing Requirements get `## MODIFIED Requirements` deltas |
| Existing repo files outside the six `- touches:` paths | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| A long description could overflow `DetailPane`'s fixed `detailWidth = 44` | Wrap, don't truncate silently — `lipgloss`'s existing `Width()` styling already soft-wraps text in this file; no new truncation logic needed |
| `## Summary` section extraction regex could misfire on a proposal whose Summary contains nested `##` subheadings | Match up to the next `## ` header or end-of-file, same boundary-detection shape `titleRe`'s single-line match avoids needing entirely — bounded, tested against this repo's own existing proposal.md files as fixtures |
| None of the six touched files are touched by any other in-flight proposal | Confirmed via `wave-plan-build build --json` at Phase 2.3 of this feature's own authoring |
