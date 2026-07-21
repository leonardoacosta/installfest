# Design: wavetui-memory-timeline

## Why this needs new sources (correcting the originating exploration's premise)

The exploration that produced this proposal's brief claimed all three timelines were "already
flowing through wavetui-core's BeadsSource/OpenSpecSource, no new data source needed, just a new
derived view over the same events." Re-reading `wavetui-core`'s own `design.md` § Store data
model before designing this proposal (adversarial verification, not assumed) shows that claim
does not hold:

- `BeadsSource`/`OpenSpecSource` publish "something changed" signals onto the bus. The `Store` is
  the single writer and re-queries `bd`/`openspec` CLIs for **current** truth on every debounced
  change — per `wavetui-core`'s own invariant, it explicitly does NOT infer or retain per-event
  history ("Store derives normalized queue state by re-querying CLIs, never by inferring from
  which file changed"). There is no history buffer anywhere in `Item`/`Snapshot`.
- `OpenSpecSource` watches `openspec/changes/` only. It never watches `openspec/changes/archive/`
  — an archived proposal falls out of its watch set entirely, by design (archived proposals are
  no longer "in the queue").

Both are correct designs for wavetui-core's purpose (a live queue of CURRENT work), but neither
gives this proposal what it needs (a HISTORICAL record). This proposal therefore adds three new,
independent, read-only history sources. This is not scope creep against the "depends on
wavetui-core only" constraint — it is exactly the extension `wavetui-core`'s own design
anticipates: "Root model exposes a pluggable pane and focus-ring architecture for future sibling
panes," and sources publish onto the shared bus without touching Store or each other's state.
None of the three new sources modify `wavetui-core`'s `store.go`, `bus.go`, or existing sources.

## On-demand querying, not Snapshot-resident

`BeadsSource`/`OpenSpecSource` recompute a full `Snapshot` for ALL items on every debounced
filesystem change, because current-state queue rendering needs every item's current truth at
once. Per-item HISTORY does not share that requirement — folding three full history queries
(interactions.jsonl scan, git-log for the item's archived proposal, git-log for the memory
directory) into every Snapshot tick, for every item, would be wasted work for every item that
isn't currently selected.

Instead, the three timeline sources are queried **on-demand, scoped to the currently selected
item only**, triggered by a selection-change signal — the same signal `DetailPane` already
depends on to know "whichever row is currently selected in `QueuePane`" (that mechanism lives in
the root model, per `wavetui-core`'s design; this proposal reuses it rather than inventing a
second selection-tracking path). The three queries run in a goroutine group off the render path
and push their result back via `Program.Send()` once complete — the same non-blocking,
never-poll-in-`Update()` discipline `wavetui-core` mandates for Store snapshots, applied here to
per-item history instead of global state. `MemoryTimelinePane.Update()` never itself invokes
`bd`, `git`, or filesystem reads directly; it only ever reacts to a `TimelineMsg` sent from the
query goroutine group.

```
selection change (root model)
        │
        ▼
  timeline query dispatcher (debounced ~200ms — avoid firing on rapid arrow-key scrolling)
        │
        ├──► BeadsHistorySource.Query(ctx, itemID)        ──► []Entry (source=bead)
        ├──► OpenSpecArchiveSource.Query(ctx, item)        ──► []Entry (source=archive) | none
        └──► MemoryHistorySource.Query(ctx, projectRoot)   ──► []Entry (source=memory)
                        │
                        ▼
              merge.Interleave(entries...) — date-grouped, precision-aware
                        │
                        ▼
              Program.Send(TimelineMsg{Entries, Errors})
                        │
                        ▼
              MemoryTimelinePane.Update() renders
```

## Memory directory resolution

Claude Code's per-project memory lives at `~/.claude/projects/<flattened-cwd>/memory/`, where
`<flattened-cwd>` replaces every `/` in the target project's absolute path with `-` (Claude Code's
own convention — verified against this repo's own live memory directory,
`-home-nyaptor-dev-personal-installfest`). Two resolution steps this source MUST get right,
both verified live against this machine rather than assumed:

