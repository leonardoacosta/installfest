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

- [x] [2.1] Implement `internal/dispatch/tmux.go` target resolution: prefer `item.Session.PaneID` when set, else score `cc-tmux conductor list --json` candidates same-window > same-session > other with ties prompting rather than picking silently, reuse `cc-tmux conductor dispatch --mode switch` for the no-paste switch action per `design.md` § TmuxDispatcher primitive choice [beads:if-6o5w]
  - depends on: 1.2
  - Scoring composes locality (self window/session, read from `$TMUX_PANE` via `#{window_index}`/`#{session_name}`) as the primary tier and project/branch affinity (`@cc-project`/`@cc-branch`) as a within-tier tie-break — design.md's own phrasing didn't fully specify how the four `conductor list` fields compose; resolved and documented in `design.md` § Target resolution addendum. Live-verified against this repo's own tmux server: `#{window_index}` (bare number), not `#{window_id}` ("@N"), is what actually matches `conductor list --json`'s `window` field — caught during informal real-tmux verification, would have silently broken same-window scoring otherwise.
- [x] [2.2] Implement bracketed-paste delivery in `tmux.go`: `#{pane_in_mode}` check (refuse with `ErrPaneInCopyMode`), `item.Session` mid-turn-streaming check (refuse with `ErrSessionStreaming`), `@cc-state` active-pane read (same primitive `wavetui-sessions`' `TmuxSource` already reads) for the refusal decision, then `load-buffer` + `paste-buffer -p` + a SEPARATE `send-keys Enter` call — never a single `send-keys -l` carrying the full multi-line prompt, per the cc-tmux conductor `send-prompt` gap cited in `design.md` § TmuxDispatcher primitive choice [beads:if-jjgq]
  - depends on: 2.1
  - "Actively streaming" has no dedicated `store.SessionLink` field; resolved to `@cc-state == "active"` (cc-tmux's own busy signal), gated by `Zombie == false` — documented in `design.md` § Mid-turn safety addendum.
- [x] [2.3] Implement `internal/dispatch/clipboard.go` `ClipboardDispatcher`: OSC52 write to `/dev/tty` with terminfo/`$TERM_PROGRAM` feature detection plus a per-project config override, pbcopy-equivalent fallback resolved via `exec.LookPath` in order `pbcopy` (Darwin) -> `xclip -selection clipboard` -> `xsel --clipboard --input` -> `wl-copy`, surfacing failure rather than silently no-op'ing when none resolve, per `design.md` § ClipboardDispatcher [beads:if-7vb8]
  - depends on: 1.2
- [x] [2.4] Implement `internal/dispatch` `Resolver`: linked-pane priority -> scored tmux candidates -> `ClipboardDispatcher` fallback (zero candidates or no tmux session), calling `validateDispatchTarget` on every resolved pane/item ID before any dispatch call, per `design.md` § Target resolution [beads:if-9b4i]
  - depends on: 2.1, 2.2, 2.3
  - Resolved the DB-phase-flagged regex inconsistency (`idShapeRe` doesn't match tmux pane IDs like `%12`): kept `idShapeRe`/`validateDispatchTarget` unchanged for bead/proposal IDs, added a separate `paneIDShapeRe`/`validateTmuxPaneID` (`^%[0-9]+$`) for pane IDs — narrower-scoped fix over widening the shared charclass. Documented in `dispatch.go`'s doc comments and `design.md` § Dispatch-boundary validation addendum. Every pane ID `TmuxDispatcher` resolves is validated via `validateTmuxPaneID` inside its own `Dispatch` before crossing into any tmux invocation (see `tmux.go`) — `Resolver` itself never re-extracts a raw pane ID, so it has no separate call to make.

## UI Batch

- [x] [3.1] Implement `QueuePane` Start key binding: dispatches the highlighted item via `Resolver`+`Dispatcher` in one action, renders an immediate failure badge on any `Dispatch` error (no automatic retry per `design.md` § No automatic retry), renders "queued — session busy" on an `ErrSessionStreaming` refusal [beads:if-7icl]
  - depends on: 2.4
  - "enter" dispatches via a wired `dispatch.Dispatcher` (`SetDispatcher`), promptText resolved as `"/apply " + item.ID` (RESOLVED: no prior convention existed — this fleet's own `/apply <bead-id-or-spec-name>` accepts either id-space, so the dispatched prompt hands the target session the exact command an operator would otherwise type by hand; documented in `queuepane.go`'s `renderPromptText`). Badge rendering required a `rebuildRows()` extraction so a dispatch outcome is visible immediately (same frame), not only on the next incoming `Snapshot` — table rows were previously only rebuilt inside `Update`. Blocker/Stale column widened 18->24 to fit the literal design.md phrase "queued — session busy" (21 runes) without ellipsis-truncation.
- [x] [3.2] Implement `QueuePane` select mode: multi-select accumulation ordered by `Item.FanOutScore` descending, rendering one `ConflictsFor` warning row per overlapping path (naming both item IDs, never silently dropping a candidate) [beads:if-suye]
  - depends on: 1.4, 3.1
  - "space" toggles the highlighted item into/out of the selection set (`[x] ` row marker); "esc" clears it. `SelectedForWave()` returns the FanOutScore-descending, ID-tiebroken slice both this task's conflict rendering and `[3.3]`'s writer consume.
- [x] [3.3] Implement wave finalization writer per the format confirmed at `[1.1]` (design.md's own recommendation is JSON, not a bead — do not implement against the unconfirmed default if `[1.1]`'s resolution differs) [beads:if-8zls]
  - depends on: 1.1, 3.2
  - `internal/wave/writer.go`: `BuildFile`/`WriteFile`, JSON per `[1.1]`'s confirmed resolution (`decisions.jsonl`). Reuses `internal/config.AtomicWriteFile` (wavetui-core's existing temp-file-in-same-dir-then-rename helper, whose own doc comment already invites this) rather than re-implementing the same atomic-write dance — Reader Gate. "w" in `QueuePane` calls the writer via `SetWaveWriter`, clearing the selection only on success.
- [x] [3.4] Wire `cmd/wavetui/main.go`: instantiate the `Resolver` + both `Dispatcher`s, thread into `QueuePane`; capture REAL runtime evidence rendering against a real tmux session in this repo (paste rendered pty output) [beads:if-m5bc]
  - depends on: 2.4, 3.3
  - `main.go` wires `dispatch.NewResolver(NewTmuxDispatcher(), NewClipboardDispatcher(cfg.ForceOSC52))` into `QueuePane.SetDispatcher`, and `wave.WriteFile`/`BuildFile` into `SetWaveWriter` at a fixed `.wavetui-wave.json` path beside the project's `.wavetui.toml`. Added `Config.ForceOSC52` (additive TOML key `force_osc52`) — `clipboard.go`'s `ForceOSC52` field doc comment already named this config file as its intended source. **Real-tmux evidence** (disposable `wavetui-dispatch-verify` session, created/killed by the run itself, never the real session `0`): `TmuxDispatcher.Dispatch` against a live pane's `#{pane_id}` pasted `echo WAVETUI_DISPATCH_VERIFY_OK` and a separate Enter landed and executed — `tmux capture-pane` showed the command AND its printed output. **Bug found + fixed while capturing the ClipboardDispatcher fallback evidence**: `runPipeCommand` (`clipboard.go`, API batch) captured stderr via a Go-managed pipe (`bytes.Buffer`); every real Linux fallback candidate (xclip/xsel/wl-copy — all X11/Wayland-selection-ownership tools) forks into the background on success, and the daemonized grandchild inherits that pipe's still-open write end, hanging `cmd.Wait()` (and the whole synchronous `Dispatch` call) forever — reproduced live (>5 min hang against a real successful `wl-copy` write) before the fix. Fixed by routing stderr to `os.DevNull` (a real `*os.File`, dup2'd directly, no Go-side pipe/goroutine) instead of a buffer — verified live afterward: `wl-copy` write + `wl-paste` readback round-tripped a marker string in 0.02s, and the OSC52 path completed without error too.

## E2E Batch

- [ ] [4.1] `go test` for `internal/dispatch/tmux.go`: candidate scoring (same-window > same-session > other, tie prompts rather than picks), copy-mode refusal, mid-turn-streaming refusal, and the bracketed-paste call sequence via a mock tmux runner asserting exactly three separate calls (`load-buffer`, `paste-buffer -p`, `send-keys Enter`) — never a single `send-keys -l` call carrying the full multi-line prompt — plus `validateDispatchTarget` regex accept/reject cases [beads:if-p1ru]
  - depends on: 2.1, 2.2
- [ ] [4.2] `go test` for `internal/dispatch/clipboard.go`: OSC52 path, `exec.LookPath` fallback order under a faked `$PATH` containing only `xclip` (asserts it never attempts the literal `pbcopy` binary name on that fixture), and the surfaced (not swallowed) failure when nothing resolves [beads:if-jyjj]
  - depends on: 2.3
- [ ] [4.3] `go test` for `internal/wave`: `ConflictsFor` fixtures (overlapping paths naming both item IDs, zero-overlap case, and `FanOutScore`-descending ordering in the caller's selection view) [beads:if-vot6]
  - depends on: 1.4
- [ ] [4.4] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` in a real tmux session inside this repo, Start-dispatch one item and confirm the prompt lands in a live pane (paste pty output), multi-select two items sharing a touched file and confirm the conflict warning renders before finalize, detach/unset `$TMUX` and confirm Start falls back to the clipboard path instead of failing silently (paste evidence of the clipboard fallback firing) — paste the terminal/pty output as evidence [beads:if-dtfn]
  - depends on: 3.4, 4.1, 4.2, 4.3
