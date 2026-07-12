# Plan 007: Harden every conductor dispatch path — reconciled state, honest exit codes, target validation, readiness poll, worktree collision guard, popup quoting

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` if that file exists — unless a reviewer dispatched you
> and told you they maintain the index.
>
> **Drift check (run first)**:
> `git -C /home/nyaptor/dev/personal/installfest diff --stat 60a1441..HEAD -- apps/cc-tmux/src/cc_tmux/conductor.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/skills/cc-dispatch/SKILL.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW (feature is disabled by default — `@cc-conductor-enabled` off)
- **Depends on**: none (see "Cross-plan coordination" below)
- **Category**: bug
- **Planned at**: commit `60a1441`, 2026-07-11

## Repo facts you must know (you have zero other context)

- Repo: personal dotfiles ("installfest", `~/dev/personal/installfest`, chezmoi-managed).
- Target app: `apps/cc-tmux` — a tmux + Claude Code plugin. **Python 3.10+, STDLIB ONLY**
  (pyproject.toml constraint). Do NOT add any dependency, do NOT add pytest — the test
  suite is the built-in `self-test` command.
- Quality gates: `apps/cc-tmux/bin/cc-tmux self-test` (pure-function suite, non-zero exit
  on any failure) and `apps/cc-tmux/bin/cc-tmux doctor` (env diagnostics, always exit 0).
  Baseline at 60a1441: `cc-tmux self-test: 42/42 passed`, exit 0.
- **New pure functions MUST get self-test coverage** in
  `apps/cc-tmux/src/cc_tmux/testing.py` (this plan adds 6 tests → 48/48).
- Design invariants (from the `tmux.py` module header) that constrain every fix here:
  1. tmux pane options are the ONLY tracked-state store — add no new state files;
  2. views derive, never store;
  3. real-transition guard;
  4. hot path `active` skips git identity;
  5. **fail open** — hook/status entrypoints exit 0 and never block tmux/Claude.
  Conductor *dispatch* is NOT a fail-open read: its own module docstring says genuine
  misuse exits non-zero, so returning 1/2 on dispatch failures is contract-conformant.
- Plugin dual-install: the tmux side runs repo HEAD via the `~/.tmux/plugins/cc-tmux`
  symlink; the Claude hook side runs a SNAPSHOT at
  `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`. **This plan touches neither
  `hooks.json` nor register-path code, so no plugin version bump / operator gate is
  needed.** (The SKILL.md doc edit reaches the Claude-side snapshot only after the next
  version bump — acceptable; see Maintenance notes.)
- Commit pattern: `type(scope): subject`, ad-hoc lane, **targeted `git add` of the three
  in-scope files only**. NOTE: `git status` at plan time already shows unrelated dirty
  files (`apps/cc-tmux/.claude-plugin/marketplace.json`, `plugin.json`) — do NOT stage
  them.

## Why this matters

The conductor (`cc-tmux conductor …`) is the dispatch arm that routes prompts into other
Claude panes and spawns new task windows/worktrees. Six verified defects make dispatch
silently wrong: (1) it reads raw `@cc-state` without ever reconciling, so a pane whose
Claude died but whose shell survived stays "tracked" and `send-prompt` types the prompt +
Enter **into a bare shell, executing prompt text as shell commands**; (2) failed tmux
dispatch actions are discarded and the command exits 0, contradicting the module's own
documented exit-code contract; (3) a typo'd explicit `--target` for a spawn silently falls
back to the invoking pane's cwd — from the conductor session that is its arbitrary start
directory, i.e. a silent wrong-project dispatch; (4) prompt seeding after spawn uses a
blind `time.sleep(0.5)` and can be silently swallowed by the starting TUI; (5) spawned
worktrees/branches accumulate with no lifecycle end and collide on same-second double
dispatch; (6) the popup interpolates a user-configurable session name unquoted into a
shell string. The feature is disabled by default, so risk is low — but the code ships, the
skill documents it as usable, and each fix is small and testable.

## Current state (all excerpts fresh from commit 60a1441)

Files and roles:

- `apps/cc-tmux/src/cc_tmux/conductor.py` (485 lines) — the conductor module; ALL code
  changes land here.
- `apps/cc-tmux/src/cc_tmux/testing.py` (958 lines) — built-in self-test suite; new tests
  land here.
- `apps/cc-tmux/skills/cc-dispatch/SKILL.md` (72 lines) — authoritative dispatch CLI doc;
  two small doc edits land here.
- `apps/cc-tmux/src/cc_tmux/tmux.py` — READ ONLY for this plan. Provides:
  `_run_tmux(args) -> Optional[str]` (lines 129–150: returns stripped stdout, **`None` on
  any failure** — note success for output-less commands like `send-keys` is `""`, which is
  falsy, so failure checks MUST be `is None`); `reconcile(claude_ids_fn)` (lines 629–650:
  rate-limited self-heal returning the current hop-pane list); `_heal_stale` (605–626:
  clears `@cc-state` on panes no longer running Claude); `switch_to_pane(pane_id) -> bool`
  (391–400); `current_pane_id()` (371–376).
- `apps/cc-tmux/src/cc_tmux/cli.py` — READ ONLY. Provides
  `_pane_ids_running_claude(rows) -> set` (line 748), the `claude_ids_fn` that cli.py's
  own four reconcile call sites pass (lines 172, 365, 393, 408). cli.py imports conductor
  at module load (`from .conductor import cmd_conductor`, line 24) — so conductor must
  import cli **deferred inside a function**, never at module top, to avoid a cycle.

### conductor.py — the defect sites

Module docstring contract (lines 22–24):

