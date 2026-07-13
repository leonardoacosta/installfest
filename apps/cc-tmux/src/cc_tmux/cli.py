"""CLI handlers and dispatch for ``cc-tmux``.

Each subcommand maps to a ``cmd_<name>()`` handler returning an int exit code.
:func:`main` wires argparse (from :mod:`cc_tmux.parser`) to the dispatch table.

Fail-open contract (invariant 5): a handler invoked by a Claude hook or a tmux
keybinding must never crash the caller. :func:`main` swallows handler exceptions
and returns 0 for every command EXCEPT ``self-test`` (whose whole job is to
report failure via a non-zero exit).
"""

from __future__ import annotations

import json
import os
import re
import select
import subprocess
import sys
import time
from pathlib import Path
from typing import Callable, Dict, List, Optional, Tuple

from . import log, notify, nx_agent, registry, render, tmux, usage
from .conductor import cmd_conductor
from .parser import build_parser
from .usage import cmd_usage
from .priority import (
    VALID_STATES,
    group_by_state,
    pending_panes,
    select_next,
    sort_panes,
)

# Global navigation breadcrumb (NOT tracked-pane state — see invariant 1). Records
# the pane we hopped away from so `back` can return to it.
_PREV_PANE_OPT = "@cc-prev-pane"
_CYCLE_MODE_OPT = "@cc-cycle-mode"

# View / rendering options (Req-5/Req-7).
_TRACK_FOCUS_OPT = "@cc-track-focus"           # MRU visit tracking (default on)
_INBOX_CLEARED_OPT = "@cc-inbox-cleared-at"   # dismiss stamp (a view filter)
_STATUS_FORMAT_OPT = "@cc-status-format"       # @cc-status template
_WINDOW_RENAME_OPT = "@cc-window-rename"       # opt-in window auto-rename
_WINDOW_RENAME_FORMAT_OPT = "@cc-window-rename-format"  # "state" (default) | "title"
_TAB_NAME_MAX = 20                             # project-code + session-title combined budget
_STATUS_INBOX_STYLE_OPT = "@cc-status-inbox-{state}-style"  # per-state badge style

# Sub-agent tab-icon overlay (cc-tmux-subagent-tab-icon): how long a background
# (run_in_background=true) Task dispatch counts as "active" after launch, since
# no hook signals its completion (see proposal.md's mechanism finding). "a few
# minutes" per tasks.md task 2.2 -> 300s (5 min), overridable per the
# `_TRACK_FOCUS_OPT`-style global-option-with-default idiom used throughout
# this module.
_SUBAGENT_BG_TIMEOUT_OPT = "@cc-subagent-bg-timeout"
_DEFAULT_SUBAGENT_BG_TIMEOUT = 300.0

# No remaining stub subcommands: every registered command has a handler below.
_STUB_OWNERS: Dict[str, str] = {}


# ---------------------------------------------------------------------------
# Implemented handlers
# ---------------------------------------------------------------------------

def cmd_register(args) -> int:
    """Record a pane's Claude state. The Claude-hook entrypoint (Req-3)."""
    pane = args.pane or tmux.current_pane_id()
    if not pane:
        return 0  # fail open: no pane context, nothing to record

    # set_pane_state returns whether this was a REAL transition (invariant 3).
    changed = tmux.set_pane_state(
        pane,
        args.state,
        task=args.task,
        wait_reason=args.reason,
    )
    # OS notification + terminal focus fire ONLY on a real transition (Req-6).
    notify.react(pane, args.state, changed)

    # SessionStart hook payloads carry the resolved session title (custom, via
    # /rename or -n, else Claude's own default) — capture it for the opt-in
    # `title` window-rename format (tab naming). We also capture session_id here
    # (-> @cc-session-id) so nx_agent lookups can key on it; session_id is stable
    # for the session's lifetime and SessionStart re-fires on resume/clear/compact,
    # so scoping capture to SessionStart (mirroring session_title) is sufficient.
    # Every other hook event is ignored here since only SessionStart's payload is
    # treated as authoritative for these fields.
    #
    # NOTE (cc-tmux-bar-cleanup): the model letter used to be captured here too
    # (SessionStart payload `model` field -> @cc-model), but that path was
    # confirmed empty on every live pane (the field is absent/unusable at
    # SessionStart, and it misses mid-session `/model` switches by design).
    # The session-bar now reads the model letter fresh on every render from
    # `session-context.<pane>.json` instead — see `_read_session_context`.
    hook_payload = _read_hook_stdin()
    if hook_payload.get("hook_event_name") == "SessionStart":
        title = hook_payload.get("session_title")
        if title:
            tmux.set_pane_title(pane, title)
        session_id = hook_payload.get("session_id")
        if session_id:
            tmux._set_opt(pane, tmux.OPT_SESSION_ID, session_id)

    # Sub-agent dispatch tracking (cc-tmux-subagent-tab-icon): the mechanism is
    # the Task tool's own PreToolUse/PostToolUse pair (hooks.json), NOT
    # SubagentStart/Stop — SubagentStop never fires on this Claude Code version
    # (see proposal.md "Critical mechanism finding"). A foreground (blocking)
    # dispatch is exact: --subagent-start increments @cc-subagent-fg,
    # --subagent-stop decrements it. A background dispatch
    # (tool_input.run_in_background: true) has no reliable completion signal —
    # its own PostToolUse fires almost immediately once launched, not when it
    # finishes — so a background start is appended to the time-boxed
    # @cc-subagent-bg heuristic instead of the fg counter, and aged out lazily
    # on every read via prune_background_entries (below), never via an
    # explicit stop.
    if getattr(args, "subagent_start", False):
        tool_input = hook_payload.get("tool_input")
        run_in_background = bool(
            isinstance(tool_input, dict) and tool_input.get("run_in_background")
        )
        if run_in_background:
            tmux.append_subagent_bg(pane, time.time())
        else:
            tmux.increment_subagent_fg(pane)
    if getattr(args, "subagent_stop", False):
        # A PostToolUse Task return has no reliable way to know whether the
        # ENDING dispatch was foreground or background (the payload shape
        # doesn't carry that info cleanly at stop time, and either kind can
        # return here) — but only a FOREGROUND start ever increments
        # @cc-subagent-fg in the first place, so unconditionally decrementing
        # (floored at 0 via tmux.decrement_subagent_fg) is correct: a
        # background dispatch's stop is a harmless no-op against a counter it
        # never touched, and background activity ages out of @cc-subagent-bg
        # via the timeout instead, never via this stop signal.
        tmux.decrement_subagent_fg(pane)

    # Opt-in window auto-rename tracks the highest-priority state in the window (Req-7).
    # _maybe_rename_window now reports actual tmux success/failure (not just
    # "issued"), so the same return value threads into both the existing
    # rename_fired field and the new rename_succeeded field below.
    rename_fired = _maybe_rename_window(pane)
    _trace_register(hook_payload, pane, rename_fired, rename_succeeded=rename_fired)
    return 0


def _read_hook_stdin() -> dict:
    """Best-effort parse of the Claude Code hook JSON payload from stdin.

    Every ``cc-tmux register`` invocation wired from ``hooks.json`` gets the
    hook's event JSON piped to stdin; the same binary also runs interactively
    (keybindings, manual testing) with no stdin data at all. This must never
    block either path — a short ``select()`` poll distinguishes "data already
    buffered" from "no pipe" instead of calling the blocking ``read()``
    unconditionally. Fail-open: any error, timeout, or non-JSON input -> {}.
    """
    try:
        if sys.stdin.isatty():
            return {}
        ready, _, _ = select.select([sys.stdin], [], [], 0.05)
        if not ready:
            return {}
        raw = sys.stdin.read()
        return json.loads(raw) if raw.strip() else {}
    except Exception:
        return {}


_REGISTER_TRACE_FILE = "cc-tmux-register-trace.log"
_REGISTER_TRACE_MAX_LINES = 2000
# Trim only when the file exceeds ~2x its line cap (2000 lines ~= 260 KB
# observed; 512 KB ~= 4000 lines). The common path is then a single O(1)
# O_APPEND write instead of a full read+rewrite per hook fire — which also
# removes the read-modify-write lost-line race between concurrent pane hooks
# on the hot path (the rare trim can still race; acceptable for a diagnostic).
_REGISTER_TRACE_TRIM_BYTES = 512 * 1024


