"""tmux operations and the pane-option state store.

**Invariant 1 (design.md): tmux pane options are the ONLY state store.** There is
no external file, no cache, no parallel structure. Every view (cycle, inbox,
status) derives from one :func:`get_hop_panes` read, so views cannot disagree,
and state auto-deletes when a pane closes.

The tracked options on each Claude pane:

    @cc-state        waiting | idle | active
    @cc-timestamp    epoch seconds when the state was last set
    @cc-visited      epoch seconds the pane was last focused (recency tiebreak)
    @cc-task         short human summary of what the pane is doing
    @cc-wait-reason  question | plan | permission | elicitation  (only when waiting)
    @cc-project      resolved project name (git toplevel basename, or dir name)
    @cc-branch       resolved git branch
    @cc-title        Claude Code session title (SessionStart hook payload; opt-in
                      `title` window-rename format only — see cli._title_window_name)

NOTE (cc-tmux-bar-cleanup): there used to be a ``@cc-model`` option here, written
from the SessionStart hook payload's ``model`` field. That path was confirmed
empty on every live pane and also missed mid-session ``/model`` switches, so it
was removed — the session-bar row now reads the model letter fresh on every
render from ``session-context.<pane>.json`` (see cli._read_session_context)
instead of from pane-option state.

**Invariant 3 (real-transition guard):** :func:`set_pane_state` returns whether
``@cc-state`` actually changed, so callers fire auto-hop / app-focus ONLY on a
real transition and never re-yank focus on a re-asserted state.

**Invariant 4 (hot path skips git identity):** ``active`` is the most frequent
register and must stay cheap, so :func:`set_pane_state` resolves git identity
only for ``waiting`` / ``idle``. Callers may also invoke
:func:`set_pane_git_identity` directly (e.g. the inbox backfills on open).

**Invariant 5 (fail open):** with no ``$TMUX`` or no ``tmux`` binary, every
function no-ops (returns ``None`` / ``False`` / ``[]``) rather than raising, so a
hook can never block Claude.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import time
from dataclasses import dataclass
from typing import Callable, Dict, List, Optional

from . import log
from .priority import PENDING_STATES, STATE_PRIORITY, VALID_STATES

# Field separator for the batched list-panes read. Unit Separator (0x1f) will not
# appear in tmux identifiers, session/window names, or normal task text.
_FS = "\x1f"

# Pane-option names (single source of the @cc-* namespace).
OPT_STATE = "@cc-state"
OPT_TIMESTAMP = "@cc-timestamp"
OPT_VISITED = "@cc-visited"
OPT_TASK = "@cc-task"
OPT_WAIT_REASON = "@cc-wait-reason"
OPT_PROJECT = "@cc-project"
OPT_BRANCH = "@cc-branch"
OPT_TITLE = "@cc-title"

_ALL_OPTS = (
    OPT_STATE,
    OPT_TIMESTAMP,
    OPT_VISITED,
    OPT_TASK,
    OPT_WAIT_REASON,
    OPT_PROJECT,
    OPT_BRANCH,
    OPT_TITLE,
)

# Global (server) options for the daemon-free reconcile rate limit (design.md
# Decision 1). The stamp lives in a tmux GLOBAL option, dies with the server, and
# never touches an external file.
OPT_LAST_RECONCILE = "@cc-last-reconcile"
OPT_RECONCILE_INTERVAL = "@cc-reconcile-interval"
_DEFAULT_RECONCILE_INTERVAL = 10.0


@dataclass
class PaneInfo:
    """One tracked Claude pane, materialized from its tmux pane options."""

    id: str
    session: str
    window: str
    state: str
    timestamp: float
    visited: float = 0.0
    task: str = ""
    wait_reason: str = ""
    project: str = ""
    branch: str = ""


@dataclass
class WindowInfo:
    """One window in the invoking client's current session, for the tabs row.

    ``state`` is the highest-priority ``@cc-state`` among the window's tracked
    panes (same :data:`~cc_tmux.priority.STATE_PRIORITY` ordering
    :func:`get_window_top_state` uses for a single window), or ``""`` when the
    window has no tracked Claude pane. Source: :func:`get_window_tabs`.
    """

    id: str
    index: str
    name: str
    state: str = ""


# ---------------------------------------------------------------------------
# tmux availability + command runner
# ---------------------------------------------------------------------------

def tmux_available() -> bool:
    """True only when running inside tmux with a usable ``tmux`` binary."""
    if not os.environ.get("TMUX"):
        return False
    return shutil.which("tmux") is not None


def _run_tmux(args: List[str], *, check_available: bool = True) -> Optional[str]:
    """Run ``tmux <args>`` and return stripped stdout, or ``None`` on any failure.

    Fail-open: returns ``None`` (never raises) when tmux is unavailable or the
    command errors. This is the single choke point tests monkeypatch.
    """
    if check_available and not tmux_available():
        return None
    try:
        proc = subprocess.run(
            ["tmux", *args],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        log.warn("tmux %s failed: %s", " ".join(args), exc)
        return None
    if proc.returncode != 0:
        log.debug("tmux %s rc=%s stderr=%s", " ".join(args), proc.returncode, proc.stderr.strip())
        return None
    return proc.stdout.rstrip("\n")


# ---------------------------------------------------------------------------
# Reads
# ---------------------------------------------------------------------------

def get_hop_panes(exclude_session: Optional[str] = None) -> List[PaneInfo]:
    """The single, authoritative read of every tracked pane.

    One ``tmux list-panes -a`` call materializes all panes; only those carrying a
    valid ``@cc-state`` are returned (untracked panes are dropped). ``exclude_session``
    filters out a session by name (used to hide the conductor's own session).
    """
    fmt = _FS.join(
        [
            "#{pane_id}",
            "#{session_name}",
            "#{window_index}",
            "#{@cc-state}",
            "#{@cc-timestamp}",
            "#{@cc-visited}",
            "#{@cc-task}",
            "#{@cc-wait-reason}",
            "#{@cc-project}",
            "#{@cc-branch}",
        ]
    )
    out = _run_tmux(["list-panes", "-a", "-F", fmt])
    if not out:
        return []

    panes: List[PaneInfo] = []
    for line in out.split("\n"):
        if not line:
            continue
        parts = line.split(_FS)
        if len(parts) != 10:
            continue
        pane_id, session, window, state, ts, visited, task, reason, project, branch = parts
        if state not in VALID_STATES:
            continue  # untracked pane (no @cc-state, or garbage)
        if exclude_session is not None and session == exclude_session:
            continue
        try:
            timestamp = float(ts) if ts else 0.0
        except ValueError:
            timestamp = 0.0
        try:
            visited_at = float(visited) if visited else 0.0
        except ValueError:
            visited_at = 0.0
        panes.append(
            PaneInfo(
                id=pane_id,
                session=session,
                window=window,
                state=state,
                timestamp=timestamp,
                visited=visited_at,
                task=task,
                wait_reason=reason,
                project=project,
                branch=branch,
            )
        )
    return panes


def get_window_top_state(window_target: str) -> str:
    """Highest-priority ``@cc-state`` among ``window_target``'s panes, or ``""``.

    A scoped ``list-panes -t <window>`` read (NOT the full server-wide
    :func:`get_hop_panes` scan) — this is invoked by ``cc-tmux window-icon``
    once per window on every status-bar refresh (``status-interval``), so it
    needs to stay cheap regardless of how many other windows/sessions exist.
    ``""`` means no tracked (Claude) pane in that window — callers should
    treat that as "show no icon", not an error. Fail-open: no tmux -> ''.
    """
    fmt = _FS.join(["#{pane_id}", "#{@cc-state}"])
    out = _run_tmux(["list-panes", "-t", window_target, "-F", fmt])
    if not out:
        return ""
    states: List[str] = []
    for line in out.split("\n"):
        if not line:
            continue
        parts = line.split(_FS)
        if len(parts) != 2:
            continue
        _pane_id, state = parts
        if state in VALID_STATES:
            states.append(state)
    if not states:
        return ""
    return min(states, key=lambda s: STATE_PRIORITY.get(s, len(STATE_PRIORITY)))


def get_window_top_pane(window_target: str) -> str:
    """Id of the highest-priority ``@cc-state`` pane in ``window_target``, or ``""``.

    The pane-id analogue of :func:`get_window_top_state` — same scoped
    ``list-panes -t <window>`` read and same priority sort, but returns the
    winning pane's id instead of its state string. Used by the session-bar to
    pick the window's representative pane (the one whose ``@cc-model`` /
    ``@cc-project`` / ``@cc-branch`` the row renders). ``""`` means no tracked
    (Claude) pane in that window. Fail-open: no tmux -> ''.
    """
    fmt = _FS.join(["#{pane_id}", "#{@cc-state}"])
    out = _run_tmux(["list-panes", "-t", window_target, "-F", fmt])
    if not out:
        return ""
    candidates: List[tuple[str, str]] = []
    for line in out.split("\n"):
        if not line:
            continue
        parts = line.split(_FS)
        if len(parts) != 2:
            continue
        pane_id, state = parts
        if state in VALID_STATES:
            candidates.append((pane_id, state))
    if not candidates:
        return ""
    return min(candidates, key=lambda c: STATE_PRIORITY.get(c[1], len(STATE_PRIORITY)))[0]


def get_window_tabs() -> List[WindowInfo]:
    """Every window in the invoking client's current session, with its top state.

    Two batched reads (not one ``get_window_top_state`` call per window, which
    would be O(windows) tmux subprocess round-trips on every status-bar
    refresh): a ``list-windows`` for id/index/name, and a single session-scoped
    ``list-panes -s`` for every tracked pane's window id + state. Both omit an
    explicit ``-t`` — the same implicit current-session resolution
    :func:`cmd_session_bar`/:func:`cmd_beads_bar`'s window-scoped
    ``#{window_id}`` argument relies on already (a ``#()`` job spawned from a
    client's status-format string resolves default targets against that
    client's session). Per-window state reuses
    :data:`~cc_tmux.priority.STATE_PRIORITY` — the same waiting > idle > active
    precedence :func:`get_window_top_state` applies to a single window — rather
    than re-deriving the ordering. This is the data source for
    :func:`cc_tmux.render.render_tabs_row`. Fail-open: no tmux / no windows ->
    ``[]``.
    """
    fmt_w = _FS.join(["#{window_id}", "#{window_index}", "#{window_name}"])
    windows_out = _run_tmux(["list-windows", "-F", fmt_w])
    if not windows_out:
        return []

    windows: List[WindowInfo] = []
    for line in windows_out.split("\n"):
        if not line:
            continue
        parts = line.split(_FS)
        if len(parts) != 3:
            continue
        window_id, index, name = parts
        windows.append(WindowInfo(id=window_id, index=index, name=name))
    if not windows:
        return []

    fmt_p = _FS.join(["#{window_id}", "#{@cc-state}"])
    panes_out = _run_tmux(["list-panes", "-s", "-F", fmt_p])
    top_state: Dict[str, str] = {}
    if panes_out:
        by_window: Dict[str, List[str]] = {}
        for line in panes_out.split("\n"):
            if not line:
                continue
            parts = line.split(_FS)
            if len(parts) != 2:
                continue
            window_id, state = parts
            if state in VALID_STATES:
                by_window.setdefault(window_id, []).append(state)
        for window_id, states in by_window.items():
            top_state[window_id] = min(
                states, key=lambda s: STATE_PRIORITY.get(s, len(STATE_PRIORITY))
            )

    for w in windows:
        w.state = top_state.get(w.id, "")
    return windows


def session_count_glyph(project: str) -> str:
    """Session-count glyph for ``project`` — ``◌`` / ``◉`` / ``◉ N`` for 0 / 1 / 2+.

    Counts :func:`get_hop_panes` rows whose ``@cc-project`` equals ``project``
    and maps the count to the same glyph semantics ``nexus-statusline`` uses in
    ``renderStatusline`` (``apps/nexus-statusline/src/index.ts``): a single
    tracked session shows a filled ``◉``, none shows a hollow ``◌``, and 2+
    append the count (``◉ N``). Returns the bare glyph string — tmux styling is
    applied by the render/config layer, not here. Fail-open: no tmux -> ``◌``.
    """
    count = sum(1 for pane in get_hop_panes() if pane.project == project)
    if count > 1:
        return f"◉ {count}"
    if count == 1:
        return "◉"
    return "◌"


def get_pane_option(pane_id: str, option: str) -> str:
    """Read a single pane option value ('' if unset). Fail-open -> ''."""
    out = _run_tmux(["show-options", "-p", "-v", "-t", pane_id, option])
    return out or ""


def get_global_option(option: str) -> str:
    """Read a single global (server) option value ('' if unset). Fail-open -> ''."""
    out = _run_tmux(["show-options", "-g", "-v", option])
    return out or ""


def set_global_option(option: str, value: str) -> None:
    """Set a global (server) option. Used for navigation breadcrumbs, not pane state."""
    _run_tmux(["set-option", "-g", option, value])


def current_pane_id() -> Optional[str]:
    """The id of the pane the CLI is running in ($TMUX_PANE), or None."""
    env_pane = os.environ.get("TMUX_PANE", "").strip()
    if env_pane:
        return env_pane
    return _run_tmux(["display-message", "-p", "#{pane_id}"]) or None


def current_window_id() -> str:
    """Id of the active window for the invoking client's session, or ``''``.

    Used by ``cc-tmux tabs-row`` (:func:`cc_tmux.cli.cmd_tabs_row`) to mark the
    active tab distinctly in the combined row. Unlike :func:`current_pane_id`
    (hook-invoked, has a ``$TMUX_PANE`` env fast path), tabs-row is invoked
    from a status-format job with no equivalent env var, so this always shells
    out. Fail-open: no tmux -> ``''``.
    """
    return _run_tmux(["display-message", "-p", "#{window_id}"]) or ""


def switch_to_pane(pane_id: str) -> bool:
    """Focus a specific pane across sessions/windows. Returns success. Fail-open."""
    if not tmux_available():
        return False
    ok = _run_tmux(["switch-client", "-t", pane_id]) is not None
    # select-window/select-pane are cheap and make the focus deterministic even
    # when switch-client only moved the session.
    _run_tmux(["select-window", "-t", pane_id])
    _run_tmux(["select-pane", "-t", pane_id])
    return ok


def iter_panes_with_process() -> List[dict]:
    """Every pane (tracked or not) with its pid + foreground command.

    Used by ``discover`` to find already-running Claude sessions. Returns a list
    of dicts: ``{"id", "session", "window", "pid", "command", "state"}`` where
    ``state`` is the pane's current ``@cc-state`` ('' when untracked). Fail-open
    -> ``[]``.
    """
    fmt = _FS.join(
        [
            "#{pane_id}",
            "#{session_name}",
            "#{window_index}",
            "#{pane_pid}",
            "#{pane_current_command}",
            "#{@cc-state}",
        ]
    )
    out = _run_tmux(["list-panes", "-a", "-F", fmt])
    if not out:
        return []
    rows: List[dict] = []
    for line in out.split("\n"):
        if not line:
            continue
        parts = line.split(_FS)
        if len(parts) != 6:
            continue
        pane_id, session, window, pid, command, state = parts
        rows.append(
            {
                "id": pane_id,
                "session": session,
                "window": window,
                "pid": pid,
                "command": command,
                "state": state,
            }
        )
    return rows


# ---------------------------------------------------------------------------
# Pure transition decision (unit-testable, no tmux)
# ---------------------------------------------------------------------------

def is_real_transition(old_state: str, new_state: str) -> bool:
    """Whether moving old_state -> new_state is a REAL state change.

    The heart of invariant 3. Pure and side-effect free so it is unit-testable
    without a live tmux. A re-asserted state (old == new) is NOT a transition.
    """
    return old_state != new_state


# ---------------------------------------------------------------------------
# Writes
# ---------------------------------------------------------------------------

def _set_opt(pane_id: str, option: str, value: str) -> None:
    _run_tmux(["set-option", "-p", "-t", pane_id, option, value])


def _unset_opt(pane_id: str, option: str) -> None:
    _run_tmux(["set-option", "-p", "-u", "-t", pane_id, option])


def set_pane_state(
    pane_id: str,
    state: str,
    *,
    task: Optional[str] = None,
    wait_reason: Optional[str] = None,
    timestamp: Optional[float] = None,
    resolve_git: Optional[bool] = None,
    git_resolver: Optional[Callable[[str], None]] = None,
) -> bool:
    """Set a pane's tracked state; return whether ``@cc-state`` actually changed.

    Returns ``True`` only on a real transition (invariant 3). The return value is
    the real-transition guard callers use to decide whether to auto-hop / focus.

    Git identity (invariant 4): resolved only for ``waiting`` / ``idle`` by
    default (``active`` — the hot path — skips it). Pass ``resolve_git`` to force
    the decision either way. ``git_resolver`` is an injection seam for tests.

    Fail-open: with no tmux, returns ``False`` (no change) and writes nothing.
    """
    if state not in VALID_STATES:
        log.warn("ignoring unknown state %r for pane %s", state, pane_id)
        return False
    if not tmux_available():
        return False

    old_state = get_pane_option(pane_id, OPT_STATE)
    changed = is_real_transition(old_state, state)

    _set_opt(pane_id, OPT_STATE, state)
    _set_opt(pane_id, OPT_TIMESTAMP, str(timestamp if timestamp is not None else time.time()))

    if task is not None:
        _set_opt(pane_id, OPT_TASK, task)

    # wait_reason only makes sense while waiting; clear it otherwise.
    if state == "waiting":
        if wait_reason is not None:
            _set_opt(pane_id, OPT_WAIT_REASON, wait_reason)
    else:
        _unset_opt(pane_id, OPT_WAIT_REASON)

    # Hot-path guard: resolve git identity only for pending states unless forced.
    if resolve_git is None:
        resolve_git = state in PENDING_STATES
    if resolve_git:
        resolver = git_resolver or set_pane_git_identity
        resolver(pane_id)

    return changed


def set_pane_git_identity(pane_id: str) -> None:
    """Resolve and store ``@cc-project`` / ``@cc-branch`` for a pane.

    Uses the pane's current working directory: project = git toplevel basename
    (falling back to the directory basename outside a repo), branch = current git
    branch (empty outside a repo / detached). Fail-open: writes nothing it cannot
    resolve.
    """
    cwd = _run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_current_path}"])
    if not cwd:
        return

    project = _git_toplevel_name(cwd) or os.path.basename(os.path.normpath(cwd))
    branch = _git_branch(cwd)

    if project:
        _set_opt(pane_id, OPT_PROJECT, project)
    if branch:
        _set_opt(pane_id, OPT_BRANCH, branch)


def set_pane_title(pane_id: str, title: str) -> None:
    """Store the Claude Code session title for the opt-in ``title`` window-rename
    format (``@cc-window-rename-format title`` — see cli._title_window_name).

    ``title`` comes from the SessionStart hook payload's ``session_title`` field:
    the custom title if the user set one (``/rename`` or ``-n``), else Claude's
    own default. Fail-open: no tmux, or an empty title, writes nothing.
    """
    if not tmux_available() or not title:
        return
    _set_opt(pane_id, OPT_TITLE, title)


def set_pane_visited(pane_id: str, timestamp: Optional[float] = None) -> None:
    """Stamp ``@cc-visited`` with the current epoch (MRU recency tiebreak).

    Called on ``pane-focus-in`` for tracked panes so the most-recently-focused
    pane surfaces first within its priority group. Fail-open: no tmux -> no-op.
    """
    if not tmux_available():
        return
    _set_opt(pane_id, OPT_VISITED, str(timestamp if timestamp is not None else time.time()))


def clear_pane_state(pane_id: str) -> None:
    """Remove every ``@cc-*`` option from a pane (SessionEnd cleanup). Fail-open."""
    if not tmux_available():
        return
    for opt in _ALL_OPTS:
        _unset_opt(pane_id, opt)


# ---------------------------------------------------------------------------
# Daemon-free reconcile (design.md Decision 1)
# ---------------------------------------------------------------------------

def should_reconcile(last: float, now: float, interval: float) -> bool:
    """Pure rate-limit gate: True when at least ``interval`` seconds elapsed since
    ``last`` (``last`` == 0 means never reconciled -> always True). Unit-testable
    without a live tmux (task 1.6)."""
    return (now - last) >= interval


def _float_global(option: str) -> float:
    raw = get_global_option(option)
    try:
        return float(raw) if raw else 0.0
    except ValueError:
        return 0.0


def _reconcile_interval() -> float:
    """Min seconds between process scans; overridable via ``@cc-reconcile-interval``."""
    raw = get_global_option(OPT_RECONCILE_INTERVAL)
    try:
        val = float(raw) if raw else _DEFAULT_RECONCILE_INTERVAL
    except ValueError:
        val = _DEFAULT_RECONCILE_INTERVAL
    return val if val > 0 else _DEFAULT_RECONCILE_INTERVAL


def _heal_stale(
    panes: List[PaneInfo],
    claude_ids_fn: Callable[[List[dict]], set],
) -> List[PaneInfo]:
    """Clear stale ``@cc-state`` left by a kill -9'd Claude.

    A tracked pane still present in tmux but no longer running Claude gets its
    ``@cc-*`` state cleared. A FAILED process scan (empty result) clears nothing
    rather than mass-wiping live sessions. Returns a fresh read when anything was
    healed, else the input list.
    """
    rows = iter_panes_with_process()
    if not rows:
        return panes  # scan failed / unavailable -> do not clear anything
    claude_ids = claude_ids_fn(rows)
    present = {row["id"] for row in rows}
    healed = False
    for pane in panes:
        if pane.id in present and pane.id not in claude_ids:
            clear_pane_state(pane.id)
            healed = True
    return get_hop_panes() if healed else panes


def reconcile(
    claude_ids_fn: Callable[[List[dict]], set],
    *,
    now: Optional[float] = None,
) -> List[PaneInfo]:
    """Rate-limited self-heal shared by the read entry points (design.md Dec. 1).

    Returns the current (possibly healed) hop-pane list. The process scan runs at
    most once per ``@cc-reconcile-interval`` seconds, gated by the
    ``@cc-last-reconcile`` global-option stamp, so the status bar's frequent
    render never pays a scan on every tick. ``claude_ids_fn`` maps
    :func:`iter_panes_with_process` rows to the set of pane ids running Claude
    (injected by the caller to avoid a cli<->tmux import cycle). Fail-open.
    """
    panes = get_hop_panes()
    if not tmux_available():
        return panes
    now = now if now is not None else time.time()
    if not should_reconcile(_float_global(OPT_LAST_RECONCILE), now, _reconcile_interval()):
        return panes
    set_global_option(OPT_LAST_RECONCILE, str(now))
    return _heal_stale(panes, claude_ids_fn)


# ---------------------------------------------------------------------------
# git helpers (used only off the hot path)
# ---------------------------------------------------------------------------

def _run_git(cwd: str, args: List[str]) -> Optional[str]:
    if shutil.which("git") is None:
        return None
    try:
        proc = subprocess.run(
            ["git", "-C", cwd, *args],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    return proc.stdout.strip()


def _git_toplevel_name(cwd: str) -> str:
    top = _run_git(cwd, ["rev-parse", "--show-toplevel"])
    if not top:
        return ""
    return os.path.basename(os.path.normpath(top))


def _git_branch(cwd: str) -> str:
    branch = _run_git(cwd, ["rev-parse", "--abbrev-ref", "HEAD"])
    if not branch or branch == "HEAD":
        return ""
    return branch