```python
* **Invariant 5 (fail open):** reads (``list``, ``context``) always exit 0; only
  genuine misuse (unknown/absent dispatch target, no ``claude`` binary for a
  spawn, a git failure for a worktree, popup while disabled) exits non-zero.
```

CD-7 — unquoted session name (line 222; `session_name()` at 108–110 returns the raw
`@cc-conductor-session` global option, only `.strip()`ed):

```python
    opened = tmux._run_tmux(["display-popup", "-E", f"tmux attach-session -t {name}"])
```

CD-1 — dispatch reads raw, never-reconciled state (lines 245–247 and 306–311):

```python
def _dispatchable_panes() -> List[tmux.PaneInfo]:
    """Every tracked pane except the conductor's own session, in priority order."""
    return sort_panes(tmux.get_hop_panes(exclude_session=session_name()))
...
def _pane_state(pane_id: str) -> Optional[str]:
    """Current tracked state of a pane among the dispatchable set, or ``None``."""
    for p in _dispatchable_panes():
        if p.id == pane_id:
            return p.state
    return None
```

`tmux.reconcile()` is called from exactly four sites, all in cli.py (cycle 172, inbox 365,
picker-data 393, status 408) — no conductor entry point ever heals stale state.

CD-3 — results discarded, exit 0 regardless (lines 314–319 and 322–339):

```python
def _dispatch_switch(target: Optional[str]) -> int:
    if not target:
        sys.stderr.write("cc-tmux conductor: switch requires --target <pane>.\n")
        return 2
    tmux.switch_to_pane(target)
    return 0


def _dispatch_send_prompt(target: Optional[str], prompt: Optional[str], force: bool) -> int:
    if not target:
        sys.stderr.write("cc-tmux conductor: send-prompt requires --target <pane>.\n")
        return 2
    if prompt is None:
        sys.stderr.write("cc-tmux conductor: send-prompt requires --prompt <text>.\n")
        return 2
    state = _pane_state(target)
    if state == "active" and not force:
        sys.stderr.write(
            f"cc-tmux conductor: pane {target} is active (busy); "
            "re-run with --force to send anyway.\n"
        )
        return 1
    # -l sends the text literally, then a separate Enter submits it.
    tmux._run_tmux(["send-keys", "-t", target, "-l", prompt])
    tmux._run_tmux(["send-keys", "-t", target, "Enter"])
    return 0
```

Note the two compounding holes: `_pane_state()` returns `None` for any untracked/unknown
target, and `None != "active"` **bypasses the busy-guard entirely**, then both `send-keys`
results are discarded and the function returns 0 even for a nonexistent pane like `%999`.

CD-4 — silent cwd fallback on a bad explicit target, no expanduser (lines 342–351):

```python
def _resolve_dir(target: Optional[str]) -> Optional[str]:
    """Directory for a spawn: the target if it is a dir, else the current pane cwd."""
    if target and os.path.isdir(target):
        return os.path.abspath(target)
    pane = tmux.current_pane_id()
    if pane:
        cwd = tmux._run_tmux(["display-message", "-p", "-t", pane, "#{pane_current_path}"])
        if cwd and os.path.isdir(cwd):
            return cwd
    return None
```

Settled scoping (do NOT re-litigate): the *omitted*-target cwd fallback from an ordinary
pane is documented by-design (SKILL.md line 35: "Falls back to the current pane's
directory if `--target` is omitted"), and the skill's `~/dev/oo` examples work because the
invoking shell expands the unquoted tilde. The verified defect is the *explicit-but-
invalid* target silently falling through `os.path.isdir` to the fallback, plus the
conductor-context fallback resolving to the conductor's own arbitrary start directory.

CD-5 — blind sleep before seeding (lines 354–370; sleep at 367):

```python
def _open_window(cwd: str, prompt: Optional[str]) -> int:
    """Open a new window running claude in ``cwd`` and seed ``prompt`` if given."""
    if shutil.which("claude") is None:
        sys.stderr.write("cc-tmux conductor: no 'claude' binary on PATH.\n")
        return 1
    new_pane = tmux._run_tmux(
        ["new-window", "-P", "-F", "#{pane_id}", "-c", cwd, "claude"]
    )
    if new_pane is None:
        sys.stderr.write("cc-tmux conductor: could not open a new window.\n")
        return 1
    if prompt is not None:
        # Give claude a beat to start before seeding the prompt.
        time.sleep(0.5)
        tmux._run_tmux(["send-keys", "-t", new_pane, "-l", prompt])
        tmux._run_tmux(["send-keys", "-t", new_pane, "Enter"])
    return 0
```

The readiness poll must NOT depend on the spawned pane's `@cc-state` appearing (the
Claude-side hook that sets it runs from the plugin snapshot and has been observed
disabled) — poll pane *content* instead.

CD-6 — no worktree lifecycle end, second-resolution stamp (lines 402–422):

```python
    stamp = time.strftime("%Y%m%d-%H%M%S")
    branch = f"conductor/{stamp}"
    wt_path = os.path.join(toplevel, ".worktrees", f"conductor-{stamp}")
    added = _git(toplevel, ["worktree", "add", "-b", branch, wt_path])
    if added is None:
        sys.stderr.write("cc-tmux conductor: git worktree add failed.\n")
        return 1
    return _open_window(wt_path, prompt)
```

Nothing anywhere in `apps/cc-tmux` removes/prunes `.worktrees/conductor-*` dirs or
`conductor/<stamp>` branches. Same-second double dispatch collides (fails cleanly, exit 1
via the `added is None` branch).

Imports at top of conductor.py today (lines 32–43): `os, shutil, subprocess, sys, time`,
`pathlib.Path`, `typing.List/Optional`, `from . import log, tmux`,
`from .priority import sort_panes`. No `shlex`, no `Tuple`, no `Callable`.

