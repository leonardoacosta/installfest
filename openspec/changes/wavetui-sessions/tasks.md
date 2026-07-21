---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-yufp -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Extend `wavetui-core`'s `internal/store/store.go` additively: add `Item.Session [beads:if-pjqd]
  *SessionLink` and `Snapshot.RateLimitBanner *RateLimitSignal` fields per `design.md` § Store
  additive fields — no existing field renamed, removed, or re-typed
- [x] [1.2] Implement `internal/sources/session_link.go`: exact `/apply <id>` match on `user`-type [beads:if-xa0y]
  transcript lines, cwd+claim-timestamp-proximity fallback (default 10-minute window, config from
  `wavetui-core`'s `internal/config`), subagent sidechain inheritance via `parentUuid`, cwd trusted
  over directory-name flattening per `design.md` § Session linkage algorithm
  - depends on: 1.1

## API Batch

- [x] [2.1] Implement `internal/sources/transcript.go` tail+decode: fsnotify watch on [beads:if-7s7m]
  `~/.claude/projects/<flattened-path>/*.jsonl`, per-file byte offset with partial-line remainder
  buffer, offset reset to 0 on file-size-less-than-offset (truncation/replacement), tolerant
  decode ignoring unknown `type` values and unknown fields per `design.md` § Verified transcript
  fields
  - depends on: 1.1
- [x] [2.2] Implement context gauge + zombie detection in `transcript.go`: cumulative [beads:if-sazd]
  `input_tokens` + `cache_read_input_tokens` sum from `assistant`-type `message.usage` entries vs
  approximate model-window size, 70% threshold handoff badge, zombie badge requiring
  transcript-inactivity (>= 15min config) cross-checked against `TmuxSource` pane state when
  available (never either signal alone), one-key release action wired to a `bd release` call —
  never automatic
  - depends on: 2.1, 1.2
- [x] [2.3] Implement error feed + token meter in `transcript.go`: classify `tool_result` error [beads:if-630f]
  shapes (read-first violations, string-not-found edit failures, `gate.sh BLOCKED` output,
  generic/unclassified fallback) attributed to the linked item and agent metadata; accumulate
  `output_tokens` by model per session/item/wave and flag opus running in an executor lane
  - depends on: 2.1, 1.2
- [x] [2.4] Implement rate-limit signal emission in `transcript.go`: detect a rate-limit indicator [beads:if-zzg2]
  in the transcript stream and publish a `RateLimitSignal` event onto `wavetui-core`'s bus —
  emission only, no consuming queue/scheduling logic
  - depends on: 2.1
- [x] [2.5] Implement `internal/sources/tmux.go`: `@cc-state` pane-option read [beads:if-31fw]
  (`tmux show-options -p -v -t <pane> @cc-state`) as primary path for every cc-tmux-tagged pane,
  process-tree walk (`ps -axo pid,ppid,comm`) fallback for untagged panes only, no positional
  ("adjacent pane") inference between panes, per `design.md` § Alternatives / Related Work
  - depends on: 1.1

## UI Batch

- [x] [3.1] Implement `internal/ui/sessionspane.go`: implements `wavetui-core`'s `Pane` interface, [beads:if-a1wq]
  renders pane identity (when known via `TmuxSource`), context-percent gauge, and zombie badge
  with the one-key release action; attaches to the existing focus ring without root-model changes
  - depends on: 2.2, 2.5
- [x] [3.2] Implement `internal/ui/kpibar.go`: implements the `Pane` interface, renders [beads:if-el0s]
  continue-count proxy, rate-limit-incident counter (increments on each `RateLimitSignal`), and
  stale-claim minutes (elapsed time since the oldest currently-zombie-badged claim went stale)
  - depends on: 2.3, 2.4
- [x] [3.3] Wire both panes into `cmd/wavetui/main.go`'s existing pane slice (append-only — no [beads:if-hnxc]
  reordering or removal of `QueuePane`/`DetailPane`) and confirm the focus ring cycles through all
  four panes; capture runtime evidence rendering against this repo's own live transcript + tmux
  state (paste rendered pty output)
  - depends on: 3.1, 3.2

## E2E Batch

- [ ] [4.1] `go test` for `internal/sources/transcript.go`: offset tracking across multiple reads, [beads:if-ti9t]
  partial-line buffering, truncation-triggered offset reset, tolerant decode of all ten observed
  `type` values plus one synthetic unknown type, context-gauge threshold crossing, zombie
  cross-check (transcript-inactivity alone vs. tmux-active override), error-feed classification
  fixtures, token-meter per-model accumulation, rate-limit signal emission
  - depends on: 2.1, 2.2, 2.3, 2.4
- [ ] [4.2] `go test` for `internal/sources/tmux.go`: `@cc-state`-tagged pane read path, [beads:if-43es]
  untagged-pane process-tree fallback, no-result (not a guess) when neither path finds a match,
  no positional inference between adjacent panes
  - depends on: 2.5
- [ ] [4.3] `go test` for `internal/sources/session_link.go`: exact `/apply <id>` match, [beads:if-phfg]
  cwd+timestamp fallback (both conditions required, cwd-alone and timestamp-alone rejection
  cases), sidechain-inherits-parent linkage, cwd-over-flattening trust
  - depends on: 1.2
- [ ] [4.4] `go test` for the additive `internal/store/store.go` fields: confirm existing [beads:if-x6ap]
  `wavetui-core` store tests still pass unmodified, plus new coverage for `SessionLink`/
  `RateLimitSignal` snapshot immutability
  - depends on: 1.1
- [ ] [4.5] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` against this repo's own live [beads:if-ffu4]
  Claude Code transcript and tmux session, confirm `SessionsPane` shows a context-percent gauge
  that updates as the transcript grows, confirm `KPIBar` renders and increments its rate-limit
  counter on a simulated signal, confirm a session run outside any cc-tmux-tracked pane still
  gets a zombie badge from inactivity alone, confirm a malformed transcript line degrades only
  the sessions pane (not a crash) — paste the terminal/pty output as evidence
  - depends on: 3.3, 4.1, 4.2, 4.3, 4.4