1. **`~/.claude` may itself be a symlink to a separate repo.** On this machine,
   `~/.claude` → `~/dev/cc`. `MemoryHistorySource` MUST resolve the real path
   (`filepath.EvalSymlinks`) before doing anything git-related — a raw `~/.claude/...` path fed to
   `git -C` would still work via the symlink on most platforms, but resolving explicitly avoids a
   class of "wrong working directory" bugs on any platform/config where it doesn't.
2. **The memory directory's git root is independent of the target project's own git root.**
   Verified live: `git -C ~/.claude/projects/-home-nyaptor-dev-personal-installfest/memory
   rev-parse --show-toplevel` → `/home/nyaptor/dev/cc`, NOT `installfest`. `MemoryHistorySource`
   MUST run `git rev-parse --show-toplevel` from inside the (symlink-resolved) memory directory
   itself to find the correct repo root, never assume it equals the target project's root that
   `wavetui` is otherwise operating against.

Given the resolved memory directory:

- If `journal.md` exists inside it AND contains dated entries (a heading or line matching a date
  pattern), parse those as first-person entries — `source=journal`, rendered at full confidence.
- Else, if the resolved git root is a real git repo, run
  `git log --follow -p -- <memory-dir-relative-to-repo-root>` and reconstruct one entry per commit
  touching that path (commit date + a short summary derived from the diff header — never
  synthesized prose beyond what the commit/diff already states). Label every such entry
  `source=distilled` and render it visually distinct (see Requirements below) — it is a diff-
  derived approximation of what happened, not a first-person record.
- Else (directory absent, or present but not a git repo — e.g. a fresh install before any
  `git init`), render an "unavailable" badge for the memory lane only. This is expected to be the
  common case for any project that has never had a Claude Code session, and is not an error.

No dated `journal.md` convention was found anywhere searched during authoring (this repo's own
memory directory is topic-keyed files — `feedback_*.md`, `project_*.md`, `MEMORY.md` — not a
single growing journal; a repo-wide search across `~/dev/cc` for "journal.md" and the exploration
brief's cited "Move 4" convention returned zero hits). The git-log-reconstruction path is
therefore the REALISTIC DEFAULT for every project today — this pane must be fully usable through
that path alone. The dated-journal path exists so that if a future memory-writing convention
adopts it, this pane picks it up automatically with no further wavetui change; this proposal
recommends (but does not implement or enforce) that convention.

## Bead lifecycle source: `.beads/interactions.jsonl`

`rules/BEADS.md` documents `.beads/interactions.jsonl` as bd's own append-only audit log,
explicitly intended by upstream to be versioned in git and read directly for "auditing... dataset
generation" — a stable, documented export format, unlike `.beads/*.db` (bd's unstable internal
schema, which `wavetui-core` correctly avoids parsing directly). `BeadsHistorySource` reads this
file line-by-line (JSONL — one JSON object per line), filtering to rows whose subject bead ID
matches the selected item (and, for an epic/feature item, its children — reusing whatever
parent/child traversal `wavetui-core`'s `BeadsSource` already exposes for the current snapshot,
not re-deriving bead hierarchy). Recognized interaction kinds map to timeline entries:
creation, claim, close (with reason text when present), and comment/decision-resolution rows.
An unrecognized interaction `kind` value is rendered as a generic "activity" entry rather than
dropped — tolerant-decode, matching `wavetui-core`'s house style for forward-compat against a
future bd release adding new kinds.

A missing `.beads/interactions.jsonl` (bd never initialized, or `bd hooks install` never run in
that project) degrades to an "unavailable" badge for the bead-lifecycle lane only — same
badge-not-crash precedent `wavetui-core` established for a missing `.beads/`/`openspec/changes/`
directory outright.

## OpenSpec archive milestone source

`OpenSpecArchiveSource` answers one question per selected item: "did this item's proposal ever
get archived, and when?" It does not watch anything (no fsnotify) — it is a point-in-time query
run only when an item is selected. Given a proposal slug (from the selected item, when the
selected item IS a proposal-kind `Item`; bead-kind items with no associated proposal slug get no
archive entry at all — this is a normal, badge-free empty lane, not a degraded state), it looks
for a matching directory under `openspec/changes/archive/` (archived proposals are prefixed with
their archive date, so a substring/glob match on the slug is required, not an exact path) and,
if found, runs `git log -1 --format=%aI --diff-filter=A -- <archived-path>` to recover the
add-to-archive commit's timestamp as the milestone date. No git history for that path, or no
matching archived directory at all, is treated as "this item was never archived" — an expected,
non-error outcome, not a badge.