### testing.py — the harness you extend

- Tiny stdlib harness: `_check(cond, msg)` raises on failure (lines 30–32).
- Import line 19: `from . import cli, paths, priority, registry, render, tmux, usage` —
  you will add `conductor` to it.
- Registration: append `("name", fn)` tuples to the `_TESTS` list (lines 889–932; last
  entry today is `("cli.read_session_context", _test_cli_read_session_context)` at 931).
- Monkeypatch exemplar: `_TmuxMock` context manager (lines 199–226) swaps
  `tmux.tmux_available` and `tmux._run_tmux` and records calls — model any tmux-touching
  test on it. Env-save/restore + tempdir exemplar: `_test_registry_resolve_project_code`
  (lines 288–312).

### skills/cc-dispatch/SKILL.md — the doc you amend

Mode table row (line 35): `spawn-task` … "Falls back to the current pane's directory if
`--target` is omitted."
Exit codes (lines 57–62):

```markdown
- `0` — dispatched (or a read succeeded).
- `1` — refused: target pane is `active` without `--force`, no `claude` binary for a
  spawn, git worktree failure, or the conductor is disabled for `--popup`.
- `2` — misuse: missing `--mode`, `--target`, or a required `--prompt`.
```

## Commands you will need

Run from the repo root `/home/nyaptor/dev/personal/installfest`.

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Baseline gate | `./apps/cc-tmux/bin/cc-tmux self-test` | `cc-tmux self-test: 42/42 passed`, exit 0 (BEFORE any edit) |
| Gate after all steps | `./apps/cc-tmux/bin/cc-tmux self-test --verbose` | `48/48 passed`, exit 0 |
| Syntax check | `python3 -m py_compile apps/cc-tmux/src/cc_tmux/conductor.py apps/cc-tmux/src/cc_tmux/testing.py` | exit 0, no output |
| Env diagnostics | `./apps/cc-tmux/bin/cc-tmux doctor` | prose checklist, exit 0 |
| Runtime probe (untracked target) | `./apps/cc-tmux/bin/cc-tmux conductor dispatch --mode send-prompt --target %999 --prompt hi; echo "exit=$?"` | stderr refusal mentioning the untracked pane, `exit=1` (works inside AND outside tmux — outside tmux the pane list is empty, so `%999` is untracked either way) |
| Runtime probe (bad spawn target) | `./apps/cc-tmux/bin/cc-tmux conductor dispatch --mode spawn-task --target /nonexistent-dir-xyz --prompt hi; echo "exit=$?"` | stderr "not a directory" misuse message, `exit=2` |

There is no pnpm/pytest/lint gate for this app — self-test + doctor + py_compile are the
whole gate set.

## Scope

**In scope** (the only files you may modify):

- `apps/cc-tmux/src/cc_tmux/conductor.py`
- `apps/cc-tmux/src/cc_tmux/testing.py`
- `apps/cc-tmux/skills/cc-dispatch/SKILL.md`

**Out of scope** (do NOT touch, even though they look related):

- `apps/cc-tmux/src/cc_tmux/tmux.py` — `reconcile`/`_heal_stale`/`_run_tmux` are REUSED
  as-is; changing them belongs to other plans.
- `apps/cc-tmux/src/cc_tmux/cli.py` and `parser.py` — the conductor argparse flags and
  `_pane_ids_running_claude` already exist; no flag changes are needed.
- `apps/cc-tmux/hooks/hooks.json`, `.claude-plugin/plugin.json`,
  `.claude-plugin/marketplace.json` — touching these triggers the plugin-version-bump
  operator gate this plan deliberately avoids. `marketplace.json`/`plugin.json` are
  already dirty in the working tree from another session — leave them unstaged.
- `~/dev/personal/nexus` (any file) — cross-repo work is plan 004's territory.
- Adding any *automatic* worktree reaper/daemon — CD-6 is a collision guard + docs only;
  an auto-reaper would violate the no-new-background-process design stance (settled).

**Settled findings — do not "fix" these** (they were audited and refuted/accepted):
representative-pane choice for the session bar; empty usage segment on zero active
accounts; waiting/idle-only git resolution; `send-keys -l` newline behavior; duplicated
segment markup copies; cc-tmux as passive reader of the roadmap-pulse cache.

## Git workflow

- Work directly on the current branch (`main`) — ad-hoc lane, no feature branch.
- ONE commit at the end. Write the message to a temp file first, then commit with `-F`
  (repo convention; avoids shell-mangling of multi-line messages):

  ```bash
  printf 'fix(cc-tmux): harden conductor dispatch paths\n\nReconciled pane reads + untracked-target refusal (CD-1), checked tmux\nresults with non-zero exit on dispatch failure (CD-3), explicit spawn\ntarget validation with no silent cwd fallback (CD-4), bounded readiness\npoll before prompt seeding (CD-5), worktree name collision guard + docs\n(CD-6), shell-quoted popup attach command (CD-7).\n' > /tmp/commit-msg-007.txt
  git add apps/cc-tmux/src/cc_tmux/conductor.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/skills/cc-dispatch/SKILL.md
  git commit -F /tmp/commit-msg-007.txt
  ```

- Do NOT `git add .` / `-A`. Do NOT push unless the operator instructed it.

## Steps

### Step 0: Baseline

Run the drift check from the header, then:

