# Plan 002: Close the permission-approval hole, stamp @cc-timestamp only on real transitions, and cover /clear + compact in the SessionStart matcher

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index. If `plans/README.md` does not exist, note that in your
> report instead of creating it.
>
> **Drift check (run first)**:
> `git -C /home/nyaptor/dev/personal/installfest diff --stat 60a1441..HEAD -- apps/cc-tmux/hooks/hooks.json apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition. Other plans (001, 003, 004) may be
> executing concurrently in this same working tree against OTHER files — that
> is expected and is not drift; only changes to the three in-scope files above
> matter.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: MED (hook wiring change; runtime effect gated on plugin snapshot propagation — see Operator gate)
- **Depends on**: none (but the final operator gate should be coordinated with plan 001 — one plugin version bump can cover both plans; see Step 8)
- **Category**: bug
- **Planned at**: commit `60a1441`, 2026-07-11

## Repo facts (inline — the executor has zero prior context)

- Repo: personal dotfiles ("installfest"), `/home/nyaptor/dev/personal/installfest`, chezmoi-managed, project code `if`.
- Target app: `apps/cc-tmux` — a tmux + Claude Code plugin. **Python 3.10+ STDLIB ONLY** (`apps/cc-tmux/pyproject.toml` constraint) — no new dependencies, no external test runner.
- Quality gates:
  - `apps/cc-tmux/bin/cc-tmux self-test` — pure-function suite, exits non-zero on failure. Baseline at 60a1441: `cc-tmux self-test: 42/42 passed`, exit 0.
  - `apps/cc-tmux/bin/cc-tmux doctor` — env diagnostics, ALWAYS exits 0.
  - New/changed pure logic MUST get self-test coverage in `apps/cc-tmux/src/cc_tmux/testing.py`.
- Design invariants (from the `apps/cc-tmux/src/cc_tmux/tmux.py` module header — every fix must honor them):
  1. tmux pane options are the ONLY tracked-state store — no new state files for pane state.
  2. Views derive, never store.
  3. Real-transition guard — `set_pane_state` returns whether `@cc-state` actually changed; reactive behavior (notify/focus) fires only on a real transition.
  4. Hot path: `active` is the most frequent register and skips git identity resolution — it must stay cheap.
  5. Fail open — every hook/status entrypoint exits 0, never blocks tmux or Claude.
- Plugin dual-install (critical for the operator gate): the tmux side runs repo HEAD via a `~/.tmux/plugins/cc-tmux` symlink, but the **Claude hook side runs a SNAPSHOT** at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`. Changes to `hooks.json` (and to any code invoked by Claude hooks) are **DEAD at runtime until the plugin version is bumped and the plugin updated**. The plugin was re-enabled by the operator on 2026-07-11 (it had been silently disabled by the 0.1.1 update).
- tmux 3.6a, `status-interval 1`.
- Commit pattern: `type(scope): subject`, ad-hoc lane, targeted `git add <paths>` only (never `git add .`).

## Why this matters

cc-tmux tracks each Claude pane's state (`waiting` / `idle` / `active`) via Claude Code hooks so tmux surfaces (tab icons, cycle ring, notification inbox, status counts) can route the user's attention. Three defects in the transition matrix break that routing:

1. **STC-1 (high)**: When a permission prompt fires, the pane is set `waiting` — but no hook ever returns it to `active` after the user APPROVES, because `PostToolUse`/`PostToolUseFailure` only match `AskUserQuestion|ExitPlanMode`. A long Bash/Edit run after approval renders as `waiting` (wrong icon, wrongly top of the cycle ring and inbox pending set) until Stop or the next prompt. The inbox/cycle surfaces exist to say "this pane needs you" — a false `waiting` erodes exactly that.
2. **STC-3 (medium)**: `set_pane_state` stamps `@cc-timestamp` on EVERY register, including re-asserts (idle -> idle). The inbox dismiss contract ("a fresh TRANSITION reappears" — `cli.py:370`) and the priority ordering ("most-recent state change") both read the timestamp as transition time, so a re-fired `idle_prompt` Notification or a resume `SessionStart` resurrects dismissed inbox rows and reshuffles ordering with no real state change.
3. **STC-4 (medium)**: `/clear` fires `SessionEnd` (which wipes all `@cc-*` options off the pane) then `SessionStart` with `source=clear` — which does NOT match the `startup|resume` matcher. The still-live Claude pane becomes fully untracked (no tab icon, absent from cycle/inbox/counts) until the next `UserPromptSubmit`.

