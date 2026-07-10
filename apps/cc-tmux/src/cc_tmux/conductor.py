"""Conductor — persistent orchestrator session + task dispatch (Req-9, task 1.10).

The conductor is a persistent, detached tmux session running its own Claude that
sees a live snapshot of every tracked pane and routes work to them. It is
**disabled by default** (``@cc-conductor-enabled`` off): with the flag off, no
keybinding registers (the tmux entrypoint guards on it) and ``conductor --popup``
refuses. Shipping it same-batch therefore adds code but not default surface area.

Governing rules carried into this module:

* **Session-name filter (Req-9):** the conductor's own session is excluded from
  every pane view — ``get_hop_panes(exclude_session=<name>)`` — so it never
  dispatches to itself or lists itself.
* **CLI SHAPE lives in the cc-dispatch skill (Req-10), not here.** This module is
  the single home of the conductor *instructions* (:data:`CONDUCTOR_INSTRUCTIONS`,
  which is where mode SELECTION is described); the flag shape is documented once
  in ``skills/cc-dispatch``.
* **Invariant 1 (pane options are the only tracked-state store):** the conductor
  uses tmux *global* options only as navigation breadcrumbs (via
  ``cc_tmux.tmux.set_global_option``), never as a parallel pane-state store. The
  instruction text is config content, persisted to a file, not derived pane state.
* **Invariant 5 (fail open):** reads (``list``, ``context``) always exit 0; only
  genuine misuse (unknown/absent dispatch target, no ``claude`` binary for a
  spawn, a git failure for a worktree, popup while disabled) exits non-zero.

Env contract: the detached session is created with ``CC_TMUX_CONDUCTOR=1`` so the
plugin's SessionStart / UserPromptSubmit context-injection hook — shell-guarded on
that variable — runs the interpreter ONLY inside the conductor, never in ordinary
Claude panes.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import List, Optional

from . import log, tmux
from .priority import sort_panes

# The four dispatch modes. Single source for the parser's ``--mode`` choices; the
# human-facing description of WHEN to use each lives in CONDUCTOR_INSTRUCTIONS and
# the cc-dispatch skill.
CONDUCTOR_MODES: List[str] = ["switch", "send-prompt", "spawn-task", "spawn-worktree"]

# Global options (config, not tracked pane state).
_ENABLED_OPT = "@cc-conductor-enabled"
_SESSION_OPT = "@cc-conductor-session"
_DEFAULT_SESSION = "conductor"

# Marker env var set on the detached session so context injection self-guards.
_CONDUCTOR_ENV = "CC_TMUX_CONDUCTOR"


# The mode-selection playbook the conductor's Claude sees on every prompt. This is
# the ONE place mode selection is described (Req-9); the flag shape is in the
# cc-dispatch skill. Kept plain-text so it drops straight into hook stdout.
CONDUCTOR_INSTRUCTIONS = """\
# cc-tmux Conductor

You are the Conductor: a persistent Claude session that orchestrates the other
Claude panes visible in this tmux server. You never do the downstream work
yourself — you route it to the right pane. A live snapshot of every tracked pane
(excluding your own session) is injected below on every prompt.

## Dispatch modes — pick by the situation

- switch        — the user just wants to LOOK at a pane. Move focus there; do not
                  send any prompt. Use for "show me", "jump to", "take me to".
- send-prompt   — a pane already owns the right context/repo and should receive a
                  new instruction. REFUSED against an `active` pane (one already
                  working) unless the user explicitly forces it — never interrupt
                  a busy pane by default.
- spawn-task    — the work needs a FRESH pane in an existing project checkout.
                  Opens a new window in that project's root and seeds the task.
- spawn-worktree— the work must NOT touch the current checkout (parallel branch,
                  risky change). Creates a fresh git worktree and opens a pane in
                  it, so the main working tree is untouched.

## Rules

- Prefer send-prompt to an idle/waiting pane that already has the context over
  spawning a new one — spawning is for genuinely new, parallel work.
- Never send-prompt to an `active` pane unless the user is explicit; surface that
  it is busy and ask.
- The exact CLI flag shape is documented in the `cc-dispatch` skill; use it as the
  authoritative reference for `cc-tmux conductor dispatch ...` arguments.
