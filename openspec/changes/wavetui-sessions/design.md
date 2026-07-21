# Design: wavetui-sessions

## Architecture

```
                fsnotify (per-file offset)         tmux show-options -p -v -t <pane> @cc-state
  ~/.claude/projects/<flattened>/*.jsonl ──►  TranscriptSource      TmuxSource  ◄── tmux panes
                                                      │                  │
                                              (fallback: ps -axo pid,ppid,comm
                                               walk, panes cc-tmux hasn't tagged)
                                                      │                  │
                                                 typed events (session, gauge, zombie,
                                                 error, token, rate-limit, pane-state)
                                                      ▼
                                          wavetui-core's event bus (reused, not rebuilt)
                                                      │
                                                      ▼
                                          wavetui-core's Store (single writer, additive fields)
                                                      │
                                             immutable Snapshot
                                                      ▼
                                          bubbletea Program.Send() (reused, not rebuilt)
                                                      │
                                                      ▼
                                wavetui-core's root model focus ring (existing, unmodified)
                                                  ├── QueuePane (wavetui-core, unchanged)
                                                  ├── DetailPane (wavetui-core, unchanged)
                                                  ├── SessionsPane (this proposal)
                                                  └── KPIBar (this proposal)
```

Both sources follow `wavetui-core`'s invariants unchanged: sources never touch Store state or
each other directly; the Store is the only writer; snapshots are immutable copy-on-write; renders
coalesce to ~10fps; every source failure renders as a badge, never a panic; no goroutine runs
without a `context.Context` for cancellation.

## Verified transcript fields (adversarial verification, not assumed)

The pre-loaded exploration context flagged transcript `usage` field names as UNVERIFIED and
required dumping a real transcript file before finalizing the parser. Done during authoring
against this session's own live transcript:

```bash
F=~/.claude/projects/-home-nyaptor-dev-personal-installfest/<session-id>.jsonl
python3 -c "
import json
with open('$F') as f:
    lines = f.readlines()
kinds = {}
for l in lines:
    d = json.loads(l)
    kinds[d.get('type')] = kinds.get(d.get('type'), 0) + 1
print(kinds)
"
```

Result (347-line real session file):

```
{'last-prompt': 20, 'custom-title': 20, 'agent-name': 20, 'mode': 20, 'permission-mode': 20,
 'attachment': 76, 'file-history-snapshot': 3, 'user': 62, 'assistant': 99, 'system': 6,
 'file-history-delta': 1}
```

`assistant` lines carry `message.usage`:

```json
{
  "input_tokens": 2,
  "cache_creation_input_tokens": 138737,
  "cache_read_input_tokens": 0,
  "output_tokens": 641,
  "server_tool_use": {"web_search_requests": 0, "web_fetch_requests": 0},
  "service_tier": "standard",
  "cache_creation": {"ephemeral_1h_input_tokens": 138737, "ephemeral_5m_input_tokens": 0},
  "inference_geo": "not_available",
  "iterations": [{"input_tokens": 2, "output_tokens": 641, "cache_read_input_tokens": 0,
                   "cache_creation_input_tokens": 138737, "cache_creation": {...}, "type": "message"}],
  "speed": "standard"
}
```

`user` lines carry `cwd`, `sessionId`, `isSidechain`, `parentUuid`, `gitBranch`, `timestamp`,
`entrypoint`, `userType`, `uuid`, `version`, `message`.

**What this confirms**: `input_tokens` + `cache_read_input_tokens` (the two fields the context
gauge sums against an approximate model-window size) exist under exactly those names — the
context-gauge Requirement below can cite them directly rather than a guessed name. `cwd` and
`sessionId` exist on `user` lines, confirming the cwd-trust-over-flattening and session-linkage
Requirements are implementable as specified.

**What this does NOT confirm**: the `type` vocabulary is far richer than "user/assistant only" —
ten distinct top-level `type` values were observed in one real session, several of which
(`last-prompt`, `custom-title`, `agent-name`, `mode`, `permission-mode`, `attachment`,
`file-history-snapshot`, `file-history-delta`) carry no `usage`/`cwd` field at all and are
irrelevant to `TranscriptSource`'s derived state. The tolerant-decode Requirement (unknown types
ignored, not treated as parse failures) is not a defensive nice-to-have here — it is the only
thing standing between this parser and crashing on the very first line-type variety a real
transcript throws at it. This finding also means a future Claude Code version adding an eleventh
`type` value is an expected, not exceptional, event.

## Alternatives / Related Work

**cc-tmux** (`openspec/specs/cc-tmux/spec.md`) already solves the "is this pane running Claude,
and what state is it in" problem via Claude Code hooks writing a `@cc-state` tmux pane option
(`waiting`/`idle`/`active`), auto-deleted when the pane closes. Cited requirements:

- "Claude pane state is tracked in tmux pane options" (`spec.md` line 6) — the push-based
  mechanism this proposal's `TmuxSource` reads from, rather than rebuilding via a process-tree
  walk or a `tmux list-panes` poll. Push-based is strictly better here: it doesn't race a
  mid-transition state (a poll can catch a pane between `active` and `idle`) and doesn't need to
  guess which pane's cwd matches a project root — cc-tmux's hooks already know exactly which pane
  is which session, because they fire from inside that session.
- "A dedicated tmux status row surfaces open/ready beads and proposals" (`spec.md` line 646) — an
  existing counts-only summary sourced from nx-agent's `roadmap-pulse` endpoint. NOT reused here,
  same reasoning `wavetui-core` already documented for its own sources: the endpoint has no
  per-item title/blocker/task-progress, and `wavetui`'s panes need full item detail. `TmuxSource`
  reads `@cc-state` directly instead.

**Read mechanism chosen**: `cc_tmux.tmux.get_pane_option(pane_id, option)`
(`apps/cc-tmux/src/cc_tmux/tmux.py:387`) wraps exactly `tmux show-options -p -v -t <pane_id>
<option>` with fail-open-to-empty-string semantics. Inspecting `apps/cc-tmux/src/cc_tmux/cli.py`'s
full subcommand list (`cmd_register`, `cmd_clear`, `cmd_cycle`, `cmd_back`, `cmd_switch`,
`cmd_focus`, `cmd_discover`, `cmd_self_test`, `cmd_doctor`, `cmd_inbox`, `cmd_inbox_clear`,
`cmd_picker_data`, `cmd_accounts_popup`, `cmd_accounts_popup_launch`) turned up no dedicated
per-pane state-query subcommand exported for external consumers — `cmd_picker_data` emits
`label\tpane_id` rows for tracked panes (a candidate-pane discovery signal) but not per-pane
`@cc-state` in a structured form. `TmuxSource` therefore shells the same raw `tmux show-options`
call `get_pane_option` wraps, directly — this is the plugin's own documented single source of
truth (a tmux pane option), not an internal implementation detail being reached around. If
`cc_tmux` later ships a structured query subcommand, `TmuxSource` should switch to it in a
follow-up proposal; not blocking here.

**Process-tree fallback** (`ps -axo pid,ppid,comm`, no `/proc` on macOS — matches this repo's own
cross-platform constraint) is kept ONLY for panes cc-tmux has not tagged: cc-tmux not installed,
or a pane outside its hook coverage. `TmuxSource` never assumes "the adjacent pane is the target" —
the fallback walks the actual process tree rooted at each candidate pane's shell PID looking for a
`claude` process, and produces no result (not a guess) when none is found.

**Considered and rejected**: polling `tmux list-panes` on an interval instead of reading
`@cc-state` per pane. Rejected for the race-condition reason above (a poll interval can observe a
pane mid-transition) and because cc-tmux's hooks already fire synchronously on the real state
transition — reading the resulting pane option is strictly more current than any poll interval
could be.

## Session linkage algorithm

1. **Exact match (cheap, preferred)**: scan `user`-type transcript lines' `message` text for a
   literal `/apply <id>` substring (id-shaped: matches the same bead/spec-slug grammar
   `wavetui-core`'s `BeadsSource`/`OpenSpecSource` already parse). First match wins; a session
   only links to one item at a time.
2. **Fallback (cwd + claim-timestamp proximity)**: when no exact match exists, compare the
   transcript's `cwd` field (trusted over directory-name flattening — flattening is lossy and
   collision-prone, e.g. two projects that only differ in a character stripped by flattening)
   against the claiming item's known repo path, and the transcript's earliest `timestamp` against
   the item's claim timestamp (`bd show <id> --json` claim metadata, already available from
   `wavetui-core`'s `BeadsSource`). A match requires both cwd equality and timestamp proximity
   within a configurable window (default 10 minutes) — cwd alone is too coarse (multiple sessions
   can share a repo) and timestamp alone is too coarse (multiple items can be claimed close
   together).
3. **Subagent sidechain linkage**: a transcript line with `isSidechain: true` links back to its
   parent session via `parentUuid` — a sidechain's own session ID is never matched independently;
   it always inherits its parent's item linkage.

## Zombie detection: two independent signals, never either alone

A claimed item is zombie-badged only when BOTH:
- its linked transcript has not grown (no new lines appended) in >= N minutes (config, default
  15), AND
- when `TmuxSource` has data for the pane this session started in, that pane's `@cc-state` is
  NOT `active` (fail-open when `TmuxSource` has no data for that pane — inactivity alone still
  badges, since not every session runs inside a cc-tmux-tracked pane).

Rationale from the pre-loaded exploration context: a slow single tool call can look identical to
true inactivity from the transcript side alone, and a process can outlive its pane (or a pane can
survive a killed shell) from the tmux side alone. Requiring cross-check where data exists is the
mitigation; it is never a hard requirement, since tmux coverage is not guaranteed.