Fixes 1 and 2 are coupled: the PostToolUse catch-all makes `register --state active` fire on every tool completion, and without the transition-only timestamp gate each of those re-asserts would restamp `@cc-timestamp` (churning inbox durations and ordering every tool call). This plan ships both together, timestamp gate first.

## Current state (verified at 60a1441)

Files and roles:

- `apps/cc-tmux/hooks/hooks.json` — the entire Claude-hook transition matrix (148 lines). Owns STC-1 and STC-4.
- `apps/cc-tmux/src/cc_tmux/tmux.py` — pane-option state store; `set_pane_state` at lines 470–520. Owns STC-3.
- `apps/cc-tmux/src/cc_tmux/testing.py` — stdlib-only self-test suite; `_TmuxMock` (lines 199–226) mocks tmux for `set_pane_state` tests; `_TESTS` registry at lines 889–932.
- `apps/cc-tmux/src/cc_tmux/cli.py` — READ ONLY for this plan. `cmd_register` (line 57) is the hook entrypoint; `cmd_inbox` (line 357) documents the dismiss contract; `cmd_clear` (line 160) is the SessionEnd wipe.

### hooks.json — the gaps

`hooks.json:3-9` (SessionStart matcher excludes `clear` and `compact`):

```json
    "SessionStart": [
      {
        "matcher": "startup|resume",
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/bin/cc-tmux register --state idle",
```

`hooks.json:58-81` (PostToolUse / PostToolUseFailure match only the two dialog tools — approving a permission prompt for Bash/Edit/etc. never flips the pane back):

```json
    "PostToolUse": [
      {
        "matcher": "AskUserQuestion|ExitPlanMode",
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/bin/cc-tmux register --state active",
            "timeout": 10
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "matcher": "AskUserQuestion|ExitPlanMode",
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/bin/cc-tmux register --state active",
            "timeout": 10
          }
        ]
      }
    ],
```

`hooks.json:82-92` sets `waiting --reason permission` on `Notification` matcher `permission_prompt`; `hooks.json:136-146` wires `SessionEnd` to `cc-tmux clear` (full `@cc-*` wipe via `tmux.clear_pane_state`, tmux.py:568). Matcherless entries already exist in this file (`UserPromptSubmit` lines 20–34, `Stop` 114–124, `StopFailure` 125–135, `SessionEnd` 136–146) — omitting `"matcher"` means "match everything" and is the established style here.

### tmux.py — unconditional timestamp stamp

`tmux.py:497-501` (inside `set_pane_state`):

```python
    old_state = get_pane_option(pane_id, OPT_STATE)
    changed = is_real_transition(old_state, state)

    _set_opt(pane_id, OPT_STATE, state)
    _set_opt(pane_id, OPT_TIMESTAMP, str(timestamp if timestamp is not None else time.time()))
```

`changed` is computed but only returned — it never gates the timestamp write. The module header documents the option as last-SET, not last-CHANGED (`tmux.py:11`):

```
    @cc-timestamp    epoch seconds when the state was last set
```

### Consumers that read the timestamp as TRANSITION time (why the gate is safe and correct)

`cli.py:369-371` (`cmd_inbox` dismiss contract):

```python
        # active is never hidden; waiting/idle hide only if dismissed (older than
        # the cleared-at stamp). A fresh transition (newer timestamp) reappears.
        if pane.state == "active" or pane.timestamp > cleared_at:
```

`priority.py:10-13` (module docstring): "Within a group, the most-recently *visited* pane surfaces first (`@cc-visited` desc), falling back to the most-recent state change (`timestamp` desc)". `render.inbox_rows` renders time-in-state from this timestamp. No consumer wants last-register semantics. The only caller of `set_pane_state` passing state from hooks is `cmd_register` (cli.py:64); `cmd_discover` (cli.py:233) sets `idle` only on untracked panes (untracked -> idle is a real transition, so it still stamps). No production caller passes the `timestamp=` kwarg (grep-verified: only testing.py would).

### Settled context — do NOT re-litigate