"""


# ---------------------------------------------------------------------------
# Config reads
# ---------------------------------------------------------------------------

def _truthy(value: str) -> bool:
    return value.strip().lower() in ("1", "on", "true", "yes")


def is_enabled() -> bool:
    """Whether the conductor is enabled (``@cc-conductor-enabled``, default off)."""
    return _truthy(tmux.get_global_option(_ENABLED_OPT))


def session_name() -> str:
    """The conductor session name (``@cc-conductor-session``, default ``conductor``)."""
    return tmux.get_global_option(_SESSION_OPT).strip() or _DEFAULT_SESSION


# ---------------------------------------------------------------------------
# Instruction persistence (config content, not pane state)
# ---------------------------------------------------------------------------

def _instructions_path() -> Path:
    """Stable on-disk location for the (regeneratable) conductor instructions."""
    base = os.environ.get("XDG_STATE_HOME", "").strip()
    root = Path(base) if base else Path(os.path.expanduser("~")) / ".local" / "state"
    return root / "cc-tmux" / "conductor-instructions.md"


def load_instructions() -> str:
    """Current instructions: the persisted file if present, else the built-in canon."""
    path = _instructions_path()
    try:
        if path.is_file():
            text = path.read_text(encoding="utf-8")
            if text.strip():
                return text
    except OSError as exc:
        log.warn("conductor: reading instructions failed: %s", exc)
    return CONDUCTOR_INSTRUCTIONS


def write_instructions(text: str = CONDUCTOR_INSTRUCTIONS) -> Optional[Path]:
    """Persist instructions to the instructions file; return the path or ``None``."""
    path = _instructions_path()
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        tmp = path.with_suffix(path.suffix + ".tmp")
        tmp.write_text(text, encoding="utf-8")
        os.replace(tmp, path)
    except OSError as exc:
        log.warn("conductor: writing instructions failed: %s", exc)
        return None
    return path


# ---------------------------------------------------------------------------
# Session lifecycle
# ---------------------------------------------------------------------------

def _session_exists(name: str) -> bool:
    return tmux._run_tmux(["has-session", "-t", name]) is not None


def _kill_session(name: str) -> None:
    tmux._run_tmux(["kill-session", "-t", name])


def _create_session(name: str) -> bool:
    """Create the detached conductor session running ``exec claude``.

    Sets ``CC_TMUX_CONDUCTOR=1`` on the session so context injection self-guards.
    Returns success. Requires a ``claude`` binary (genuine misuse otherwise).
    """
    if shutil.which("claude") is None:
        log.warn("conductor: no 'claude' binary on PATH; cannot create session")
        return False
    created = tmux._run_tmux(
        [
            "new-session",
            "-d",
            "-s",
            name,
            "-e",
            f"{_CONDUCTOR_ENV}=1",
            "exec claude",
        ]
    )
    return created is not None


def _ensure_session(name: str) -> bool:
    if _session_exists(name):
        return True
    return _create_session(name)


# ---------------------------------------------------------------------------
# Actions
# ---------------------------------------------------------------------------

def _popup(respawn: bool) -> int:
    """Open a popup attached to the conductor session (created on demand).

    Refuses (exit 1) when the conductor is disabled. With ``respawn`` the session
    is killed first so it restarts against refreshed instructions.
    """
    if not is_enabled():
        sys.stderr.write(
            "cc-tmux conductor: disabled (@cc-conductor-enabled is off). "
            "Enable it with: tmux set -g @cc-conductor-enabled on\n"
        )
        return 1
    if not tmux.tmux_available():
        sys.stderr.write("cc-tmux conductor: not inside tmux.\n")
        return 1

    name = session_name()
    if respawn:
        _kill_session(name)
    if not _ensure_session(name):
        sys.stderr.write(
            "cc-tmux conductor: could not start the conductor session "
            "(is the 'claude' binary on PATH?).\n"
        )
        return 1

    opened = tmux._run_tmux(["display-popup", "-E", f"tmux attach-session -t {name}"])
    if opened is None:
        sys.stderr.write("cc-tmux conductor: display-popup failed.\n")
        return 1
    return 0


def _kill() -> int:
    """Kill the conductor session. Fail open (killing a missing session is fine)."""
    _kill_session(session_name())
    return 0


def _update_instructions() -> int:
    """Regenerate the conductor instruction file from the built-in canon (Req-10)."""
    path = write_instructions(CONDUCTOR_INSTRUCTIONS)
    if path is None:
        sys.stderr.write("cc-tmux conductor: could not write instructions file.\n")
        return 1
    print(f"cc-tmux conductor: instructions written to {path}")
    return 0


def _dispatchable_panes() -> List[tmux.PaneInfo]:
    """Every tracked pane except the conductor's own session, in priority order."""
    return sort_panes(tmux.get_hop_panes(exclude_session=session_name()))


def _list(as_json: bool) -> int:
    """Emit the dispatchable panes (Req-9). A read: always exit 0 (fail open)."""
    panes = _dispatchable_panes()
    if as_json:
        import json

        rows = [
            {
                "id": p.id,
                "session": p.session,
                "window": p.window,
                "state": p.state,
                "project": p.project,
                "branch": p.branch,
                "task": p.task,
                "wait_reason": p.wait_reason,
                "timestamp": p.timestamp,
            }
            for p in panes
        ]
        print(json.dumps(rows))
    else:
        for p in panes:
            label = p.project or p.session
            print(f"{p.id}\t{p.state}\t{p.session}:{p.window}\t{label}\t{p.task}")
    return 0


