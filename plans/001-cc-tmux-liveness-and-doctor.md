# Plan 001: Hook-liveness detection + doctor truthfulness + reconcile revival

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git -C /home/nyaptor/dev/personal/installfest diff --stat 60a1441..HEAD -- apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/cc-tmux.tmux`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug (observability blind spot + dead self-heal path)
- **Planned at**: commit `60a1441`, 2026-07-11

## Repo facts (inline — the executor has zero context)

- Repo: personal dotfiles ("installfest"), `/home/nyaptor/dev/personal/installfest`,
  chezmoi-managed, project code `if`.
- Target app: `apps/cc-tmux` — a tmux + Claude Code plugin. **Python 3.10+,
  STDLIB ONLY** (pyproject.toml constraint — never add a dependency).
- Quality gates:
  - `apps/cc-tmux/bin/cc-tmux self-test` — pure-function suite, non-zero exit on
    any failure. Baseline at 60a1441: `cc-tmux self-test: 42/42 passed`.
  - `apps/cc-tmux/bin/cc-tmux doctor` — environment diagnostics, ALWAYS exits 0
    (PASS/FAIL/WARN/INFO rows are prose, never an exit code).
  - Every NEW pure function MUST get self-test coverage in
    `apps/cc-tmux/src/cc_tmux/testing.py`.
- Design invariants (from the `tmux.py` module header — every fix must honor them):
  1. tmux pane options are the ONLY tracked-state store — no new state files for
     pane state.
  2. Views derive, never store.
  3. Real-transition guard (`set_pane_state` returns whether state changed).
  4. Hot path `active` skips git identity.
  5. Fail open — every hook/status entrypoint exits 0, never blocks tmux/Claude.
- tmux 3.6a, `status-interval 1` (status re-renders every second). Whole rows
  render via top-level `status-format[0..2]` slots; `#()` nested inside
  `window-status-format` never re-evaluates on this tmux version (known).
- **Dual-install split**: the tmux side (status rows, keybindings, doctor) runs
  repo HEAD via the `~/.tmux/plugins/cc-tmux` symlink; the Claude hook side
  (`register`/`clear`) runs a SNAPSHOT at
  `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`, refreshed only by
  `claude plugin update`. **This plan touches only reader-side code (doctor,
  tabs-row, docstrings, tmux wiring) — no hooks.json change, no register-path
  change — so NO plugin version bump and NO snapshot update are required or
  wanted here.** Do NOT bump `apps/cc-tmux/.claude-plugin/plugin.json`.
- Incident context: the plugin was silently DISABLED by the 0.1.1 update
  (every hook dead, pane state frozen) and the operator re-enabled it
  2026-07-11. During that outage, `cc-tmux doctor` reported all-healthy —
  that blindness is what this plan fixes.
- Commit pattern: `type(scope): subject`, ad-hoc lane, TARGETED `git add`
  (named files only — never `git add .`).
- Beads issue prefix: `if-` (not needed unless a STOP condition fires).

## Why this matters

`cc-tmux doctor` is the ONLY production observability surface for the plugin
(debug logging is `CC_TMUX_DEBUG`-gated and off in production), and today it
green-lights a total outage: its "claude plugin registered" row PASSes on a
bare substring match even when the plugin is disabled, it has no row that
notices hooks have stopped writing state, and it has no row covering the
dual-install version split. Separately, the stale-state self-heal
(`tmux.reconcile`) is wired to surfaces that never render, so a `kill -9`'d
Claude pane shows a frozen state on the always-visible tabs row forever. After
this plan: doctor FAILs on a disabled plugin, FAIL/WARNs on dead hook traffic,
WARNs on snapshot-vs-repo drift, and the once-per-tick tabs-row drives the
existing rate-limited heal (~1 process scan per 10s).

## Current state

All excerpts are fresh reads at commit `60a1441` (working tree verified
identical for `apps/cc-tmux/` via empty `git diff 60a1441 -- apps/cc-tmux`).

### Files

- `apps/cc-tmux/src/cc_tmux/cli.py` — CLI handlers; contains `cmd_doctor`
  (lines 247–350), `cmd_status` (401–415), `cmd_tabs_row` (721–741),
  `_pane_ids_running_claude` (748–774), `_cc_state_dir` (575–582),
  `_REGISTER_TRACE_FILE` (118), `_trace_register` (122–157).
- `apps/cc-tmux/src/cc_tmux/tmux.py` — pane-option state store;
  `reconcile` (629–650), `_heal_stale` (605–626), `iter_panes_with_process`
  (403–442), `_DEFAULT_RECONCILE_INTERVAL = 10.0` (83). NOT modified by this
  plan — read-only reference.
- `apps/cc-tmux/src/cc_tmux/testing.py` — self-test suite; `_TESTS` registry
  at lines 889–932 (42 entries), runner `run_self_test` at 935.
- `apps/cc-tmux/cc-tmux.tmux` — tmux-side plugin body; dead `@cc-status` /
  `@cc-status-inbox` wiring at lines 120–121.