**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test` → `cc-tmux self-test: 42/42 passed`,
exit 0. If not 42/42, STOP (another plan's executor may be mid-flight in this tree).

### Step 1 (CD-7): Shell-quote the popup attach command

In `conductor.py`:

1. Add `import shlex` to the stdlib import block (alphabetical: after `import os`,
   before `import shutil`).
2. Directly above `_popup` (line 196), add a pure helper:

   ```python
   def _attach_command(name: str) -> str:
       """Shell command for ``display-popup -E``: attach to the conductor session.

       ``name`` is user config (``@cc-conductor-session``) — quote it so spaces or
       shell metacharacters can neither break the attach nor inject into the
       popup's shell. Pure (testable).
       """
       return f"tmux attach-session -t {shlex.quote(name)}"
   ```

3. Replace line 222's f-string with the helper:

   ```python
   opened = tmux._run_tmux(["display-popup", "-E", _attach_command(name)])
   ```

In `testing.py`:

4. Add `conductor` to the line-19 import:
   `from . import cli, conductor, paths, priority, registry, render, tmux, usage`
5. Add (near the other cli/render tests, e.g. after `_test_cli_read_session_context`):

   ```python
   def _test_conductor_attach_command() -> None:
       import shlex as _shlex

       _check(
           conductor._attach_command("conductor") == "tmux attach-session -t conductor",
           "plain name must pass through unquoted-equivalent",
       )
       hostile = "bad name; rm -rf /"
       _check(
           _shlex.split(conductor._attach_command(hostile))
           == ["tmux", "attach-session", "-t", hostile],
           "hostile name must survive shell splitting as ONE argv token",
       )
   ```

6. Register in `_TESTS` (append after the last entry, line 931):
   `("conductor.attach_command", _test_conductor_attach_command),`

**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test` → `43/43 passed`, exit 0.
**Verify**: `grep -n 'shlex.quote' apps/cc-tmux/src/cc_tmux/conductor.py` → 1 match.

### Step 2 (CD-3): Check tmux results on dispatch paths; report failures non-zero

All in `conductor.py`. Remember: `tmux._run_tmux` success for output-less commands is
`""` — **test `is None`, never truthiness**.

1. `_dispatch_switch` (lines 314–319): capture the bool `tmux.switch_to_pane` already
   returns:

   ```python
   def _dispatch_switch(target: Optional[str]) -> int:
       if not target:
           sys.stderr.write("cc-tmux conductor: switch requires --target <pane>.\n")
           return 2
       if not tmux.switch_to_pane(target):
           sys.stderr.write(f"cc-tmux conductor: could not switch to pane {target}.\n")
           return 1
       return 0
   ```

2. `_dispatch_send_prompt` tail (lines 336–339): check both sends:

   ```python
       # -l sends the text literally, then a separate Enter submits it.
       sent = tmux._run_tmux(["send-keys", "-t", target, "-l", prompt])
       entered = tmux._run_tmux(["send-keys", "-t", target, "Enter"])
       if sent is None or entered is None:
           sys.stderr.write(f"cc-tmux conductor: send-keys to pane {target} failed.\n")
           return 1
       return 0
   ```

3. `_open_window` seeding (lines 365–369): check both sends; the window DID open, so name
   the pane in the error so the operator can seed manually:

   ```python
       if prompt is not None:
           # Give claude a beat to start before seeding the prompt.
           time.sleep(0.5)
           sent = tmux._run_tmux(["send-keys", "-t", new_pane, "-l", prompt])
           entered = tmux._run_tmux(["send-keys", "-t", new_pane, "Enter"])
           if sent is None or entered is None:
               sys.stderr.write(
                   f"cc-tmux conductor: window opened ({new_pane}) but seeding "
                   "the prompt failed; paste it manually.\n"
               )
               return 1
       return 0
   ```

   (The `time.sleep(0.5)` line is replaced in Step 6 — leave it for now.)

4. Update the module docstring Invariant 5 bullet (lines 22–24) to match reality:

   ```python
   * **Invariant 5 (fail open):** reads (``list``, ``context``) always exit 0; genuine
     misuse (unknown/absent dispatch target, no ``claude`` binary for a spawn, a git
     failure for a worktree, popup while disabled) AND a dispatch action whose tmux
     call itself failed exit non-zero — a dispatch is a write, not a fail-open read.
   ```

In `skills/cc-dispatch/SKILL.md`, extend the exit-`1` bullet (lines 60–61) to:

```markdown
- `1` — refused or failed: target pane is `active` without `--force`, the target pane is
  not a tracked Claude pane (unknown/stale — see `--force`), no `claude` binary for a
  spawn, git worktree failure, the tmux dispatch action itself failed, or the conductor
  is disabled for `--popup`.
```

(The "not a tracked Claude pane" clause is implemented in Step 3 — writing the doc once
here avoids editing the same bullet twice.)

