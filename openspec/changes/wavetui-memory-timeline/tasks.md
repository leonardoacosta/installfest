---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-v9j3 -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Scaffold `apps/wavetui/internal/timeline/` package layout: shared `Entry`/`Precision`/`MatchConfidence` types per `design.md` § Interleaved rendering and § Journal-to-bead matching, no logic yet [beads:if-hziw]
- [x] [1.2] Implement `internal/timeline/beads_history.go`: `BeadsHistorySource.Query(ctx, itemID string, childIDs []string)` reads bd's `interactions.jsonl` audit log (kept under `.beads/`) line-by-line (JSONL), filters to rows matching the given bead ID(s), maps recognized interaction kinds (create/claim/close-with-reason/comment) to `Entry` values with `source=bead`, unrecognized kinds map to a generic "activity" `Entry` rather than being dropped, missing file returns an `unavailable` badge state rather than an error [beads:if-yhok]
  - depends on: 1.1
- [x] [1.3] Implement `internal/timeline/openspec_archive.go`: `OpenSpecArchiveSource.Query(ctx, proposalSlug string)` globs `openspec/changes/archive/` for a matching dated-prefixed directory, runs `git log -1 --format=%aI --diff-filter=A -- <path>` to recover the archive-landing timestamp as a single `source=archive` `Entry`; no match returns an empty result, not an error [beads:if-1a88]
  - depends on: 1.1

## API Batch

- [x] [2.1] Implement `internal/timeline/memory_history.go` memory-directory resolution: compute `~/.claude/projects/<flattened-cwd>/memory/` from the target project's absolute path (flatten `/` to `-` per Claude Code's convention), resolve symlinks (`filepath.EvalSymlinks`), and resolve that directory's OWN git root via `git -C <resolved-dir> rev-parse --show-toplevel` — never assume it equals the target project's own repo root, per `design.md` § Memory directory resolution [beads:if-tp05]
  - depends on: 1.1
- [x] [2.2] Implement `internal/timeline/memory_history.go` journal-preferred path: when the resolved memory directory contains a `journal.md` with dated entries (heading or line matching a date pattern), parse and return them as `source=journal` `Entry` values at full confidence; no git-log call is made on this path [beads:if-9loh]
  - depends on: 2.1
- [x] [2.3] Implement `internal/timeline/memory_history.go` git-log-fallback path: when no `journal.md` exists but the resolved memory directory's git root is a real repo, run `git log --follow -p -- <memory-dir-relative-path>` from that resolved root and reconstruct one `source=distilled` `Entry` per commit (commit date + a diff-header-derived summary, never generated prose); when neither path applies (directory absent, or present but not a git repo), return an `unavailable` badge state for the memory lane only [beads:if-mxz1]
  - depends on: 2.1
- [x] [2.4] Implement journal-to-bead matching in `internal/timeline/memory_history.go`: parse an inline bead-ID reference (this repo's bead-ID grammar, bracketed form) from an entry's text as a confident match; absent that, fuzzy-match by nearest-timestamp proximity to a `BeadsHistorySource` entry within a configurable window (default 10 minutes, reusing `wavetui-sessions`' cwd+timestamp-proximity pattern per `design.md` § Journal-to-bead matching — cited, not re-derived); confident matches carry full `MatchConfidence`, fuzzy matches carry a tentative `MatchConfidence`, no match leaves the entry unmatched [beads:if-vdul]
  - depends on: 2.2, 2.3, 1.2

## UI Batch

- [ ] [3.1] Implement `internal/timeline/merge.go`: `Interleave(entries ...[]Entry) []DateGroup` sorts entries into date groups, orders full-timestamp entries chronologically within a date group, and never asserts an intra-day ordering between a date-only entry and a timestamped entry sharing the same date, per `design.md` § Interleaved rendering and the spec's precision-aware-ordering requirement [beads:if-n95r]
  - depends on: 1.2, 1.3, 2.4
- [ ] [3.2] Implement the on-demand query dispatcher: on a selection-change signal from the root model's existing selection-threading mechanism (the same one `DetailPane` already depends on), debounce ~200ms, run all three sources' `Query` calls concurrently off the render path, call `merge.Interleave` on completion, and `Program.Send()` a `TimelineMsg` — no source or git/bd invocation ever runs inside `Update()`, per `design.md` § On-demand querying, not Snapshot-resident [beads:if-1wjx]
  - depends on: 3.1
- [ ] [3.3] Implement `internal/ui/memorytimelinepane.go`: implements `wavetui-core`'s `Pane` interface, renders `TimelineMsg`'s date-grouped entries with source tags, `source=distilled` visually labeled as a distilled change, tentative-confidence matches rendered dimmed/question-marked, per-lane "unavailable" badges independent of the other two lanes, an empty state (not a badge) when the selected item has no history in any lane; attaches to the existing focus ring via append only (no reordering or removal of any existing pane) [beads:if-0lxr]
  - depends on: 3.2
- [ ] [3.4] Wire `MemoryTimelinePane` into `cmd/wavetui/main.go`'s existing pane slice (append-only) and confirm the focus ring cycles through it alongside all previously landed panes; capture runtime evidence rendering against this repo's own bd `interactions.jsonl` audit log (kept under `.beads/`), `openspec/changes/archive/`, and this project's own Claude Code memory directory (paste rendered pty output) [beads:if-yjs4]
  - depends on: 3.3

## E2E Batch

- [ ] [4.1] `go test` for `internal/timeline/beads_history.go`: recognized-kind mapping (create/claim/close-with-reason/comment), unrecognized-kind generic-activity fallback, child-bead filtering for an epic/feature selection, missing-file unavailable state [beads:if-0aug]
  - depends on: 1.2
- [ ] [4.2] `go test` for `internal/timeline/openspec_archive.go`: matching archived directory resolves a timestamp, no-match returns empty (not an error), slug-substring matching against a dated-prefixed archive directory name [beads:if-9kca]
  - depends on: 1.3
- [ ] [4.3] `go test` for `internal/timeline/memory_history.go`: journal-preferred path parses dated entries, git-log-fallback path reconstructs `source=distilled` entries from fixture commits, resolved-git-root-independent-of-target-project-root behavior (fixture with two separate repo roots), absent-directory and non-git-directory unavailable states, journal-to-bead confident-match via inline ref, fuzzy-match via timestamp proximity (both conditions required — bare proximity outside the window is rejected), no-match-leaves-unmatched case [beads:if-5nnb]
  - depends on: 2.1, 2.2, 2.3, 2.4
- [ ] [4.4] `go test` for `internal/timeline/merge.go`: date-grouping correctness, chronological ordering within a date group for timestamped entries, no-fabricated-intra-day-ordering for mixed-precision same-date entries, multi-date chronological group ordering [beads:if-3xuh]
  - depends on: 3.1
- [ ] [4.5] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` against this repo's own bd `interactions.jsonl` audit log (kept under `.beads/`), `openspec/changes/archive/`, and this project's Claude Code memory directory; select an item with real bead lifecycle history and confirm the timeline pane populates; select an item whose proposal was archived and confirm the archive milestone appears; confirm memory entries render via the git-log-fallback path with a visible "distilled change" label (since no `journal.md` exists in this repo today); confirm a fuzzy journal-to-bead match renders dimmed/question-marked; temporarily rename the `interactions.jsonl` audit log aside and confirm only the bead-lifecycle lane badges unavailable while the other two lanes still render — paste the terminal/pty output as evidence [beads:if-d216]
  - depends on: 3.4, 4.1, 4.2, 4.3, 4.4
