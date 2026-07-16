"""argparse definition for the ``cc-tmux`` CLI.

This module owns ONLY the argument structure — no handler logic. The subcommand
name is stored in ``args.command`` and :mod:`cc_tmux.cli` maps it to a
``cmd_<name>()`` handler. Keeping the parser handler-free avoids a circular
import (cli imports the parser; the parser references no handlers).

Subcommands (all implemented): register, cycle, back, switch, focus, discover,
clear, self-test, doctor (core); inbox, inbox-clear, picker-data (integration
surface — these take no arguments, so their parsers stay bare); accounts-popup
(argless, cc-tmux-account-switcher-popup), accounts-popup-launch (argless,
cc-tmux-status-bar-popup-polish task 3.4 follow-up — opens the popup itself,
unlike accounts-popup's data-only contract); render-all (argless except the
window id, cc-tmux-tabs-and-rename-fix / plan 005) and conductor (Req-9, with
its own action + flags).
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
    p_register.add_argument(
        "--subagent-start",
        dest="subagent_start",
        action="store_true",
        help="Task tool PreToolUse: a sub-agent dispatch started (increments "
             "@cc-subagent-fg, or appends to @cc-subagent-bg for a background dispatch).",
    )
    p_register.add_argument(
        "--subagent-stop",
        dest="subagent_stop",
        action="store_true",
        help="Task tool PostToolUse: a sub-agent dispatch returned (decrements "
             "@cc-subagent-fg, floored at 0).",
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

    # -- accounts-popup: account-switcher popup body (cc-tmux-account-switcher-popup)
    # Data/render only, argless — resolves the current window itself (mirrors
    # tabs-row). Invoked from the row-2 account-label click's MouseDown1Status
    # binding in cc-tmux.tmux, wrapped there in `display-popup`.
    sub.add_parser(
        "accounts-popup",
        help="Emit the account-switcher popup body (row-2 account-label click).",
    )

    # -- accounts-popup-launch: opens the popup itself, sizing `-h` to content
    # (cc-tmux-status-bar-popup-polish task 3.4 follow-up, 2026-07-14, beads
    # if-s1yu). Argless — invoked from `cc-tmux.tmux`'s MouseDown1Status
    # binding via `run-shell` instead of a static `display-popup ...` string,
    # so the outer popup height can be computed fresh at click time. See
    # `cli.cmd_accounts_popup_launch`'s docstring for why this ONE subcommand
    # is allowed to call `tmux display-popup` itself (mirrors `conductor`'s
    # `--popup` action) despite `accounts-popup` staying data-only.
    sub.add_parser(
        "accounts-popup-launch",
        help="Open the account-switcher popup, sized to the real content height.",
    )

    # -- render-all: all three status rows from ONE interpreter spawn ----------
    # Invoked FROM status-format[0] (`#(cc-tmux render-all #{window_id})`).
    # Prints the tabs row on stdout and writes rows 2/3 to the global user
    # options @cc-row-session / @cc-row-beads, consumed by status-format[1]/[2]
    # via bare `#{@cc-row-session}` lookup (zero extra processes).
    p_render_all = sub.add_parser(
        "render-all",
        help="Emit the tabs row and publish rows 2/3 as @cc-row-* options (one spawn per tick).",
    )
    p_render_all.add_argument(
        "window",
        help="Active window id (#{window_id} from the status-format context).",
    )
    # cc-tmux-mobile-portrait-tabs task 2.1/2.2: optional trailing dimensions
    # (#{client_width}/#{client_height}) for portrait-mode tab-padding + row-wrap
    # detection. nargs="?" + default=None keeps a bare `render-all <window>`
    # invocation (manual testing, older tmux.conf.tmpl) from erroring out —
    # cmd_render_all treats a missing value as "unknown / assume landscape".
    p_render_all.add_argument(
        "client_width",
        nargs="?",
        type=int,
        default=None,
        help="Client width in columns (#{client_width}), for portrait-mode tab sizing.",
    )
    p_render_all.add_argument(
        "client_height",
        nargs="?",
        type=int,
        default=None,
        help="Client height in rows (#{client_height}), for portrait-mode tab sizing.",
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