def trace_needs_trim(size_bytes: int, threshold: int = _REGISTER_TRACE_TRIM_BYTES) -> bool:
    """Pure gate: trim the register trace only past the byte threshold."""
    return size_bytes > threshold


def _trace_register(
    hook_payload: dict, pane_id: Optional[str], rename_fired: bool, *, rename_succeeded: bool
) -> None:
    """Append one diagnostic JSON line per ``register`` call (rename-trigger debugging).

    Written to ``<state-dir>/cc-tmux-register-trace.log`` (:func:`_cc_state_dir`,
    same base dir as the session-context / roadmap-pulse caches) so a
    multi-hour session's real hook traffic can be correlated against whether
    :func:`_maybe_rename_window` actually renamed the window on each call.
    ``rename_attempted`` is hardcoded true here since ``cmd_register`` calls
    ``_maybe_rename_window`` unconditionally — kept as an explicit field (not
    inlined into ``rename_fired``) in case that call ever becomes conditional.
    ``rename_succeeded`` is distinct from ``rename_fired``/``rename_attempted``:
    it reports whether the ``rename-window`` tmux command actually succeeded
    (``_maybe_rename_window``'s ``_run_tmux``-backed return value, per the
    rename-fix in this same change) rather than merely "was a rename call
    issued this hook fire" — so a transient tmux failure (stale pane id, a
    race with the window closing, a non-zero ``rename-window`` exit) now shows
    up in the trace as ``rename_succeeded: false`` instead of looking
    identical to a real success.
    Append-by-default (plan 005): the common path is a single O_APPEND write;
    trimming to the last ``_REGISTER_TRACE_MAX_LINES`` lines (atomic via
    ``.tmp`` + ``os.replace`` — same pattern as ``conductor.write_instructions``)
    only happens once the file passes :func:`trace_needs_trim`'s byte
    threshold, so the file never grows unboundedly. This is a diagnostic trace
    file, NOT part of the invariant-1 pane-option state store — fail-open: any
    error here must never break ``cmd_register``.
    """
    try:
        entry = {
            "ts": time.time(),
            "hook_event_name": hook_payload.get("hook_event_name"),
            "pane_id": pane_id or None,
            "rename_attempted": True,
            "rename_fired": rename_fired,
            "rename_succeeded": rename_succeeded,
        }
        path = _cc_state_dir() / _REGISTER_TRACE_FILE
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry, sort_keys=True) + "\n")
        if trace_needs_trim(path.stat().st_size):
            lines = path.read_text(encoding="utf-8").splitlines()
            lines = lines[-_REGISTER_TRACE_MAX_LINES:]
            tmp = path.with_suffix(path.suffix + ".tmp")
            tmp.write_text("\n".join(lines) + "\n", encoding="utf-8")
            os.replace(tmp, path)
    except Exception:
        pass


def cmd_clear(args) -> int:
    """Clear a pane's @cc-* state (SessionEnd hook)."""
    pane = args.pane or tmux.current_pane_id()
    if not pane:
        return 0
    tmux.clear_pane_state(pane)
    return 0


def cmd_cycle(args) -> int:
    """Hop to the next pending pane in attention-priority order (Req-4)."""
    mode = args.mode or tmux.get_global_option(_CYCLE_MODE_OPT) or "priority"
    panes = tmux.reconcile(_pane_ids_running_claude)
    current = tmux.current_pane_id()
    target = select_next(panes, current, mode)
    if target is None:
        return 0
    if current and current != target.id:
        tmux.set_global_option(_PREV_PANE_OPT, current)
    tmux.switch_to_pane(target.id)
    return 0


def cmd_back(args) -> int:
    """Jump back to the previously-focused pane across sessions/windows (Req-4)."""
    prev = tmux.get_global_option(_PREV_PANE_OPT)
    if not prev:
        return 0
    current = tmux.current_pane_id()
    # Swap the breadcrumb so `back` is itself reversible.
    if current and current != prev:
        tmux.set_global_option(_PREV_PANE_OPT, current)
    tmux.switch_to_pane(prev)
    return 0


def cmd_switch(args) -> int:
    """Focus a specific pane, recording the breadcrumb for `back`."""
    current = tmux.current_pane_id()
    if current and current != args.pane:
        tmux.set_global_option(_PREV_PANE_OPT, current)
    tmux.switch_to_pane(args.pane)
    return 0


def cmd_focus(args) -> int:
    """Record a pane visit for the MRU recency tiebreak (Decision 2).

    Invoked by the ``pane-focus-in[9909]`` hook on every focus. Stamps
    ``@cc-visited`` IFF the pane is tracked (carries a valid ``@cc-state``);
    silent no-op for untracked panes so a plain terminal focus writes nothing.
    Fail-open: never crashes the hook.
    """
    pane = getattr(args, "pane_id", "")
    if not pane:
        return 0
    if tmux.get_pane_option(pane, tmux.OPT_STATE) not in VALID_STATES:
        return 0  # untracked pane -> do not stamp
    tmux.set_pane_visited(pane)
    return 0


def cmd_discover(args) -> int:
    """Auto-register already-running Claude sessions on plugin load (Req-11)."""
    rows = tmux.iter_panes_with_process()
    claude_ids = _pane_ids_running_claude(rows)
    registered = 0
    for row in rows:
        if row["id"] not in claude_ids:
            continue
        if row["state"] in VALID_STATES:
            continue  # already tracked, leave it
        # idle is the correct initial state for an already-running session.
        tmux.set_pane_state(row["id"], "idle")
        registered += 1
    if not args.quiet:
        print(f"cc-tmux: registered {registered} running Claude session(s)")
    return 0


def cmd_self_test(args) -> int:
    """Run the built-in pure-function test suite (Req-13, task 1.13)."""
    from .testing import run_self_test

    return run_self_test(verbose=args.verbose)


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


_HOOK_STALE_AFTER_SECS = 1800.0  # 30 min without any @cc-timestamp movement


def hook_freshness(timestamps: List[float], now: float,
                   stale_after: float = _HOOK_STALE_AFTER_SECS) -> str:
    """'none' (no tracked panes), 'fresh', or 'stale' for the doctor liveness row.

    Pure (self-tested). 'stale' means tracked panes exist but the NEWEST
    @cc-timestamp is older than ``stale_after`` - the disabled-plugin
    signature: panes still carry state while no hook has written for ages.
    """
    real = [t for t in timestamps if t > 0]
    if not real:
        return "none"
    return "fresh" if (now - max(real)) <= stale_after else "stale"


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


