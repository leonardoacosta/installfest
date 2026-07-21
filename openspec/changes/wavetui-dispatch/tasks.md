---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-7mq2 -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] [user] DECISION: wave-file format (JSON vs Markdown) and whether a finalized wave is itself a bead — `design.md` § Open Question presents the tradeoff and recommends JSON, not a bead. searched: `wavetui-core` design.md § Store data model (forward-compat placeholder only, no format chosen), `wavetui-sessions` design.md § Store additive fields (same); no documented convention in this repo commits a format for this cross-proposal machine artifact. [type:config] [beads:if-4exl]
  - Resolved during this run's Phase 0d preflight — see `decisions.jsonl` (`{"task": "1.1", "action": "answer", "by": "leo", "verdict": "JSON, not a bead", ...}`, run `apply-all-20260721-021527`). Settled: the wave file format is JSON; a finalized wave is NOT itself a bead. No I/O implemented by this task — this is the decision record consumed by `[1.4]`'s design and the UI batch's `[3.3]` finalization writer.
- [x] [1.2] Scaffold `apps/wavetui/internal/dispatch/`: `Dispatcher` interface (`Dispatch(ctx, item, promptText) error`), error types `ErrPaneInCopyMode`/`ErrSessionStreaming`, and `validateDispatchTarget` (id-shaped regex `^[A-Za-z0-9_-]+$`) per `design.md` § Dispatcher interface / § Dispatch-boundary validation [beads:if-4f2x]
- [x] [1.3] Extend `wavetui-core`'s `internal/store/store.go` additively: add `Item.TouchedFiles []string` per `design.md` § Store additive field — no existing field renamed, removed, or re-typed [beads:if-dym7]
- [x] [1.4] Implement `apps/wavetui/internal/wave/wave.go` skeleton: `ConflictsFor(candidates []store.Item) map[string][]string` pure file-overlap detection per `design.md` § Store additive field — no I/O in this task, finalization writer lands in the UI batch [beads:if-fxn9]
  - depends on: 1.3

## API Batch

- [ ] [2.1] Implement `internal/dispatch/tmux.go` target resolution: prefer `item.Session.PaneID` when set, else score `cc-tmux conductor list --json` candidates same-window > same-session > other with ties prompting rather than picking silently, reuse `cc-tmux conductor dispatch --mode switch` for the no-paste switch action per `design.md` § TmuxDispatcher primitive choice [beads:if-6o5w]
  - depends on: 1.2
- [ ] [2.2] Implement bracketed-paste delivery in `tmux.go`: `#{pane_in_mode}` check (refuse with `ErrPaneInCopyMode`), `item.Session` mid-turn-streaming check (refuse with `ErrSessionStreaming`), `@cc-state` active-pane read (same primitive `wavetui-sessions`' `TmuxSource` already reads) for the refusal decision, then `load-buffer` + `paste-buffer -p` + a SEPARATE `send-keys Enter` call — never a single `send-keys -l` carrying the full multi-line prompt, per the cc-tmux conductor `send-prompt` gap cited in `design.md` § TmuxDispatcher primitive choice [beads:if-jjgq]
  - depends on: 2.1
- [ ] [2.3] Implement `internal/dispatch/clipboard.go` `ClipboardDispatcher`: OSC52 write to `/dev/tty` with terminfo/`$TERM_PROGRAM` feature detection plus a per-project config override, pbcopy-equivalent fallback resolved via `exec.LookPath` in order `pbcopy` (Darwin) -> `xclip -selection clipboard` -> `xsel --clipboard --input` -> `wl-copy`, surfacing failure rather than silently no-op'ing when none resolve, per `design.md` § ClipboardDispatcher [beads:if-7vb8]
  - depends on: 1.2
- [ ] [2.4] Implement `internal/dispatch` `Resolver`: linked-pane priority -> scored tmux candidates -> `ClipboardDispatcher` fallback (zero candidates or no tmux session), calling `validateDispatchTarget` on every resolved pane/item ID before any dispatch call, per `design.md` § Target resolution [beads:if-9b4i]
  - depends on: 2.1, 2.2, 2.3

## UI Batch

- [ ] [3.1] Implement `QueuePane` Start key binding: dispatches the highlighted item via `Resolver`+`Dispatcher` in one action, renders an immediate failure badge on any `Dispatch` error (no automatic retry per `design.md` § No automatic retry), renders "queued — session busy" on an `ErrSessionStreaming` refusal [beads:if-7icl]
  - depends on: 2.4
- [ ] [3.2] Implement `QueuePane` select mode: multi-select accumulation ordered by `Item.FanOutScore` descending, rendering one `ConflictsFor` warning row per overlapping path (naming both item IDs, never silently dropping a candidate) [beads:if-suye]
  - depends on: 1.4, 3.1
- [ ] [3.3] Implement wave finalization writer per the format confirmed at `[1.1]` (design.md's own recommendation is JSON, not a bead — do not implement against the unconfirmed default if `[1.1]`'s resolution differs) [beads:if-8zls]
  - depends on: 1.1, 3.2
- [ ] [3.4] Wire `cmd/wavetui/main.go`: instantiate the `Resolver` + both `Dispatcher`s, thread into `QueuePane`; capture runtime evidence rendering against a real tmux session in this repo (paste rendered pty output) [beads:if-m5bc]
  - depends on: 2.4, 3.3

## E2E Batch

- [ ] [4.1] `go test` for `internal/dispatch/tmux.go`: candidate scoring (same-window > same-session > other, tie prompts rather than picks), copy-mode refusal, mid-turn-streaming refusal, and the bracketed-paste call sequence via a mock tmux runner asserting exactly three separate calls (`load-buffer`, `paste-buffer -p`, `send-keys Enter`) — never a single `send-keys -l` call carrying the full multi-line prompt — plus `validateDispatchTarget` regex accept/reject cases [beads:if-p1ru]
  - depends on: 2.1, 2.2
- [ ] [4.2] `go test` for `internal/dispatch/clipboard.go`: OSC52 path, `exec.LookPath` fallback order under a faked `$PATH` containing only `xclip` (asserts it never attempts the literal `pbcopy` binary name on that fixture), and the surfaced (not swallowed) failure when nothing resolves [beads:if-jyjj]
  - depends on: 2.3
- [ ] [4.3] `go test` for `internal/wave`: `ConflictsFor` fixtures (overlapping paths naming both item IDs, zero-overlap case, and `FanOutScore`-descending ordering in the caller's selection view) [beads:if-vot6]
  - depends on: 1.4
- [ ] [4.4] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` in a real tmux session inside this repo, Start-dispatch one item and confirm the prompt lands in a live pane (paste pty output), multi-select two items sharing a touched file and confirm the conflict warning renders before finalize, detach/unset `$TMUX` and confirm Start falls back to the clipboard path instead of failing silently (paste evidence of the clipboard fallback firing) — paste the terminal/pty output as evidence [beads:if-dtfn]
  - depends on: 3.4, 4.1, 4.2, 4.3