**Verify**: `python3 -m py_compile apps/cc-tmux/src/cc_tmux/conductor.py` → exit 0.
**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test` → `43/43 passed`, exit 0.
**Verify**: `grep -c 'is None' apps/cc-tmux/src/cc_tmux/conductor.py` → at least 4
(pre-existing 2 at lines 169/223-area plus the new send-keys checks).

### Step 3 (CD-1): Reconcile before dispatch; refuse untracked send-prompt targets

All in `conductor.py`.

1. Replace `_dispatchable_panes` (lines 245–247) so every conductor read/dispatch goes
   through the same rate-limited self-heal cli.py's entry points use:

   ```python
   def _dispatchable_panes() -> List[tmux.PaneInfo]:
       """Every tracked pane except the conductor's own session, in priority order.

       Reads through :func:`cc_tmux.tmux.reconcile` so stale ``@cc-state`` left by a
       dead Claude is healed (rate-limited, fail-open) before the conductor lists,
       snapshots, or dispatches — a dead pane must not receive a prompt into its
       surviving bare shell.
       """
       from . import cli  # deferred: cli.py imports this module at load time

       panes = tmux.reconcile(cli._pane_ids_running_claude)
       name = session_name()
       return sort_panes([p for p in panes if p.session != name])
   ```

   Notes for you: `tmux.reconcile` has no `exclude_session` parameter (it wraps a bare
   `get_hop_panes()`), hence the post-filter on `p.session`. The deferred import is
   REQUIRED — a top-level `from . import cli` would recreate the import cycle the
   `claude_ids_fn` injection exists to avoid (see tmux.py:641).

2. Add a pure refusal-decision helper directly above `_dispatch_send_prompt`:

   ```python
   def _send_prompt_refusal(state: Optional[str], force: bool) -> Optional[str]:
       """Reason ``send-prompt`` must be refused, or ``None`` to proceed. Pure.

       ``state is None`` means the target is not in the dispatchable set at all
       (untracked, unknown, or just healed away) — typing into it would land in
       whatever process owns the pane, potentially a bare shell that EXECUTES the
       prompt text. ``--force`` overrides both refusals.
       """
       if force:
           return None
       if state is None:
           return (
               "is not a tracked Claude pane (unknown or stale target); "
               "re-run with --force to send anyway"
           )
       if state == "active":
           return "is active (busy); re-run with --force to send anyway"
       return None
   ```

3. Rewire `_dispatch_send_prompt`'s guard (replacing the `state ==
   "active" and not force` block, lines 329–335) and document the residual race:

   ```python
       state = _pane_state(target)
       reason = _send_prompt_refusal(state, force)
       if reason:
           sys.stderr.write(f"cc-tmux conductor: pane {target} {reason}.\n")
           return 1
       # Residual TOCTOU: the state above was read through a reconciled snapshot
       # moments ago, but the pane can still die between that read and the
       # send-keys below. Reconciling shrinks the stale window from unbounded to
       # seconds; it cannot eliminate it. Accepted residual risk.
   ```

In `testing.py`, add + register:

```python
def _test_conductor_send_prompt_refusal() -> None:
    _check(conductor._send_prompt_refusal("idle", False) is None, "idle must be sendable")
    _check(conductor._send_prompt_refusal("waiting", False) is None, "waiting must be sendable")
    active = conductor._send_prompt_refusal("active", False)
    _check(active is not None and "active" in active, "active must refuse with busy reason")
    untracked = conductor._send_prompt_refusal(None, False)
    _check(untracked is not None and "not a tracked" in untracked, "None state must refuse")
    _check(conductor._send_prompt_refusal(None, True) is None, "--force overrides untracked")
    _check(conductor._send_prompt_refusal("active", True) is None, "--force overrides busy")
```

Register: `("conductor.send_prompt_refusal", _test_conductor_send_prompt_refusal),`

**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test` → `44/44 passed`, exit 0.
**Verify**: `grep -n 'tmux.reconcile' apps/cc-tmux/src/cc_tmux/conductor.py` → 1 match
inside `_dispatchable_panes`.
**Verify** (runtime): `./apps/cc-tmux/bin/cc-tmux conductor dispatch --mode send-prompt
--target %999 --prompt hi; echo "exit=$?"` → stderr `pane %999 is not a tracked Claude
pane …`, `exit=1`.

### Step 4 (CD-4): Validate spawn targets — no silent cwd fallback for explicit or conductor-context dispatch

All in `conductor.py`.

1. Extend the typing import (line 40) to `from typing import Callable, List, Optional, Tuple`
   (`Callable` is used in Step 5; adding both now avoids touching the line twice).
2. Replace `_resolve_dir` (lines 342–351) with a `(dir, error)` shape:

   ```python
   def _resolve_dir(target: Optional[str]) -> Tuple[Optional[str], str]:
       """Directory for a spawn: ``(directory, "")`` or ``(None, reason)``.

       Three rules (in order):
       * An EXPLICIT ``--target`` that is not a directory is misuse — it never
         falls back to the invoking pane's cwd (silent wrong-project dispatch).
         ``~`` is expanded so a quoted tilde also works.
       * With no target, inside the conductor session (``CC_TMUX_CONDUCTOR=1``)
         the cwd fallback is refused — the conductor's cwd is its arbitrary
         start directory, not a project root.
       * Otherwise the documented fallback: the invoking pane's current path.
       """
       if target:
           expanded = os.path.abspath(os.path.expanduser(target))
           if os.path.isdir(expanded):
               return expanded, ""
           return None, f"--target {target!r} is not a directory (explicit targets never fall back)"
       if os.environ.get(_CONDUCTOR_ENV, "").strip() == "1":
           return None, "dispatch from the conductor requires an explicit --target <dir>"
       pane = tmux.current_pane_id()
       if pane:
           cwd = tmux._run_tmux(["display-message", "-p", "-t", pane, "#{pane_current_path}"])
           if cwd and os.path.isdir(cwd):
               return cwd, ""
       return None, "no --target given and the current pane directory could not be resolved"
   ```

   (`_CONDUCTOR_ENV` is the existing module constant `"CC_TMUX_CONDUCTOR"`, line 56 —
   set on the conductor session at creation, line 179, and inherited by every process the
   conductor's Claude spawns.)

3. Update both callers to the tuple shape:

   ```python
   def _dispatch_spawn_task(target: Optional[str], prompt: Optional[str]) -> int:
       cwd, err = _resolve_dir(target)
       if cwd is None:
           sys.stderr.write(f"cc-tmux conductor: spawn-task: {err}.\n")
           return 2
       return _open_window(cwd, prompt)
   ```

   and identically in `_dispatch_spawn_worktree` (lines 402–409), keeping its message
   prefix `spawn-worktree:`.

In `skills/cc-dispatch/SKILL.md`:

4. Amend the `spawn-task` row (line 35) fallback sentence to:
   "Falls back to the current pane's directory if `--target` is omitted (refused inside
   the conductor session — pass an explicit `--target` there; an explicit `--target` that
   is not a directory is a misuse error, never a fallback)." Apply the same parenthetical
   to the `spawn-worktree` row (line 36).
