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
from typing import Callable, Dict, List, Optional

from . import log, notify, registry, render, tmux
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
    hook_payload = _read_hook_stdin()
    if hook_payload.get("hook_event_name") == "SessionStart":
        title = hook_payload.get("session_title")
        if title:
            tmux.set_pane_title(pane, title)

    # Opt-in window auto-rename tracks the highest-priority state in the window (Req-7).
    _maybe_rename_window(pane)
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

    The status bar is tmux's frequent-render surface, so it doubles as the
    de-facto reconcile heartbeat — but the scan is rate-limited (design.md
    Decision 1), so a render pays at most one process scan per interval.
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


def _maybe_rename_window(pane_id: str) -> None:
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
    """
    if not _truthy(tmux.get_global_option(_WINDOW_RENAME_OPT)):
        return
    panes = tmux.get_hop_panes()
    me = next((p for p in panes if p.id == pane_id), None)
    if me is None:
        return
    siblings = [p for p in panes if p.session == me.session and p.window == me.window]
    if not siblings:
        return

    fmt = (tmux.get_global_option(_WINDOW_RENAME_FORMAT_OPT) or "state").strip().lower()
    if fmt == "title":
        name = _title_window_name(me)
    else:
        cwd = tmux._run_tmux(
            ["display-message", "-p", "-t", pane_id, "#{pane_current_path}"]
        )
        name = os.path.basename(os.path.normpath(cwd)) if cwd else (me.project or "")

    if not name:
        return
    tmux._run_tmux(["set-window-option", "-t", pane_id, "automatic-rename", "off"])
    tmux._run_tmux(["rename-window", "-t", pane_id, name])


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