def cmd_doctor(args) -> int:
    """Environment diagnostics checklist — PASS/FAIL/WARN rows, ALWAYS exit 0.

    Diagnostics, not tests (design.md Decision 4): doctor does environment I/O and
    reports prose; ``self-test`` runs pure asserts and exits non-zero on failure.
    Opposite exit contracts, deliberately separate subcommands.
    """
    import re
    import shutil
    import sys
    from pathlib import Path

    rows: List[tuple] = []

    def add(status: str, label: str, detail: str = "") -> None:
        rows.append((status, label, detail))

    # tmux >= 3.2 (parse `tmux -V`; strip the trailing letter suffix like 3.6a).
    tmux_bin = shutil.which("tmux")
    if not tmux_bin:
        add("FAIL", "tmux >= 3.2", "tmux not found on PATH")
    else:
        ver_out = tmux._run_tmux(["-V"], check_available=False) or ""
        ver = re.sub(r"[^0-9.]", "", ver_out)
        parts = ver.split(".") if ver else []
        try:
            major = int(parts[0])
            minor = int(parts[1]) if len(parts) > 1 else 0
            ok = major > 3 or (major == 3 and minor >= 2)
            add("PASS" if ok else "FAIL", "tmux >= 3.2", ver_out or ver)
        except (ValueError, IndexError):
            add("WARN", "tmux >= 3.2", f"could not parse version: {ver_out!r}")

    # fzf on PATH
    fzf = shutil.which("fzf")
    add("PASS", "fzf on PATH", fzf) if fzf else add("WARN", "fzf on PATH", "fzf not found (menu fallback used)")

    # python >= 3.10
    py_ok = sys.version_info >= (3, 10)
    py_ver = f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}"
    add("PASS" if py_ok else "FAIL", "python >= 3.10", py_ver)

    # $TMUX set
    in_tmux = bool(os.environ.get("TMUX"))
    add("PASS", "$TMUX set", os.environ.get("TMUX", "")) if in_tmux else add("FAIL", "$TMUX set", "not running inside tmux")

    # ~/.tmux/plugins/cc-tmux symlink resolves
    link = Path.home() / ".tmux" / "plugins" / "cc-tmux"
    try:
        if not link.exists():
            add("WARN", "plugin symlink", f"{link} absent (may run from source tree)")
        elif link.is_symlink():
            add("PASS", "plugin symlink", f"{link} -> {link.resolve()}")
        else:
            add("PASS", "plugin symlink", f"{link} (real copy)")
    except OSError as exc:
        add("FAIL", "plugin symlink", f"{link}: {exc}")

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

    # pane-focus-in[9909] hook present when @cc-track-focus is on
    if not tmux.tmux_available():
        add("WARN", "focus hook", "tmux unavailable (skipped)")
    else:
        track = tmux.get_global_option(_TRACK_FOCUS_OPT)
        tracking_on = track.strip().lower() not in ("off", "0", "false", "no")
        if not tracking_on:
            add("WARN", "focus hook", "@cc-track-focus off (visit tracking disabled)")
        else:
            hooks = tmux._run_tmux(["show-hooks", "-g", "pane-focus-in"], check_available=False) or ""
            if "pane-focus-in[9909]" in hooks:
                add("PASS", "focus hook", "pane-focus-in[9909] installed")
            else:
                add("FAIL", "focus hook", "pane-focus-in[9909] missing (reload the plugin)")

    # tracked-pane count
    panes = tmux.get_hop_panes()
    count = len(panes)
    add("INFO", "tracked panes", str(count))

    # Live Claude-process count — computed once, shared by both hook rows below.
    # live_exc (not None) means the check itself failed; surfaced by the "hook
    # liveness" row exactly as before this refactor.
    live_exc: Optional[Exception] = None
    try:
        live: Optional[int] = len(_pane_ids_running_claude(tmux.iter_panes_with_process()))
    except Exception as exc:  # noqa: BLE001 - surfaced by hook liveness row below
        live = None
        live_exc = exc

    # hook freshness — pure per-pane @cc-timestamp verdict (plan 005 RPF-3
    # follow-up): a different, simpler signal than the "hook liveness" row
    # below (which needs the live Claude process count + register-trace file).
    # 'stale' is the disabled-plugin signature IF Claude is actively running
    # right now — gated on live-Claude-count (if-s0go) because per-pane
    # @cc-timestamp age alone is NOT a valid staleness signal on its own (an
    # idle-but-healthy pane legitimately freezes for hours with no hook firing).
    verdict = hook_freshness([p.timestamp for p in panes], time.time())
    if verdict == "none":
        add("INFO", "hook freshness", "no tracked panes (nothing to assess)")
    elif verdict == "fresh":
        newest_ts = max(p.timestamp for p in panes if p.timestamp > 0)
        add("PASS", "hook freshness", f"newest @cc-timestamp {int(time.time() - newest_ts)}s ago")
    elif live:
        add("WARN", "hook freshness",
            "newest @cc-timestamp > 30min old with Claude actively running - "
            "hooks may not be reaching cc-tmux (check plugin enabled + version)")
    else:
        add("INFO", "hook freshness",
            "newest @cc-timestamp > 30min old, but no Claude pane is currently "
            "running - likely a legitimately idle pane, not a dead hook")

    # hook liveness — is the Claude hook side writing state while panes run?
    try:
        if live_exc is not None:
            raise live_exc
        candidates: List[float] = [p.timestamp for p in panes if p.timestamp > 0]
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

    # -- render ---------------------------------------------------------------
    print("cc-tmux doctor")
    width = max((len(label) for _s, label, _d in rows), default=0)
    for status, label, detail in rows:
        line = f"  {status:<4} {label:<{width}}"
        if detail:
            line += f"  {detail}"
        print(line)
    return 0


# ---------------------------------------------------------------------------
# Inbox / picker (Req-5, task 1.6)
# ---------------------------------------------------------------------------

def cmd_inbox(args) -> int:
    """Emit the notification-inbox rows (attention first, dismiss-filtered).

    Data only: ``label\\tpane_id`` per line, consumed by the fzf popup / menu in
    the tmux entrypoint. ``ctrl-x`` in that view runs ``inbox-clear`` (a view
    filter, never a state mutation — invariant 2), so status counts are
    unaffected and active rows always stay visible.
    """
    panes = tmux.reconcile(_pane_ids_running_claude)
    cleared_at = _float_opt(_INBOX_CLEARED_OPT)
    visible = []
    for pane in sort_panes(panes):
        # active is never hidden; waiting/idle hide only if dismissed (older than
        # the cleared-at stamp). A fresh transition (newer timestamp) reappears.
        if pane.state == "active" or pane.timestamp > cleared_at:
            visible.append(pane)
    _emit_rows(visible)
    return 0


def cmd_inbox_clear(args) -> int:
    """Dismiss the current waiting/idle inbox entries (a view filter, Req-5).

    Bumps a global cleared-at stamp; the inbox hides pending panes whose state
    predates it. NEVER mutates pane state, so status counts are untouched.
    """
    tmux.set_global_option(_INBOX_CLEARED_OPT, str(time.time()))
    return 0


def cmd_picker_data(args) -> int:
    """Emit every tracked pane for the jump-to picker (``label\\tpane_id``).

    Unlike the inbox, the picker is unfiltered (no dismiss stamp) so any pane —
    including ``active`` — can be jumped to.
    """
    _emit_rows(sort_panes(tmux.reconcile(_pane_ids_running_claude)))
    return 0


# ---------------------------------------------------------------------------
# Status sources + window rename (Req-7, task 1.8)
# ---------------------------------------------------------------------------

def cmd_status(args) -> int:
    """Emit the status-bar pane counts via ``@cc-status-format`` (Req-7).

    NOT wired by default: cc-tmux.tmux no longer publishes ``@cc-status``
    (the option had zero consumers). Users may wire ``#(cc-tmux status)``
    into their own status line manually. The per-tick reconcile heartbeat
    lives in :func:`cmd_tabs_row` (status-format[0]); the reconcile call
    below is kept (rate-limited) for anyone who does wire this surface.
    """
    groups = group_by_state(tmux.reconcile(_pane_ids_running_claude))
    counts = {state: len(members) for state, members in groups.items()}
    fmt = tmux.get_global_option(_STATUS_FORMAT_OPT) or render.DEFAULT_STATUS_FORMAT
    icons = render.resolve_icons(tmux.get_global_option)
    out = render.render_status(fmt, counts, icons)
    if out:
        print(out)
    return 0


def cmd_status_inbox(args) -> int:
    """Emit clickable pending-pane badges for an optional second status line.

    Each badge is wrapped in ``#[range=pane|<id>]`` so tmux's default
    ``MouseDown1Status`` switch-client gives click-to-hop for free. Per-state
    styling comes from ``@cc-status-inbox-<state>-style``.
    """
    icons = render.resolve_icons(tmux.get_global_option)
    badges: List[str] = []
    for pane in pending_panes(tmux.get_hop_panes()):
        style = tmux.get_global_option(_STATUS_INBOX_STYLE_OPT.format(state=pane.state))
        icon = icons.get(pane.state, pane.state)
        label = pane.project or pane.session
        prefix = f"#[{style}]" if style else ""
        badges.append(
            f"#[range=pane|{pane.id}]{prefix} {icon} {label} #[norange]#[default]"
        )
    if badges:
        print(" ".join(badges))
    return 0


# ---------------------------------------------------------------------------
# View helpers (shared by inbox / picker / status)
# ---------------------------------------------------------------------------

def _emit_rows(panes: List["tmux.PaneInfo"]) -> None:
    """Print aligned ``label\\tpane_id`` rows for fzf / menu consumption."""
    icons = render.resolve_icons(tmux.get_global_option)
    for label, pane_id in render.inbox_rows(panes, icons, time.time()):
        print(f"{label}\t{pane_id}")


def _float_opt(option: str) -> float:
    raw = tmux.get_global_option(option)
    try:
        return float(raw) if raw else 0.0
    except ValueError:
        return 0.0


def _truthy(value: str) -> bool:
    return value.strip().lower() in ("1", "on", "true", "yes")


# ---------------------------------------------------------------------------
# Sub-agent tab-icon overlay helpers (cc-tmux-subagent-tab-icon)
# ---------------------------------------------------------------------------