def _context() -> int:
    """Emit instructions + a live pane snapshot for the conductor's hook injection.

    Wired (by the plugin's conductor SessionStart / UserPromptSubmit hook, itself
    shell-guarded on ``CC_TMUX_CONDUCTOR=1``) so ONLY the conductor's Claude gets
    this on every prompt. Plain stdout — Claude appends it as prompt context. A
    read: always exit 0.
    """
    lines = [load_instructions().rstrip("\n"), "", "## Live pane snapshot", ""]
    panes = _dispatchable_panes()
    if not panes:
        lines.append("(no tracked Claude panes right now)")
    else:
        for p in panes:
            ident = " ".join(x for x in (p.project, p.branch) if x) or p.session
            reason = f" [{p.wait_reason}]" if p.wait_reason else ""
            task = f" — {p.task}" if p.task else ""
            lines.append(
                f"- {p.id}  {p.state}{reason}  {p.session}:{p.window}  {ident}{task}"
            )
    sys.stdout.write("\n".join(lines) + "\n")
    return 0


# ---------------------------------------------------------------------------
# Dispatch modes
# ---------------------------------------------------------------------------

def _pane_state(pane_id: str) -> Optional[str]:
    """Current tracked state of a pane among the dispatchable set, or ``None``."""
    for p in _dispatchable_panes():
        if p.id == pane_id:
            return p.state
    return None


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


def _dispatch_spawn_task(target: Optional[str], prompt: Optional[str]) -> int:
    cwd = _resolve_dir(target)
    if cwd is None:
        sys.stderr.write(
            "cc-tmux conductor: spawn-task needs --target <project dir> "
            "(or a resolvable current pane directory).\n"
        )
        return 2
    return _open_window(cwd, prompt)


def _git(cwd: str, args: List[str]) -> Optional[str]:
    if shutil.which("git") is None:
        return None
    try:
        proc = subprocess.run(
            ["git", "-C", cwd, *args],
            capture_output=True,
            text=True,
            timeout=30,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        log.warn("conductor: git %s failed: %s", " ".join(args), proc.stderr.strip())
        return None
    return proc.stdout.strip()


def _dispatch_spawn_worktree(target: Optional[str], prompt: Optional[str]) -> int:
    repo = _resolve_dir(target)
    if repo is None:
        sys.stderr.write(
            "cc-tmux conductor: spawn-worktree needs --target <git repo dir> "
            "(or a resolvable current pane directory).\n"
        )
        return 2
    toplevel = _git(repo, ["rev-parse", "--show-toplevel"])
    if not toplevel:
        sys.stderr.write(f"cc-tmux conductor: {repo} is not inside a git repository.\n")
        return 1

    stamp = time.strftime("%Y%m%d-%H%M%S")
    branch = f"conductor/{stamp}"
    wt_path = os.path.join(toplevel, ".worktrees", f"conductor-{stamp}")
    added = _git(toplevel, ["worktree", "add", "-b", branch, wt_path])
    if added is None:
        sys.stderr.write("cc-tmux conductor: git worktree add failed.\n")
        return 1
    return _open_window(wt_path, prompt)


def _dispatch(mode: Optional[str], target: Optional[str], prompt: Optional[str], force: bool) -> int:
    if mode is None:
        sys.stderr.write(
            "cc-tmux conductor: dispatch requires --mode "
            f"{{{','.join(CONDUCTOR_MODES)}}}.\n"
        )
        return 2
    if mode == "switch":
        return _dispatch_switch(target)
    if mode == "send-prompt":
        return _dispatch_send_prompt(target, prompt, force)
    if mode == "spawn-task":
        return _dispatch_spawn_task(target, prompt)
    if mode == "spawn-worktree":
        return _dispatch_spawn_worktree(target, prompt)
    # argparse constrains --mode to CONDUCTOR_MODES, so this is unreachable.
    sys.stderr.write(f"cc-tmux conductor: unknown mode {mode!r}.\n")
    return 2


# ---------------------------------------------------------------------------
# CLI handler
# ---------------------------------------------------------------------------

def cmd_conductor(args) -> int:
    """Conductor entrypoint (Req-9). Routes the flags to the right action.

    Reads (``list`` / ``context``) fail open with exit 0; lifecycle + dispatch
    return non-zero only on genuine misuse (bad target, popup while disabled, a
    missing ``claude`` binary for a spawn, git failure).
    """
    action = getattr(args, "action", None)

    if action == "list":
        return _list(getattr(args, "json", False))
    if action == "context":
        return _context()
    if action == "dispatch":
        return _dispatch(
            getattr(args, "mode", None),
            getattr(args, "target", None),
            getattr(args, "prompt", None),
            getattr(args, "force", False),
        )

    # Flag-form actions (no positional).
    if getattr(args, "update_instructions", False):
        return _update_instructions()
    if getattr(args, "kill", False):
        return _kill()
    if getattr(args, "popup", False):
        return _popup(getattr(args, "respawn", False))

    # No action selected: print a concise status line (a read -> exit 0).
    enabled = "on" if is_enabled() else "off"
    print(
        f"cc-tmux conductor: {enabled} (session '{session_name()}'). "
        "Use --popup, list, dispatch, --update-instructions, or --kill."
    )
    return 0