- No new background process / revalidation spawning in cc-tmux (settled by the 2026-07-11-cc-tmux-session-usage-bars design).
- Empty usage segment on zero-isActive is documented Invariant-5 fail-open.
- Representative-pane choice (waiting>idle>active) for the session bar is by design.
- waiting/idle-only git resolution is documented invariant 4 — do not add git resolution to the `active` path.
- Plan 001 owns `doctor`/`reconcile` changes; plan 004 owns the nexus repo (`~/dev/personal/nexus`). Do not touch either.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Self-test | `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` | `cc-tmux self-test: N/N passed`, exit 0 (42/42 at baseline; 43/43 after Step 3) |
| Doctor | `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux doctor` | prints checklist, exit 0 |
| JSON validity | `python3 -m json.tool /home/nyaptor/dev/personal/installfest/apps/cc-tmux/hooks/hooks.json > /dev/null && echo OK` | `OK` |
| Hot-path timing | `time /home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux register --state active` | exit 0, wall time well under 300ms |

No install step exists — `bin/cc-tmux` is a PYTHONPATH shim over `src/`; it only needs `python3`.

## Scope

**In scope** (the ONLY files you may modify):

- `apps/cc-tmux/hooks/hooks.json`
- `apps/cc-tmux/src/cc_tmux/tmux.py` (only `set_pane_state` + the two docstring lines named in Step 1)
- `apps/cc-tmux/src/cc_tmux/testing.py` (add one test + one `_TESTS` row)

**Out of scope** (do NOT touch, even though they look related):

- `apps/cc-tmux/src/cc_tmux/cli.py` — `doctor`/`reconcile` belong to plan 001; `cmd_register`/`cmd_inbox` need no change for these fixes.
- `apps/cc-tmux/src/cc_tmux/render.py`, `priority.py`, `notify.py` — consumers of the timestamp; the fix is producer-side.
- `apps/cc-tmux/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` version fields — the bump is an OPERATOR decision coordinated across plans 001/002 (Step 8). Do not bump unilaterally.
- `~/dev/personal/nexus` (any file) — plan 004's repo, separate remote and commit rules.
- `apps/cc-tmux/skills/`, `home/dot_config/tmux/tmux.conf` — no matrix documentation lives there (grep-verified); nothing to sync.

## Git workflow

- Work on the current branch (`main`) — ad-hoc lane, no new branch.
- ONE commit for this plan, targeted adds only:
  `git add apps/cc-tmux/hooks/hooks.json apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py`
- Message style (matches repo log, e.g. `fix(cc-tmux): restore tab click-to-switch via range=window, fix spacing`):
  `fix(cc-tmux): close permission-approval hole, transition-only timestamps, SessionStart clear/compact`
- Write the commit message to a temp file and use `git commit -F <file>` (repo convention).
- Do NOT push and do NOT open a PR — the operator batches pushes across the concurrent plans.

## Steps

Order matters: the timestamp gate (Steps 1–3) must land in the same commit as the hooks.json catch-all (Step 4) — the catch-all multiplies `active` re-assert fires, and only the gate keeps those re-asserts from churning `@cc-timestamp`.

### Step 1: Gate the `@cc-timestamp` write on a real transition (STC-3)

In `apps/cc-tmux/src/cc_tmux/tmux.py`, replace lines 500–501:

```python
    _set_opt(pane_id, OPT_STATE, state)
    _set_opt(pane_id, OPT_TIMESTAMP, str(timestamp if timestamp is not None else time.time()))
```

with:

```python
    _set_opt(pane_id, OPT_STATE, state)
    # @cc-timestamp records the last REAL transition, not the last register:
    # cmd_inbox's dismiss contract ("a fresh transition reappears") and the
    # priority ordering ("most-recent state change") both read it as
    # transition time, so a re-asserted state must not restamp. An explicit
    # ``timestamp`` kwarg is a deliberate caller override and always writes.
    if changed or timestamp is not None:
        _set_opt(pane_id, OPT_TIMESTAMP, str(timestamp if timestamp is not None else time.time()))
```

Then update the two doc lines to match the new semantics:

1. `tmux.py:11` — change
   `@cc-timestamp    epoch seconds when the state was last set`
   to
   `@cc-timestamp    epoch seconds of the last REAL state transition (re-asserts do not restamp)`
2. In the `set_pane_state` docstring (lines 480–489), after the "Returns ``True`` only on a real transition (invariant 3)..." paragraph, add one line:
   `` ``@cc-timestamp`` is stamped only on a real transition (or when ``timestamp`` is passed explicitly) — re-asserted states keep their existing stamp. ``

Do not change anything else in the function — the `@cc-state` write, wait-reason handling, and git-identity guard stay exactly as they are.