def prune_background_entries(entries: List[float], now: float, timeout: float) -> List[float]:
    """Pure: drop background-dispatch launch epochs older than ``timeout`` seconds.

    Called on every READ of ``@cc-subagent-bg`` (never only on write) so aging
    is lazy and correct even with zero further hook activity — a background
    agent that silently dies leaves no stop signal, so this timeout is the
    ONLY cleanup mechanism (proposal.md). Pure and unit-testable (no tmux).
    """
    return [t for t in entries if (now - t) <= timeout]


def _subagent_bg_timeout() -> float:
    """``@cc-subagent-bg-timeout`` override, or the default (see the option's
    docstring above ``_SUBAGENT_BG_TIMEOUT_OPT``). Mirrors tmux.py's
    ``_reconcile_interval`` global-option-with-default idiom."""
    raw = tmux.get_global_option(_SUBAGENT_BG_TIMEOUT_OPT)
    try:
        val = float(raw) if raw else _DEFAULT_SUBAGENT_BG_TIMEOUT
    except ValueError:
        val = _DEFAULT_SUBAGENT_BG_TIMEOUT
    return val if val > 0 else _DEFAULT_SUBAGENT_BG_TIMEOUT


def _window_subagent_counts(window_target: str) -> Tuple[int, int]:
    """``(fg_count, pruned_bg_count)`` for ``window_target``'s representative pane.

    Legacy call site (:func:`cmd_window_icon` — kept for confs that haven't
    migrated to the ``render-all``/tabs-row job, see that function's
    docstring). Uses the SAME representative-pane choice
    (:func:`tmux.get_window_top_pane`) that :func:`cmd_window_icon` already
    scopes its state lookup to, unlike the live tabs-row path
    (:func:`_build_tabs_row`) which sums/unions across every tracked pane in
    the window via :func:`tmux.get_window_tabs` — single-pane here vs
    multi-pane there is intentional: this is the narrower, single-pane legacy
    surface. Fail-open: no representative pane -> ``(0, 0)``.
    """
    pane = tmux.get_window_top_pane(window_target)
    if not pane:
        return 0, 0
    fg = tmux.get_subagent_fg(pane)
    bg_entries = prune_background_entries(
        tmux.get_subagent_bg(pane), time.time(), _subagent_bg_timeout()
    )
    return fg, len(bg_entries)


def _maybe_rename_window(pane_id: str) -> bool:
    """Rename a pane's window when ``@cc-window-rename`` is on (default off, Req-7).

    Two formats, selected by ``@cc-window-rename-format`` (default ``state``):

    - ``state`` (default): the dir basename alone.
    - ``title``: ``<project-code>·<session-title>``, hard-truncated to
      ``_TAB_NAME_MAX`` chars combined — see :func:`_title_window_name`.

    NOTE: neither format includes a state icon here anymore. The tab icon is
    now animated (see ``render.animated_icon`` / ``cmd_window_icon``), which
    needs a wall-clock-driven re-render that a hook-triggered ``rename-window``
    call cannot provide (hooks fire irregularly, not on a timer). The icon is
    rendered separately, from the tmux ``window-status-format`` string itself
    (``#(cc-tmux window-icon #{window_id})``), re-evaluated on every
    status-bar refresh — see tmux.conf.tmpl / the theme ``.conf`` files.

    ``automatic-rename`` is forced off either way so tmux does not clobber the
    name tmux-conf itself already disables (see tmux.conf.tmpl).

    Returns ``True`` iff ``rename-window`` was actually issued *and tmux
    confirmed it* (``_run_tmux``'s ``None``-on-failure contract), ``False``
    for every early-return path (opt-out, untracked pane, no siblings, empty
    resolved name) as well as an issued-but-failed ``rename-window`` call —
    used by :func:`_trace_register` to distinguish "ran but declined" and
    "ran, tmux rejected it" from "renamed" in the diagnostic trace.
    """
    if not _truthy(tmux.get_global_option(_WINDOW_RENAME_OPT)):
        return False
    panes = tmux.get_hop_panes()
    me = next((p for p in panes if p.id == pane_id), None)
    if me is None:
        return False
    siblings = [p for p in panes if p.session == me.session and p.window == me.window]
    if not siblings:
        return False

    fmt = (tmux.get_global_option(_WINDOW_RENAME_FORMAT_OPT) or "state").strip().lower()
    if fmt == "title":
        name = _title_window_name(me)
    else:
        cwd = tmux._run_tmux(
            ["display-message", "-p", "-t", pane_id, "#{pane_current_path}"]
        )
        name = os.path.basename(os.path.normpath(cwd)) if cwd else (me.project or "")

    if not name:
        return False
    tmux._run_tmux(["set-window-option", "-t", pane_id, "automatic-rename", "off"])
    result = tmux._run_tmux(["rename-window", "-t", pane_id, name])
    return result is not None


_TITLE_DIVIDER = "·"  # middle dot — visually distinct from the icon's own space


def compose_title_name(code: str, title: str, fallback: str = "") -> str:
    """``<code>·<title>`` hard-truncated to ``_TAB_NAME_MAX`` chars combined.

    Pure and unit-testable (no tmux). Falls back to whichever half resolved —
    ``code`` alone, ``title`` alone, or ``fallback`` (e.g. ``@cc-project``) —
    rather than going blank when only one piece is known. Does NOT include the
    leading state icon — that's prefixed by the caller (:func:`_maybe_rename_window`)
    outside this budget.
    """
    parts = [p for p in (code, title) if p]
    combined = _TITLE_DIVIDER.join(parts) if len(parts) > 1 else "".join(parts) or fallback
    return combined[:_TAB_NAME_MAX]


def _title_window_name(pane) -> str:
    """``<project-code>·<session-title>`` for the ``title`` window-rename format.

    ``code`` resolves from the dotfiles project registry (``registry.py``) by
    the pane's current directory; ``title`` is whatever ``@cc-title`` holds
    (set from the SessionStart hook payload in :func:`cmd_register`). The
    leading state icon is NOT included here — see :func:`_maybe_rename_window`.
    """
    cwd = tmux._run_tmux(["display-message", "-p", "-t", pane.id, "#{pane_current_path}"])
    code = registry.resolve_project_code(cwd) if cwd else ""
    title = tmux.get_pane_option(pane.id, tmux.OPT_TITLE)
    return compose_title_name(code, title, fallback=pane.project or "")


def cmd_window_icon(args) -> int:
    """Emit the current tab-icon glyph for a window (animated tab icon).

    Invoked FROM a tmux ``window-status-format``/``window-status-current-format``
    string (``#(cc-tmux window-icon #{window_id})``) — tmux re-runs this on
    every status-bar refresh, which is what drives the animation (this
    process holds no timer of its own). Prints ``"<glyph> "`` (with a trailing
    separator space) when the window has a tracked pane, or nothing at all for
    an untracked (non-Claude) window — the caller's format string relies on
    that to avoid a stray double-space, see tmux.conf.tmpl / theme .conf files.
    Fail-open: any error -> print nothing, exit 0 (never blocks the status bar).
    """
    state = tmux.get_window_top_state(args.window)
    if not state:
        return 0
    fg, bg = _window_subagent_counts(args.window)
    icon = render.resolve_tab_icon(state, time.time(), fg, bg)
    if icon:
        sys.stdout.write(f"{icon} ")
    return 0


# ---------------------------------------------------------------------------
# Session + beads status rows (cc-tmux-session-usage-bars, rows 2 + 3)
#
# Row 2 resolves the window's representative pane via _resolve_session_pane
# (tmux-active pane first, falling back to the priority pick used by the
# animated tab icon / get_window_top_*); row 3 has its own active-pane
# fallback (_beads_pane, BEADS-03). Both read plain values and hand them to
# render.py's pure composition functions. Fail-open throughout: every read
# degrades to a partial/empty render, never a raised exception.
# ---------------------------------------------------------------------------

def _cc_state_dir() -> Path:
    """The Claude state dir holding session-context / roadmap-pulse cache files.

    Honours ``CLAUDE_CONFIG_DIR`` (the standard CC config-dir override), else
    ``~/.claude``. Both cache families live under ``<config>/scripts/state``.
    """
    base = os.environ.get("CLAUDE_CONFIG_DIR", "").strip() or os.path.expanduser("~/.claude")
    return Path(base) / "scripts" / "state"


