# Design: wavetui-core

## Architecture

```
                 fsnotify + debounce            re-query via CLI (--json only)
  .beads/*  ───────────────────────►  BeadsSource   ──────────────────────►  bd list / bd ready
  openspec/changes/* ────────────────► OpenSpecSource ────────────────────►  openspec show / tasks.md parse

                        typed events
  BeadsSource, OpenSpecSource  ───────────────►  event bus  ───────────────►  Store (single writer)
                                                                                   │
                                                                          immutable Snapshot
                                                                                   ▼
                                                                        bubbletea Program.Send()
                                                                                   │
                                                                                   ▼
                                                                     root model (focus ring)
                                                                       ├── QueuePane
                                                                       └── DetailPane
```

Sources never touch Store state directly and never touch each other. Each source's only output
is a typed event published to the bus. The Store is the only component that mutates derived
state, and it does so by re-querying the relevant CLI after a debounce window — never by parsing
a changed file's contents directly, and never by inferring semantic meaning from *which* file
changed (a `.beads/id.db-wal` write means "something in beads changed", not "issue X changed").

The bubbletea `Program` is a pure consumer: the Store pushes an immutable `Snapshot` value via
`Program.Send()` whenever state changes (coalesced to ~10fps so an fs-event burst does not spam
renders), and the root model's `Update()` never contains watcher logic — it only ever reacts to
a `SnapshotMsg`.

## Store data model (forward-compat for wavetui-dispatch's wave-file feature)

`wavetui-dispatch` (a sibling proposal, out of scope here) will eventually need to know about
wave files and whether a wave file is itself a bead. This proposal does not implement that, but
the Store's `Item` struct is deliberately generic so a future wave-file source can publish
`Item`s into the same Store without a schema change:

```go
type ItemKind string

const (
    KindBead     ItemKind = "bead"
    KindProposal ItemKind = "proposal"
    // KindWaveFile is intentionally NOT added here — wavetui-dispatch's concern.
    // The enum is a plain string type specifically so a sibling proposal can add
    // a new kind without touching this file's exported API.
)

type Item struct {
    ID           string
    Kind         ItemKind
    Title        string
    CreatedAt    time.Time
    Blocker      *BlockerNote // nil when unblocked
    FanOutScore  int          // count of transitive dependents this item unblocks
    TaskProgress *TaskProgress // nil when not applicable (e.g. a bead with no sub-tasks)
    Stale        bool         // true when the backing CLI call failed and this is last-good data
}

type Snapshot struct {
    Items     []Item
    Errors    []SourceError // per-source badge state, never a panic
    Generated time.Time
}
```

`Snapshot` is passed by value at the point it leaves the Store (copy-on-write) — the UI holds its
own copy and the Store's next mutation cannot retroactively change a snapshot the UI already
rendered.

## Blocker-note grammar (formalized here — did not exist anywhere in the codebase before this proposal)

Grammar, applied to a single line (bead notes text, or a `proposal.md` `## Context` line):

```
blocked: <type> - <reason> (see <ref>)
```

- `<type>` — one of `decision`, `dependency`, `external`, `review`. Unknown types are accepted
  but render with a generic badge (forward-compat: a future type does not need a parser change).
- `<reason>` — free text, required, up to the optional `(see <ref>)` suffix or end of line.
- `(see <ref>)` — optional. `<ref>` is typically a bead ID, slug, or URL; rendered as a clickable
  reference in `DetailPane` when present.

Regex: `^blocked:\s*([\w-]+)\s*-\s*(.+?)(?:\s*\(see\s+([^)]+)\))?$` (case-insensitive on the
`blocked:` prefix only — type and reason are case-preserved for display).

**Location decision**: the convention lives in whichever text field the source already has —
bead notes for `BeadsSource`, and the `proposal.md` `## Context` section (as a plain bullet, not
a new required header) for `OpenSpecSource`. No new file or frontmatter field is introduced;
this keeps the convention usable immediately in existing beads/proposals without a migration.
A line that does not match the grammar is simply not treated as a blocker note — no error, no
badge — consistent with the "tolerant decoding everywhere" edge case.

## Alternatives / Related Work

**cc-tmux** (`openspec/specs/cc-tmux/spec.md`) already does hook-driven pane-state tracking and
a beads/openspec summary row sourced from nx-agent's roadmap-pulse endpoint. It is an
always-visible status STRIP (one line in a tmux status bar); `wavetui-core` is a full-screen
INTERACTIVE app with per-item detail. The two are complementary:

- cc-tmux answers "is anything waiting on me, at a glance, without leaving my current pane."
- wavetui answers "let me look at the actual queue, in detail, and drill into one item."

wavetui does not consume cc-tmux's roadmap-pulse endpoint because that endpoint is counts-only
(no per-item title/blocker/task-progress) — `BeadsSource`/`OpenSpecSource` shell `bd`/`openspec`
directly for full item detail instead. A future sibling proposal (`wavetui-sessions` or
`wavetui-dispatch`) may choose to integrate with cc-tmux's pane-state tracking for a
sessions-aware pane; that decision is explicitly deferred to those proposals.

**Considered and rejected**: parsing `.beads/*.db` directly with a Go SQLite driver instead of
shelling `bd`. Rejected because `bd`'s schema is not a stable public contract (it changes across
`bd` releases — see `rules/BEADS.md`'s several documented `bd` CLI-version migrations), while
`bd list --json` / `bd ready --json` are the documented stable interface. Watching the WAL files
without parsing them (just as a debounce trigger) avoids the schema-coupling risk entirely.

## Pane extensibility

`internal/ui` defines a small `Pane` interface (`Update(Snapshot) Pane`, `View() string`,
`Focusable() bool`) that `QueuePane` and `DetailPane` both implement. The root model holds an
ordered slice of `Pane` plus a focus index (the "focus ring"). Sibling proposals (sessions pane,
KPI bar, error feed) attach by appending to that slice — no root-model rework needed, per the
Done Means this proposal is scoped to enable but not itself required to prove beyond compiling
against the interface.