## Store additive fields (coordination note with `wavetui-core`)

`wavetui-core`'s `Item` struct (already shipped, see its own `design.md` § Store data model) is
extended additively — no existing field renamed or removed:

```go
type Item struct {
    // ... existing wavetui-core fields unchanged ...

    Session *SessionLink // nil when no linked Claude Code session
}

type SessionLink struct {
    SessionID     string
    PaneID        string    // "" when TmuxSource has no match for this session
    ContextPct    float64   // 0-100, derived from cumulative input+cache-read tokens vs model window
    LastActivity  time.Time
    Zombie        bool
    ZombieSince   time.Time
    ErrorCount    int
    TokensByModel map[string]int64 // output tokens, keyed by model name
}
```

`Snapshot` gains one field: `RateLimitBanner *RateLimitSignal` (nil when no active signal),
independent of any single `Item`.

**API-batch addendum (tasks.md [2.2]/[2.3]/[2.4])**: implementing the Error-feed and Token-meter
Requirements against `spec.md`'s scenarios ("the error is added to that item's error feed with
its error class recorded") surfaced two fields the DB batch's initial extraction of this section
did not anticipate — both additive, no existing field renamed/removed:

```go
type SessionLink struct {
    // ... fields above unchanged ...

    // Errors is the classified tool_result error feed for this session, most
    // recent last. ErrorCount stays a cheap rolling total (unchanged shape);
    // Errors is the richer per-entry record spec.md's Error-feed Requirement
    // needs ("recorded... with its error class"). No pane in this proposal
    // renders the feed itself (SessionsPane/KPIBar only surface ErrorCount
    // and stale-claim minutes per their own Requirements) — this is
    // forward-compat scaffolding for a later proposal's error-feed UI, the
    // same precedent wavetui-core's design.md already set for Item.Deps
    // ("no source in this batch produces dependency edges... internal
    // scaffolding a later source publishes into").
    Errors []ErrorEntry

    // ExecutorLaneFlag is true when this session is a Task-dispatched
    // subagent (isSidechain: true — the transcript-native signal this
    // repo's own session-linkage algorithm already uses for subagent
    // detection) whose assistant lines used an opus-tier model. spec.md's
    // "opus running in an executor lane" scenario cites "the linked item's
    // agent-role metadata," which does not exist as a real Store/Item field
    // today (Item carries no agent-role column) — isSidechain is the
    // closest verified transcript-native proxy for "this is a dispatched
    // executor, not the orchestrating top-level session," consistent with
    // this fleet's own documented model-routing convention (opus reserved
    // for orchestration, see CLAUDE.md's Project Registry: "opus =
    // orchestrates agents"). Documented as a heuristic, not a verified
    // field, same honesty standard as the Rate-limit section below.
    ExecutorLaneFlag bool
}

// ErrorEntry is one classified tool_result error attributed to a linked
// session.
type ErrorEntry struct {
    Timestamp time.Time
    Class     string // "read_first_violation" | "edit_string_not_found" | "gate_blocked" | "unclassified"
    Agent     string // "" when not determinable from transcript agent metadata
    Message   string
}
```

**Wave rollup deferred**: spec.md's Token-meter Requirement also says output-token totals roll up
"to the wave... when wave metadata is available from the linked item." No `Item.Wave` field
exists anywhere in the Store (wave-file support is explicitly `wavetui-dispatch`'s concern, out of
scope here per this proposal's own Scope/OUT list) — the conditional is satisfied by construction
(never available, so the rollup never fires) rather than built out. If `wavetui-dispatch` later
adds wave metadata to `Item`, this rollup becomes a real follow-up, not a design change here.

**Rate-limit indicator, unverified positive example**: no session available during this batch's
authoring hit a real rate-limited response (`isApiErrorMessage: true` was confirmed as a real,
present top-level field on `assistant` lines in this session's own live transcripts, but every
observed value was `false` — no live positive example exists to confirm the exact accompanying
message text). `TranscriptSource` treats `isApiErrorMessage: true` combined with a
rate-limit-shaped keyword match in the assistant's rendered text (`rate limit`, `429`,
`overloaded`, `rate_limit_error`) as the signal — a conservative superset of Anthropic's known
error-type strings, not a field-name-verified match. Same standing mitigation as the transcript
schema drift risk above: a future Claude Code release changing this shape degrades to "signal
never fires," never a crash, per the tolerant-decode invariant.

## Rate-limit backpressure: emit only, never consume

This proposal emits a `RateLimitSignal` event onto the bus when `TranscriptSource` observes a
rate-limit indicator in the transcript stream (matches the same class of signal Claude Code's own
UI surfaces on a 429/rate-limited response). `KPIBar` renders the resulting banner. Building the
headless-dispatch queue that would PAUSE on this signal is explicitly out of scope — a sibling
proposal's concern, per `## Scope` in `proposal.md`.