def _read_roadmap_pulse(pane_id: str) -> Tuple[str, Optional[float]]:
    """``(content, age_sec)`` of ``roadmap-pulse.<code>.line`` for ``pane_id``'s project.

    Resolves the pane's cwd (``#{pane_current_path}``) to a registry short code
    (longest-prefix match, NOT the ``@cc-project`` display name) and reads the
    matching roadmap-pulse cache line plus its mtime age (``time.time() -
    st_mtime``, floored at 0), so the caller can flag stale counts (plan 006 /
    BEADS-01). Any line starting with ``radar:`` is dropped here, defensively —
    the producer (``~/dev/cc`` ``roadmap-pulse``) stopped emitting that line in
    ``--line`` mode as of commit ``88d0558e``, but a stale/rolled-back cache
    file on disk could still carry one; stripping it at the read layer means
    every caller sees clean content regardless of producer version or cache
    age (cc-tmux-row3-openspec-beads-format task 2.1). Fail-open: no pane, no
    cwd, no code, missing/unreadable/empty file -> ``("", None)``; content
    readable but stat fails -> ``(content, None)``.
    """
    if not pane_id:
        return "", None
    try:
        cwd = tmux._run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_current_path}"])
        if not cwd:
            return "", None
        code = registry.resolve_project_code(cwd)
        if not code:
            return "", None
        path = _cc_state_dir() / f"roadmap-pulse.{code}.line"
        raw = path.read_text(encoding="utf-8").strip()
        content = "\n".join(
            ln for ln in raw.splitlines() if not ln.startswith("radar:")
        ).strip()
        try:
            age: Optional[float] = max(0.0, time.time() - path.stat().st_mtime)
        except Exception:
            age = None
        return content, age
    except Exception:
        return "", None


# session-context.<pane>.json freshness cutoff (plan 003): the writer
# (nexus-statusline) refreshes ts on every statusline render, i.e. every turn.
# A file older than this is a dead session or a recycled pane id — render it
# as absent rather than confidently wrong. Writer-side GC (>6h prune) is inert
# exactly when the writer stalls, so the reader enforces its own cutoff.
# Post-migration (cc-tmux-adopt-nx-context-and-git-status, then if-hrbd) this
# gate governs only the ``model_letter`` (index 0) return of
# ``_read_session_context`` — the only field any caller still reads from this
# legacy file. ``context_used_pct`` (index 1) and the branch/dirty/ahead
# fields it also carries are no longer consumed by any caller; SES is now
# sourced exclusively via :func:`_resolve_ses_pct` (nx-agent), for both row 2
# and the accounts popup.
SESSION_CONTEXT_MAX_AGE_SECS = 900.0


def _read_session_context(pane_id: str) -> Tuple[str, Optional[float], str, bool, int]:
    """``(model_letter, context_used_pct, branch, dirty, ahead)`` from
    ``session-context.<pane>.json``.

    nexus-statusline writes
    ``{context_used_pct, model, ts, branch?, dirty?, ahead?}`` per pane (keyed
    on the raw ``#{pane_id}`` e.g. ``%3``). ``model`` is a single-letter tag
    (F/O/H/S) refreshed on every statusline render — unlike the old
    SessionStart-hook path this replaces (see ``cmd_register``'s note), it also
    tracks mid-session ``/model`` switches. ``context_used_pct`` is 0-100;
    divided by 100 here to keep the 0..1 ratio convention used elsewhere in this
    module. ``branch``/``dirty``/``ahead`` are optional (absent on an older
    nexus binary that hasn't been redeployed yet -> ``("", False, 0)``
    defaults, backward compatible). A ``ts`` older than the freshness cutoff
    (defined just above) is treated as stale and rendered as fully absent
    (plan 003) — the git fields share that same freshness gate, they have no
    cutoff logic of their own. Fail-open: missing pane id / file / bad shape /
    non-numeric -> ``("", None, "", False, 0)`` for the piece that failed.

    Consumption note (as of if-hrbd): although this function still returns the
    full 5-tuple, only ONE element is read by any caller — ``model_letter``
    (index 0) by :func:`_build_session_bar`. ``context_used_pct`` (index 1)
    used to also be read by :func:`cmd_accounts_popup`, but that was the
    if-hrbd gap: cc-tmux-adopt-nx-context-and-git-status migrated row 2's SES
    off this legacy file onto nx-agent while leaving the popup on this stale
    path, and confirmed live (2026-07-13) that this file no longer exists on
    disk at all, so the popup's SES was unconditionally blank. Both surfaces
    now share :func:`_resolve_ses_pct` (nx-agent) for SES — ``context_used_pct``
    and ``branch``/``dirty``/``ahead`` (indices 1-4) are no longer consumed by
    anything; they remain in the return tuple purely for backward-compatible
    parsing of the legacy file's shape and MUST NOT be assumed live by new
    callers.
    """
    if not pane_id:
        return "", None, "", False, 0
    try:
        data = json.loads((_cc_state_dir() / f"session-context.{pane_id}.json").read_text(encoding="utf-8"))
    except Exception:
        return "", None, "", False, 0

    ts = data.get("ts")
    if isinstance(ts, bool) or not isinstance(ts, (int, float)):
        return "", None, "", False, 0
    if time.time() - float(ts) > SESSION_CONTEXT_MAX_AGE_SECS:
        return "", None, "", False, 0

    letter = data.get("model")
    if not isinstance(letter, str):
        letter = ""
    letter = letter[:1]

    pct = data.get("context_used_pct")
    if isinstance(pct, bool) or not isinstance(pct, (int, float)):
        pct = None
    else:
        pct = float(pct) / 100.0

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


def _active_usage() -> Tuple[str, Optional[float], Optional[float]]:
    """``(account_label, 5H util, 7D util)`` for the active credential, or ``('', None, None)``.

    Delegates to :func:`usage.active_usage` — a short-TTL on-disk cache over
    the ~4MB /credentials fetch, so the 1Hz session-bar tick does not re-fetch
    and re-parse the full payload every second (plan 003). Fail-open.
    """
    try:
        return usage.active_usage()
    except Exception:
        return "", None, None


def _resolve_session_pane(window: str) -> str:
    """The window's representative pane for row 2 — tmux-active pane first.

    Prefers ``window``'s actually-focused pane (:func:`tmux.get_window_active_pane`,
    ``#{pane_id}`` via ``display-message``) over the priority pick
    (:func:`tmux.get_window_top_pane`, waiting > idle > active, ties broken by
    tmux's own iteration order) whenever the focused pane is a tracked Claude
    pane (its ``@cc-state`` is in :data:`VALID_STATES`). This is what makes row
    2 reflect the pane the user is actually looking at instead of an arbitrary
    tie-break winner in a multi-pane window. Falls back to
    :func:`tmux.get_window_top_pane` when the focused pane is untracked (a
    plain shell pane focused next to a background Claude pane) or when no
    active pane resolves at all. Fail-open: any missing piece degrades to the
    priority-pick fallback, never an exception.
    """
    active = tmux.get_window_active_pane(window)
    if active and tmux.get_pane_option(active, tmux.OPT_STATE) in VALID_STATES:
        return active
    return tmux.get_window_top_pane(window)


_MISSING = object()  # sentinel: distinguishes "nx key absent" from "nx key == 0"


def _local_git_status(pane: str) -> tmux.GitStatusCounts:
    """The local ``@cc-git-status`` pane option, JSON-decoded into a :class:`tmux.GitStatusCounts`.

    This is the per-field fallback source :func:`_resolve_git_status` reaches for
    whenever nx's response lacks a given key. Malformed/missing JSON, a non-dict
    payload, or an individual field that is absent/non-int (``bool`` excluded —
    it is an ``int`` subclass but never a valid count) all fail open to ``0`` for
    that field — never raises on a bad ``json.loads`` (mirrors the former
    ``_parse_dirty_counts``'s bool-excluded int check, now applied per-field
    instead of all-or-nothing).
    """
    try:
        decoded = json.loads(tmux.get_pane_option(pane, tmux.OPT_GIT_STATUS))
    except (ValueError, TypeError):
        decoded = None
    if not isinstance(decoded, dict):
        return tmux.GitStatusCounts()

    def _int_field(key: str) -> int:
        val = decoded.get(key)
        if isinstance(val, bool) or not isinstance(val, int):
            return 0
        return val

    return tmux.GitStatusCounts(
        modified=_int_field("modified"),
        untracked=_int_field("untracked"),
        deleted=_int_field("deleted"),
        renamed=_int_field("renamed"),
        ahead=_int_field("ahead"),
        behind=_int_field("behind"),
    )