5. Extend the exit-`2` bullet (line 62) to:
   "`2` — misuse: missing `--mode`, `--target`, or a required `--prompt`; an explicit
   spawn `--target` that is not a directory; or a spawn from the conductor session
   without an explicit `--target`."

In `testing.py`, add + register (model env handling on
`_test_registry_resolve_project_code`, lines 288–312 — save/restore around the body):

```python
def _test_conductor_resolve_dir() -> None:
    saved = os.environ.get("CC_TMUX_CONDUCTOR")
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-conductor-test-")
    try:
        os.environ.pop("CC_TMUX_CONDUCTOR", None)

        # Explicit valid target -> resolved, no error.
        got, err = conductor._resolve_dir(tmpdir)
        _check(got == os.path.abspath(tmpdir) and err == "", "valid explicit target must resolve")

        # Explicit invalid target -> (None, reason); NEVER a silent fallback.
        got, err = conductor._resolve_dir(os.path.join(tmpdir, "nope"))
        _check(got is None and "not a directory" in err, "invalid explicit target must refuse")

        # Conductor context without a target -> (None, reason requiring --target).
        os.environ["CC_TMUX_CONDUCTOR"] = "1"
        got, err = conductor._resolve_dir(None)
        _check(got is None and "explicit --target" in err, "conductor context must require --target")
    finally:
        if saved is None:
            os.environ.pop("CC_TMUX_CONDUCTOR", None)
        else:
            os.environ["CC_TMUX_CONDUCTOR"] = saved
        shutil.rmtree(tmpdir, ignore_errors=True)
```

Register: `("conductor.resolve_dir", _test_conductor_resolve_dir),`
(The ordinary-pane fallback branch needs a live/mocked tmux — deliberately untested here;
the branch is unchanged behavior.)

**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test` → `45/45 passed`, exit 0.
**Verify** (runtime): `./apps/cc-tmux/bin/cc-tmux conductor dispatch --mode spawn-task
--target /nonexistent-dir-xyz --prompt hi; echo "exit=$?"` → stderr
`spawn-task: --target '/nonexistent-dir-xyz' is not a directory …`, `exit=2`.

### Step 5 (CD-6): Worktree name collision guard + lifecycle docs

In `conductor.py`:

1. Add a pure slot-picker above `_dispatch_spawn_worktree` (uses the `Callable`/`Tuple`
   imports from Step 4):

   ```python
   def _worktree_slot(
       toplevel: str,
       stamp: str,
       exists: Callable[[str], bool] = os.path.exists,
   ) -> Tuple[str, str]:
       """``(branch, path)`` for a fresh conductor worktree.

       The stamp has second resolution, so a same-second double dispatch would
       collide; suffix ``-2``, ``-3``, … until the path is free. Pure via the
       injected ``exists``. Falls back to a pid suffix after 99 tries (paranoia
       bound — never expected).
       """
       for n in range(1, 100):
           suffix = "" if n == 1 else f"-{n}"
           path = os.path.join(toplevel, ".worktrees", f"conductor-{stamp}{suffix}")
           if not exists(path):
               return f"conductor/{stamp}{suffix}", path
       pid_suffix = f"-{os.getpid()}"
       return (
           f"conductor/{stamp}{pid_suffix}",
           os.path.join(toplevel, ".worktrees", f"conductor-{stamp}{pid_suffix}"),
       )
   ```

2. Use it in `_dispatch_spawn_worktree`, replacing the three stamp/branch/wt_path lines
   (415–417):

   ```python
       stamp = time.strftime("%Y%m%d-%H%M%S")
       branch, wt_path = _worktree_slot(toplevel, stamp)
   ```

   (A branch existing WITHOUT its path — e.g. a hand-removed worktree with a kept
   branch — still fails cleanly via the existing `added is None` → exit 1 path; a git
   round-trip branch check is deliberately not added.)

3. Document the lifecycle end in the module docstring: append one bullet to the
   "Governing rules" list (after the Invariant 5 bullet):

   ```python
   * **Worktree lifecycle is manual by design:** ``spawn-worktree`` creates
     ``.worktrees/conductor-<stamp>`` on branch ``conductor/<stamp>`` and never
     removes either (no background reaper — same no-new-daemon stance as the
     rest of the plugin). Clean up with ``git worktree remove <path>`` +
     ``git branch -D conductor/<stamp>``, or cc's ``wt reap`` for stale
     ``.worktrees/`` entries older than 24h.
   ```

In `skills/cc-dispatch/SKILL.md`:

4. Append the same guidance to the `spawn-worktree` row (line 36): "Worktrees/branches
   are NOT auto-removed — clean up with `git worktree remove` + `git branch -D`, or a
   stale-worktree reaper such as `wt reap`."

In `testing.py`, add + register:

```python
def _test_conductor_worktree_slot() -> None:
    top = "/repo"
    # Free slot -> bare stamp, no suffix.
    branch, path = conductor._worktree_slot(top, "20260711-120000", exists=lambda _p: False)
    _check(branch == "conductor/20260711-120000", f"bare branch wrong: {branch}")
    _check(path == "/repo/.worktrees/conductor-20260711-120000", f"bare path wrong: {path}")

    # First two taken -> -3 suffix on BOTH branch and path.
    taken = {
        "/repo/.worktrees/conductor-20260711-120000",
        "/repo/.worktrees/conductor-20260711-120000-2",
    }
    branch, path = conductor._worktree_slot(top, "20260711-120000", exists=lambda p: p in taken)
    _check(branch == "conductor/20260711-120000-3", f"suffixed branch wrong: {branch}")
    _check(path == "/repo/.worktrees/conductor-20260711-120000-3", f"suffixed path wrong: {path}")