## Journal-to-bead matching

Without an explicit convention, matching a `source=journal`/`source=distilled` entry to a
specific bead is inherently fuzzy. Two tiers, most-confident first:

1. **Inline bead-ID reference** — if the entry's text contains a bracketed reference matching
   this repo's bead-ID grammar (e.g. `[if-tkva]`), that is a confident match: `source=journal`
   entries with an inline ref render at full confidence, no visual hedge.
2. **Timestamp-proximity fallback** — absent an inline ref, match by nearest-timestamp proximity
   to a bead lifecycle event within a configurable window (default 10 minutes — reusing the exact
   proximity-matching pattern `wavetui-sessions`' `session_link.go` already established for
   linking a claimed item to its Claude Code session by cwd+timestamp proximity, not inventing a
   second fuzzy-matching algorithm). A fuzzy match renders visually tentative — dimmed text plus a
   `?` marker — never asserted as certain. Git-log-derived (`source=distilled`) entries are
   ALWAYS fuzzy-matched (a commit diff has no inline-ref convention to look for), so every
   `distilled`-source entry always carries the tentative-match rendering when matched to a bead
   at all, and simply renders unmatched (bucketed by date only) when no bead lifecycle event
   falls within the proximity window.

This proposal recommends inline bead-ID refs as the convention going forward for any FUTURE
memory-writing tooling — never retroactively enforced on existing content, which has no such
convention to comply with.

## Interleaved rendering and precision-aware ordering

`merge.Interleave` takes the three sources' entry slices and produces one ordered list, grouped
by date. Each `Entry` carries a `Precision` (`Timestamp` or `DateOnly`) alongside its time value.
Entries are sorted by date first; within a single date, entries carrying full-timestamp precision
sort chronologically among themselves, but the merge function NEVER interleaves a date-only entry
(typical for git-log-derived history on an older commit, or a bead event pulled from
"createdAt"-only), and never asserts a specific intra-day position for a date-only entry relative
to a timestamped one on the same date — those render together as one same-day group with no
implied ordering between them, matching the instruction in this proposal's Done Means to never
fabricate cross-source ordering across different-precision sources.

## Hard boundary: render, never distill

This is the single most important constraint on this proposal, restated here as the design-level
non-goal it is: `MemoryTimelinePane` and all three of its sources are **strictly read-only**.
None of `internal/timeline/beads_history.go`, `openspec_archive.go`, `memory_history.go`, or
`merge.go` ever opens a memory/journal/bead-note file for writing, and none of them summarize,
rewrite, paraphrase, or otherwise generate new content from what they read — they render existing
content verbatim (or, for `distilled` entries, a mechanical diff-header extraction, never a
generated summary). Summarization/distillation of memory content is a deliberate, SEPARATE,
future batch job ("memory-distill", referenced in the originating exploration, out of scope
here) — if this pane became a writer or a summarizer, the exact memory-thrash problem that
motivated keeping distillation as a separate batch-only process would return. `tasks.md` contains
zero tasks that open a memory/journal/bead-note path in a write mode.

## Pane implementation

`MemoryTimelinePane` implements `wavetui-core`'s `Pane` interface (`Update(Snapshot) Pane`,
`View() string`, `Focusable() bool`) and attaches to the existing focus ring by appending to the
root model's pane slice — no reordering or removal of any existing pane, matching the append-only
precedent `wavetui-sessions` and `wavetui-decision-lanes` both already established for their own
pane additions. Its `View()` renders the date-grouped, source-tagged, precision-aware, confidence-
annotated list produced by `merge.Interleave`; when no item is selected, or the selected item has
no history in any of the three lanes yet, it renders an empty state rather than a badge (empty is
not an error — no history to show is a normal outcome for a freshly created item).
