"""argparse definition for the ``cc-tmux`` CLI.

This module owns ONLY the argument structure — no handler logic. The subcommand
name is stored in ``args.command`` and :mod:`cc_tmux.cli` maps it to a
``cmd_<name>()`` handler. Keeping the parser handler-free avoids a circular
import (cli imports the parser; the parser references no handlers).

Subcommands split into two ownership groups:
  * Implemented here (Script batch core): register, cycle, back, switch,
    discover, clear, self-test.
  * Stubbed for other engineers: inbox, inbox-clear, picker-data, status,
    status-inbox, usage, conductor. The parser registers the name so wiring is
    complete; the handler raises NotImplementedError until its owner lands it.
"""

from __future__ import annotations

import argparse

from .priority import VALID_CYCLE_MODES, VALID_STATES

# Wait reasons a `register --state waiting` may carry (Req-3 mapping).
WAIT_REASONS = ["question", "plan", "permission", "elicitation"]

PROG = "cc-tmux"


def build_parser() -> argparse.ArgumentParser:
    """Construct the full cc-tmux argument parser."""
    parser = argparse.ArgumentParser(
        prog=PROG,
        description="Claude Code + tmux plugin: track, cycle, and dispatch parallel Claude panes.",
    )
    sub = parser.add_subparsers(dest="command", metavar="<command>")

    # -- register: the Claude-hook entrypoint that writes pane state ----------
    p_register = sub.add_parser(
        "register",
        help="Record a pane's Claude state (invoked by Claude Code hooks).",
    )
    p_register.add_argument(
        "--state",
        required=True,
        choices=sorted(VALID_STATES),
        help="New state for the pane.",
    )
    p_register.add_argument(
        "--reason",
        choices=WAIT_REASONS,
        default=None,
        help="Why the pane is waiting (only meaningful with --state waiting).",
    )
    p_register.add_argument("--task", default=None, help="Short task summary for the pane.")
    p_register.add_argument(
        "--pane",
        default=None,
        help="Target pane id (defaults to $TMUX_PANE / the current pane).",
    )

    # -- cycle: advance to the next attention-priority pane -------------------
    p_cycle = sub.add_parser("cycle", help="Hop to the next pending pane (priority order).")
    p_cycle.add_argument(
        "--mode",
        choices=VALID_CYCLE_MODES,
        default=None,
        help="Cycle mode; defaults to @cc-cycle-mode (falls back to 'priority').",
    )

    # -- back: jump to the previously-focused pane ----------------------------
    sub.add_parser("back", help="Jump back to the previously-focused pane (across sessions).")

    # -- switch: focus a specific pane ----------------------------------------
    p_switch = sub.add_parser("switch", help="Switch focus to a specific pane.")
    p_switch.add_argument("--pane", required=True, help="Target pane id to switch to.")

    # -- discover: auto-register already-running Claude sessions --------------
    p_discover = sub.add_parser(
        "discover",
        help="Auto-register already-running Claude sessions (on plugin load).",
    )
    p_discover.add_argument(
        "--quiet",
        action="store_true",
        help="Suppress human output (used from the tmux entrypoint).",
    )

    # -- clear: drop a pane's tracked state -----------------------------------
    p_clear = sub.add_parser("clear", help="Clear a pane's @cc-* state (SessionEnd).")
    p_clear.add_argument(
        "--pane",
        default=None,
        help="Target pane id (defaults to $TMUX_PANE / the current pane).",
    )

    # -- self-test: pure-function unit tests ----------------------------------
    p_selftest = sub.add_parser("self-test", help="Run the built-in pure-function test suite.")
    p_selftest.add_argument(
        "--verbose",
        action="store_true",
        help="Print each test result, not just the summary.",
    )

    # -- stubs owned by other engineers ---------------------------------------
    sub.add_parser("inbox", help="Notification inbox (fzf/menu).")  # owned by task 1.6
    sub.add_parser("inbox-clear", help="Dismiss inbox waiting/idle entries.")  # owned by task 1.6
    sub.add_parser("picker-data", help="Emit picker rows for the fzf popup.")  # owned by task 1.6
    sub.add_parser("status", help="Emit status-bar pane counts.")  # owned by task 1.7
    sub.add_parser("status-inbox", help="Emit clickable pending-pane badges.")  # owned by task 1.7
    sub.add_parser("usage", help="Emit the Claude usage status segment.")  # owned by task 1.8
    sub.add_parser("conductor", help="Conductor dispatch / popup.")  # owned by task 1.9

    return parser