- `apps/cc-tmux/.claude-plugin/plugin.json` — `"version": "0.1.1"`.
  Read-only reference (doctor will read it at runtime); do NOT edit.

### Defect 1 — doctor blesses a disabled plugin (cli.py:305–321)

```python
    # Claude plugin registered (WARN if `claude` absent)
    claude_bin = shutil.which("claude")
    if not claude_bin:
        add("WARN", "claude plugin registered", "claude not on PATH (skipped)")
    else:
        try:
            proc = subprocess.run(
                ["claude", "plugin", "list"],
                capture_output=True, text=True, timeout=10,
            )
            listing = f"{proc.stdout}\n{proc.stderr}"
            if "cc-tmux" in listing:
                add("PASS", "claude plugin registered", "cc-tmux present")
            else:
                add("WARN", "claude plugin registered", "cc-tmux not in `claude plugin list`")
        except (OSError, subprocess.SubprocessError) as exc:
            add("WARN", "claude plugin registered", f"`claude plugin list` failed: {exc}")
```

A disabled plugin still appears in the listing (the live incident: disabled
since the 0.1.1 update, doctor said PASS). The CLI has a better interface —
verified live on this machine 2026-07-11:

```
$ claude plugin list --json
[ ...,
 {
  "id": "cc-tmux@cc-tmux",
  "version": "0.1.1",
  "scope": "user",
  "enabled": true,
  "installPath": "/home/nyaptor/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1",
  "installedAt": "2026-07-11T13:59:26.597Z",
  "lastUpdated": "2026-07-12T01:50:52.792Z"
 }, ... ]
```

`enabled` is a real boolean; `installPath` and `version` give the snapshot leg
of the dual-install split for free.

### Defect 2 — no hook-liveness row anywhere (cli.py:338–340 is the last doctor row)

```python
    # tracked-pane count
    count = len(tmux.get_hop_panes())
    add("INFO", "tracked panes", str(count))
```

The full doctor row set at 60a1441 is: tmux version, fzf, python, `$TMUX`,
plugin symlink, claude-plugin-registered, focus hook, tracked-pane count.
Nothing compares hook traffic against running Claude processes. Every
ingredient already exists:

- `_pane_ids_running_claude(rows)` (cli.py:748) — pane ids whose process tree
  contains Claude, fed from `tmux.iter_panes_with_process()` (tmux.py:403).
- `PaneInfo.timestamp` — `@cc-timestamp`, epoch seconds of the last register,
  via `tmux.get_hop_panes()`.
- The register trace log — the only durable hook-traffic record, currently
  written by `_trace_register` (cli.py:122–157) and read by NO code anywhere:
  path `_cc_state_dir() / _REGISTER_TRACE_FILE`
  (= `~/.claude/scripts/state/cc-tmux-register-trace.log`, honoring
  `CLAUDE_CONFIG_DIR`), one JSON object per line with a `"ts"` epoch field,
  e.g. (live file, 2026-07-11):
  `{"hook_event_name": null, "pane_id": "%3", "rename_attempted": true, "rename_fired": true, "ts": 1783821536.3019927}`

Signal design (from the audit, confirmed sound): per-pane `@cc-timestamp` age
alone is NOT valid (an idle pane legitimately freezes for hours); the valid
signal is global — "N panes are running Claude right now, and the newest
register evidence across ALL sources is M minutes old / absent". hooks.json
fires `register` on SessionStart, UserPromptSubmit, PreToolUse
(AskUserQuestion|ExitPlanMode), PostToolUse, PostToolUseFailure, Notification
(permission_prompt|elicitation_dialog|idle_prompt), and Stop — so any
in-use session produces registers constantly, and even a finishing session
fires a final Stop register.

### Defect 3 — the reconcile self-heal never runs in production

`cmd_status` docstring claims heartbeat status (cli.py:401–408):

```python
def cmd_status(args) -> int:
    """Emit the status-bar pane counts via ``@cc-status-format`` (Req-7).

    The status bar is tmux's frequent-render surface, so it doubles as the
    de-facto reconcile heartbeat — but the scan is rate-limited (design.md
    Decision 1), so a render pays at most one process scan per interval.
    """
    groups = group_by_state(tmux.reconcile(_pane_ids_running_claude))
```

But `@cc-status` (set at cc-tmux.tmux:120) has ZERO consumers — grep across
`home/` (all deployed tmux configs + all theme .conf files) and
`apps/cc-tmux/README.md` + `apps/cc-tmux/docs/` finds no `@cc-status`
interpolation anywhere (verified 2026-07-11). `tmux.reconcile` is called at
exactly four cli.py sites: 172 (`cmd_cycle`), 365 (`cmd_inbox`), 393
(`cmd_picker_data`), 408 (`cmd_status`) — the first three are manual
keybindings, the fourth never renders. The actual per-tick surfaces —
`cmd_tabs_row` (721, status-format[0]), `cmd_session_bar` (667,
status-format[1]), `cmd_beads_bar` (704, status-format[2]),
`cmd_window_icon` (545) — call no reconcile. So a `kill -9`'d Claude (which
fires no SessionEnd hook) leaves `@cc-state` frozen on the tabs row
indefinitely, despite `reconcile` (tmux.py:629–650) being rate-limited via
`@cc-last-reconcile` + `@cc-reconcile-interval` (default 10.0s) precisely so a
per-second render tick can afford it.