**Verify**: `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` → `cc-tmux self-test: 42/42 passed`, exit 0. (The existing `_test_set_pane_state_writes_state_and_timestamp` exercises a REAL transition — active -> waiting — so it must still pass. If it fails, your gate is wrong; the timestamp must still be written when `changed` is True.)

### Step 2: Add self-test coverage for the re-assert gate

In `apps/cc-tmux/src/cc_tmux/testing.py`, add this test directly after `_test_set_pane_state_writes_state_and_timestamp` (which ends at line 281). Model: same `_TmuxMock` pattern as that test (testing.py:269–281).

```python
def _test_set_pane_state_reassert_skips_timestamp() -> None:
    # Re-asserted state (idle -> idle): @cc-state may be rewritten but
    # @cc-timestamp must NOT be restamped — the inbox dismiss contract
    # (cli.cmd_inbox: "a fresh transition reappears") and priority ordering
    # read the timestamp as TRANSITION time.
    with _TmuxMock("idle") as mock:
        tmux.set_pane_state("%1", "idle", git_resolver=lambda _p: None)
    wrote_ts = any(c[0] == "set-option" and tmux.OPT_TIMESTAMP in c for c in mock.calls)
    _check(not wrote_ts, "re-assert must NOT restamp @cc-timestamp")

    # An explicit timestamp kwarg is a caller override: writes even on re-assert.
    with _TmuxMock("idle") as mock2:
        tmux.set_pane_state("%1", "idle", timestamp=123.0, git_resolver=lambda _p: None)
    wrote_override = any(
        c[0] == "set-option" and tmux.OPT_TIMESTAMP in c and "123.0" in c for c in mock2.calls
    )
    _check(wrote_override, "explicit timestamp kwarg must write even on re-assert")
```

Register it in the `_TESTS` list (lines 889–932) directly after the `("tmux.set_pane_state_writes", _test_set_pane_state_writes_state_and_timestamp),` row:

```python
    ("tmux.set_pane_state_reassert_ts", _test_set_pane_state_reassert_skips_timestamp),
```

**Verify**: `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` → `cc-tmux self-test: 43/43 passed`, exit 0.

### Step 3: Prove the new test actually bites (mutation check)

Temporarily revert the Step 1 gate (make the timestamp write unconditional again), run self-test, and confirm `tmux.set_pane_state_reassert_ts` FAILS (output contains `FAIL tmux.set_pane_state_reassert_ts`). Then restore the gate.

**Verify**: after restoring, `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` → `43/43 passed`, exit 0, and `git diff apps/cc-tmux/src/cc_tmux/tmux.py` shows only the Step 1 changes.

### Step 4: Widen PostToolUse / PostToolUseFailure to catch-all (STC-1)

In `apps/cc-tmux/hooks/hooks.json`, delete the `"matcher": "AskUserQuestion|ExitPlanMode",` line from BOTH the `PostToolUse` entry (line 60) and the `PostToolUseFailure` entry (line 72). Nothing else in either entry changes. Omitting `matcher` means "fire on every tool" — the established style of this file's `UserPromptSubmit`/`Stop`/`SessionEnd` entries.

Why replace rather than add a second catch-all entry: the existing entries run the identical command (`register --state active`); a separate catch-all alongside them would double-fire on AskUserQuestion/ExitPlanMode. Removing the matcher IS the catch-all, with no duplicate fires.

Effect: any tool completion (success or failure) now re-registers `active`. Coupled with Step 1, a completion during an already-`active` turn is a pure re-assert — no timestamp churn, no reactive behavior (invariant 3 already gates `notify.react` on `changed`). The user-visible fix: after approving a permission prompt, the pane returns to `active` at the approved tool's completion (and stays correct through every subsequent tool), instead of rendering `waiting` until Stop or the next prompt. Note the honest limitation: no Claude hook fires at the approval moment itself, so during a single long approved tool run the pane still reads `waiting` until that tool completes — this plan closes the "stuck until Stop/next prompt" hole, which is the confirmed defect (STC-1).

**Verify**:
- `python3 -m json.tool /home/nyaptor/dev/personal/installfest/apps/cc-tmux/hooks/hooks.json > /dev/null && echo OK` → `OK`
- `grep -c 'AskUserQuestion|ExitPlanMode' /home/nyaptor/dev/personal/installfest/apps/cc-tmux/hooks/hooks.json` → `0` (the PreToolUse entries use the single-tool matchers `"AskUserQuestion"` and `"ExitPlanMode"` separately — those MUST remain untouched: `grep -c '"matcher": "AskUserQuestion"' ...hooks.json` → `1`)