```

Register: `("conductor.worktree_slot", _test_conductor_worktree_slot),`

**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test` → `46/46 passed`, exit 0.

### Step 6 (CD-5): Replace the blind 0.5s sleep with a bounded readiness poll

In `conductor.py` — the poll watches pane CONTENT (first TUI paint), never `@cc-state`
(the hook that sets it runs from the possibly-stale/disabled Claude-side plugin snapshot):

1. Add module-level constants (near `_CONDUCTOR_ENV`, line 56):

   ```python
   # Prompt-seeding readiness poll (spawn modes). Bounded; on timeout we still
   # seed (best effort — same failure surface as the old fixed sleep, never worse).
   _READY_TIMEOUT = 10.0
   _READY_INTERVAL = 0.25
   _READY_GRACE = 0.3
   ```

2. Add two helpers above `_open_window`:

   ```python
   def _pane_ready(content: Optional[str]) -> bool:
       """True once a spawned pane shows any painted content. Pure.

       The window is created running ``claude`` directly (no shell prompt), so a
       blank capture means the TUI has not painted yet; any non-whitespace output
       means startup has begun and the input loop is imminent.
       """
       return bool(content and content.strip())


   def _wait_for_pane_ready(
       pane_id: str,
       *,
       timeout: float = _READY_TIMEOUT,
       interval: float = _READY_INTERVAL,
       capture: Optional[Callable[[], Optional[str]]] = None,
       sleep: Callable[[float], None] = time.sleep,
       clock: Callable[[], float] = time.monotonic,
   ) -> bool:
       """Poll until the pane paints or ``timeout`` elapses. Injectable for tests."""
       if capture is None:
           capture = lambda: tmux._run_tmux(["capture-pane", "-p", "-t", pane_id])  # noqa: E731
       deadline = clock() + timeout
       while clock() < deadline:
           if _pane_ready(capture()):
               return True
           sleep(interval)
       return False
   ```

3. In `_open_window`, replace the `time.sleep(0.5)` line (and its comment) from Step 2's
   version with:

   ```python
           # Bounded readiness poll: wait for claude's first paint instead of a
           # blind sleep; on timeout, seed anyway (fail-open best effort). The
           # grace beat lets the TUI finish entering raw mode after first paint
           # so the seeded keys are not flushed by terminal-mode setup.
           if not _wait_for_pane_ready(new_pane):
               log.warn("conductor: pane %s not ready after %.1fs; seeding anyway", new_pane, _READY_TIMEOUT)
           time.sleep(_READY_GRACE)
   ```

   (Everything after — the checked `sent`/`entered` sends from Step 2 — stays.)

In `testing.py`, add + register:

```python
def _test_conductor_pane_ready() -> None:
    _check(conductor._pane_ready(None) is False, "None capture -> not ready")
    _check(conductor._pane_ready("") is False, "empty capture -> not ready")
    _check(conductor._pane_ready("   \n\n  ") is False, "whitespace capture -> not ready")
    _check(conductor._pane_ready("Welcome to Claude Code") is True, "painted -> ready")


def _test_conductor_wait_for_pane_ready() -> None:
    # Ready on the first capture: no sleeps consumed.
    sleeps: List[float] = []
    ok = conductor._wait_for_pane_ready(
        "%1", timeout=5.0, interval=0.25,
        capture=lambda: "hello", sleep=sleeps.append, clock=lambda: 0.0,
    )
    _check(ok is True and sleeps == [], "immediately-ready pane must not sleep")

    # Never ready: fake clock advances past the deadline -> False, bounded.
    ticks = iter([0.0, 1.0, 2.0, 3.0])
    ok = conductor._wait_for_pane_ready(
        "%1", timeout=2.5, interval=0.25,
        capture=lambda: "", sleep=lambda _s: None, clock=lambda: next(ticks),
    )
    _check(ok is False, "never-ready pane must time out False")
```

Register both:
`("conductor.pane_ready", _test_conductor_pane_ready),` and
`("conductor.wait_for_pane_ready", _test_conductor_wait_for_pane_ready),`

**Verify**: `./apps/cc-tmux/bin/cc-tmux self-test --verbose` → `48/48 passed`, exit 0,
with all six `conductor.*` rows shown as `ok`.
**Verify**: `grep -n 'time.sleep(0.5)' apps/cc-tmux/src/cc_tmux/conductor.py` → no
matches (exit 1).

### Step 7: Final gates + commit

1. `python3 -m py_compile apps/cc-tmux/src/cc_tmux/conductor.py apps/cc-tmux/src/cc_tmux/testing.py` → exit 0.
2. `./apps/cc-tmux/bin/cc-tmux self-test` → `48/48 passed`, exit 0.
3. `./apps/cc-tmux/bin/cc-tmux doctor` → exit 0.
4. Both runtime probes from "Commands you will need" → exit 1 and exit 2 respectively.
5. `git status --porcelain` → modified: exactly the three in-scope files (plus the
   pre-existing unrelated `.claude-plugin/marketplace.json` + `plugin.json` lines, which
   you leave unstaged).
6. Commit per "Git workflow" above.

## Test plan

