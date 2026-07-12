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
import select
import subprocess
import sys
import time
from pathlib import Path
from typing import Callable, Dict, List, Optional, Tuple

from . import log, notify, registry, render, tmux, usage
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
_TAB_NAME_MAX = 10                             # project-code + session-title combined budget
_STATUS_INBOX_STYLE_OPT = "@cc-status-inbox-{state}-style"  # per-state badge style

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
    # `title` window-rename format (tab naming). Every other hook event is
    # ignored here since only SessionStart's payload includes session_title.
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

    # Opt-in window auto-rename tracks the highest-priority state in the window (Req-7).
    rename_fired = _maybe_rename_window(pane)
    _trace_register(hook_payload, pane, rename_fired)
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


def _trace_register(hook_payload: dict, pane_id: Optional[str], rename_fired: bool) -> None:
    """Append one diagnostic JSON line per ``register`` call (rename-trigger debugging).

    Written to ``<state-dir>/cc-tmux-register-trace.log`` (:func:`_cc_state_dir`,
    same base dir as the session-context / roadmap-pulse caches) so a
    multi-hour session's real hook traffic can be correlated against whether
    :func:`_maybe_rename_window` actually renamed the window on each call.
    ``rename_attempted`` is hardcoded true here since ``cmd_register`` calls
    ``_maybe_rename_window`` unconditionally — kept as an explicit field (not
    inlined into ``rename_fired``) in case that call ever becomes conditional.
    Capped to the last ``_REGISTER_TRACE_MAX_LINES`` lines (read-trim-rewrite,
    atomic via ``.tmp`` + ``os.replace`` — same pattern as
    ``conductor.write_instructions``) so the file never grows unboundedly. This
    is a diagnostic trace file, NOT part of the invariant-1 pane-option state
    store — fail-open: any error here must never break ``cmd_register``.
    """
    try:
        entry = {
            "ts": time.time(),
            "hook_event_name": hook_payload.get("hook_event_name"),
            "pane_id": pane_id or None,
            "rename_attempted": True,
            "rename_fired": rename_fired,
        }
        path = _cc_state_dir() / _REGISTER_TRACE_FILE
        path.parent.mkdir(parents=True, exist_ok=True)
        lines: List[str] = []
        if path.is_file():
            lines = path.read_text(encoding="utf-8").splitlines()
        lines = lines[-(_REGISTER_TRACE_MAX_LINES - 1):]
        lines.append(json.dumps(entry, sort_keys=True))
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
    count = len(tmux.get_hop_panes())
    add("INFO", "tracked panes", str(count))

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

    Returns ``True`` iff ``rename-window`` was actually issued this call,
    ``False`` for every early-return path (opt-out, untracked pane, no
    siblings, empty resolved name) — used by :func:`_trace_register` to
    distinguish "ran but declined" from "renamed" in the diagnostic trace.
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
    tmux._run_tmux(["rename-window", "-t", pane_id, name])
    return True


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
    icon = render.animated_icon(state, time.time())
    if icon:
        sys.stdout.write(f"{icon} ")
    return 0


# ---------------------------------------------------------------------------
# Session + beads status rows (cc-tmux-session-usage-bars, rows 2 + 3)
#
# Both entrypoints resolve the window's representative pane the same way the
# animated tab icon does (get_window_top_*), read plain values, and hand them
# to render.py's pure composition functions. Fail-open throughout: every read
# degrades to a partial/empty render, never a raised exception.
# ---------------------------------------------------------------------------

def _cc_state_dir() -> Path:
    """The Claude state dir holding session-context / roadmap-pulse cache files.

    Honours ``CLAUDE_CONFIG_DIR`` (the standard CC config-dir override), else
    ``~/.claude``. Both cache families live under ``<config>/scripts/state``.
    """
    base = os.environ.get("CLAUDE_CONFIG_DIR", "").strip() or os.path.expanduser("~/.claude")
    return Path(base) / "scripts" / "state"


def _read_roadmap_pulse(pane_id: str) -> str:
    """Raw ``roadmap-pulse.<code>.line`` content for ``pane_id``'s project, or ``''``.

    Resolves the pane's cwd (``#{pane_current_path}``) to a registry short code
    (longest-prefix match, NOT the ``@cc-project`` display name) and reads the
    matching roadmap-pulse cache line. Fail-open: no pane, no cwd, no code,
    missing/unreadable/empty file -> ``''``.
    """
    if not pane_id:
        return ""
    try:
        cwd = tmux._run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_current_path}"])
        if not cwd:
            return ""
        code = registry.resolve_project_code(cwd)
        if not code:
            return ""
        return (_cc_state_dir() / f"roadmap-pulse.{code}.line").read_text(encoding="utf-8").strip()
    except Exception:
        return ""


# session-context.<pane>.json freshness cutoff (plan 003): the writer
# (nexus-statusline) refreshes ts on every statusline render, i.e. every turn.
# A file older than this is a dead session or a recycled pane id — render it
# as absent rather than confidently wrong. Writer-side GC (>6h prune) is inert
# exactly when the writer stalls, so the reader enforces its own cutoff.
SESSION_CONTEXT_MAX_AGE_SECS = 900.0


