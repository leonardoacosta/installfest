---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-4ol7 -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Add additive `CWD string` field to `store.SessionLink` in [beads:if-j4qi]
  `apps/wavetui/internal/store/store.go`, per this proposal's `specs/wavetui/spec.md` MODIFIED
  "SessionsPane renders..." Requirement — no existing field renamed, removed, or re-typed.

## API Batch

- [x] [2.1] Thread `agg.cwd` into `toStoreSessionLink` in [beads:if-9kks]
  `apps/wavetui/internal/sources/transcript.go` so `SessionLink.CWD` carries the same value
  `resolveLink` already matched against, per spec.md's "the matched cwd is available on the
  linked item's SessionLink" scenario
  - depends on: 1.1

## UI Batch

- [x] [3.1] Render `SessionLink.CWD` in `SessionsPane.renderRow` [beads:if-ec7h]
  (`apps/wavetui/internal/ui/sessionspane.go`) alongside the existing pane/context%/zombie
  fields; guard against `defaultSessionsWidth` row overflow (basename or truncated form, per
  proposal.md's Risks table)
  - depends on: 2.1
- [x] [3.2] Change `SessionsPane.View`'s static "Sessions" header text [beads:if-s3tv]
  (`apps/wavetui/internal/ui/sessionspane.go`) to state its per-selected-item scope, per
  spec.md's "the header states the pane's per-item scope" scenario

## E2E Batch

- [x] [4.1] `go test` for `apps/wavetui/internal/store/store.go`: confirm existing `SessionLink` [beads:if-lonx]
  field tests still pass unmodified, plus new coverage confirming `CWD` round-trips through
  `Snapshot` immutability
  - depends on: 1.1
- [x] [4.2] `go test` for `apps/wavetui/internal/sources/transcript.go`: confirm [beads:if-50dm]
  `toStoreSessionLink`'s output `CWD` matches `agg.cwd` for a fixture session, existing tests
  still pass unmodified
  - depends on: 2.1
- [x] [4.3] Runtime-verify: run `apps/wavetui/cmd/wavetui` against this repo's own live Claude [beads:if-sr6k]
  Code transcript and tmux session, confirm `SessionsPane` renders a non-empty cwd value for a
  currently-linked session's row, and confirm the header text reads as scoped to the selected
  item — paste the terminal/pty output as evidence
  - depends on: 3.1, 3.2, 4.1, 4.2
  - Degraded per dispatch's own allowance ("full interactive pty verification ... is optional if
    build+vet+all tests pass"): `go build ./cmd/wavetui` succeeds (binary built and removed after
    check), `go vet ./...` clean, and `TestTranscriptSourceThreadsCWDIntoSessionLink` /
    `TestSnapshotRoundTripsSessionLinkCWD` runtime-prove `SessionLink.CWD` reaches the Snapshot a
    real `SessionsPane.Update` consumes end-to-end from a fixture transcript line carrying
    `"cwd":"/home/nyaptor/dev/personal/installfest"`. No live pty/tmux session was captured.
