# Plan 004: Carry git dirty/ahead through session-context.json (nx write + cc-tmux read) + fix stale @cc-branch unset bug

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` (create the row if the index exists; if `plans/README.md`
> does not exist yet, skip this — the advisor owns creating it) — unless a
> reviewer dispatched you and told you they maintain the index.
>
> **Drift check (run first)**:
> `git -C /home/nyaptor/dev/personal/installfest diff --stat 60a1441..HEAD -- apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/render.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py`
> Files WILL have changed if plan 003 already executed (expected — this plan
> depends on 003). Compare the "Current state" excerpts below against the live
> code before proceeding; the pre-003 excerpts are labeled as such. On any
> mismatch NOT explained by plan 003's ts-cutoff change, treat it as a STOP
> condition. Also check the nexus repo:
> `git -C /home/nyaptor/dev/personal/nexus log --oneline -1` — this plan was
> written against nexus HEAD `0a3a1fb3`.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (cross-repo: two sides must version-tolerate each other; hook-path code needs a plugin version bump to reach the Claude side)
- **Depends on**: plans/003-*.md (the session-context `ts` staleness cutoff in `_read_session_context` — plan 003 and this plan edit the SAME function; 003 MUST land first)
- **Category**: bug + dx
- **Planned at**: installfest commit `60a1441`, nexus commit `0a3a1fb3`, 2026-07-11
- **Cross-plan ownership**: This plan owns the session-context git-field pipeline (nexus write side + cc-tmux read side), the `set_pane_git_identity` stale-unset fix, and the two tmux.py doc-drift fixes. It does NOT own the `ts` cutoff itself (plan 003) or any usage/account-segment behavior (settled elsewhere).

## Why this matters

cc-tmux's session bar (row 2) shows a pane's git branch from the `@cc-branch`
tmux pane option, which is only refreshed by Claude Code hook events — when
hooks stall or the plugin is disabled (which happened: the 0.1.1 plugin update
silently disabled the plugin until the operator re-enabled it 2026-07-11), the
branch display freezes at whatever was last written. Meanwhile
nexus-statusline already computes `{branch, dirty, ahead}` via git on EVERY
statusline render (~1s cadence) at the exact call site where it writes
`session-context.<pane>.json` for cc-tmux — and then discards the git data
from that payload. Carrying it through delivers (a) dirty `*` / ahead `^N`
indicators the bar has never had, and (b) hook-independent branch freshness,
at zero added git-subprocess cost. Separately, `set_pane_git_identity` has a
stale-value bug even with healthy hooks: it only writes truthy values, so
when the branch resolves to `""` (detached HEAD, mid-rebase, cwd left the
repo) the PREVIOUS branch keeps rendering as current. Two small doc-drift
fixes in `tmux.py` ride along (verified false claims that would mislead the
next maintainer).

Verified audit findings this implements: GIT-FRESH-3 (CONFIRMED, high),
GIT-FRESH-2 (CONFIRMED, medium), GIT-FRESH-4 (CONFIRMED, low).

## Current state

### Repos and constraints (inline facts — executor has zero context)

- **installfest** (`/home/nyaptor/dev/personal/installfest`, chezmoi dotfiles
  repo, code `if`): target is `apps/cc-tmux` — Python 3.10+ **stdlib-only**
  (pyproject constraint; no new dependencies). Quality gates:
  `apps/cc-tmux/bin/cc-tmux self-test` (pure-function suite, non-zero exit on
  failure; **42/42 passing at 60a1441**, plan 003 may have added more) and
  `apps/cc-tmux/bin/cc-tmux doctor` (env diagnostics, always exit 0). New pure
  functions MUST get self-test coverage in `src/cc_tmux/testing.py`.
- **Design invariants** (from the `tmux.py` module header — every fix must
  honor them): (1) tmux pane options are the ONLY tracked-state store — no
  new state files for pane state (short-TTL caches of EXTERNAL data, like
  `session-context.<pane>.json`, are acceptable: they cache external HTTP/
  statusline data, not pane state — roadmap-pulse/session-context precedent);
  (2) views derive, never store; (3) real-transition guard; (4) hot path
  `active` skips git identity; (5) **fail open** — every hook/status
  entrypoint exits 0, never blocks tmux or Claude.
- **Plugin dual-install gate**: the tmux side runs repo HEAD via the
  `~/.tmux/plugins/cc-tmux` symlink (session-bar changes are live on the next
  status refresh after edit). The Claude hook side runs a SNAPSHOT at
  `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/` — the `tmux.py`
  `set_pane_git_identity` fix (Step 5) only reaches the Claude side after a
  plugin version bump + plugin update. That is an OPERATOR decision (see
  Step 9) — do not bump versions unilaterally.
- **nexus** (`/home/nyaptor/dev/personal/nexus`): separate repo with its own
  remote. Any nexus change is a SEPARATE commit under that repo's conventions,
  and pushing it requires operator approval (git-ops rule). The deployed
  statusline is a compiled bun binary at `~/.local/bin/nexus-statusline`
  (built via `bun build --compile`) — source edits do nothing at runtime until
  the operator rebuilds/redeploys (Step 9).
- **Version tolerance (hard requirement)**: old nexus binary + new cc-tmux
  must work (git fields absent → current behavior, fall back to `@cc-branch`);
  new nexus + old cc-tmux must work (extra JSON keys ignored — cc-tmux only
  `data.get()`s known keys). Both directions hold with the design below.

### nexus write side (`apps/nexus-statusline/src/index.ts`, at `0a3a1fb3`)

`getGitStatus` (lines 354–385) already computes everything per render:

```ts
// index.ts:348-352
interface GitInfo {
  branch: string;
  dirty: boolean;
  ahead: number;
}
// index.ts:354  function getGitStatus(dir: string): GitInfo | null { ... }
// returns { branch, dirty, ahead }; null when branch is empty (detached) or git fails
```

`writeSessionContext` (lines 684–706) takes no git parameter and serializes
only three keys:

```ts
// index.ts:684-702
export function writeSessionContext(
  usedPct: number | null | undefined,
  modelLetter: string | null | undefined,
): void {
  try {
    const pane = process.env.TMUX_PANE;
    if (!pane || usedPct == null) return;
    const path = sessionContextPath(pane);
    const tmp = `${path}.tmp`;
    writeFileSync(
      tmp,
      JSON.stringify({
        context_used_pct: usedPct,
        ...(modelLetter ? { model: modelLetter } : {}),
        ts: nowSecs(),
      }),
      { mode: 0o600 },
    );
    renameSync(tmp, path);
```

The call site in `main()` computes git and discards it from the harvest:

```ts
// index.ts:1562-1563
  // git is still needed for branch + dirty detection (no CC equivalent yet)
  const git = getGitStatus(projectDir);
// index.ts:1580
  writeSessionContext(resolvedContext?.usedPct, modelFamilyLetter(ccInput.model));
```

Existing tests: `src/index.test.ts` lines 780–823,
`describe("writeSessionContext — per-pane cache (cc-tmux-bar-cleanup)")`.
NOTE: the test at line 811–816 asserts the EXACT key set
`["context_used_pct", "model", "ts"]` — it will fail once git keys are added
and must be updated (Step 7). Baseline: `bun test src/index.test.ts` →
**113 pass, 0 fail** (verified 2026-07-11).

### cc-tmux read side (`apps/cc-tmux/src/cc_tmux/cli.py`, PRE-003 excerpt at `60a1441`)

```python
# cli.py:607-636 (pre-003 — plan 003 adds a ts staleness gate here)
def _read_session_context(pane_id: str) -> Tuple[str, Optional[float]]:
    """``(model_letter, context_used_pct)`` from ``session-context.<pane>.json``.
    ...
    """
    if not pane_id:
        return "", None
    try:
        data = json.loads((_cc_state_dir() / f"session-context.{pane_id}.json").read_text(encoding="utf-8"))
    except Exception:
        return "", None

    letter = data.get("model")
    if not isinstance(letter, str):
        letter = ""

    pct = data.get("context_used_pct")
    if isinstance(pct, bool) or not isinstance(pct, (int, float)):
        pct = None
    else:
        pct = float(pct) / 100.0

    return letter, pct
```

The only production caller is `cmd_session_bar`:

```python
# cli.py:685-698 (at 60a1441)
    project = tmux.get_pane_option(pane, tmux.OPT_PROJECT)
    branch = tmux.get_pane_option(pane, tmux.OPT_BRANCH)
    ...
    model_letter, ses_pct = _read_session_context(pane)
    account_label, five_h_pct, seven_d_pct = _active_usage()

    out = render.render_session_bar(
        session_count, model_letter, project, branch,
        account_label, ses_pct, five_h_pct, seven_d_pct,
    )
```

### Renderer (`apps/cc-tmux/src/cc_tmux/render.py`, at `60a1441`)

```python
# render.py:216-247 (branch segment of the pure renderer)
def render_session_bar(
    session_count: int,
    model_letter: str,
    project: str,
    branch: str,
    account_label: str,
    ses_pct: Optional[float],
    five_h_pct: Optional[float],
    seven_d_pct: Optional[float],
) -> str:
    ...
    left_parts = [f"#[fg={DIM}]{_session_glyph(session_count)}"]
    if model_letter:
        left_parts.append(f"#[fg={CYAN}]{model_letter}")
    if project:
        left_parts.append(f"#[fg={DIM}]{project}")
    if branch:
        left_parts.append(f"#[fg={DIM}]>")
        left_parts.append(f"#[fg={BRANCH}]{branch}")
    left = " ".join(left_parts) + "#[default]"
```

Color constants: `render.py:201` defines `BRANCH = "#B267E6"`; `render.py`
imports `from .usage import CYAN, DIM, color_for, pct_for` (line 17);
`usage.py:51-54` also defines `YELLOW = "#FAC760"` (not yet imported by
render.py — Step 3 adds it).

### Stale-branch bug (`apps/cc-tmux/src/cc_tmux/tmux.py`, at `60a1441`)

```python
# tmux.py:523-541
def set_pane_git_identity(pane_id: str) -> None:
    """Resolve and store ``@cc-project`` / ``@cc-branch`` for a pane.
    ...
    """
    cwd = _run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_current_path}"])
    if not cwd:
        return

    project = _git_toplevel_name(cwd) or os.path.basename(os.path.normpath(cwd))
    branch = _git_branch(cwd)

    if project:
        _set_opt(pane_id, OPT_PROJECT, project)
    if branch:
        _set_opt(pane_id, OPT_BRANCH, branch)
```

`_git_branch` (tmux.py:681-685) returns `""` for non-repo cwd AND detached
HEAD (`rev-parse --abbrev-ref HEAD` yields literal `HEAD`) AND when git is
missing/times out. Nothing ever unsets `@cc-branch` except full SessionEnd
`clear_pane_state` (tmux.py:568-573), so a pane that moves outside a repo or
detaches gets a MIXED stale identity: fresh `@cc-project` (dir-basename
fallback always rewrites it), stale `@cc-branch`. Helpers that exist:
`_unset_opt(pane_id, option)` at tmux.py:466-467.

### Doc drift (`apps/cc-tmux/src/cc_tmux/tmux.py`, at `60a1441`)

1. `tmux.py:33-34` (module header, Invariant 4 paragraph) claims:
   `Callers may also invoke :func:`set_pane_git_identity` directly (e.g. the
   inbox backfills on open).` — FALSE: a full grep of `apps/cc-tmux/src` shows
   the only references are the `def` (line 523) and the internal default
   resolver in `set_pane_state` (line 517: `resolver = git_resolver or
   set_pane_git_identity`). `cmd_inbox` is a read-only view with no backfill.
2. `get_window_top_pane` docstring, `tmux.py:253-255`: `...the one whose
   ``@cc-model`` / ``@cc-project`` / ``@cc-branch`` the row renders` — but
   `@cc-model` was removed (module header NOTE, lines 20-25; absent from the
   `OPT_*` constants and `_ALL_OPTS`, lines 58-76).

### Self-test infrastructure (`apps/cc-tmux/src/cc_tmux/testing.py`, at `60a1441`)

- `_test_cli_read_session_context` (lines 850-882): fixture-file pattern —
  sets `CLAUDE_CONFIG_DIR` to a tempdir, writes
  `scripts/state/session-context.%9.json`, asserts parse + fail-open. NOTE
  its fixture used `"ts": 123` at 60a1441; plan 003's cutoff should have
  changed that to a fresh timestamp — your new fixtures MUST use a fresh
  `time.time()`-based ts or the 003 gate will zero them out.
- `_test_render_session_bar` (lines 659-685): pure-render assertions via
  `_check(cond, msg)`.
- `_TmuxMock` (lines 199-226): context manager that swaps
  `tmux.tmux_available` and `tmux._run_tmux`, recording calls in `.calls`.
  Its `_run` returns `""` for everything except a `show-options` on
  `@cc-state` — NOT sufficient alone for `set_pane_git_identity` (which needs
  a non-empty `display-message` cwd), so Step 6's test monkeypatches manually.
- Test registry: `_TESTS` list at lines 889-932, entries are
  `("dotted.name", _test_fn)` tuples.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| cc-tmux gate | `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` | `cc-tmux self-test: N/N passed`, exit 0 |
| cc-tmux gate (verbose) | `.../bin/cc-tmux self-test --verbose` (if flag unsupported, plain run is fine) | per-test `ok` lines |
| cc-tmux env check | `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux doctor` | exit 0 (always) |
| nexus tests | `cd /home/nyaptor/dev/personal/nexus/apps/nexus-statusline && bun test src/index.test.ts` | `0 fail` (baseline 113 pass) |
| nexus typecheck | `cd /home/nyaptor/dev/personal/nexus/apps/nexus-statusline && bun run typecheck` | exit 0 |
| Live bar render (optional, needs tmux) | `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux session-bar <window-id>` | a `#[fg=...]...` format string or nothing; exit 0 |

## Scope

**In scope — installfest** (`/home/nyaptor/dev/personal/installfest`):
- `apps/cc-tmux/src/cc_tmux/cli.py` — `_read_session_context`, `cmd_session_bar`
- `apps/cc-tmux/src/cc_tmux/render.py` — `render_session_bar` + the `usage` import line
- `apps/cc-tmux/src/cc_tmux/tmux.py` — `set_pane_git_identity`, module-header line 33-34, `get_window_top_pane` docstring
- `apps/cc-tmux/src/cc_tmux/testing.py` — extend/add tests + `_TESTS` entries
- `plans/README.md` — status row only (if it exists)

**In scope — nexus** (`/home/nyaptor/dev/personal/nexus`, SEPARATE repo/commit, operator-gated push):
- `apps/nexus-statusline/src/index.ts` — `writeSessionContext` + its `main()` call site
- `apps/nexus-statusline/src/index.test.ts` — writeSessionContext describe block

**Out of scope** (do NOT touch, even though they look related):
- `_read_session_context`'s `ts` cutoff logic — plan 003 owns it; consume it, never modify it.
- `apps/cc-tmux/src/cc_tmux/usage.py` and the account/usage segments — settled (zero-isActive fail-open, `--` for unpolled accounts are BY DESIGN).
- Representative-pane selection (`get_window_top_pane` code — docstring fix only): waiting>idle>active choice is by design.
- roadmap-pulse cache handling and any background/revalidation process — settled by spec: cc-tmux is a passive reader; NO new spawning.
- `apps/cc-tmux/.claude-plugin/plugin.json` / `marketplace.json` version bump and `hooks.json` — operator decision (Step 9), possibly shared with other plans landing the same day.
- `getGitStatus` internals in nexus index.ts — it already returns what we need.
- Divergent segment-markup copies between nx and cc-tmux renderers — known accepted duplication (USE-5); do not "unify".

## Git workflow

- installfest: ad-hoc lane, current branch (`main`), ONE commit, targeted adds
  (`git add apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/render.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py`).
  Message style `type(scope): subject`, e.g.
  `feat(cc-tmux): read git dirty/ahead from session-context, unset stale @cc-branch`.
  Note: `.beads/issues.jsonl` is gitignored in THIS repo (installfest exception) — do not force-add it.
- nexus: SEPARATE commit in `/home/nyaptor/dev/personal/nexus`, message style
  matches its log (e.g. `fix(credentials): parse live utilization...`), so:
  `feat(statusline): carry git branch/dirty/ahead into session-context harvest`.
  **Do NOT push either repo without operator approval** (nexus has its own remote; git-ops rule).

## Steps

Order: cc-tmux read side first (backward-compatible — absent fields behave
exactly as today), then nexus write side. The system is never broken between
steps in either order, but this order lets every installfest gate run before
any cross-repo work starts.

### Step 0: Confirm plan 003 landed

Open `apps/cc-tmux/src/cc_tmux/cli.py` and read `_read_session_context`.
It must contain a `ts`-based staleness gate (plan 003) — some check of
`data.get("ts")` against current time that returns the fail-open value when
stale.

**Verify**: `grep -n '"ts"\|get("ts")\|\bts\b' /home/nyaptor/dev/personal/installfest/apps/cc-tmux/src/cc_tmux/cli.py | head` → at least one hit inside `_read_session_context`. If ZERO hits and the function matches the pre-003 excerpt above verbatim → STOP (dependency not landed).

### Step 1: Widen `_read_session_context` to parse optional git fields

In `apps/cc-tmux/src/cc_tmux/cli.py`, change `_read_session_context` to
return a 5-tuple `Tuple[str, Optional[float], str, bool, int]` =
`(model_letter, context_used_pct, branch, dirty, ahead)`.

- Every existing fail-open return (`no pane id`, `read/parse failure`, and
  003's stale-ts return) becomes `("", None, "", False, 0)`.
- After the existing `letter`/`pct` parsing (and INSIDE the fresh path — the
  git fields must flow through 003's staleness gate, so a stale file yields
  the all-empty tuple and the caller falls back to `@cc-branch`), add:

```python
    branch = data.get("branch")
    if not isinstance(branch, str):
        branch = ""

    dirty = data.get("dirty") is True

    ahead_raw = data.get("ahead")
    if isinstance(ahead_raw, bool) or not isinstance(ahead_raw, int) or ahead_raw < 0:
        ahead = 0
    else:
        ahead = ahead_raw

    return letter, pct, branch, dirty, ahead
```

- Update the docstring: payload is now
  `{context_used_pct, model, ts, branch?, dirty?, ahead?}`; absent git keys
  (older nexus binary) → `("", False, 0)` defaults (backward compatible).

**Verify**: `python3 -c "import sys; sys.path.insert(0, '/home/nyaptor/dev/personal/installfest/apps/cc-tmux/src'); from cc_tmux import cli; print(cli._read_session_context(''))"` → `('', None, '', False, 0)`

### Step 2: Consume the new fields in `cmd_session_bar`

In the same file, `cmd_session_bar` (near cli.py:685-698 at 60a1441):

```python
    model_letter, ses_pct, ctx_branch, dirty, ahead = _read_session_context(pane)
    # Prefer the session-context branch (refreshed per statusline render,
    # hook-independent) over @cc-branch (hook-coupled, can go stale) whenever
    # the context file is fresh (003's ts gate) and carries a branch.
    branch = ctx_branch or tmux.get_pane_option(pane, tmux.OPT_BRANCH)
```

Keep the existing `project = tmux.get_pane_option(pane, tmux.OPT_PROJECT)`
read; delete the old unconditional `branch = tmux.get_pane_option(...)` line
(it moves into the fallback above — note `_read_session_context` is called
BEFORE the branch resolution now, so reorder accordingly). Pass the new
fields to the renderer as keyword args:

```python
    out = render.render_session_bar(
        session_count, model_letter, project, branch,
        account_label, ses_pct, five_h_pct, seven_d_pct,
        dirty=dirty, ahead=ahead,
    )
```

Update `cmd_session_bar`'s docstring sentence about reading "@cc-project /
@cc-branch" to say `@cc-branch` is the fallback when the session-context
file lacks a fresh branch.

**Verify**: `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` → all pass (the existing 5-tuple unpack in the test file is fixed in Step 6; if this run fails ONLY inside `cli.read_session_context` on tuple arity, proceed to Step 6 which updates it — any OTHER failure is a STOP).

### Step 3: Render dirty `*` / ahead `^N` markers in `render_session_bar`

In `apps/cc-tmux/src/cc_tmux/render.py`:

- Line 17: extend the import to `from .usage import CYAN, DIM, YELLOW, color_for, pct_for`.
- Change the signature (keyword-only with defaults, so any caller not updated
  yet renders exactly as before — pure-function backward compatibility):

```python
def render_session_bar(
    session_count: int,
    model_letter: str,
    project: str,
    branch: str,
    account_label: str,
    ses_pct: Optional[float],
    five_h_pct: Optional[float],
    seven_d_pct: Optional[float],
    *,
    dirty: bool = False,
    ahead: int = 0,
) -> str:
```

- Replace the branch block with:

```python
    if branch:
        left_parts.append(f"#[fg={DIM}]>")
        seg = f"#[fg={BRANCH}]{branch}"
        if dirty:
            seg += f"#[fg={YELLOW}]*"
        if ahead > 0:
            seg += f"#[fg={YELLOW}]^{ahead}"
        left_parts.append(seg)
```

Markers are gated on `branch` being non-empty — a dirty/ahead marker with no
branch shown would be noise. Update the docstring (mention `*` = dirty
worktree, `^N` = N commits ahead of upstream, both YELLOW, both dropped when
no branch renders — fail-open).

**Verify**: `python3 -c "import sys; sys.path.insert(0, '/home/nyaptor/dev/personal/installfest/apps/cc-tmux/src'); from cc_tmux import render; out = render.render_session_bar(1, 'F', 'if', 'main', '', None, None, None, dirty=True, ahead=3); print('*' in out and '^3' in out); print(render.render_session_bar(1, 'F', 'if', 'main', '', None, None, None) == render.render_session_bar(1, 'F', 'if', 'main', '', None, None, None, dirty=False, ahead=0))"` → `True` twice

### Step 4: Unset stale `@cc-branch` in `set_pane_git_identity` (GIT-FRESH-2)

In `apps/cc-tmux/src/cc_tmux/tmux.py`, `set_pane_git_identity`
(lines 523-541 at 60a1441), change the branch write to:

```python
    if branch:
        _set_opt(pane_id, OPT_BRANCH, branch)
    else:
        # '' is a definitive "no branch" resolution (outside any repo,
        # detached HEAD / mid-rebase) — unset rather than let the previous
        # branch keep rendering as current (stale-value bug). Fail-open bias:
        # show nothing over showing wrong.
        _unset_opt(pane_id, OPT_BRANCH)
```

Leave the `if project:` write as-is (project has a non-git fallback and is
always truthy for a real cwd). Update the docstring's "Fail-open: writes
nothing it cannot resolve" sentence — branch now UNSETS on a definitive
empty resolution; only an unresolvable cwd still writes nothing.

Known tradeoff (accepted by the verified finding): `_git_branch` also
returns `""` when the git binary is missing or times out, which will now
clear a previously-valid branch. That is the fail-open direction (blank >
wrong) — do not add a distinction between "no branch" and "git failed".

**Verify**: `grep -n -A6 'if branch:' /home/nyaptor/dev/personal/installfest/apps/cc-tmux/src/cc_tmux/tmux.py | grep -c '_unset_opt(pane_id, OPT_BRANCH)'` → `1`

### Step 5: Fix the two tmux.py doc drifts (GIT-FRESH-4)

1. Module header line 33-34: replace
   `Callers may also invoke :func:`set_pane_git_identity` directly (e.g. the inbox backfills on open).`
   with
   `:func:`set_pane_git_identity` is invoked only via :func:`set_pane_state`'s resolver seam.`
2. `get_window_top_pane` docstring (lines 253-255): drop `@cc-model` —
   `...the one whose ``@cc-project`` / ``@cc-branch`` the row renders...`
   (the model letter comes from `session-context.<pane>.json`, per the module
   header's own removal NOTE at lines 20-25).

**Verify**: `grep -n 'inbox backfills' /home/nyaptor/dev/personal/installfest/apps/cc-tmux/src/cc_tmux/tmux.py` → no output, AND `grep -n '@cc-model' /home/nyaptor/dev/personal/installfest/apps/cc-tmux/src/cc_tmux/tmux.py` → hits ONLY inside the lines-20-25 removal NOTE (none in `get_window_top_pane`).

### Step 6: Self-test coverage (installfest)

In `apps/cc-tmux/src/cc_tmux/testing.py`:

a. **Extend `_test_cli_read_session_context`** (lines 850-882 at 60a1441;
   plan 003 has likely already modified it — extend, don't rewrite): update
   every tuple assertion to the 5-tuple (`("", None, "", False, 0)` for the
   fail-open cases). Add cases, using a FRESH ts (`time.time()`) in every
   fixture so 003's cutoff passes:
   - full payload `{"context_used_pct": 42, "model": "F", "ts": <fresh>, "branch": "main", "dirty": true, "ahead": 3}` → `("F", 0.42, "main", True, 3)`
   - legacy payload without git keys → `(..., "", False, 0)` (old-nexus tolerance)
   - garbage git types `{"branch": 5, "dirty": "yes", "ahead": -2, ...}` → `("", False, 0)` defaults; also `"ahead": true` → `0` (bool-is-int guard)

b. **Extend `_test_render_session_bar`** (lines 659-685): add
   - `render.render_session_bar(1, "F", "if", "main", "", None, None, None, dirty=True, ahead=2)` → output contains `f"#[fg={render.BRANCH}]main"`, a YELLOW `*`, and `^2`
   - same call with `dirty=False, ahead=0` → no `*` after the branch and no `^`
   - empty branch + `dirty=True, ahead=5` → neither marker appears (gated on branch)
   - no-kwargs call → byte-identical to `dirty=False, ahead=0` (backward compat)

c. **New test `_test_tmux_set_pane_git_identity_unsets_branch`** (place after
   `_test_set_pane_state_writes_state_and_timestamp`, ~line 282). `_TmuxMock`
   alone is insufficient (its `_run` returns `""` for `display-message`, which
   makes `set_pane_git_identity` early-return), so monkeypatch manually with
   try/finally restore, following `_TmuxMock.__enter__/__exit__`'s
   save-restore idiom:

```python
def _test_tmux_set_pane_git_identity_unsets_branch() -> None:
    calls: List[List[str]] = []

    def fake_run(args, *, check_available: bool = True):
        calls.append(list(args))
        if args and args[0] == "display-message":
            return "/tmp/somewhere"
        return ""

    saved_run = tmux._run_tmux
    saved_top = tmux._git_toplevel_name
    saved_branch = tmux._git_branch
    tmux._run_tmux = fake_run  # type: ignore[assignment]
    tmux._git_toplevel_name = lambda cwd: "proj"  # type: ignore[assignment]
    tmux._git_branch = lambda cwd: ""  # type: ignore[assignment]
    try:
        tmux.set_pane_git_identity("%7")
    finally:
        tmux._run_tmux = saved_run  # type: ignore[assignment]
        tmux._git_toplevel_name = saved_top  # type: ignore[assignment]
        tmux._git_branch = saved_branch  # type: ignore[assignment]

    wrote_project = any(
        c[0] == "set-option" and tmux.OPT_PROJECT in c and "proj" in c for c in calls
    )
    unset_branch = any(
        c[0] == "set-option" and "-u" in c and tmux.OPT_BRANCH in c for c in calls
    )
    set_branch = any(
        c[0] == "set-option" and "-u" not in c and tmux.OPT_BRANCH in c for c in calls
    )
    _check(wrote_project, "empty-branch resolution must still write @cc-project")
    _check(unset_branch, "empty-branch resolution must UNSET @cc-branch (stale-value bug)")
    _check(not set_branch, "empty-branch resolution must not SET @cc-branch")
```

d. **Register** the new test in `_TESTS` (after the
   `tmux.set_pane_state_writes` entry):
   `("tmux.set_pane_git_identity_unsets_branch", _test_tmux_set_pane_git_identity_unsets_branch),`

**Verify**: `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` → `cc-tmux self-test: N/N passed` with N >= 43 (42 baseline at 60a1441 + your 1 new registration + whatever 003 added) and `0 FAILED`; exit code 0.

### Step 7: nexus write side — carry git into the payload

In `/home/nyaptor/dev/personal/nexus/apps/nexus-statusline/src/index.ts`:

a. Extend `writeSessionContext` (line 684) with a third optional parameter
   and spread its fields (the `GitInfo` interface at lines 348-352 is already
   exported via `export type { ..., GitInfo, ... }` at line 1549):

```ts
export function writeSessionContext(
  usedPct: number | null | undefined,
  modelLetter: string | null | undefined,
  git?: GitInfo | null,
): void {
  ...
      JSON.stringify({
        context_used_pct: usedPct,
        ...(modelLetter ? { model: modelLetter } : {}),
        ...(git ? { branch: git.branch, dirty: git.dirty, ahead: git.ahead } : {}),
        ts: nowSecs(),
      }),
```

   Key names `branch`/`dirty`/`ahead` are the contract cc-tmux Step 1 parses —
   do not rename. Update the function's doc comment: git fields ride along
   when `getGitStatus` resolved (null → keys omitted; consumer treats absent
   keys as "no data"). Note the existing `usedPct == null` early-return means
   git is also skipped on those frames — accepted: the prior file (with its
   older `ts`) stays in place, and cc-tmux's ts gate governs freshness.

b. Call site (line 1580):

```ts
  writeSessionContext(resolvedContext?.usedPct, modelFamilyLetter(ccInput.model), git);
```

   (`git` is already in scope from line 1563.)

**Verify**: `cd /home/nyaptor/dev/personal/nexus/apps/nexus-statusline && bun run typecheck` → exit 0.

### Step 8: nexus tests

In `src/index.test.ts`, `describe("writeSessionContext — per-pane cache ...")`
(lines 780-823):

a. **Update** the exact-keys test at lines 811-816: `writeSessionContext(62, "O")`
   (no git arg) must still produce exactly `["context_used_pct", "model", "ts"]` —
   keep it, it now proves the git-absent shape (old-consumer tolerance).
b. **Add** two tests modeled on the existing ones in the same describe block:
   - `writeSessionContext(62, "F", { branch: "main", dirty: true, ahead: 3 })`
     → written JSON has `branch === "main"`, `dirty === true`, `ahead === 3`,
     and key set `["ahead", "branch", "context_used_pct", "dirty", "model", "ts"]` (sorted).
   - `writeSessionContext(62, "F", null)` → `"branch" in written === false`
     (null git omits all three keys, same shape as the two-arg call).

**Verify**: `cd /home/nyaptor/dev/personal/nexus/apps/nexus-statusline && bun test src/index.test.ts` → `0 fail`, pass count >= 115 (baseline 113 + 2 new).

### Step 9: Commits + operator gates (STOP point — report, do not improvise)

1. installfest: single commit, targeted adds (see Git workflow). Do not push
   without approval.
2. nexus: separate commit in the nexus repo. Do NOT push — nexus has its own
   remote; pushing requires explicit operator approval.
3. Report to the operator that TWO deploy actions are theirs to take, in
   either order (both sides tolerate the other being older):
   - **nexus redeploy**: rebuild the compiled binary (`bun run build` in
     `apps/nexus-statusline`) and install it over `~/.local/bin/nexus-statusline`
     (the live statusline binary, wired via `~/.claude/settings.json:526`).
     Until then the new payload is not written and the bar simply keeps
     current behavior.
   - **plugin version bump** (for Step 4's hook-path fix): the Claude hook
     side runs the snapshot at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`;
     `set_pane_git_identity`'s unset fix reaches it only after a version bump
     + plugin update. Other plans from this batch may share one bump — the
     operator coordinates it; do not edit `plugin.json`/`marketplace.json` yourself.

**Verify**: `git -C /home/nyaptor/dev/personal/installfest status --short -- apps/cc-tmux` → only the four in-scope files staged/committed, nothing else; `git -C /home/nyaptor/dev/personal/nexus status --short` → only the two in-scope files.

## Test plan

- installfest — extend `_test_cli_read_session_context` + `_test_render_session_bar`,
  add `_test_tmux_set_pane_git_identity_unsets_branch`, all in
  `apps/cc-tmux/src/cc_tmux/testing.py` (Step 6 lists the exact cases:
  happy path with git fields, legacy payload without them, garbage-typed
  fields, bool-`ahead` guard, marker rendering on/off, branch-gated markers,
  no-kwargs backward compat, project-written/branch-unset on empty resolution).
  Structural patterns to mimic: `_test_cli_read_session_context` (fixture
  file + `CLAUDE_CONFIG_DIR` swap) and `_TmuxMock`'s save/restore idiom.
- nexus — extend the `writeSessionContext` describe block in
  `apps/nexus-statusline/src/index.test.ts` (Step 8 lists the cases). Mimic
  the existing tests at lines 795-822 (same `TMUX_PANE` env dance + afterEach
  cleanup).
- Verification: `bin/cc-tmux self-test` → all pass, 0 FAILED;
  `bun test src/index.test.ts` → 0 fail, >= 115 pass.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `/home/nyaptor/dev/personal/installfest/apps/cc-tmux/bin/cc-tmux self-test` exits 0, output ends `passed` with `0 FAILED` absent, total >= 43
- [ ] `python3 -c "import sys; sys.path.insert(0, '/home/nyaptor/dev/personal/installfest/apps/cc-tmux/src'); from cc_tmux import cli; print(cli._read_session_context(''))"` prints `('', None, '', False, 0)`
- [ ] `grep -c '_unset_opt(pane_id, OPT_BRANCH)' apps/cc-tmux/src/cc_tmux/tmux.py` (from installfest root) → `1` (the new else-branch in `set_pane_git_identity`; the literal appears zero times at 60a1441 — `clear_pane_state` unsets via its loop variable `opt`, so it never matches)
- [ ] `grep -n 'inbox backfills' apps/cc-tmux/src/cc_tmux/tmux.py` → no matches
- [ ] `grep -n '@cc-model' apps/cc-tmux/src/cc_tmux/tmux.py` → matches only within the module-header removal NOTE (lines ~20-25), none inside `get_window_top_pane`
- [ ] `cd /home/nyaptor/dev/personal/nexus/apps/nexus-statusline && bun run typecheck` exits 0
- [ ] `cd /home/nyaptor/dev/personal/nexus/apps/nexus-statusline && bun test src/index.test.ts` → `0 fail`, >= 115 pass
- [ ] `git -C /home/nyaptor/dev/personal/installfest status --short` shows no modified files outside the installfest in-scope list; same for nexus
- [ ] `plans/README.md` status row updated (only if the file exists)

## STOP conditions

Stop and report back (do not improvise) if:

- Step 0 finds no `ts` staleness gate in `_read_session_context` (plan 003
  not landed — this plan MUST run after it; the two edit the same function).
- `_read_session_context` at execution time returns something other than a
  2-tuple `(letter, pct)` shape extended by 003 — e.g. another concurrent
  plan already widened it. Reconcile manually? No: STOP and report the
  observed signature.
- `render_session_bar`'s signature differs from the 8-positional-parameter
  excerpt (another plan touched the renderer).
- The nexus `writeSessionContext` / `main()` call-site code does not match
  the excerpts (nexus HEAD moved past `0a3a1fb3` in a conflicting way).
- The exact-keys nexus test still fails after Step 8's update (schema
  mismatch between what you wrote and what the writer emits — key-name drift
  breaks the cross-repo contract; fix names, never invent new ones).
- Any self-test failure persists after two fix attempts.
- You find yourself wanting to edit `plugin.json`, `marketplace.json`,
  `hooks.json`, or push either repo — those are operator gates (Step 9).

## Maintenance notes

- **Cross-repo schema contract**: `session-context.<pane>.json` now carries
  optional `branch` (string), `dirty` (bool), `ahead` (int >= 0) beside
  `context_used_pct`/`model`/`ts`. Writer: nexus `writeSessionContext`.
  Reader: cc-tmux `_read_session_context`. Any future key rename must land
  reader-first (tolerant) then writer, or in lockstep with a redeploy of both.
- **Freshness semantics are owned by plan 003's ts gate**: the git fields
  deliberately have NO freshness logic of their own — if the gate's cutoff
  changes, branch/dirty/ahead freshness changes with it. Reviewers should
  scrutinize that Step 1's git parsing sits INSIDE the fresh path.
- **`@cc-branch` is now fallback-only** on the session bar (still primary for
  any other consumer, e.g. inbox rows). If a future change makes the session
  bar fully session-context-driven, `set_pane_git_identity`'s branch write
  becomes inbox-only — re-audit callers before removing it.
- **Deliberately deferred**: behind-count (`rev-list @{upstream}..HEAD` only
  counts ahead — nexus `getGitStatus` doesn't compute behind); dirty/ahead in
  the tabs row or inbox (session bar only for now); distinguishing "git
  failed" from "no branch" in `set_pane_git_identity` (accepted fail-open
  tradeoff, documented in Step 4).
- The operator deploy gates (nexus binary rebuild, plugin version bump) are
  the two places this change can look "landed but dead" — if the bar never
  shows `*`/`^N` after commit, check `~/.local/bin/nexus-statusline`'s mtime
  and the plugin snapshot version FIRST (config presence != runtime liveness).
