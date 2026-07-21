# Design: wavetui-dispatch

## Architecture

```
QueuePane (Start / select-mode)
      │  Item + rendered promptText
      ▼
Dispatcher interface  ──►  TmuxDispatcher   ──► cc-tmux `conductor list --json` (target discovery)
      │                         │            ──► cc-tmux `conductor dispatch --mode switch` (no paste)
      │                         └───────────► raw tmux load-buffer/paste-buffer -p + send-keys Enter
      │                                        (bracketed paste — the ONE primitive not reused, see below)
      └──► ClipboardDispatcher  ──► OSC52 write to /dev/tty, else pbcopy-equivalent via exec.LookPath
```

`HeadlessDispatcher` (out of scope, `wavetui-daemon`'s concern) is drawn dashed in intent only —
no code ships here — but the interface signature below is shaped so it slots in without a
breaking change.

## Dispatcher interface

```go
type Dispatcher interface {
    // Dispatch delivers promptText for item to whatever target this Dispatcher resolves.
    // Returns immediately on failure — see "no automatic retry" below. ctx carries cancellation
    // only; it is not a retry budget.
    Dispatch(ctx context.Context, item store.Item, promptText string) error
}
```

One method, deliberately narrow. `HeadlessDispatcher` (a future `claude -p` scheduler,
`wavetui-daemon`'s concern) can implement this exact signature later — a headless dispatch is
still "deliver promptText for item," it just resolves to a subprocess invocation instead of a
pane or clipboard. Nothing in this signature assumes a tmux pane exists, which is why it was
designed this way from day one per the operator's explicit instruction rather than being bolted
on after `TmuxDispatcher` shipped.

## Target resolution (before calling Dispatch)

A `Resolver` (not part of the `Dispatcher` interface — it picks WHICH dispatcher and WHICH pane,
`Dispatcher.Dispatch` just executes) runs this order, reusing `wavetui-sessions`'s existing link
when present rather than re-deriving it:

1. `item.Session != nil && item.Session.PaneID != ""` (from `wavetui-sessions`'s `TmuxSource`) —
   use that pane directly, skip scoring.
2. No linked session: query `cc-tmux conductor list --json`, score candidates
   same-window > same-session > other (matched against the item's project/branch via the same
   fields `conductor list` already returns: `project`, `branch`, `window`, `session`). A tie at
   the top score PROMPTS the operator (an `AskUserQuestion`-shaped inline pane list, never a
   silent pick).
3. Zero candidates, or no `$TMUX` session at all (checked via `cc-tmux conductor list --json`
   returning an empty array vs. a CLI error) — fall back to `ClipboardDispatcher`.

### Addendum (API batch, tasks.md [2.1]): how the four fields actually compose, and against whom

This section named all four `conductor list --json` fields (`project`, `branch`, `window`,
`session`) as the scoring inputs without specifying two things a real implementation needs: (a)
how project/branch and window/session combine into one ranking, and (b) what an "item's
project/branch" even means, since `store.Item` carries no project/branch field of its own (the
DB batch's `[1.3]` extraction — `TouchedFiles` only — is the complete, closed set of additive
fields for this proposal; adding a new `Item` field would be a DB-batch-scope change, not an
API-batch one).

**Resolved**: every item a single `wavetui` process surfaces belongs to the one repo that process
is running against — there is no per-item project/branch to track, because there is only one
project in scope for the whole run. "The item's project/branch" is therefore **wavetui's own**
project/branch, read once per resolution from `$TMUX_PANE` (the pane wavetui's own process
happens to be running in, if any) via the exact same primitives `conductor list` itself uses to
populate those fields on other rows: `@cc-project`/`@cc-branch` pane options for project/branch,
and `#{session_name}`/`#{window_index}` for session/window locality (verified live against this
repo's own tmux server that `#{window_index}` — a bare number — not `#{window_id}` — the `@N`
form — is what actually matches `conductor list --json`'s reported `window` value).

Composition: window/session locality is the PRIMARY tier (same-window scores higher than
same-session, which scores higher than other); project/branch affinity is a SECONDARY,
within-tier tie-break only (the gap between locality tiers exceeds the maximum affinity bonus, so
affinity can never promote a candidate across a locality boundary). This keeps the
"zero candidates" check in point 3 above strictly literal — reading solely from `conductor
list`'s raw array length/error, never a project-filtered subset — while still using project/
branch as real, documented ranking data per this section's original citation of the field. When
wavetui itself is not running inside any tmux pane (or one cc-tmux has not tagged), self
project/branch/session/window all resolve to `""` and every candidate falls through to the
"other" tier uniformly — a safe, if low-confidence, degrade: a single remaining candidate is
still an unambiguous pick, and 2+ equally-unscored candidates correctly tie and prompt (point 2's
own "never a silent pick" invariant), rather than being treated as a bug to special-case around.

### Addendum (API batch, tasks.md [2.2]): what "actively streaming" resolves to

§ Mid-turn safety below cites `<session actively streaming>` as a signal "the session state
wavetui-sessions' `TranscriptSource` already derives from transcript activity" — but
`store.SessionLink` (wavetui-sessions' own shipped shape) carries no such field; the closest
candidates (`LastActivity`, `Zombie`) are respectively too coarse (any activity, not specifically
"mid-turn right now") and already named as a separate, independent condition in that same bullet.

**Resolved**: "actively streaming" maps to cc-tmux's own `@cc-state == "active"` pane option —
exactly the busy/mid-turn signal cc-tmux's own `send-prompt` refusal already checks
(`conductor.py`'s `_send_prompt_refusal`) before typing into a pane, and exactly the primitive
this section's own "TmuxDispatcher primitive choice" subsection already names as the mechanism to
re-implement rather than shell out to `conductor dispatch --mode send-prompt` for. Combined with
the `Zombie == false` guard already stated in the bullet, the refusal condition is: linked session
exists, is not zombie-badged, AND its pane's `@cc-state` reads `active`. Fail-open (no refusal)
when `@cc-state` has no data for that pane — same convention as every other `@cc-state` read in
this codebase (wavetui-sessions' own zombie detection already treats tmux-state and
transcript-activity as independent, fail-open signals; this reuses that precedent rather than
inventing a new one).

## TmuxDispatcher primitive choice (Reader Gate citation + documented gap)

**Reused as-is:**
- `cc-tmux conductor list --json` for pane enumeration — exact shape documented in
  `apps/cc-tmux/skills/cc-dispatch/SKILL.md`: `{id, session, window, state, project, branch,
  task, wait_reason, timestamp}` per row. `TmuxDispatcher` never re-implements pane discovery.
- `cc-tmux conductor dispatch --mode switch --target <pane-id>` when the UI action is "just
  look at this pane" (no prompt) — no paste involved, cc-tmux's own implementation
  (`apps/cc-tmux/src/cc_tmux/conductor.py::_dispatch_switch`) is correct as-is and reused
  directly.

**NOT reused — one primitive, cited gap:** `apps/cc-tmux/src/cc_tmux/conductor.py` lines
378-401 (`_dispatch_send_prompt`) implements `send-prompt` as:

```python
sent = tmux._run_tmux(["send-keys", "-t", target, "-l", prompt])
entered = tmux._run_tmux(["send-keys", "-t", target, "Enter"])
```

`send-keys -l` sends the string as literal keystrokes, not through tmux's bracketed-paste mode.
For a short single-line prompt this is harmless. Wavetui's dispatched prompts are frequently
multi-line (task descriptions, spec excerpts) — a literal multi-line string passed to `-l` types
each embedded newline as a keystroke into whatever process owns the pane. If that process is a
shell, an early newline can execute a partial command; if it's an already-running Claude REPL
without its own paste buffering, the same partial-submission risk applies. This is exactly the
"quoting/injection hazard" a single non-bracketed `send-keys` call creates, and it is the reason
the safety invariant below is non-negotiable. Separately, `conductor.py` never reads
`#{pane_in_mode}` before pasting — a pane in copy-mode silently eats the paste.

Both gaps are narrow and specific to the paste-and-submit action only — not a reason to avoid
`conductor` entirely (list/switch above are still reused). `TmuxDispatcher`'s own paste path:

```go
func (d *TmuxDispatcher) sendPrompt(ctx context.Context, pane, prompt string) error {
    if inMode, _ := tmuxDisplay(pane, "#{pane_in_mode}"); inMode == "1" {
        return ErrPaneInCopyMode // caller surfaces a warning, never force-pastes through it
    }
    bufName := fmt.Sprintf("wavetui-dispatch-%d", time.Now().UnixNano())
    if err := tmuxRun("load-buffer", "-b", bufName, "-"); err != nil { // prompt fed via stdin
        return err
    }
    defer tmuxRun("delete-buffer", "-b", bufName)
    if err := tmuxRun("paste-buffer", "-b", bufName, "-p", "-t", pane); err != nil {
        return err
    }
    return tmuxRun("send-keys", "-t", pane, "Enter") // separate call — never appended to -l
}
```

`-p` on `paste-buffer` requests bracketed-paste wrapping when the target application supports
it — the same mechanism a terminal emulator's native paste uses, which is what the exploration's
"never a single send-keys with a literal multi-line string" invariant is protecting against.

`TmuxDispatcher` re-implements cc-tmux's own active-pane refusal (reading the `@cc-state` pane
option directly via the same `tmux show-options -p -v -t <pane> @cc-state` primitive
`wavetui-sessions`'s `TmuxSource` already reads, per that proposal's own citation of
`cc_tmux.tmux.get_pane_option()`) rather than shelling into `conductor dispatch --mode
send-prompt` for the refusal check — the refusal and the paste are one atomic decision in this
proposal's flow, and splitting "ask conductor if it's refused" from "then paste with our own
mechanism" would reopen a TOCTOU window wider than cc-tmux's own (already-documented, already
residual) one.

If cc-tmux ever ships bracketed paste in its own `send-prompt` mode, this fallback can collapse
to a pure shell-out in a follow-up proposal — noted in `proposal.md` § Risks, not blocking here.

## Mid-turn safety (non-negotiable per the exploration)

Before any dispatch (paste or clipboard), check `item.Session` (from `wavetui-sessions`):

- `item.Session != nil && item.Session.Zombie == false && <session actively streaming>` — the
  session state wavetui-sessions' `TranscriptSource` already derives from transcript activity —
  `TmuxDispatcher` refuses with `ErrSessionStreaming`; `QueuePane` renders this as "queued —
  session busy" rather than silently discarding the dispatch. No automatic queue-and-retry is
  implemented here (see "no automatic retry" below) — the operator re-triggers Start once the
  session goes idle.
- `item.Session == nil` — no linked session tracked, proceed to normal target resolution.

## ClipboardDispatcher

```go
type ClipboardDispatcher struct {
    ForceOSC52 bool // per-project config override for terminals that lie about capability
}
```

**OSC52 path**: write `\x1b]52;c;<base64(promptText)>\x07` directly to `/dev/tty` (works over
ssh and through tmux's `allow-passthrough` — bypasses the pane entirely, no target resolution
needed). Feature-detected via `$TERM_PROGRAM`/terminfo `Ms` capability presence; `ForceOSC52`
(loaded from the same per-project TOML config `wavetui-core`'s `internal/config/config.go`
already parses) overrides a false-negative detection for a terminal that supports OSC52 but
doesn't advertise it.

**pbcopy-equivalent fallback**: this repo's own shell already solves this — `home/dot_zsh/rc/
linux.zsh` lines 45-53 alias `pbcopy` to `xclip -selection clipboard`, else `xsel --clipboard
--input`, else `wl-copy`, in that order; `home/dot_zsh/rc/darwin.zsh` line 30 notes `pbcopy` is
native on macOS. **Gotcha this proposal must not repeat**: a Go binary invoked directly (not
through an interactive zsh) never sees that alias — `exec.Command("pbcopy", ...)` on Linux will
fail with "executable file not found" even on a machine where `pbcopy` works fine in a shell
prompt. `ClipboardDispatcher` therefore re-implements the same FALLBACK ORDER against the real
binaries via `exec.LookPath`: `pbcopy` (Darwin only) -> `xclip -selection clipboard` ->
`xsel --clipboard --input` -> `wl-copy` -> give up and surface the failure (never silent).

## Dispatch-boundary validation

```go
var idShapeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateDispatchTarget(id string) error {
    if !idShapeRe.MatchString(id) {
        return fmt.Errorf("dispatch target %q is not id-shaped, refusing to cross the dispatch boundary", id)
    }
    return nil
}
```

Applied to `item.ID` (bead IDs, proposal slugs — this codebase's canonical id grammar).
`promptText` itself is NEVER validated against this regex — it is free-form prose by design (that
is the entire point of a dispatch) and is delivered exclusively through the bracketed-paste
buffer / OSC52 payload, never through shell argument interpolation or string concatenation into a
command line. Bead titles and notes are user/agent-authored text and MUST NOT be treated as an
id — if a caller ever needs a title rendered into a dispatch, it goes through `promptText`, never
through `validateDispatchTarget`.

### Addendum (API batch, tasks.md [2.1]-[2.4]): pane IDs get their own validator, not a widened regex

The DB batch flagged that `idShapeRe` above — copied verbatim into `dispatch.go`'s `[1.2]`
scaffold — does not match a tmux pane ID like `%12` (`%` is absent from the charclass), even
though this section's original text implied `validateDispatchTarget` applies to
`item.Session.PaneID` too. `[2.4]`'s `Resolver` is the first task that actually calls a validator
on a real pane ID, so the inconsistency had to be resolved rather than carried forward.

**Resolved: a separate, narrowly-scoped pattern for pane IDs — not a widened `idShapeRe`.**

```go
var paneIDShapeRe = regexp.MustCompile(`^%[0-9]+$`)

func validateTmuxPaneID(id string) error {
    if !paneIDShapeRe.MatchString(id) {
        return fmt.Errorf("dispatch target %q is not pane-ID-shaped, refusing to cross the dispatch boundary", id)
    }
    return nil
}
```

Bead IDs/proposal slugs and tmux pane IDs are two different id-spaces with different, non-
overlapping legal grammars — a bead ID never contains `%`, and a tmux pane ID is never anything
but `%` followed by digits (tmux's own fixed format, confirmed against this repo's live tmux
server: `%12`, `%276`, etc., never anything else). Widening `idShapeRe` to admit `%` would loosen
the validator every `item.ID` call site relies on, for a character no bead or proposal slug will
ever legitimately need — the wrong direction for a boundary validator whose entire purpose is
excluding non-id-shaped (potentially attacker-controllable) text from a shell invocation. A
second pattern matching tmux's actual pane-ID grammar exactly protects the pane-ID call sites
(`TmuxDispatcher.Dispatch`/`Switch` in `tmux.go`) without touching `idShapeRe`'s existing
guarantee at all. `validateTmuxPaneID` is applied to every pane ID `TmuxDispatcher` resolves
(`item.Session.PaneID`, or a scored `conductor list --json` candidate's `id`) immediately before
it crosses into a `tmux`/`cc-tmux` invocation.

## No automatic retry

`Dispatcher.Dispatch` returns a plain `error` on any failure (tmux command failed, clipboard
write failed, session refused). Callers (`QueuePane`'s Start handler, the wave executor in
`internal/wave`) surface the error immediately as a UI-visible failure badge and stop — no
backoff loop, no re-attempt, no queue-and-retry-later. Retry storms against tmux or against a
downstream `claude` session are a real failure mode (a busy pane refused ten times in a tight
loop is worse than refused once); the smarter, rate-limit-aware retry policy that WOULD be safe
belongs to `wavetui-daemon` (out of scope here per `proposal.md` § Scope), which has visibility
into API rate limits this proposal's synchronous dispatch path does not.

## Store additive field (coordination note with `wavetui-core` and `wavetui-sessions`)

`wavetui-core`'s `Item` struct (already shipped, see its own `design.md` § Store data model,
already extended once by `wavetui-sessions`'s `Session *SessionLink` field) gains one more
additive field — no existing field renamed or removed:

```go
type Item struct {
    // ... existing wavetui-core + wavetui-sessions fields unchanged ...

    TouchedFiles []string // from OpenSpecSource's `- touches:` parse; empty (not nil-error) for beads
}
```

Populated by `OpenSpecSource` (already parsing `proposal.md` per `wavetui-core`'s existing
`[2.2]` task) reading the same `- touches:` line `wave-plan-build`'s `parse_proposal_paths`
already treats as the author-declared, authoritative contract for conflict detection
(`~/.claude/scripts/bin/wave-plan-build`, confirmed by inspection: it merges `- touches:` paths
with text-extracted candidates and documents that line as the override for "noisy text
extraction"). `wavetui`'s `internal/wave` package is a SEPARATE Go implementation of the same
idea — file-overlap conflict detection between candidate items in a wave — not a call-out to
the Python script; it exists because wavetui's wave assembly happens interactively inside a TUI
session, not as a one-shot CLI build step.

```go
func ConflictsFor(candidates []store.Item) map[string][]string {
    // path -> item IDs that both touch it, len(...) > 1 means a real conflict
}
```

`QueuePane`'s select-mode renders one warning row per conflicting path, naming both item IDs —
never silently drops either candidate from the wave.

## Open Question: wave-file format (flagged, not decided)

Neither `wavetui-core` nor `wavetui-sessions` commits to a wave-file format — both left their
data models forward-compat-shaped for it (`wavetui-core`'s `design.md` § Store data model says
so explicitly). This proposal is where the decision would land, and three proposals' downstream
work depends on which way it goes, so it is presented as a tradeoff with a recommendation, per
the operator's explicit instruction — **not** silently picked, and flagged again in
`proposal.md` § Risks as needing Leo's confirmation before implementation.

| Option | For | Against |
|--------|-----|---------|
| **JSON** (`wave.json` per finalized wave) | Matches this fleet's own machine-artifact convention for exactly this kind of file (`wave-plan.json`, `wave-state.json` under `scripts/state/` — see `documentation-writer`'s operational-docs canon "machine-artifact convention": a file consumed by tooling, not hand-edited, is JSON). Trivial to `encoding/json` round-trip in Go with no ambiguity. | Not human-diffable in a PR the way a markdown file is; not directly readable without a viewer. |
| **Markdown** (`wave.md`, `tasks.md`-shaped) | Human-reviewable in an editor/PR without tooling; consistent with `proposal.md`/`tasks.md` as this repo's primary authored-artifact medium. | This repo's own history has repeatedly hit markdown-checkbox-parser footguns for exactly this class of structured-data-in-prose file (wrapped `- touches:` lines dropping paths, a literal `<!-- beads:epic:TBD -->` placeholder breaking spec-sync, single-line-anchored regexes silently closing zero tasks) — a fresh Go parser for a NEW markdown dialect risks reproducing that footgun class rather than reusing a format the fleet has already hardened. |

**Recommendation: JSON, and NOT a bead.** A finalized wave file is consumed by `TmuxDispatcher`/
future `HeadlessDispatcher`, never hand-edited — the machine-artifact convention applies
directly, and Go's `encoding/json` gives an unambiguous round-trip with zero new parser
footguns. Against making it a bead: auditability is better served by each individual dispatched
item logging its own dispatch action (a `bd comment` or an `interactions.jsonl`-style entry —
this repo already treats `.beads/interactions.jsonl` as the audit-log medium per
`rules/BEADS.md`) than by minting a new beads issue-type for an ephemeral local planning
artifact; a wave file is closer to `.beads/last-touched`-class local state than to a tracked
work item. **This recommendation is not implemented until confirmed** — the `[user]` task in
`tasks.md`'s DB Batch blocks on it.
