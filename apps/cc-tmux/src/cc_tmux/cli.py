"""CLI handlers and dispatch for ``cc-tmux``.

Each subcommand maps to a ``cmd_<name>()`` handler returning an int exit code.
:func:`main` wires argparse (from :mod:`cc_tmux.parser`) to the dispatch table.

Fail-open contract (invariant 5): a handler invoked by a Claude hook or a tmux
keybinding must never crash the caller. :func:`main` swallows handler exceptions
and returns 0 for every command EXCEPT ``self-test`` (whose whole job is to
report failure via a non-zero exit).
"""

from __future__ import annotations

import os
import subprocess
import time
from typing import Callable, Dict, List, Optional

from . import log, notify, render, tmux
from .conductor import cmd_conductor
from .parser import build_parser
from .usage import cmd_usage
from .priority import (
    STATE_PRIORITY,
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
_INBOX_CLEARED_OPT = "@cc-inbox-cleared-at"   # dismiss stamp (a view filter)
_STATUS_FORMAT_OPT = "@cc-status-format"       # @cc-status template
_WINDOW_RENAME_OPT = "@cc-window-rename"       # opt-in window auto-rename
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
    # Opt-in window auto-rename tracks the highest-priority state in the window (Req-7).
    _maybe_rename_window(pane)
    return 0


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
    panes = tmux.get_hop_panes()
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
    panes = _self_heal(tmux.get_hop_panes())
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
    _emit_rows(sort_panes(_self_heal(tmux.get_hop_panes())))
    return 0


# ---------------------------------------------------------------------------
# Status sources + window rename (Req-7, task 1.8)
# ---------------------------------------------------------------------------

def cmd_status(args) -> int:
    """Emit the status-bar pane counts via ``@cc-status-format`` (Req-7)."""
    groups = group_by_state(tmux.get_hop_panes())
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


def _self_heal(panes: List["tmux.PaneInfo"]) -> List["tmux.PaneInfo"]:
    """Clear stale state left by a kill -9'd Claude (Req-5 self-heal).

    A tracked pane still present in tmux but no longer running Claude gets its
    ``@cc-*`` state cleared. A FAILED process scan (empty result) shows
    everything rather than mass-clearing live sessions.
    """
    rows = tmux.iter_panes_with_process()
    if not rows:
        return panes  # scan failed / unavailable -> do not clear anything
    claude_ids = _pane_ids_running_claude(rows)
    present = {row["id"] for row in rows}
    healed = False
    for pane in panes:
        if pane.id in present and pane.id not in claude_ids:
            tmux.clear_pane_state(pane.id)
            healed = True
    return tmux.get_hop_panes() if healed else panes


def _truthy(value: str) -> bool:
    return value.strip().lower() in ("1", "on", "true", "yes")


def _maybe_rename_window(pane_id: str) -> None:
    """Rename a pane's window to ``<state-icon> <dir basename>`` when enabled.

    Icon tracks the highest-priority Claude state in the window; the directory
    basename stays a stable label. ``automatic-rename`` is forced off so tmux
    does not clobber the name. Opt-in via ``@cc-window-rename`` (default off).
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
    top = min(siblings, key=lambda p: STATE_PRIORITY.get(p.state, len(STATE_PRIORITY)))
    icon = render.resolve_icons(tmux.get_global_option).get(top.state, top.state)

    cwd = tmux._run_tmux(
        ["display-message", "-p", "-t", pane_id, "#{pane_current_path}"]
    )
    base = os.path.basename(os.path.normpath(cwd)) if cwd else (me.project or "")
    name = f"{icon} {base}".strip()

    tmux._run_tmux(["set-window-option", "-t", pane_id, "automatic-rename", "off"])
    tmux._run_tmux(["rename-window", "-t", pane_id, name])


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
    "discover": cmd_discover,
    "self-test": cmd_self_test,
    "inbox": cmd_inbox,
    "inbox-clear": cmd_inbox_clear,
    "picker-data": cmd_picker_data,
    "status": cmd_status,
    "status-inbox": cmd_status_inbox,
    "usage": cmd_usage,
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