- New tests (all in `apps/cc-tmux/src/cc_tmux/testing.py`, registered in `_TESTS`):
  1. `conductor.attach_command` — quoting round-trip incl. hostile name (CD-7).
  2. `conductor.send_prompt_refusal` — idle/waiting proceed; active refused; None
     (untracked) refused; `--force` overrides both (CD-1).
  3. `conductor.resolve_dir` — explicit valid target resolves; explicit invalid target
     refuses (no fallback); conductor-context no-target refuses (CD-4).
  4. `conductor.worktree_slot` — no collision → bare stamp; two collisions → `-3` suffix
     on branch AND path (CD-6).
  5. `conductor.pane_ready` — None/empty/whitespace not ready; painted ready (CD-5).
  6. `conductor.wait_for_pane_ready` — immediate ready with zero sleeps; bounded timeout
     via injected clock (CD-5).
- Structural patterns to mimic: `_test_reconcile_rate_limit` (testing.py:169, pure gate
  test), `_TmuxMock` (testing.py:199, if you find you need to mock tmux — the tests above
  are designed NOT to need it), `_test_registry_resolve_project_code` (testing.py:288,
  env save/restore + tempdir).
- Verification: `./apps/cc-tmux/bin/cc-tmux self-test --verbose` → all pass, total 48.

## Done criteria

Machine-checkable. ALL must hold (run from repo root):

- [ ] `./apps/cc-tmux/bin/cc-tmux self-test` prints `48/48 passed` and exits 0
- [ ] `./apps/cc-tmux/bin/cc-tmux doctor` exits 0
- [ ] `python3 -m py_compile apps/cc-tmux/src/cc_tmux/conductor.py apps/cc-tmux/src/cc_tmux/testing.py` exits 0
- [ ] `grep -c 'shlex.quote' apps/cc-tmux/src/cc_tmux/conductor.py` → `1`
- [ ] `grep -n 'time.sleep(0.5)' apps/cc-tmux/src/cc_tmux/conductor.py` → no matches
- [ ] `grep -c 'tmux.reconcile' apps/cc-tmux/src/cc_tmux/conductor.py` → `1`
- [ ] `./apps/cc-tmux/bin/cc-tmux conductor dispatch --mode send-prompt --target %999 --prompt hi` exits 1 with a "not a tracked Claude pane" stderr line
- [ ] `./apps/cc-tmux/bin/cc-tmux conductor dispatch --mode spawn-task --target /nonexistent-dir-xyz --prompt hi` exits 2 with a "not a directory" stderr line
- [ ] `git status --porcelain` shows no modified files outside the three in-scope paths
      (the pre-existing `.claude-plugin/marketplace.json` + `plugin.json` modifications
      may remain, unstaged)
- [ ] `plans/README.md` status row updated, if that file exists

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `60a1441`, or the baseline
  self-test is not exactly `42/42 passed` before your first edit (concurrent plan
  executors share this tree).
- The "Current state" excerpts do not match the live code at the cited lines.
- Any step's verification fails twice after a reasonable fix attempt.
- A fix appears to require modifying `tmux.py`, `cli.py`, `parser.py`, `hooks.json`, or
  `.claude-plugin/*` — those are out of scope / other plans' territory.
- Adding the deferred `from . import cli` in Step 3 produces an ImportError or circular-
  import at runtime (probe: `./apps/cc-tmux/bin/cc-tmux conductor list --json` must exit
  0 and print a JSON array, possibly `[]`).
- You are tempted to add a background process, a new state file, or a `@cc-state`-based
  readiness wait — all three violate settled design constraints stated above.

## Maintenance notes

- **Residual TOCTOU is accepted, documented in-code** (Step 3): a pane can still die
  between the reconciled state read and `send-keys`. Anyone wanting stronger guarantees
  must NOT solve it with a lock file or daemon (invariant 1 / no-new-background-process);
  the next honest increment would be a just-in-time `#{pane_current_command}` check,
  which was deliberately left out to keep dispatch to one extra tmux round-trip.
- **Reconcile in `_dispatchable_panes` is rate-limited** (10s default via
  `@cc-reconcile-interval`, shared stamp `@cc-last-reconcile`): if the status bar
  reconciled <10s ago, a dispatch may still see up-to-10s-stale state. That is the
  designed ceiling, not a bug.
- **SKILL.md edits reach the Claude-side plugin snapshot only after the next plugin
  version bump + update** (snapshot at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`).
  The tmux-side code runs repo HEAD immediately via the `~/.tmux/plugins/cc-tmux`
  symlink. No action needed now; just know the doc lags until the next bump.
- Observed at authoring time: `command -v cc-tmux` resolves to nothing on this machine —
  the skill's bare `cc-tmux …` examples assume a PATH entry that may not exist. If
  dispatch from a Claude session fails with command-not-found, that is a PATH/wiring
  issue OUTSIDE this plan; report it, do not fix it here.
- Reviewer scrutiny points: every new failure check on `tmux._run_tmux` results must be
  `is None` (a successful `send-keys` returns `""`, which is falsy); the deferred
  `from . import cli` must stay inside the function body; `_worktree_slot` suffixes must
  stay in lock-step on branch and path.
- Deferred by design (not forgotten): automatic worktree reaping (manual
  `git worktree remove` / `wt reap` documented instead); a git-side branch-existence
  check in `_worktree_slot` (the `added is None` → exit 1 path already fails clean); a
  tmux-mocked test for `_resolve_dir`'s ordinary-pane fallback branch (unchanged
  behavior).

## Cross-plan coordination

This plan is one of several drafted concurrently against `60a1441`. Ownership boundary:
**this plan owns `conductor.py`** (plus its tests and the cc-dispatch skill doc). Any
change to `cli.py`, `tmux.py`, `render.py`, `usage.py`, hook wiring, or the nexus repo
(`~/dev/personal/nexus`) belongs to other plans (nexus work is plan 004) — reference
them, never implement their steps. If another executor is mid-flight (baseline self-test
count ≠ 42), coordinate via the operator rather than proceeding.