def _nx_field(nx_source: dict, key: str, local_value: int) -> int:
    """One field of the per-field dual-source rule: prefer ``nx_source[key]`` when
    PRESENT (``.get(key, _MISSING)`` — presence-checked, not truthiness, so a
    legitimate nx ``0`` still counts as "nx has this field" and is preferred over
    ``local_value``), else fall back to ``local_value``. Also falls back when the
    present nx value is the wrong type (``bool`` excluded, non-``int``) — a
    malformed nx field degrades to local rather than propagating garbage.
    """
    val = nx_source.get(key, _MISSING)
    if val is _MISSING or isinstance(val, bool) or not isinstance(val, int):
        return local_value
    return val


def _resolve_git_status(pane: str) -> Tuple[str, "tmux.GitStatusCounts"]:
    """``(branch, GitStatusCounts)`` for row 2 — per-field nx/local dual source.

    **Branch** — UNCHANGED from the prior ``_resolve_branch_dirty`` logic: nx's
    ``GET /projects/:id/status`` ``git.branch`` (keyed by the pane's resolved
    registry project code, via the same ``display-message #{pane_current_path}``
    -> :func:`registry.resolve_project_code` pattern :func:`_read_roadmap_pulse`
    uses) when nx returns a ``git`` dict, else the local ``@cc-branch`` pane
    option.

    **Each of the six** :class:`tmux.GitStatusCounts` **fields, independently**
    (spec's "per-field dual-source, not an all-or-nothing block"): prefer nx's
    value when nx's response actually carries that key, else the corresponding
    field of :func:`_local_git_status`. Presence, not truthiness, gates the
    preference (:func:`_nx_field`) — a legitimate nx ``0`` still wins over local.

    Two nesting depths on nx's ``git`` object, per the proposal's documented nx
    response shape (``{"branch":..., "dirty": {"modified": N, "untracked": N},
    ...}``):

    * ``modified``/``untracked`` — nx nests these under a ``dirty`` sub-object
      today (confirmed, ``nx_agent.project_git_status``'s own docstring). Resolved
      from ``git["dirty"]``.
    * ``deleted``/``renamed``/``ahead``/``behind`` — nx sends none of these today;
      resolved from ``git`` ITSELF (top-level), on the anticipatory assumption
      that a future nx schema expansion (bead ``nx-mbnqj``) would add them as
      top-level ``git`` keys, matching ``branch``/``headSha``/``detached``'s
      existing top-level placement rather than nesting under ``dirty`` (which is
      semantically an untracked-tree-cleanliness pair, not a home for
      ahead/behind-vs-upstream or deleted/renamed counts). **This is a judgment
      call, not a confirmed fact** — nx has no precedent for where these four
      would land; if nx ships them nested differently, only this function's
      ``nx_top`` vs ``nx_dirty`` source selection per field needs updating, the
      per-field dual-source CONTRACT itself does not change.

    Fail-open throughout: no cwd / no registry code / nx unreachable / 404 -> all
    six fields fall back to :func:`_local_git_status`; a malformed local
    ``@cc-git-status`` -> all-zero counts for whatever falls back to it.
    """
    cwd = tmux._run_tmux(["display-message", "-p", "-t", pane, "#{pane_current_path}"])
    code = registry.resolve_project_code(cwd) if cwd else ""
    git = nx_agent.project_git_status(code) if code else None

    if isinstance(git, dict):
        nx_branch = git.get("branch")
        branch = nx_branch if isinstance(nx_branch, str) else ""
    else:
        branch = tmux.get_pane_option(pane, tmux.OPT_BRANCH)

    local = _local_git_status(pane)

    nx_top = git if isinstance(git, dict) else {}
    nx_dirty = nx_top.get("dirty")
    nx_dirty = nx_dirty if isinstance(nx_dirty, dict) else {}

    counts = tmux.GitStatusCounts(
        modified=_nx_field(nx_dirty, "modified", local.modified),
        untracked=_nx_field(nx_dirty, "untracked", local.untracked),
        deleted=_nx_field(nx_top, "deleted", local.deleted),
        renamed=_nx_field(nx_top, "renamed", local.renamed),
        ahead=_nx_field(nx_top, "ahead", local.ahead),
        behind=_nx_field(nx_top, "behind", local.behind),
    )
    return branch, counts


def _resolve_ses_pct(pane: str) -> Optional[float]:
    """Live SES (context-window-used %, 0..1 ratio) for ``pane`` via nx-agent.

    Single source of truth for SES resolution — ``GET /sessions/:id/context``
    via :func:`nx_agent.session_context`, keyed by the pane's ``@cc-session-id``
    option. An empty session-id, unreachable nx-agent, non-2xx response, or a
    malformed/non-numeric ``usedPercentage`` all degrade to ``None`` (fail-open,
    mirroring :func:`nx_agent.session_context`'s own guard).

    Shared by :func:`_build_session_bar` (row 2) and :func:`cmd_accounts_popup`
    (if-hrbd fix) so the two surfaces cannot drift onto two different SES
    sources again — that drift is exactly how if-hrbd happened: row 2 migrated
    here first while the popup was left reading the legacy per-pane file,
    which turned out to no longer exist on disk at all.
    """
    ctx = nx_agent.session_context(tmux.get_pane_option(pane, tmux.OPT_SESSION_ID))
    if not isinstance(ctx, dict):
        return None
    pct_raw = ctx.get("usedPercentage")
    if isinstance(pct_raw, bool) or not isinstance(pct_raw, (int, float)):
        return None
    return float(pct_raw) / 100.0


def _build_session_bar(window: str, pane: Optional[str] = None) -> str:
    """Build the row-2 session status-format string for a window (Req rows 2).

    Body of the former ``cmd_session_bar`` handler, extracted (plan 005) so
    :func:`cmd_render_all` can share one resolved pane across both row
    builders instead of each spawning its own process. Resolves the window's
    representative pane (unless ``pane`` is already known) via
    :func:`_resolve_session_pane` (tmux-active pane first, priority-pick
    fallback), then sources each row-2 field from its current owner
    (cc-tmux-git-status-glyphs, superseding cc-tmux-adopt-nx-context-and-git-status):

    * ``project`` — ``@cc-project`` pane option (unchanged, cc-tmux's own registry).
    * ``model_letter`` — UNCHANGED: still the legacy per-pane
      ``session-context.<pane>.json`` read via :func:`_read_session_context`
      (only its letter is used now; nx carries no model tag, so this field
      degrades to blank once nx stops writing that file — expected, disclosed).
    * ``context_used_pct`` — :func:`_resolve_ses_pct` (nx-agent
      ``GET /sessions/:id/context``, keyed by the ``@cc-session-id`` pane
      option), else ``None``. Shared with :func:`cmd_accounts_popup` (if-hrbd)
      so the two surfaces cannot drift onto two different SES sources again.
    * ``branch`` / ``git_status`` — resolved together by
      :func:`_resolve_git_status`: ``branch`` from nx-agent
      ``GET /projects/:id/status``'s ``git.branch`` when reachable, else the
      local ``@cc-branch`` pane option. ``git_status`` is a
      :class:`tmux.GitStatusCounts` whose six fields (``modified``,
      ``untracked``, ``deleted``, ``renamed``, ``ahead``, ``behind``) are each
      resolved INDEPENDENTLY — nx's value wins when nx's response actually
      carries that key (presence-gated, not truthiness), else the
      corresponding field decoded from the local ``@cc-git-status`` pane
      option (set by :func:`tmux.set_pane_git_identity`). There is no longer a
      single ``dirty`` tuple or a standalone ``ahead`` int — the retired
      ``@cc-dirty`` / ``@cc-ahead`` pane options are both gone in favor of the
      unified ``@cc-git-status`` JSON blob and this per-field dual-source
      resolution.

    Usage (account label + 5H / 7D) comes from :func:`_active_usage`. Left side
    is model/project/git identity; right side is the account label +
    SES:/5H:/7D: gauges.
    Fail-open: any missing piece degrades to a partial render; no pane -> ``""``.
    """
    if pane is None:
        pane = _resolve_session_pane(window)
    if not pane:
        return ""

    project = tmux.get_pane_option(pane, tmux.OPT_PROJECT)

    # model_letter: UNCHANGED — still the legacy per-pane file's letter (the
    # other fields _read_session_context returns are no longer consumed here).
    model_letter = _read_session_context(pane)[0]

    # context_used_pct: shared nx-agent resolution (if-hrbd — see _resolve_ses_pct).
    ses_pct = _resolve_ses_pct(pane)

    # branch/git-status: per-field nx primary, local @cc-git-status fallback
    # (cc-tmux-git-status-glyphs task 2.1 — replaces the former branch/dirty-only
    # _resolve_branch_dirty plus the separate always-local @cc-ahead read).
    branch, git_status = _resolve_git_status(pane)

    account_label, five_h_pct, seven_d_pct = _active_usage()

    # render.render_session_bar's `git_status` kwarg (task 3.1, UI batch) now
    # consumes the six-field GitStatusCounts directly — the old `dirty` tuple
    # / `ahead` int params are gone from its signature.
    return render.render_session_bar(
        model_letter, project, branch,
        account_label, ses_pct, five_h_pct, seven_d_pct,
        git_status=git_status,
    )