### Step 5: Extend the SessionStart matcher to `clear` and `compact` (STC-4)

In `apps/cc-tmux/hooks/hooks.json` line 5, change:

```json
        "matcher": "startup|resume",
```

to:

```json
        "matcher": "startup|resume|clear|compact",
```

Rationale: `/clear` fires `SessionEnd` (wired at hooks.json:136-146 to `cc-tmux clear`, which unsets every `@cc-*` option — `tmux.clear_pane_state`, tmux.py:568) and then `SessionStart` with `source=clear`. With the old matcher, the live pane stays untracked until the next `UserPromptSubmit`. `clear` and `compact` are both documented Claude Code SessionStart matcher values; `compact` fires no SessionEnd (no wipe) so including it is a harmless refresh — and if a given CC version never emits it, the matcher term simply never matches. The second hook in that entry (conductor context, guarded by `$CC_TMUX_CONDUCTOR`) firing on clear/compact is correct: a fresh context needs its instructions re-emitted.

**Verify**: `grep -c '"matcher": "startup|resume|clear|compact"' /home/nyaptor/dev/personal/installfest/apps/cc-tmux/hooks/hooks.json` → `1`

### Step 6: Hook-fire volume cost check (assessment required by this plan's charter)

The catch-all makes `register --state active` fire once per tool call. Confirm the hot path stays cheap:

```
time /home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux register --state active
```

**Verify**: exit 0 and total wall time well under 300ms (expected ~60–150ms; python startup dominates). Record the measured time in your final report. Context for the report: the `active` path does one pane-option read + two writes, skips git identity (invariant 4), and appends one line to a size-capped trace log (`_trace_register`, cli.py:122 — read-trim-rewrite, fail-open). At typical volumes (hundreds to low-thousands of tool calls/day) this is negligible; the hook is async with a 10s timeout and never blocks Claude (invariant 5). Note if run inside a tmux pane: the command will tag YOUR current pane `active` — harmless, self-corrects on the next hook fire; outside tmux it fail-opens to a no-op.

### Step 7: Full gates + commit

```
/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test
/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux doctor
git -C /home/nyaptor/dev/personal/installfest status --short
```

**Verify**: self-test `43/43 passed` exit 0; doctor exits 0; `status --short` shows ONLY the three in-scope files modified (plus files owned by other concurrently-executing plans — do not stage those). Then:

```
git -C /home/nyaptor/dev/personal/installfest add apps/cc-tmux/hooks/hooks.json apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py
```

Write the commit message to `/tmp/claude-1000/-home-nyaptor-dev-personal-installfest/*/scratchpad/commit-msg-002.txt` (or any temp file) and `git commit -F` it. Do NOT push.

### Step 8: OPERATOR GATE — snapshot propagation + runtime evidence (report, do not self-execute)

