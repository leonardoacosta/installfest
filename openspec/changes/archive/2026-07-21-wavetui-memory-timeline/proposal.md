---
order: 0720f
---

# Proposal: wavetui-memory-timeline — per-item interleaved bead/memory/openspec timeline pane

## Change ID
`wavetui-memory-timeline`

## Summary
Add `MemoryTimelinePane` to `apps/wavetui/`, a new focus-ring pane that, for whichever queue item
is currently selected, renders one interleaved timeline merging three sources: bead lifecycle
history (from `.beads/interactions.jsonl`), Claude Code project-memory history (a dated
`journal.md` when that convention exists, else git-log reconstruction against the memory
directory), and openspec archive milestones (from `openspec/changes/archive/`). This proposal
depends on `wavetui-core` only.

## Context
- depends on: `wavetui-core`
- **Depends on `wavetui-core`** (spec dir `openspec/changes/wavetui-core/`, capability epic
  `if-tkva`, feature bead `if-3g1c`): this proposal's new pane attaches to `wavetui-core`'s
  existing focus ring via its `Pane` interface, and its timeline sources publish onto the same
  event bus so the Store remains the only writer of shared state. Soft dependency only — both
  proposals are independently authored/reviewable, but `wavetui-core` should land first in any
  apply wave since this proposal's pane and sources build on its `Pane` interface and selection-
  threading mechanism (the same mechanism `DetailPane` already relies on to know "whichever row
  is currently selected in `QueuePane`").
- **This is proposal 5 of 7 in the wavetui dependency spine**: `wavetui-core` ->
  {`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} ->
  {`wavetui-decision-lanes`, `wavetui-daemon`}. Resolves to the SAME capability epic (`if-tkva`,
  `[CAPABILITY] wavetui`) — verified at Phase 4 Gate 4.1 below, not re-created. Does not depend
  on and does not pull in scope from `wavetui-sessions`, `wavetui-dispatch`, or
  `wavetui-decision-lanes` — those are unrelated siblings.
- **Architecture correction from the originating exploration (adversarial verification, not
  assumed)**: the exploration that motivated this proposal described all three timelines as
  "already flowing through wavetui-core's BeadsSource/OpenSpecSource, no new data source needed."
  Reading `wavetui-core`'s own `design.md` (§ Store data model) shows this is not accurate:
  `BeadsSource`/`OpenSpecSource` publish state-change SIGNALS, and the Store re-queries `bd`/
  `openspec` for CURRENT truth only — neither retains or exposes historical lifecycle events
  (created/claimed/closed-with-reason/decision resolutions), and `OpenSpecSource` explicitly does
  not watch `openspec/changes/archive/`. This proposal therefore adds genuinely new, read-only
  history sources rather than a pure derived view — exactly the kind of addition `wavetui-core`'s
  design anticipates ("Root model exposes a pluggable pane and focus-ring architecture for future
  sibling panes"; sources publish onto the shared bus without touching Store or each other's
  state). See `design.md` § Why this needs new sources for the full citation trail.
- **Memory-directory location is cross-repo, not project-local — verified against this repo's own
  live filesystem, not assumed**: a target project's Claude Code memory lives at
  `~/.claude/projects/<flattened-cwd>/memory/` (flattened per Claude Code's own convention: `/`
  replaced with `-`), and on this machine `~/.claude` is itself a symlink whose git repo root
  resolves to a SEPARATE repo (`~/dev/cc`), not the target project's own repo. Confirmed live:
  `git -C ~/.claude/projects/-home-nyaptor-dev-personal-installfest/memory rev-parse
  --show-toplevel` resolves to `/home/nyaptor/dev/cc`, not `installfest`. The git-log-
  reconstruction fallback path MUST run against the memory directory's OWN resolved git root, not
  the target project's repo — a wrong-repo `git log` would silently return nothing. See
  `design.md` § Memory directory resolution.
- **No `journal.md` dated-entry convention currently exists anywhere searched** (this repo's own
  memory dir, and a `journal.md`/"Move 4" search across `~/dev/cc`'s docs/advisor-plans returned
  zero hits) — the per-project memory store observed today is topic-keyed files
  (`feedback_*.md`/`project_*.md`/etc. plus a `MEMORY.md` index), not a single growing dated
  journal. This proposal's git-log-reconstruction fallback is therefore the REALISTIC DEFAULT
  path for every project today, not a rare edge case — the pane must be fully usable via that path
  alone, with the dated-journal path as a forward-compat convention this pane's existence
  recommends adopting (never retroactively enforced).
- **Bead lifecycle history source**: `.beads/interactions.jsonl` is bd's own append-only audit
  log, explicitly designed by upstream to be versioned in git and read directly for auditing (see
  `rules/BEADS.md`'s documented distinction from `.beads/issues.jsonl`, which is a non-authoritative
  export and is NOT the source used here). Confirmed present and git-tracked in this repo
  (`.beads/interactions.jsonl`, 218K at authoring time). Reading it directly is the sanctioned
  path — unlike `wavetui-core`'s explicit avoidance of parsing `.beads/*.db` directly (that file
  IS bd's unstable internal schema), `interactions.jsonl` is a stable, documented, versioned
  export format meant for exactly this kind of external read.
- **Openspec archive milestones**: since `OpenSpecSource` does not watch `archive/`, this proposal
  reads `git log` scoped to a proposal's archived path (`openspec/changes/archive/<dated-slug>/`)
  to recover the archive-landing timestamp, degrading to "no archive milestone" (not an error)
  when the item's proposal has never been archived or the repo has no git history for that path.
- Capability Preflight (Phase 1): not applicable, matching all four siblings' precedent — local
  Go CLI, no hosting/deploy component. Both greenfield probes (`packages/db`, `packages/api`)
  returned empty as expected for a dotfiles repo; skipped per explicit operator authorization.
- touches: `apps/wavetui/internal/timeline/beads_history.go`,
  `apps/wavetui/internal/timeline/beads_history_test.go`,
  `apps/wavetui/internal/timeline/openspec_archive.go`,
  `apps/wavetui/internal/timeline/openspec_archive_test.go`,
  `apps/wavetui/internal/timeline/memory_history.go`,
  `apps/wavetui/internal/timeline/memory_history_test.go`,
  `apps/wavetui/internal/timeline/merge.go`, `apps/wavetui/internal/timeline/merge_test.go`,
  `apps/wavetui/internal/ui/memorytimelinepane.go`

## Motivation
Understanding how one queue item actually got resolved today means separately running `bd show
<id> --long`, scrolling raw memory files by hand, and checking whether a related proposal ever
archived — three disconnected lookups with no shared timeline. `wavetui-memory-timeline` merges
all three into one interleaved, date-grouped view scoped to whichever item the operator has
selected, so the history of a decision is visible in the same place the operator is already
looking at that item's live state.

## Requirements

### Requirement: MemoryTimelinePane renders one interleaved timeline for the selected queue item
See `specs/wavetui/spec.md`.

### Requirement: BeadsHistorySource reads bead lifecycle events from .beads/interactions.jsonl, never the internal database
See `specs/wavetui/spec.md`.

### Requirement: OpenSpecArchiveSource resolves an archive-landing milestone via git log, never a database or new index
See `specs/wavetui/spec.md`.

### Requirement: MemoryHistorySource prefers a dated journal.md and falls back to git-log reconstruction against the memory directory's own resolved git root
See `specs/wavetui/spec.md`.

### Requirement: A git-log-derived memory entry is visually labeled as a distilled change, never rendered as a first-person record
See `specs/wavetui/spec.md`.

### Requirement: Journal-to-bead matching prefers an inline bead-ID reference and falls back to timestamp-proximity fuzzy matching rendered as visually tentative
See `specs/wavetui/spec.md`.

### Requirement: Timeline entries render date-grouped and never fabricate cross-source intra-day ordering when source precision differs
See `specs/wavetui/spec.md`.

### Requirement: The pane and all three timeline sources are strictly read-only over memory, journal, bead, and archive content
See `specs/wavetui/spec.md`.

### Requirement: A missing interactions log, memory directory, or archive history degrades to an unavailable badge for that lane only, never a crash
See `specs/wavetui/spec.md`.

## Scope
- **IN**: `BeadsHistorySource` (reads bd's `interactions.jsonl` audit log, kept under `.beads/`,
  filters to the selected item's bead ID and its children), `OpenSpecArchiveSource` (git-log-derived archive milestone for the
  selected item's proposal, when applicable), `MemoryHistorySource` (dated-`journal.md`-preferred,
  git-log-reconstruction-fallback, cross-repo-aware resolution of the memory directory's own git
  root), a merge/interleave function producing one date-grouped, source-tagged timeline, inline
  bead-ID-ref-preferred / timestamp-proximity-fallback journal-to-bead matching with visually
  distinct fuzzy-match rendering, `MemoryTimelinePane` implementing `wavetui-core`'s `Pane`
  interface and attaching to the existing focus ring.
- **OUT**: any summarization, rewriting, or generation of memory/journal content (a strictly
  separate, out-of-scope future "memory-distill" batch job — see `design.md` § Hard boundary:
  render, never distill, for the non-goal statement); retroactively enforcing or backfilling the
  inline-bead-ID-ref convention onto existing memory content (recommended going forward only);
  sessions pane / KPI bar (`wavetui-sessions`); dispatch/wave-file format (`wavetui-dispatch`);
  decision-lanes UI (`wavetui-decision-lanes`); daemon/background mode (`wavetui-daemon`); visual
  flair/theming beyond baseline lipgloss layout (`wavetui-flair`).

## Done Means
- Operator can select a queue item and see its bead lifecycle, memory/journal entries, and
  openspec archive milestones interleaved in one timeline pane.
- A project with no journal.md convention still shows a usable timeline via git-log
  reconstruction, visually labeled as "distilled change" rather than a first-person entry.
- Fuzzy journal-to-bead matches render visually distinct from confident matches (e.g.
  dimmed/question-marked), never asserted as certain.
- The pane never writes to memory/journal files under any code path — confirmed by a `design.md`
  non-goal statement and by `tasks.md` containing zero write-path tasks against memory content.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/timeline/beads_history.go` | `[4.1]` | `[4.5]` |
| `internal/timeline/openspec_archive.go` | `[4.2]` | `[4.5]` |
| `internal/timeline/memory_history.go` | `[4.3]` | `[4.5]` |
| `internal/timeline/merge.go` | `[4.4]` | `[4.5]` |
| `internal/ui/memorytimelinepane.go` | N/A — no pure-function render logic beyond Go compile | `[4.5]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/timeline/` | New package — three read-only history sources + merge/interleave |
| `apps/wavetui/internal/ui/memorytimelinepane.go` | New pane implementing `wavetui-core`'s `Pane` interface |
| `openspec/specs/wavetui/` | New capability spec created (`## ADDED Requirements`, same as all four prior siblings — no parent spec exists yet) |
| Existing repo files | None modified — purely additive, no `- touches:` overlap with any in-flight sibling proposal (verified via `wave-plan-build build --json`) |

## Risks
| Risk | Mitigation |
|------|-----------|
| The originating exploration's premise ("no new source needed") was factually wrong against `wavetui-core`'s actual design | Corrected in Context above via direct re-read of `wavetui-core`'s `design.md`; this proposal adds three new sources rather than silently building on a false premise. |
| `.beads/interactions.jsonl` may not exist in every target project (bd not initialized, or `bd hooks install` never run) | `BeadsHistorySource` degrades to an "unavailable" badge for the bead-lifecycle lane only, per the missing-directory-degradation precedent `wavetui-core` already established for `.beads/`/`openspec/changes/`. |
| The memory directory's git root differs from the target project's own repo, and may not exist at all for a project that has never had a Claude Code session | `MemoryHistorySource` resolves the memory directory's own git root independently (never assumes the target project's root) and degrades to "unavailable" when the memory directory itself does not exist — same badge-not-crash precedent. |
| Loading full `interactions.jsonl`/git-log history for every item on every Store re-query would be expensive at scale | Sources are queried on-demand, scoped to the currently selected item only, triggered by selection change rather than folded into the Store's per-tick `Snapshot` — a documented deviation from `BeadsSource`/`OpenSpecSource`'s always-current-Snapshot pattern, justified in `design.md` § On-demand querying, not Snapshot-resident. |
| No Go-aware `/apply` engineer agent exists in the fleet yet | Same documented `stack: t3` workaround all four prior siblings used, for the same reason (`commands/apply/references/stacks.md`'s crosswalk has no `go-cli` value yet) — cited here rather than re-derived; tracked in `wavetui-core`'s own Risks table, not duplicated as a new tracked risk. |
| Repo location (installfest vs. a standalone wavetui repo) is an open question | Same open question as all four prior siblings; authored in `installfest` per explicit operator instruction for this run, flagged here for Leo's later call, not blocking. |