def _read_session_context(pane_id: str) -> Tuple[str, Optional[float]]:
    """``(model_letter, context_used_pct)`` from ``session-context.<pane>.json``.

    nexus-statusline writes ``{context_used_pct, model, ts}`` per pane (keyed on
    the raw ``#{pane_id}`` e.g. ``%3``). ``model`` is a single-letter tag
    (F/O/H/S) refreshed on every statusline render — unlike the old
    SessionStart-hook path this replaces (see ``cmd_register``'s note), it also
    tracks mid-session ``/model`` switches. ``context_used_pct`` is 0-100;
    divided by 100 here to keep the 0..1 ratio convention used elsewhere in this
    module. A ``ts`` older than the freshness cutoff (defined just above) is
    treated as stale and rendered as absent (plan 003). Fail-open: missing
    pane id / file / bad shape / non-numeric -> ``("", None)`` for the piece
    that failed.
    """
    if not pane_id:
        return "", None
    try:
        data = json.loads((_cc_state_dir() / f"session-context.{pane_id}.json").read_text(encoding="utf-8"))
    except Exception:
        return "", None

    ts = data.get("ts")
    if isinstance(ts, bool) or not isinstance(ts, (int, float)):
        return "", None
    if time.time() - float(ts) > SESSION_CONTEXT_MAX_AGE_SECS:
        return "", None

    letter = data.get("model")
    if not isinstance(letter, str):
        letter = ""
    letter = letter[:1]

    pct = data.get("context_used_pct")
    if isinstance(pct, bool) or not isinstance(pct, (int, float)):
        pct = None
    else:
        pct = float(pct) / 100.0

    return letter, pct


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


def cmd_session_bar(args) -> int:
    """Emit the row-2 session status-format string for a window (Req rows 2).

    Invoked FROM a tmux ``status-format[1]`` string
    (``#(cc-tmux session-bar #{window_id})``), re-evaluated on every status-bar
    refresh. Resolves the window's representative pane, reads @cc-project /
    @cc-branch and the per-pane model letter (from ``session-context.<pane>.json``
    via :func:`_read_session_context`), and hands them along with the active
    account's usage (via :func:`_active_usage`) to
    :func:`render.render_session_bar`. Left side is session/model/git identity;
    right side is the account label + SES:/5H:/7D: gauges. Fail-open: any
    missing piece degrades to a partial render; empty window -> print nothing,
    exit 0.
    """
    pane = tmux.get_window_top_pane(args.window)
    if not pane:
        return 0

    project = tmux.get_pane_option(pane, tmux.OPT_PROJECT)
    branch = tmux.get_pane_option(pane, tmux.OPT_BRANCH)

    # Raw session count (an int) — render_session_bar maps it to a glyph
    # itself (render._session_glyph), so pass the count, not a pre-mapped string.
    session_count = sum(1 for p in tmux.get_hop_panes() if p.project == project) if project else 0

    model_letter, ses_pct = _read_session_context(pane)
    account_label, five_h_pct, seven_d_pct = _active_usage()

    out = render.render_session_bar(
        session_count, model_letter, project, branch,
        account_label, ses_pct, five_h_pct, seven_d_pct,
    )
    if out:
        sys.stdout.write(out)
    return 0


def cmd_beads_bar(args) -> int:
    """Emit the row-3 beads/roadmap status-format string for a window (Req rows 3).

    Invoked FROM a tmux ``status-format[2]`` string
    (``#(cc-tmux beads-bar #{window_id})``). Resolves the window's representative
    pane, reads its project's roadmap-pulse line, and hands it to
    :func:`render.render_beads_bar`. Fail-open: nothing pending -> print nothing.
    """
    pane = tmux.get_window_top_pane(args.window)
    if not pane:
        return 0
    out = render.render_beads_bar(_read_roadmap_pulse(pane))
    if out:
        sys.stdout.write(out)
    return 0


def cmd_tabs_row(args) -> int:
    """Emit the whole animated window-tabs row (cc-tmux-tabs-and-rename-fix).

    Invoked FROM a top-level status-format slot (same slot class as
    :func:`cmd_session_bar`/:func:`cmd_beads_bar` — NOT nested inside
    ``window-status-format``, whose own embedded ``#()`` job never
    re-evaluates on this tmux version, confirmed via /openspec:explore runtime
    evidence). Takes no arguments: enumerates every window in the invoking
    client's session via :func:`tmux.get_window_tabs`, resolves the active
    window via :func:`tmux.current_window_id`, and hands both to
    :func:`render.render_tabs_row`. Fail-open: no windows -> print nothing,
    exit 0 (never blocks the status bar).

    As the once-per-tick session-wide surface (status-format[0]), this is
    also the reconcile heartbeat: the call above is rate-limited by
    @cc-last-reconcile / @cc-reconcile-interval (tmux.py), so status-interval 1
    costs at most one process scan per interval.
    """
    tmux.reconcile(_pane_ids_running_claude)  # rate-limited self-heal, <=1 scan/10s
    windows = tmux.get_window_tabs()
    if not windows:
        return 0
    active_window_id = tmux.current_window_id()
    out = render.render_tabs_row(windows, active_window_id, time.time())
    if out:
        sys.stdout.write(out)
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
    "window-icon": cmd_window_icon,
    "session-bar": cmd_session_bar,
    "beads-bar": cmd_beads_bar,
    "tabs-row": cmd_tabs_row,
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