**STOP here and report back.** The committed `hooks.json` is DEAD at runtime: the Claude hook side executes the snapshot at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`, not repo HEAD. The remaining work is the operator's:

1. Bump the plugin version (`apps/cc-tmux/.claude-plugin/plugin.json` `"version": "0.1.1"` and the matching `"version": "0.1.1"` at `.claude-plugin/marketplace.json:14`) — **coordinated with plan 001** so one bump covers both plans' hook-side changes.
2. Update the plugin in Claude Code so a new snapshot directory appears under `~/.claude/plugins/cache/cc-tmux/cc-tmux/`, then confirm the plugin is still ENABLED (the 0.1.1 update silently disabled it once already).
3. Runtime evidence (required — config presence is not liveness). In a tmux pane running Claude under the new snapshot:
   - **STC-1**: trigger a real permission prompt (ask Claude to run a non-allowlisted command), approve it, let the tool finish, then `tmux show-options -p -t <pane> @cc-state` → `@cc-state active` (previously stayed `waiting` until Stop).
   - **STC-4**: run `/clear` in that Claude session, then `tmux show-options -p -t <pane> @cc-state` → `@cc-state idle` (previously: no `@cc-*` options at all until the next prompt).
   - **STC-3**: read `tmux show-options -p -t <pane> @cc-timestamp`, let two consecutive tool calls complete while the pane is already `active`, read it again → value unchanged (previously restamped on every register).

Include the three transcripted command outputs in the report.

## Test plan

- New test: `_test_set_pane_state_reassert_skips_timestamp` in `apps/cc-tmux/src/cc_tmux/testing.py`, covering (a) re-assert writes no `@cc-timestamp`, (b) explicit `timestamp=` kwarg still writes on re-assert. Real-transition stamping is already covered by the existing `_test_set_pane_state_writes_state_and_timestamp` (testing.py:269) — do not duplicate it.
- Structural pattern to mimic: `_test_set_pane_state_writes_state_and_timestamp` (testing.py:269–281) — `_TmuxMock` context manager, inspect `mock.calls`.
- The hooks.json changes are declarative JSON with no pure-function surface — their tests are the JSON-validity check (Step 4) plus the operator's runtime evidence (Step 8). Do not invent a JSON-schema test harness.
- Verification: `bin/cc-tmux self-test` → `43/43 passed`, exit 0, including the mutation check in Step 3 proving the new test fails against ungated code.

## Done criteria

Machine-checkable. ALL must hold (executor-side; Step 8 items are operator-side):

- [ ] `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` exits 0 printing `43/43 passed`, and the output of `self-test --verbose` (if supported) or the `_TESTS` list contains `tmux.set_pane_state_reassert_ts`
- [ ] `python3 -m json.tool apps/cc-tmux/hooks/hooks.json > /dev/null` exits 0
- [ ] `grep -c 'AskUserQuestion|ExitPlanMode' apps/cc-tmux/hooks/hooks.json` → `0`
- [ ] `grep -c '"matcher": "AskUserQuestion"' apps/cc-tmux/hooks/hooks.json` → `1` and `grep -c '"matcher": "ExitPlanMode"' apps/cc-tmux/hooks/hooks.json` → `1` (PreToolUse untouched)
- [ ] `grep -c '"matcher": "startup|resume|clear|compact"' apps/cc-tmux/hooks/hooks.json` → `1`
- [ ] `grep -c 'if changed or timestamp is not None:' apps/cc-tmux/src/cc_tmux/tmux.py` → `1`
- [ ] `bin/cc-tmux doctor` exits 0
- [ ] `git status --short` shows no modifications outside the three in-scope files (ignoring other plans' files, which must NOT be staged in this plan's commit)
- [ ] One commit exists with only the three in-scope paths; NOT pushed
- [ ] `plans/README.md` status row updated (or its absence noted in the report)

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since 60a1441, or the "Current state" excerpts do not match what you read (e.g. `set_pane_state` no longer has the unconditional stamp at tmux.py:500-501, or hooks.json's PostToolUse matcher is already gone — another session may have landed part of this).
- Baseline self-test is not `42/42 passed` BEFORE your first edit (pre-existing breakage is not yours to fix).
- The Step 3 mutation check does NOT fail with the gate reverted (the test is vacuous — something is wrong with the mock plumbing).
- Any fix appears to require editing `cli.py`, `render.py`, `priority.py`, or `notify.py` — that is scope creep into plan 001's territory or consumer-side churn this plan explicitly avoids.
- You are tempted to bump the plugin version yourself — that is the operator's call, coordinated with plan 001 (Step 8).
- A step's verification fails twice after a reasonable fix attempt.

## Maintenance notes

- **Timestamp semantics changed**: `@cc-timestamp` now means "last real transition", not "last register". Any future consumer wanting last-ACTIVITY time must use a different signal (e.g. `@cc-visited`, or a new option — which would need its own design review under invariant 1), not un-gate this write.
- **Reviewer focus**: (a) the gate condition `if changed or timestamp is not None` — the `timestamp is not None` arm keeps the test-injection seam alive; (b) hooks.json diff should be exactly two deleted `matcher` lines and one edited matcher string — any other churn is a red flag; (c) confirm PreToolUse entries are untouched.
- **Interaction with the catch-all**: if a future hook event or tool class must NOT flip the pane to `active` on completion, reintroduce a matcher on PostToolUse then — today every tool completion legitimately means "Claude is running again".
- **Fire-volume watch**: `<state-dir>/cc-tmux-register-trace.log` (size-capped) records every register call post-ship; if volume ever becomes a concern, that log is the measurement, not guesswork.
- **Deferred out of this plan**: a state flip at the approval MOMENT (no Claude hook exists for it — would need upstream CC support); denial-path behavior (a denied tool may fire no hook until the next tool/Stop — acceptable residual); doctor/reconcile self-heal for untracked live panes (plan 001).