`cmd_tabs_row` as it exists today (cli.py:721–741, body only):

```python
def cmd_tabs_row(args) -> int:
    """Emit the whole animated window-tabs row (cc-tmux-tabs-and-rename-fix).
    ...
    """
    windows = tmux.get_window_tabs()
    if not windows:
        return 0
    active_window_id = tmux.current_window_id()
    out = render.render_tabs_row(windows, active_window_id, time.time())
    if out:
        sys.stdout.write(out)
    return 0
```

### Defect 4 — dead `@cc-status` wiring in cc-tmux.tmux (lines 117–121)

```bash
# ---------------------------------------------------------------------------
# Status sources + one-shot discover of already-running Claude sessions.
# ---------------------------------------------------------------------------
tmux set-option -g @cc-status "#($CMD status)"
tmux set-option -g @cc-status-inbox "#($CMD status-inbox)"
```

Also referenced in the file's header comment, line 9:
`#   * wires @cc-status / @cc-status-inbox to the CLI status sources,`

**Decision (owned by this plan): DELETE the wiring.** Zero consumers, zero
documentation references, and it is what props up the false "heartbeat"
docstring. The `status` / `status-inbox` CLI subcommands STAY (a user can wire
`#(cc-tmux status)` into their own status-format manually; removing
subcommands is out of scope and would collide with plan 005's render
consolidation).

### Dual-install cross-check (part of Defect 1's fix)

Writer leg: hooks.json invokes `${CLAUDE_PLUGIN_ROOT}/bin/cc-tmux register ...`
where `CLAUDE_PLUGIN_ROOT` is the cache snapshot (installPath above). Reader
leg: `~/.tmux/plugins/cc-tmux` symlinks to the repo checkout, and
cc-tmux.tmux:30–31 resolves `CMD` through it, so tmux-side code goes live on
every commit while the writer lags until `claude plugin update`. doctor's only
install-topology row today is the "plugin symlink" row (cli.py:294–303, tmux
leg only). The new row compares the snapshot `version` from the `--json`
listing against the repo's `.claude-plugin/plugin.json` version AND
content-digests the two `src/` trees (a repo edit to register semantics
without a version bump diverges at the SAME version string — the digest is
what catches that).

## Commands you will need

| Purpose | Command (from repo root `/home/nyaptor/dev/personal/installfest`) | Expected on success |
|---|---|---|
| Self-test | `apps/cc-tmux/bin/cc-tmux self-test` | `cc-tmux self-test: 46/46 passed`, exit 0 (42 baseline + 4 new) |
| Doctor | `apps/cc-tmux/bin/cc-tmux doctor` | row list printed, exit 0 |
| Syntax check | `python3 -m py_compile apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/testing.py` | exit 0, no output |
| Bash syntax | `bash -n apps/cc-tmux/cc-tmux.tmux` | exit 0, no output |
| Pure-fn spot check | `PYTHONPATH=apps/cc-tmux/src python3 -c "..."` | see per-step |
| Tree check | `git status --porcelain apps/cc-tmux` | only the 3 in-scope files modified |

There is no install step (stdlib only) and no separate lint/typecheck gate for
this app.

## Suggested executor toolkit

- `verification-before-completion` skill (if available) before claiming done.
- Reference reading: `apps/cc-tmux/src/cc_tmux/tmux.py` lines 1–39 (the
  invariants header) and 595–650 (reconcile machinery) — read, do not edit.

## Scope

**In scope** (the only files you may modify):

- `apps/cc-tmux/src/cc_tmux/cli.py`
- `apps/cc-tmux/src/cc_tmux/testing.py`
- `apps/cc-tmux/cc-tmux.tmux`
- `plans/README.md` (status row only, at the end)

**Out of scope** (do NOT touch, even though they look related):

- `apps/cc-tmux/hooks/hooks.json` — hook matcher changes belong to plan 002.
- `apps/cc-tmux/src/cc_tmux/render.py`, `tmux.py`, `parser.py` — render/perf
  consolidation belongs to plan 005; the reconcile machinery in tmux.py
  already works and needs no change.
- `apps/cc-tmux/.claude-plugin/plugin.json` and
  `.claude-plugin/marketplace.json` — NO version bump in this plan (reader-side
  changes only; a bump would make the new snapshot-version row WARN until the
  operator runs a plugin update, for zero benefit).