def cmd_session_bar(args) -> int:
    """Emit the row-2 session status-format string for a window (Req rows 2).

    Invoked FROM a tmux ``status-format[1]`` string
    (``#(cc-tmux session-bar #{window_id})``), re-evaluated on every status-bar
    refresh. Thin wrapper over :func:`_build_session_bar` — kept for other
    machines' deployed confs that still call this subcommand directly until
    they re-apply the plan-005 conf change (see Maintenance notes).
    """
    out = _build_session_bar(args.window)
    if out:
        sys.stdout.write(out)
    return 0


def cmd_accounts_popup(args) -> int:
    """Print the account-switcher popup body (cc-tmux-account-switcher-popup).

    Non-Goals: read-only usage popup, no account SWITCHING action.

    Fetches ``/credentials`` fresh (:func:`usage._query` — the raw payload,
    not the short-TTL :func:`_active_usage` cache, because this handler needs
    every credential, not just the active triple), applies
    :func:`usage.dedupe_credentials` (if-lp8v/if-m5q6 client-side stopgap),
    and extracts a ``(label, 5H, 7D)`` triple per surviving credential via the
    same :func:`usage._account_label`/:func:`usage._extract_util` primitives
    :func:`usage.extract_active`/:func:`usage.render_usage` already build on
    — reused here rather than re-deriving the field-navigation logic. The
    credential whose ``isActive`` flag is ``True`` supplies ``active_label``.

    SES (a property of the currently-focused pane, not any credential row —
    see proposal's "SES is not an account-level metric") is resolved via
    :func:`_resolve_session_pane` + :func:`_resolve_ses_pct` off the current
    window (:func:`tmux.current_window_id`) — the SAME nx-agent path
    :func:`_build_session_bar` (row 2) uses. Fixed if-hrbd (2026-07-13): this
    used to read the legacy per-pane ``session-context.<pane>.json`` file via
    :func:`_read_session_context`, left behind when
    cc-tmux-adopt-nx-context-and-git-status migrated row 2 off that file but
    scoped this handler out — confirmed live that the legacy file no longer
    exists on disk at all, so this popup's SES was unconditionally blank.

    Prints :func:`render.render_accounts_popup`'s plain-text body, or nothing
    on any failure (fail-open, matches this module's universal contract).
    Data/render only — no ``tmux display-popup`` call lives here; the
    tmux-side ``MouseDown1Status`` binding (``cc-tmux.tmux``) wraps this
    subcommand's output in ``display-popup``, the same DATA-ONLY split
    :func:`cmd_inbox`/:func:`cmd_picker_data` already use for their fzf
    popups.
    """
    payload = usage._query()
    credentials = payload.get("credentials") if isinstance(payload, dict) else None
    deduped = usage.dedupe_credentials(credentials) if isinstance(credentials, list) else []

    active_label = ""
    accounts: List[Tuple[str, Optional[float], Optional[float]]] = []
    for cred in deduped:
        label = usage._account_label(cred)
        if not label:
            continue
        if cred.get("isActive") is True:
            active_label = label
        accounts.append((
            label,
            usage._extract_util(cred, "usage5hUsed", "usage5hLimit"),
            usage._extract_util(cred, "usage7dUsed", "usage7dLimit"),
        ))

    window = tmux.current_window_id()
    pane = _resolve_session_pane(window) if window else ""
    active_ses_pct = _resolve_ses_pct(pane) if pane else None

    out = render.render_accounts_popup(accounts, active_label, active_ses_pct)
    if out:
        print(out)
    return 0


def _beads_pane(window_target: str) -> str:
    """The pane whose cwd drives row 3: top tracked pane, else the active pane.

    Prefers :func:`tmux.get_window_top_pane` (the same representative-pane
    choice row 2 uses) but falls back to the window's plain active pane when
    no ``@cc-state`` pane exists — row 3 needs only a cwd, so it must not
    depend on hook liveness (plan 006 / BEADS-03). ``""`` when tmux is
    unavailable or the window is empty.
    """
    return tmux.get_window_top_pane(window_target) or tmux.get_window_active_pane(window_target)


# roadmap-pulse `--line` mode's two-line cache format (cc-tmux-row3-openspec-
# beads-format task 1.2, ~/dev/cc `scripts/bin/roadmap-pulse`):
#   openspec: {open} open, {unarchived} unarchived
#   beads: {ready} ready, {blocked} blocked
# Each line is optional and parsed independently — see _parse_roadmap_pulse_counts.
_OPENSPEC_LINE_RE = re.compile(r"^openspec:\s*(\d+)\s+open,\s*(\d+)\s+unarchived\s*$")
_BEADS_LINE_RE = re.compile(r"^beads:\s*(\d+)\s+ready,\s*(\d+)\s+blocked\s*$")


def _parse_roadmap_pulse_counts(
    content: str,
) -> Tuple[Optional[int], Optional[int], Optional[int], Optional[int]]:
    """Parse roadmap-pulse's two-line ``--line`` cache format into counts.

    ``content`` is the (already ``radar:``-stripped, per :func:`_read_roadmap_pulse`)
    cache text. Returns ``(openspec_open, openspec_unarchived, beads_ready,
    beads_blocked)``. The ``openspec:`` and ``beads:`` lines are matched and
    parsed independently of each other and of line order: a missing or
    unparseable ``beads:`` line degrades ONLY the beads half to ``(None, None)``
    without affecting an otherwise-valid ``openspec:`` half, and vice versa —
    the same fail-open contract the rest of this module uses (e.g.
    :func:`_read_session_context`'s per-field ``None`` degradation), so a
    malformed half never blanks the other.
    """
    openspec_open: Optional[int] = None
    openspec_unarchived: Optional[int] = None
    beads_ready: Optional[int] = None
    beads_blocked: Optional[int] = None

    for line in content.splitlines():
        line = line.strip()
        m = _OPENSPEC_LINE_RE.match(line)
        if m:
            openspec_open, openspec_unarchived = int(m.group(1)), int(m.group(2))
            continue
        m = _BEADS_LINE_RE.match(line)
        if m:
            beads_ready, beads_blocked = int(m.group(1)), int(m.group(2))

    return openspec_open, openspec_unarchived, beads_ready, beads_blocked


def _build_beads_bar(window: str, pane: Optional[str] = None) -> str:
    """Build the row-3 beads/roadmap status-format string for a window (Req rows 3).

    Body of the former ``cmd_beads_bar`` handler, extracted (plan 005) so
    :func:`cmd_render_all` can share one resolved pane across both row
    builders. Resolves the window's representative pane (unless ``pane`` is
    already known), falling back to the window's active pane when no
    ``@cc-state`` pane exists (plan 006 / BEADS-03), reads its project's
    roadmap-pulse line + cache age, parses the two-line ``openspec:``/``beads:``
    content into structured counts (:func:`_parse_roadmap_pulse_counts`), and
    hands the parsed counts plus age to :func:`render.render_beads_bar` — both
    halves currently share the single cache file's mtime as their age, since
    there is only one cache file today (forward-compatible with a future
    per-half cache split, task 2.3). Fail-open: nothing pending -> ``""``.
    """
    if pane is None:
        pane = _beads_pane(window)
    if not pane:
        return ""
    content, age_sec = _read_roadmap_pulse(pane)
    openspec_open, openspec_unarchived, beads_ready, beads_blocked = _parse_roadmap_pulse_counts(content)
    return render.render_beads_bar(
        openspec_open, openspec_unarchived, beads_ready, beads_blocked,
        openspec_age_sec=age_sec, beads_age_sec=age_sec,
    )


