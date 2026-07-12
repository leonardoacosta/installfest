"""argparse definition for the ``cc-tmux`` CLI.

This module owns ONLY the argument structure — no handler logic. The subcommand
name is stored in ``args.command`` and :mod:`cc_tmux.cli` maps it to a
``cmd_<name>()`` handler. Keeping the parser handler-free avoids a circular
import (cli imports the parser; the parser references no handlers).

Subcommands split into two ownership groups:
  * Implemented: register, cycle, back, switch, focus, discover, clear,
    self-test, doctor (core); inbox, inbox-clear, picker-data, status,
    status-inbox (integration surface — these take no arguments, so their parsers
    stay bare); usage (argless, Req-8), tabs-row (argless, cc-tmux-tabs-and-rename-fix)
    and conductor (Req-9, with its own action + flags).
"""

from __future__ import annotations

import argparse

from .conductor import CONDUCTOR_MODES
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

    # -- focus: record a pane visit (pane-focus-in hook -> MRU tiebreak) -------
    p_focus = sub.add_parser(
        "focus",
        help="Stamp @cc-visited on a tracked pane (invoked by the pane-focus-in hook).",
    )
    p_focus.add_argument("pane_id", help="Pane id that gained focus (#{pane_id}).")

    # -- doctor: environment diagnostics (PASS/FAIL/WARN, always exit 0) -------
    sub.add_parser("doctor", help="Print an environment diagnostics checklist (always exit 0).")

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

    # -- integration surface (no arguments; handlers in cli.py) ---------------
    sub.add_parser("inbox", help="Notification inbox rows (fzf/menu data).")
    sub.add_parser("inbox-clear", help="Dismiss inbox waiting/idle entries (view filter).")
    sub.add_parser("picker-data", help="Emit picker rows for the jump-to popup.")
    sub.add_parser("status", help="Emit status-bar pane counts.")
    sub.add_parser("status-inbox", help="Emit clickable pending-pane badges.")

    # -- usage: Claude multi-account usage status segment (Req-8) --------------
    # Takes no arguments (mirrors the retired tmux-nexus-creds sh script).
    sub.add_parser("usage", help="Emit the Claude usage status segment.")

    # -- window-icon: animated tab icon (Req: animated tab icon) ---------------
    # Invoked FROM a tmux window-status-format string
    # (`#(cc-tmux window-icon #{window_id})`), re-evaluated on every
    # status-bar refresh — this is what drives the animation, not a timer
    # owned by this process.
    p_window_icon = sub.add_parser(
        "window-icon",
        help="Emit the current tab-icon glyph for a window (invoked from window-status-format).",
    )
    p_window_icon.add_argument(
        "window",
        help="Target window (tmux window_id e.g. @3, or an index) to scope the state lookup to.",
    )

    # -- session-bar: row-2 session/usage status-format (cc-tmux-session-usage-bars)
    # Invoked FROM a tmux status-format[1] string
    # (`#(cc-tmux session-bar #{window_id})`), re-evaluated on every status-bar
    # refresh — same daemon-free read cadence as window-icon.
    p_session_bar = sub.add_parser(
        "session-bar",
        help="Emit the row-2 session/usage status-format string for a window (invoked from status-format[1]).",
    )
    p_session_bar.add_argument(
        "window",
        help="Target window (tmux window_id e.g. @3, or an index) to scope the lookup to.",
    )

    # -- beads-bar: row-3 beads/roadmap status-format (cc-tmux-session-usage-bars)
    # Invoked FROM a tmux status-format[2] string
    # (`#(cc-tmux beads-bar #{window_id})`), re-evaluated on every status-bar refresh.
    p_beads_bar = sub.add_parser(
        "beads-bar",
        help="Emit the row-3 beads/roadmap status-format string for a window (invoked from status-format[2]).",
    )
    p_beads_bar.add_argument(
        "window",
        help="Target window (tmux window_id e.g. @3, or an index) to scope the lookup to.",
    )

    # -- tabs-row: whole-row animated window tabs (cc-tmux-tabs-and-rename-fix)
    # Invoked FROM a top-level status-format slot (e.g. status-format[0], NOT
    # nested inside window-status-format — the per-window `#()` job embedded
    # in the default window-status-format never re-evaluates on this tmux
    # version, so the whole row renders from one top-level job instead, same
    # slot class as session-bar/beads-bar). Takes no arguments — it enumerates
    # every window in the invoking client's session itself (tmux.get_window_tabs).
    sub.add_parser(
        "tabs-row",
        help="Emit the whole animated window-tabs row (invoked from a top-level status-format slot).",
    )

    # -- conductor: persistent orchestrator session + task dispatch (Req-9) ----
    p_conductor = sub.add_parser(
        "conductor",
        help="Conductor: persistent orchestrator session + task dispatch.",
    )
    p_conductor.add_argument(
        "action",
        nargs="?",
        choices=["list", "dispatch", "context"],
        default=None,
        help="list: dispatchable panes; dispatch: route a task; context: hook snapshot.",
    )
    p_conductor.add_argument(
        "--popup",
        action="store_true",
        help="Open a popup attached to the conductor session (created on demand).",
    )
    p_conductor.add_argument(
        "--respawn",
        action="store_true",
        help="With --popup: kill and recreate the session first (picks up refreshed instructions).",
    )
    p_conductor.add_argument(
        "--kill",
        action="store_true",
        help="Kill the conductor session.",
    )
    p_conductor.add_argument(
        "--update-instructions",
        dest="update_instructions",
        action="store_true",
        help="Regenerate the conductor instruction file from the built-in canon.",
    )
    p_conductor.add_argument(
        "--json",
        action="store_true",
        help="With the 'list' action: emit JSON instead of aligned columns.",
    )
    p_conductor.add_argument(
        "--mode",
        choices=CONDUCTOR_MODES,
        default=None,
        help="Dispatch mode (see the cc-dispatch skill for the authoritative shape).",
    )
    p_conductor.add_argument(
        "--target",
        default=None,
        help="Dispatch target: a pane id (switch/send-prompt) or a directory (spawn-*).",
    )
    p_conductor.add_argument(
        "--force",
        action="store_true",
        help="With send-prompt: allow targeting an 'active' (busy) pane.",
    )
    p_conductor.add_argument(
        "--prompt",
        default=None,
        help="Prompt / task text for send-prompt and spawn-* modes.",
    )

    return parser
