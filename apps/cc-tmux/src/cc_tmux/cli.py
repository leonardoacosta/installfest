"""CLI handlers and dispatch for ``cc-tmux``.

Each subcommand maps to a ``cmd_<name>()`` handler returning an int exit code.
:func:`main` wires argparse (from :mod:`cc_tmux.parser`) to the dispatch table.

Fail-open contract (invariant 5): a handler invoked by a Claude hook or a tmux
keybinding must never crash the caller. :func:`main` swallows handler exceptions
and returns 0 for every command EXCEPT ``self-test`` (whose whole job is to
report failure via a non-zero exit).
"""

from __future__ import annotations

import subprocess
from typing import Callable, Dict, List, Optional

from . import log, tmux
from .parser import build_parser
from .priority import VALID_STATES, select_next

# Global navigation breadcrumb (NOT tracked-pane state — see invariant 1). Records
# the pane we hopped away from so `back` can return to it.
_PREV_PANE_OPT = "@cc-prev-pane"
_CYCLE_MODE_OPT = "@cc-cycle-mode"

# Stub subcommands owned by other engineers (task ids for the not-implemented msg).
_STUB_OWNERS: Dict[str, str] = {
    "inbox": "task 1.6",
    "inbox-clear": "task 1.6",
    "picker-data": "task 1.6",
    "status": "task 1.7",
    "status-inbox": "task 1.7",
    "usage": "task 1.8",
    "conductor": "task 1.9",
}


# ---------------------------------------------------------------------------
# Implemented handlers
# ---------------------------------------------------------------------------

def cmd_register(args) -> int:
    """Record a pane's Claude state. The Claude-hook entrypoint (Req-3)."""
    pane = args.pane or tmux.current_pane_id()
    if not pane:
        return 0  # fail open: no pane context, nothing to record

    # set_pane_state returns whether this was a REAL transition. The auto-hop /
    # OS-notification reaction to that boolean is owned by the notify engine
    # (task 1.6); register's job is only to write the authoritative state.
    tmux.set_pane_state(
        pane,
        args.state,
        task=args.task,
        wait_reason=args.reason,
    )
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