- `~/.claude/plugins/cache/cc-tmux/...` — NEVER edit the snapshot cache.
- `~/dev/personal/nexus` (separate repo, plan 004's territory).
- The `status` / `status-inbox` subcommands and their parser entries — keep
  them; only their tmux-side auto-wiring is deleted.
- openspec docs mentioning `#{E:@cc-status}` — historical spec text, leave it.

## Git workflow

- Work directly on `main` (this repo's ad-hoc lane; no branch unless the
  operator says otherwise).
- ONE commit at the end, targeted adds only:
  `git add apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/cc-tmux.tmux plans/README.md`
- Message style (matches `git log`, e.g. `fix(cc-tmux): restore tab
  click-to-switch via range=window, fix spacing`):
  `fix(cc-tmux): doctor liveness+enabled+snapshot rows, tabs-row reconcile heartbeat, drop dead @cc-status wiring`
- Write the commit message to a file and use `git commit -F <file>`; do NOT
  embed a heredoc in a chained command. Do not push unless the operator
  instructed it.

## Steps

### Step 1: Add pure evaluators `_evaluate_plugin_listing` and `_evaluate_hook_liveness` to cli.py

In `apps/cc-tmux/src/cc_tmux/cli.py`, directly ABOVE `def cmd_doctor(args)`
(line 247), insert three helpers. Exact target shape:

```python
def _repo_plugin_version() -> str:
    """Version from this checkout's .claude-plugin/plugin.json, or ''. Fail-open."""
    try:
        root = Path(__file__).resolve().parents[2]  # .../apps/cc-tmux
        data = json.loads((root / ".claude-plugin" / "plugin.json").read_text(encoding="utf-8"))
        ver = data.get("version")
        return ver if isinstance(ver, str) else ""
    except Exception:
        return ""


def _evaluate_plugin_listing(raw: str, repo_version: str) -> Tuple[List[Tuple[str, str, str]], str]:
    """Pure: map ``claude plugin list --json`` output to doctor rows.

    Returns ``(rows, install_path)`` where rows are ``(status, label, detail)``
    tuples covering plugin enablement and snapshot-vs-repo version, and
    ``install_path`` is the snapshot dir for the caller's src-digest check
    ('' when unavailable). Never raises — unparseable input degrades to a
    single WARN row (fail-open, invariant 5).
    """
    try:
        plugins = json.loads(raw)
        entry = next(
            (p for p in plugins
             if isinstance(p, dict) and str(p.get("id", "")).startswith("cc-tmux@")),
            None,
        )
    except Exception:
        return [("WARN", "claude plugin enabled",
                 "could not parse `claude plugin list --json` output")], ""

    if entry is None:
        return [("WARN", "claude plugin enabled",
                 "cc-tmux not in `claude plugin list --json`")], ""

    rows: List[Tuple[str, str, str]] = []
    snap_ver = entry.get("version")
    if entry.get("enabled") is True:
        rows.append(("PASS", "claude plugin enabled", f"enabled (v{snap_ver})"))
    else:
        rows.append(("FAIL", "claude plugin enabled",
                     "plugin DISABLED — all hooks dead, pane state frozen; "
                     "run: claude plugin enable cc-tmux@cc-tmux"))

    if not repo_version:
        rows.append(("WARN", "plugin snapshot version",
                     "repo .claude-plugin/plugin.json unreadable"))
    elif snap_ver == repo_version:
        rows.append(("PASS", "plugin snapshot version",
                     f"snapshot {snap_ver} == repo {repo_version}"))
    else:
        rows.append(("WARN", "plugin snapshot version",
                     f"snapshot {snap_ver} != repo {repo_version} — hook side runs "
                     "stale code; run: claude plugin update cc-tmux@cc-tmux"))

    install_path = entry.get("installPath")
    return rows, install_path if isinstance(install_path, str) else ""


_HOOK_LIVENESS_STALE_SECS = 1800.0  # 30 min; register fires on every prompt/tool/Stop


def _evaluate_hook_liveness(
    live_claude_count: int,
    newest_register_ts: Optional[float],
    now: float,
    stale_after: float = _HOOK_LIVENESS_STALE_SECS,
) -> Tuple[str, str]:
    """Pure liveness verdict: are hooks writing state while Claude panes run?

    Global signal only — per-pane @cc-timestamp age is NOT a valid staleness
    signal (an idle pane legitimately freezes for hours). FAIL is reserved for
    'Claude is running and NO register evidence exists at all'; an old-but-
    present newest register is a WARN (dead hooks OR a long-idle session).
    """
    if live_claude_count <= 0:
        return "INFO", "no panes running Claude (liveness not applicable)"
    if newest_register_ts is None or newest_register_ts <= 0:
        return ("FAIL",
                f"{live_claude_count} pane(s) running Claude but no register "
                "activity recorded — hooks look dead")
    age = now - newest_register_ts
    if age <= stale_after:
        return ("PASS",
                f"{live_claude_count} pane(s) running Claude; newest register "
                f"{age / 60.0:.1f} min ago")
    return ("WARN",
            f"{live_claude_count} pane(s) running Claude but newest register is "
            f"{age / 60.0:.0f} min old — dead hooks or long-idle session")
```

Notes: `json`, `time`, `Path`, `List`, `Optional`, `Tuple` are already
imported at cli.py module top (lines 14–21) — add no imports.

**Verify**:
`python3 -m py_compile apps/cc-tmux/src/cc_tmux/cli.py && PYTHONPATH=apps/cc-tmux/src python3 -c "
from cc_tmux import cli
rows, ip = cli._evaluate_plugin_listing('[{\"id\": \"cc-tmux@cc-tmux\", \"version\": \"0.1.1\", \"enabled\": false, \"installPath\": \"/x\"}]', '0.1.1')
print(rows[0][0], ip)
print(cli._evaluate_hook_liveness(2, None, 1000.0)[0])
"`
→ prints `FAIL /x` then `FAIL`, exit 0.

### Step 2: Rewire cmd_doctor — enabled/version/src rows replace the substring check, liveness row appended

In `cmd_doctor`, replace the entire "Claude plugin registered" block
(cli.py:305–321, quoted in "Current state" Defect 1) with:

```python
    # Claude plugin enabled + snapshot version/src (dual-install split; WARN if
    # `claude` absent). The hook WRITER runs the cache snapshot; readers run
    # repo HEAD via the ~/.tmux/plugins symlink — these rows catch the split.
    claude_bin = shutil.which("claude")
    if not claude_bin:
        add("WARN", "claude plugin enabled", "claude not on PATH (skipped)")
    else:
        try:
            proc = subprocess.run(
                ["claude", "plugin", "list", "--json"],
                capture_output=True, text=True, timeout=10,
            )
            plugin_rows, install_path = _evaluate_plugin_listing(
                proc.stdout, _repo_plugin_version()
            )
            for status, label, detail in plugin_rows:
                add(status, label, detail)
            if install_path:
                repo_src = Path(__file__).resolve().parents[1]   # .../src
                snap_src = Path(install_path) / "src"
                repo_digest = _src_digest(repo_src)
                snap_digest = _src_digest(snap_src)
                if not repo_digest or not snap_digest:
                    add("WARN", "plugin snapshot src", "could not digest src trees")
                elif repo_digest == snap_digest:
                    add("PASS", "plugin snapshot src", "snapshot src identical to repo")
                else:
                    add("WARN", "plugin snapshot src",
                        "snapshot src DIVERGED from repo — hook side runs different "
                        "code; run: claude plugin update cc-tmux@cc-tmux")
        except (OSError, subprocess.SubprocessError) as exc:
            add("WARN", "claude plugin enabled", f"`claude plugin list --json` failed: {exc}")
```

Add the digest helper next to the Step 1 helpers (above `cmd_doctor`):

```python
def _src_digest(src_dir: Path) -> str:
    """SHA-256 over sorted (relpath, bytes) of *.py under src_dir; '' on any error."""
    import hashlib
    try:
        h = hashlib.sha256()
        for p in sorted(src_dir.rglob("*.py")):
            if "__pycache__" in p.parts:
                continue
            h.update(str(p.relative_to(src_dir)).encode("utf-8"))
            h.update(b"\x00")
            h.update(p.read_bytes())
        return h.hexdigest()
    except Exception:
        return ""
```

Then, immediately AFTER the tracked-pane count block (the two lines
`count = len(tmux.get_hop_panes())` / `add("INFO", "tracked panes", str(count))`,
cli.py:338–340) and BEFORE the `# -- render ---` section, insert the liveness
row:

```python
    # hook liveness — is the Claude hook side writing state while panes run?
    try:
        live = len(_pane_ids_running_claude(tmux.iter_panes_with_process()))
        candidates: List[float] = [p.timestamp for p in tmux.get_hop_panes() if p.timestamp > 0]
        trace = _cc_state_dir() / _REGISTER_TRACE_FILE
        if trace.is_file():
            try:
                last_line = trace.read_text(encoding="utf-8").splitlines()[-1]
                ts = json.loads(last_line).get("ts")
                if isinstance(ts, (int, float)) and not isinstance(ts, bool):
                    candidates.append(float(ts))
                else:
                    candidates.append(trace.stat().st_mtime)
            except Exception:
                candidates.append(trace.stat().st_mtime)
        newest = max(candidates) if candidates else None
        status, detail = _evaluate_hook_liveness(live, newest, time.time())
        add(status, "hook liveness", detail)
    except Exception as exc:
        add("WARN", "hook liveness", f"check failed: {exc}")
```

(The trace file is a diagnostic artifact written by `_trace_register`, NOT
pane state — reading it does not violate invariant 1. This is its first-ever
reader.)

**Verify**: `apps/cc-tmux/bin/cc-tmux doctor; echo "exit=$?"` → output contains
a `claude plugin enabled` row, a `plugin snapshot version` row, a
`plugin snapshot src` row, and a `hook liveness` row; final line `exit=0`.
On this machine with the plugin currently enabled at 0.1.1 (== repo version,
src byte-identical as of 2026-07-11) and Claude sessions running, expect:
`PASS claude plugin enabled  enabled (v0.1.1)`,
`PASS plugin snapshot version  snapshot 0.1.1 == repo 0.1.1`,
`PASS plugin snapshot src  snapshot src identical to repo`, and a
`PASS`/`INFO` hook-liveness row (PASS if any register fired in the last
30 min — running this from inside an active Claude session guarantees it).
The old `claude plugin registered` label must be GONE:
`grep -c "claude plugin registered" apps/cc-tmux/src/cc_tmux/cli.py` → `0`.

### Step 3: Revive the reconcile heartbeat in cmd_tabs_row; fix the cmd_status docstring

(a) In `cmd_tabs_row` (cli.py:721), insert ONE line at the top of the body,
before `windows = tmux.get_window_tabs()`:

```python
    tmux.reconcile(_pane_ids_running_claude)  # rate-limited self-heal, <=1 scan/10s
    windows = tmux.get_window_tabs()
```

And append this sentence to the `cmd_tabs_row` docstring (before the closing
`"""`): `As the once-per-tick session-wide surface (status-format[0]), this is
also the reconcile heartbeat: the call above is rate-limited by
@cc-last-reconcile / @cc-reconcile-interval (tmux.py), so status-interval 1
costs at most one process scan per interval.`

(b) Replace the `cmd_status` docstring (cli.py:402–407, quoted in Defect 3)
with:

```python
    """Emit the status-bar pane counts via ``@cc-status-format`` (Req-7).

    NOT wired by default: cc-tmux.tmux no longer publishes ``@cc-status``
    (the option had zero consumers). Users may wire ``#(cc-tmux status)``
    into their own status line manually. The per-tick reconcile heartbeat
    lives in :func:`cmd_tabs_row` (status-format[0]); the reconcile call
    below is kept (rate-limited) for anyone who does wire this surface.
    """
```

Leave the `cmd_status` body unchanged.

**Verify** (must run from inside tmux; `$TMUX` is set in the dev session):

```bash
tmux set-option -gu @cc-last-reconcile 2>/dev/null
apps/cc-tmux/bin/cc-tmux tabs-row >/dev/null
tmux show-option -gv @cc-last-reconcile
```

→ last command prints a fresh epoch float (e.g. `1783825xxx.xxx`), proving the
tabs-row tick now stamps the reconcile rate-limit gate (i.e. `reconcile` ran).
If it prints nothing, the reconcile call is not being reached — STOP.

### Step 4: Delete the dead @cc-status wiring from cc-tmux.tmux

In `apps/cc-tmux/cc-tmux.tmux`:

- Delete lines 120–121 exactly:
  ```bash
  tmux set-option -g @cc-status "#($CMD status)"
  tmux set-option -g @cc-status-inbox "#($CMD status-inbox)"
  ```
- Update the section comment above them (lines 117–119) from
  `# Status sources + one-shot discover of already-running Claude sessions.` to
  `# One-shot discover of already-running Claude sessions (see bottom of file).`
- Update header comment line 9 from
  `#   * wires @cc-status / @cc-status-inbox to the CLI status sources,` to
  `#   * (status rows are wired by tmux.conf status-format slots, not here),`
- Leave everything else in the file untouched (keybindings, focus hook,
  `"$CMD" discover ... &` at the end).

Optionally clear the now-orphaned globals from the running server (harmless
either way; they die with the server):
`tmux set-option -gu @cc-status 2>/dev/null; tmux set-option -gu @cc-status-inbox 2>/dev/null`

**Verify**: `bash -n apps/cc-tmux/cc-tmux.tmux && grep -n "cc-status" apps/cc-tmux/cc-tmux.tmux; echo "grep_exit=$?"`
→ `bash -n` silent, grep prints NO lines, `grep_exit=1` (no matches).

### Step 5: Add self-tests for the two pure evaluators

In `apps/cc-tmux/src/cc_tmux/testing.py`, add four test functions before the
`# Runner` section (model them on `_test_cli_read_session_context`, lines
850–882 — same `_check(cond, msg)` idiom, no external runner):

```python
def _test_cli_evaluate_plugin_listing() -> None:
    raw = ('[{"id": "cc-tmux@cc-tmux", "version": "0.1.1", '
           '"enabled": true, "installPath": "/snap/0.1.1"}]')
    rows, install = cli._evaluate_plugin_listing(raw, "0.1.1")
    _check(install == "/snap/0.1.1", f"installPath not extracted: {install!r}")
    _check(rows[0] == ("PASS", "claude plugin enabled", "enabled (v0.1.1)"),
           f"enabled entry should PASS: {rows[0]!r}")
    _check(rows[1][0] == "PASS" and rows[1][1] == "plugin snapshot version",
           f"matching versions should PASS: {rows[1]!r}")


def _test_cli_evaluate_plugin_listing_degraded() -> None:
    raw_disabled = ('[{"id": "cc-tmux@cc-tmux", "version": "0.1.1", '
                    '"enabled": false, "installPath": "/snap/0.1.1"}]')
    rows, _ = cli._evaluate_plugin_listing(raw_disabled, "0.1.1")
    _check(rows[0][0] == "FAIL", f"disabled plugin must FAIL: {rows[0]!r}")

    raw_stale = ('[{"id": "cc-tmux@cc-tmux", "version": "0.1.0", '
                 '"enabled": true, "installPath": "/snap/0.1.0"}]')
    rows, _ = cli._evaluate_plugin_listing(raw_stale, "0.1.1")
    _check(rows[1][0] == "WARN", f"version mismatch must WARN: {rows[1]!r}")

    rows, install = cli._evaluate_plugin_listing("[]", "0.1.1")
    _check(rows[0][0] == "WARN" and install == "",
           f"missing entry must WARN with empty install: {rows!r}")

    rows, install = cli._evaluate_plugin_listing("not json", "0.1.1")
    _check(rows[0][0] == "WARN" and install == "",
           f"garbage input must WARN, never raise: {rows!r}")


def _test_cli_evaluate_hook_liveness() -> None:
    _check(cli._evaluate_hook_liveness(0, None, 1000.0)[0] == "INFO",
           "no live panes -> INFO")
    _check(cli._evaluate_hook_liveness(2, None, 1000.0)[0] == "FAIL",
           "live panes + no register evidence -> FAIL")
    _check(cli._evaluate_hook_liveness(2, 0.0, 1000.0)[0] == "FAIL",
           "zero/invalid ts counts as no evidence -> FAIL")


def _test_cli_evaluate_hook_liveness_ages() -> None:
    now = 100000.0
    _check(cli._evaluate_hook_liveness(1, now - 60.0, now)[0] == "PASS",
           "1-min-old register -> PASS")
    _check(cli._evaluate_hook_liveness(1, now - 1800.0, now)[0] == "PASS",
           "exactly at threshold -> PASS (boundary inclusive)")
    _check(cli._evaluate_hook_liveness(1, now - 3600.0, now)[0] == "WARN",
           "1-hour-old register with live panes -> WARN")
```

Register them at the END of the `_TESTS` list (after
`("cli.read_session_context", _test_cli_read_session_context),`):

```python
    ("cli.evaluate_plugin_listing", _test_cli_evaluate_plugin_listing),
    ("cli.evaluate_plugin_listing_degraded", _test_cli_evaluate_plugin_listing_degraded),
    ("cli.evaluate_hook_liveness", _test_cli_evaluate_hook_liveness),
    ("cli.evaluate_hook_liveness_ages", _test_cli_evaluate_hook_liveness_ages),
```

**Verify**: `apps/cc-tmux/bin/cc-tmux self-test` →
`cc-tmux self-test: 46/46 passed`, exit 0.

### Step 6: Full gates + commit

1. `apps/cc-tmux/bin/cc-tmux self-test` → `46/46 passed`, exit 0.
2. `apps/cc-tmux/bin/cc-tmux doctor; echo "exit=$?"` → all four new rows
   present, `exit=0`. Paste this output into your completion report — it is
   the runtime evidence for the doctor changes.
3. `git status --porcelain apps/cc-tmux` → exactly
   `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/testing.py`,
   `apps/cc-tmux/cc-tmux.tmux` modified, nothing else.
4. Write the commit message to a temp file, then:
   `git add apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/cc-tmux.tmux plans/README.md && git commit -F <msgfile>`
   (the repo's pre-commit hook may re-stage beads files — that is expected).
   Do not push unless instructed.

### Optional operator-gated runtime probe (do NOT run without explicit operator approval)

The FAIL path of the enabled-row can be proven live by toggling the plugin:

```bash
claude plugin disable cc-tmux@cc-tmux
apps/cc-tmux/bin/cc-tmux doctor        # expect: FAIL claude plugin enabled ...
claude plugin enable cc-tmux@cc-tmux   # RE-ENABLE IMMEDIATELY
apps/cc-tmux/bin/cc-tmux doctor        # expect: PASS claude plugin enabled ...
```

This briefly kills hooks for every running Claude session, so it requires
operator approval. Without it, the pure-function tests in Step 5 plus the live
PASS rows in Step 6 are the accepted evidence.

## Test plan

- New tests (Step 5), all in `apps/cc-tmux/src/cc_tmux/testing.py`, modeled
  structurally on `_test_cli_read_session_context` (testing.py:850–882):
  - `_test_cli_evaluate_plugin_listing` — happy path: enabled + version match
    + installPath extraction.
  - `_test_cli_evaluate_plugin_listing_degraded` — the regression this plan
    fixes (disabled → FAIL), version mismatch → WARN, missing entry → WARN,
    garbage JSON → WARN without raising (fail-open).
  - `_test_cli_evaluate_hook_liveness` — no-live-panes INFO, the outage
    signature (live panes + zero register evidence) → FAIL, invalid ts → FAIL.
  - `_test_cli_evaluate_hook_liveness_ages` — fresh PASS, boundary-inclusive
    PASS at exactly 1800s, stale WARN.
- Verification: `apps/cc-tmux/bin/cc-tmux self-test` → exit 0,
  `46/46 passed` (42 baseline + 4 new).
- Runtime: doctor output pasted (Step 6.2) + the `@cc-last-reconcile` stamp
  probe (Step 3 verify).

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `apps/cc-tmux/bin/cc-tmux self-test` exits 0 and prints `46/46 passed`
- [ ] `apps/cc-tmux/bin/cc-tmux doctor` exits 0 and its output contains all
      four strings: `claude plugin enabled`, `plugin snapshot version`,
      `plugin snapshot src`, `hook liveness`
- [ ] `grep -c "claude plugin registered" apps/cc-tmux/src/cc_tmux/cli.py` → `0`
- [ ] `grep -n "cc-status" apps/cc-tmux/cc-tmux.tmux` → no matches (exit 1)
- [ ] `grep -c "tmux.reconcile(_pane_ids_running_claude)" apps/cc-tmux/src/cc_tmux/cli.py` → `5` (the 4 existing sites + cmd_tabs_row)
- [ ] `grep -c "de-facto reconcile heartbeat" apps/cc-tmux/src/cc_tmux/cli.py` → `0`
- [ ] Inside tmux: `tmux set-option -gu @cc-last-reconcile; apps/cc-tmux/bin/cc-tmux tabs-row >/dev/null; tmux show-option -gv @cc-last-reconcile` prints an epoch float
- [ ] `git status --porcelain apps/cc-tmux` lists only the three in-scope files
- [ ] `plans/README.md` status row updated
- [ ] `apps/cc-tmux/.claude-plugin/plugin.json` is UNCHANGED (`git diff --stat -- apps/cc-tmux/.claude-plugin/` → empty)

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `60a1441`, or the code
  at the cited lines does not match the "Current state" excerpts.
- `claude plugin list --json` on this machine no longer emits a JSON array of
  objects with `id`/`enabled`/`version`/`installPath` keys (CLI format drift —
  the parse design in Step 1 would silently degrade to WARN; the plan's value
  depends on the boolean).
- A verification fails twice after a reasonable fix attempt (especially the
  `@cc-last-reconcile` probe in Step 3 — if reconcile does not stamp, do NOT
  start editing tmux.py; it is out of scope).
- The fix appears to require touching `hooks.json` (plan 002), `render.py` or
  a render-pipeline restructure (plan 005), or `tmux.py`.
- Another session has already modified `cmd_tabs_row` into a consolidated
  render entrypoint (plan 005's "render-all") — the reconcile call then
  belongs in that new entrypoint, which is plan 005's decision. Report instead
  of merging the two designs yourself.
- Baseline self-test is not `42/42 passed` before you start.

## Cross-plan coordination

- **Plan 002** owns `hooks/hooks.json` matcher changes — never touch that file
  here, even if a hook matcher looks wrong.
- **Plan 005** owns render-perf consolidation. Known conflict point:
  `cmd_tabs_row`. If 005 lands first and replaces/absorbs `cmd_tabs_row`, the
  Step 3 reconcile call moves to 005's consolidated per-tick entrypoint;
  reference plan 005 in your report rather than adapting.
- **Plan 004** (nexus repo, `~/dev/personal/nexus`) is a separate repo with its
  own remote and commit rules — nothing in this plan touches it.

## Maintenance notes

- The 30-min liveness threshold (`_HOOK_LIVENESS_STALE_SECS`) assumes
  hooks.json keeps firing `register` on UserPromptSubmit/PostToolUse/Stop. If
  plan 002 prunes those matchers substantially, revisit the threshold.
- The `plugin snapshot src` digest row is the tripwire for the dual-install
  split: any repo commit that changes `src/**.py` will make it WARN until the
  operator bumps the plugin version and runs `claude plugin update
  cc-tmux@cc-tmux`. That WARN is the row working as designed, not a bug —
  reviewers should expect it to fire after every cc-tmux src change. Note:
  because THIS plan changes `cli.py`, the row will WARN immediately after this
  plan lands, until the next operator-gated version bump + update. Say so in
  the completion report so it is not mistaken for a defect.
- Reviewer scrutiny points: (1) the doctor must still ALWAYS exit 0 — every
  new block is wrapped in try/except with WARN fallback; (2) the reconcile
  call in `cmd_tabs_row` must stay ABOVE `get_window_tabs()` so a heal is
  visible in the same tick; (3) no new state files were introduced (the trace
  log already existed; it gained a reader, not a writer).
- Deferred out of this plan: deleting the `status`/`status-inbox` subcommands
  entirely (candidate for a later entropy pass once plan 005 settles the
  render surface); a doctor row for the session-context writer seam
  (nexus-statusline liveness — MLP-3's part (b)) belongs with plan 004's
  cross-repo work.