def cmd_beads_bar(args) -> int:
    """Emit the row-3 beads/roadmap status-format string for a window (Req rows 3).

    Invoked FROM a tmux ``status-format[2]`` string
    (``#(cc-tmux beads-bar #{window_id})``). Thin wrapper over
    :func:`_build_beads_bar` — kept for other machines' deployed confs that
    still call this subcommand directly until they re-apply the plan-005 conf
    change (see Maintenance notes).
    """
    out = _build_beads_bar(args.window)
    if out:
        sys.stdout.write(out)
    return 0


def _build_tabs_row(active_window_id: str) -> str:
    """Build the whole animated window-tabs row (cc-tmux-tabs-and-rename-fix).

    Body of the former ``cmd_tabs_row`` handler, extracted (plan 005) so
    :func:`cmd_render_all` can compose it alongside rows 2/3 in one process.
    Takes the active window id as a parameter instead of resolving it via
    :func:`tmux.current_window_id` (the caller already knows it — same value
    ``#{window_id}`` supplies at the tmux status-format layer). Enumerates
    every window in the invoking client's session via
    :func:`tmux.get_window_tabs` and hands both to :func:`render.render_tabs_row`.
    Fail-open: no windows -> ``""``.

    As the once-per-tick session-wide surface (status-format[0] /
    ``render-all``), this is also the reconcile heartbeat: the call below is
    rate-limited by @cc-last-reconcile / @cc-reconcile-interval (tmux.py), so
    status-interval 1 costs at most one process scan per interval.
    """
    tmux.reconcile(_pane_ids_running_claude)  # rate-limited self-heal, <=1 scan/10s
    windows = tmux.get_window_tabs()
    if not windows:
        return ""
    # Sub-agent overlay (cc-tmux-subagent-tab-icon): prune each window's raw
    # (unpruned) @cc-subagent-bg union in place before handing windows to
    # render_tabs_row — aging policy (the timeout value) is a cli.py concern,
    # kept out of render.py so that module stays a pure function of counts.
    now = time.time()
    timeout = _subagent_bg_timeout()
    for w in windows:
        w.bg = prune_background_entries(w.bg, now, timeout)
    return render.render_tabs_row(windows, active_window_id, now)


def cmd_tabs_row(args) -> int:
    """Emit the whole animated window-tabs row (cc-tmux-tabs-and-rename-fix).

    Invoked FROM a top-level status-format slot (same slot class as
    :func:`cmd_session_bar`/:func:`cmd_beads_bar` — NOT nested inside
    ``window-status-format``, whose own embedded ``#()`` job never
    re-evaluates on this tmux version, confirmed via /openspec:explore runtime
    evidence). Thin wrapper over :func:`_build_tabs_row` — kept for other
    machines' deployed confs that still call this subcommand directly until
    they re-apply the plan-005 conf change (see Maintenance notes).
    """
    out = _build_tabs_row(tmux.current_window_id())
    if out:
        sys.stdout.write(out)
    return 0


# Global user options carrying the pre-rendered rows 2/3 for status-format[1]/[2]
# (bare `#{@cc-row-session}` lookup). Render TRANSPORT, not state (invariant 2):
# overwritten on every render-all tick, never read back by any Python code —
# only tmux's drawing pass consumes them. Rows 2/3 therefore trail the tabs row
# by at most one status-interval tick.
_ROW_SESSION_OPT = "@cc-row-session"
_ROW_BEADS_OPT = "@cc-row-beads"


def cmd_render_all(args) -> int:
    """All three status rows from one interpreter spawn (plan 005).

    Replaces the 3-spawns-per-tick wiring (tabs-row + session-bar + beads-bar
    as separate #() jobs). The window's representative pane is resolved ONCE
    and shared by both row builders. Fail-open: any failure inside a builder
    degrades that row to '' (options are ALWAYS rewritten, so a failing tick
    blanks a row rather than freezing stale content).
    """
    window = args.window
    pane = _resolve_session_pane(window)
    session_row = _build_session_bar(window, pane=pane) if pane else ""
    beads_row = _build_beads_bar(window, pane=(pane or tmux.get_window_active_pane(window)))
    tmux.set_global_option(_ROW_SESSION_OPT, session_row)
    tmux.set_global_option(_ROW_BEADS_OPT, beads_row)
    tabs = _build_tabs_row(window)
    if tabs:
        sys.stdout.write(tabs)
    return 0


# ---------------------------------------------------------------------------
# discover helpers
# ---------------------------------------------------------------------------

def _pane_ids_running_claude(rows: List[dict]) -> set:
    """Ids of panes whose foreground process (or a descendant) is Claude Code.

    Matches on the pane's own foreground command first (cheap), then walks the
    process tree via a single ``ps`` call for panes fronted by a wrapper (e.g.
    ``node``/``zsh``) whose descendant is claude. Fail-open -> {} on any error.
    """
    matched: set = set()
    needs_tree: List[dict] = []
    for row in rows:
        if _looks_like_claude(row.get("command", "")):
            matched.add(row["id"])
        else:
            needs_tree.append(row)

    if not needs_tree:
        return matched

    tree = _process_tree()
    if not tree:
        return matched

    for row in needs_tree:
        pid = row.get("pid", "")
        if pid and _tree_has_claude(pid, tree):
            matched.add(row["id"])
    return matched


def _looks_like_claude(command: str) -> bool:
    return "claude" in command.lower()


def _process_tree() -> Dict[str, tuple]:
    """Map pid -> (ppid, args) for every process. Fail-open -> {}."""
    try:
        proc = subprocess.run(
            ["ps", "-eo", "pid=,ppid=,args="],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError):
        return {}
    if proc.returncode != 0:
        return {}
    tree: Dict[str, tuple] = {}
    for line in proc.stdout.splitlines():
        parts = line.split(None, 2)
        if len(parts) < 2:
            continue
        pid, ppid = parts[0], parts[1]
        args = parts[2] if len(parts) == 3 else ""
        tree[pid] = (ppid, args)
    return tree


def _tree_has_claude(root_pid: str, tree: Dict[str, tuple]) -> bool:
    """Whether any descendant of root_pid (inclusive) runs claude. Cycle-safe."""
    # Build child index once per call is O(n); acceptable for discover (load-time).
    children: Dict[str, List[str]] = {}
    for pid, (ppid, _args) in tree.items():
        children.setdefault(ppid, []).append(pid)

    seen: set = set()
    stack = [root_pid]
    while stack:
        pid = stack.pop()
        if pid in seen:
            continue
        seen.add(pid)
        entry = tree.get(pid)
        if entry and _looks_like_claude(entry[1]):
            return True
        stack.extend(children.get(pid, []))
    return False


# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------

_DISPATCH: Dict[str, Callable[[object], int]] = {
    "register": cmd_register,
    "clear": cmd_clear,
    "cycle": cmd_cycle,
    "back": cmd_back,
    "switch": cmd_switch,
    "focus": cmd_focus,
    "discover": cmd_discover,
    "self-test": cmd_self_test,
    "doctor": cmd_doctor,
    "inbox": cmd_inbox,
    "inbox-clear": cmd_inbox_clear,
    "picker-data": cmd_picker_data,
    "status": cmd_status,
    "status-inbox": cmd_status_inbox,
    "usage": cmd_usage,
    "accounts-popup": cmd_accounts_popup,
    "window-icon": cmd_window_icon,
    "session-bar": cmd_session_bar,
    "beads-bar": cmd_beads_bar,
    "tabs-row": cmd_tabs_row,
    "render-all": cmd_render_all,
    "conductor": cmd_conductor,
}


def _stub(command: str) -> int:
    """Report a subcommand whose implementation another engineer owns.

    Prints a clear message (no traceback) and returns 2 so the parser is complete
    today while the owning engineer's handler is still pending.
    """
    import sys

    owner = _STUB_OWNERS.get(command, "another engineer")
    sys.stderr.write(
        f"cc-tmux: '{command}' is not implemented in this batch (owned by {owner}).\n"
    )
    return 2


def main(argv: Optional[List[str]] = None) -> int:
    """CLI entrypoint. Returns a process exit code."""
    parser = build_parser()
    args = parser.parse_args(argv)

    command = getattr(args, "command", None)
    if not command:
        parser.print_help()
        return 0

    handler = _DISPATCH.get(command)
    if handler is None:
        return _stub(command)

    try:
        return handler(args) or 0
    except Exception as exc:  # noqa: BLE001 - fail-open boundary
        # self-test must surface failures; every other command fails open so a
        # hook or keybinding can never crash Claude / tmux.
        if command == "self-test":
            raise
        log.warn("command %r failed, failing open: %s", command, exc)
        return 0
