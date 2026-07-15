"""Built-in pure-function test suite for ``cc-tmux self-test`` (Req-13).

These tests exercise the logic that MUST be correct without a live tmux server:
the priority sort/cycle rules, the ``set_pane_state`` transition-detection
decision (with tmux calls mocked), and path detection. No external test runner —
stdlib only, so the suite runs anywhere ``python3`` does.

Run via ``cc-tmux self-test`` (exit 0 = pass, non-zero = failure count).
"""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import os
import shutil
import subprocess
import tempfile
import time
from dataclasses import dataclass
from typing import Callable, List, Optional, Tuple

from . import cli, conductor, nx_agent, priority, registry, render, tmux, usage

# Shared with render.strip_ansi / cli's popup-width sizing (cc-tmux-status-bar-
# popup-polish task 3.4 follow-up, 2026-07-14) -- one regex, not two drifting
# copies. Kept as a local alias so the 11 existing `_strip_ansi(...)` call
# sites below don't all need renaming.
_strip_ansi = render.strip_ansi


# ---------------------------------------------------------------------------
# Tiny test harness (no deps)
# ---------------------------------------------------------------------------

class _AssertError(AssertionError):
    pass


def _check(cond: bool, msg: str) -> None:
    if not cond:
        raise _AssertError(msg)


@dataclass
class _FakePane:
    """Minimal stand-in for PaneInfo — priority.py reads state/timestamp/visited/id."""

    id: str
    state: str
    timestamp: float
    visited: float = 0.0


@dataclass
class _FakeWindow:
    """Minimal stand-in for tmux.WindowInfo — render.render_tabs_row reads id/index/name/state."""

    id: str
    index: str
    name: str
    state: str = ""


# ---------------------------------------------------------------------------
# priority.py tests
# ---------------------------------------------------------------------------

def _test_priority_constants() -> None:
    _check(priority.STATE_PRIORITY == {"waiting": 0, "idle": 1, "active": 2}, "STATE_PRIORITY drift")
    _check(priority.PENDING_STATES == frozenset({"waiting", "idle"}), "PENDING_STATES drift")
    _check("active" not in priority.PENDING_STATES, "active must not be pending")
    _check(priority.VALID_CYCLE_MODES == ["priority", "flat"], "VALID_CYCLE_MODES drift")


def _sample_panes() -> List[_FakePane]:
    # ids encode expectation; timestamps chosen so ordering is unambiguous.
    return [
        _FakePane("%active_old", "active", 10.0),
        _FakePane("%idle_new", "idle", 50.0),
        _FakePane("%waiting_old", "waiting", 20.0),
        _FakePane("%idle_old", "idle", 30.0),
        _FakePane("%waiting_new", "waiting", 40.0),
        _FakePane("%active_new", "active", 60.0),
    ]


def _test_sort_order() -> None:
    ordered = [p.id for p in priority.sort_panes(_sample_panes())]
    expected = [
        "%waiting_new",  # waiting group, newest first
        "%waiting_old",
        "%idle_new",     # idle group, newest first
        "%idle_old",
        "%active_new",   # active group, newest first
        "%active_old",
    ]
    _check(ordered == expected, f"sort order wrong: {ordered}")


def _test_group_by_state() -> None:
    groups = priority.group_by_state(_sample_panes())
    _check(set(groups) == {"waiting", "idle", "active"}, "group keys wrong")
    _check([p.id for p in groups["waiting"]] == ["%waiting_new", "%waiting_old"], "waiting bucket order")
    _check([p.id for p in groups["idle"]] == ["%idle_new", "%idle_old"], "idle bucket order")
    # empty state -> empty list, no KeyError
    _check(priority.group_by_state([])["active"] == [], "empty group should be []")


def _test_cycle_priority_mode() -> None:
    # highest non-empty pending group only -> waiting group
    ring = [p.id for p in priority.cycle_order(_sample_panes(), "priority")]
    _check(ring == ["%waiting_new", "%waiting_old"], f"priority ring wrong: {ring}")

    # no waiting -> falls through to idle group
    idle_only = [
        _FakePane("%i2", "idle", 2.0),
        _FakePane("%i1", "idle", 1.0),
        _FakePane("%a", "active", 9.0),
    ]
    ring2 = [p.id for p in priority.cycle_order(idle_only, "priority")]
    _check(ring2 == ["%i2", "%i1"], f"idle-fallthrough ring wrong: {ring2}")

    # active never cycled
    active_only = [_FakePane("%a", "active", 1.0)]
    _check(priority.cycle_order(active_only, "priority") == [], "active must not be in ring")


def _test_cycle_flat_mode() -> None:
    ring = [p.id for p in priority.cycle_order(_sample_panes(), "flat")]
    _check(
        ring == ["%waiting_new", "%waiting_old", "%idle_new", "%idle_old"],
        f"flat ring wrong: {ring}",
    )
    # active excluded even in flat mode
    _check(all("active" not in i for i in ring), "flat ring leaked active")


def _test_cycle_bad_mode_falls_back() -> None:
    ring = [p.id for p in priority.cycle_order(_sample_panes(), "nonsense")]
    priority_ring = [p.id for p in priority.cycle_order(_sample_panes(), "priority")]
    _check(ring == priority_ring, "bad mode should fall back to priority")


def _test_recency_tiebreak_within_group() -> None:
    # Same state: the more-recently-VISITED pane wins even with an OLDER timestamp
    # (Decision 2 — visited is the primary within-group key, timestamp the fallback).
    panes = [
        _FakePane("%stale_ts_recent_visit", "waiting", timestamp=10.0, visited=99.0),
        _FakePane("%fresh_ts_no_visit", "waiting", timestamp=50.0, visited=0.0),
    ]
    ordered = [p.id for p in priority.sort_panes(panes)]
    _check(ordered[0] == "%stale_ts_recent_visit", f"visited must beat timestamp: {ordered}")
    grouped = [p.id for p in priority.group_by_state(panes)["waiting"]]
    _check(grouped[0] == "%stale_ts_recent_visit", f"group_by_state tiebreak wrong: {grouped}")


def _test_group_order_unchanged_by_visits() -> None:
    # A never-visited waiting pane still outranks a heavily-visited active pane:
    # the priority GROUP dominates; recency is only a within-group tiebreak.
    panes = [
        _FakePane("%active_visited", "active", timestamp=100.0, visited=1000.0),
        _FakePane("%waiting_unvisited", "waiting", timestamp=1.0, visited=0.0),
    ]
    ordered = [p.id for p in priority.sort_panes(panes)]
    _check(ordered == ["%waiting_unvisited", "%active_visited"], f"group order broke: {ordered}")


def _test_missing_visited_timestamp_fallback() -> None:
    # No visits anywhere -> fall back to timestamp desc (legacy ordering preserved).
    panes = [
        _FakePane("%old", "idle", timestamp=10.0),
        _FakePane("%new", "idle", timestamp=20.0),
    ]
    ordered = [p.id for p in priority.sort_panes(panes)]
    _check(ordered == ["%new", "%old"], f"timestamp fallback wrong: {ordered}")


def _test_reconcile_rate_limit() -> None:
    # Pure rate-limit gate for the daemon-free reconcile (Decision 1).
    _check(tmux.should_reconcile(0.0, 100.0, 10.0) is True, "last=0 (never) -> reconcile")
    _check(tmux.should_reconcile(95.0, 100.0, 10.0) is False, "5s elapsed < 10s -> skip")
    _check(tmux.should_reconcile(90.0, 100.0, 10.0) is True, "exactly interval -> reconcile")
    _check(tmux.should_reconcile(80.0, 100.0, 10.0) is True, ">interval -> reconcile")


def _test_select_next() -> None:
    panes = _sample_panes()
    # from head -> next in ring
    nxt = priority.select_next(panes, "%waiting_new", "priority")
    _check(nxt is not None and nxt.id == "%waiting_old", "select_next step wrong")
    # from tail -> wraps to head
    nxt = priority.select_next(panes, "%waiting_old", "priority")
    _check(nxt is not None and nxt.id == "%waiting_new", "select_next wrap wrong")
    # current not in ring (e.g. an active pane) -> ring head
    nxt = priority.select_next(panes, "%active_new", "priority")
    _check(nxt is not None and nxt.id == "%waiting_new", "select_next non-member -> head")
    # current None -> head
    nxt = priority.select_next(panes, None, "flat")
    _check(nxt is not None and nxt.id == "%waiting_new", "select_next None -> head")
    # empty ring -> None
    _check(priority.select_next([_FakePane("%a", "active", 1.0)], None, "priority") is None, "empty -> None")


# ---------------------------------------------------------------------------
# tmux.set_pane_state transition-detection tests (tmux calls mocked)
# ---------------------------------------------------------------------------

class _TmuxMock:
    """Context manager that mocks tmux availability + the _run_tmux choke point.

    ``old_state`` is what a show-options read returns; every write is recorded so
    tests can assert what was set without a live server.
    """

    def __init__(self, old_state: str):
        self.old_state = old_state
        self.calls: List[List[str]] = []
        self._saved: dict = {}

    def _run(self, args: List[str], *, check_available: bool = True):
        self.calls.append(list(args))
        if args and args[0] == "show-options" and "@cc-state" in args:
            return self.old_state
        return ""  # writes / other reads

    def __enter__(self) -> "_TmuxMock":
        self._saved["available"] = tmux.tmux_available
        self._saved["run"] = tmux._run_tmux
        tmux.tmux_available = lambda: True  # type: ignore[assignment]
        tmux._run_tmux = self._run  # type: ignore[assignment]
        return self

    def __exit__(self, *exc) -> None:
        tmux.tmux_available = self._saved["available"]  # type: ignore[assignment]
        tmux._run_tmux = self._saved["run"]  # type: ignore[assignment]


def _test_is_real_transition_pure() -> None:
    _check(tmux.is_real_transition("idle", "active") is True, "diff states -> transition")
    _check(tmux.is_real_transition("idle", "idle") is False, "same state -> no transition")
    _check(tmux.is_real_transition("", "idle") is True, "unset -> tracked is a transition")


def _test_set_pane_state_returns_change() -> None:
    # Real transition: old idle -> new waiting => True
    with _TmuxMock("idle"):
        changed = tmux.set_pane_state("%1", "waiting", wait_reason="permission", git_resolver=lambda _p: None)
    _check(changed is True, "idle->waiting should report changed")

    # Re-asserted state: old idle -> new idle => False (real-transition guard)
    with _TmuxMock("idle"):
        changed = tmux.set_pane_state("%1", "idle", git_resolver=lambda _p: None)
    _check(changed is False, "idle->idle should report NO change")


def _test_set_pane_state_hot_path_skips_git() -> None:
    resolved: List[str] = []
    resolver = lambda pane_id: resolved.append(pane_id)  # noqa: E731

    # active is the hot path: git identity MUST be skipped.
    with _TmuxMock("idle"):
        tmux.set_pane_state("%1", "active", git_resolver=resolver)
    _check(resolved == [], "active register must skip git identity")

    # waiting/idle resolve git identity.
    with _TmuxMock("active"):
        tmux.set_pane_state("%1", "idle", git_resolver=resolver)
    _check(resolved == ["%1"], "idle register must resolve git identity")


def _test_set_pane_state_unknown_state() -> None:
    with _TmuxMock("idle") as mock:
        changed = tmux.set_pane_state("%1", "bogus", git_resolver=lambda _p: None)
    _check(changed is False, "unknown state -> False")
    _check(mock.calls == [], "unknown state must write nothing")


def _test_set_pane_state_writes_state_and_timestamp() -> None:
    with _TmuxMock("active") as mock:
        tmux.set_pane_state("%9", "waiting", wait_reason="question", git_resolver=lambda _p: None)
    wrote_state = any(
        c[0] == "set-option" and tmux.OPT_STATE in c and "waiting" in c for c in mock.calls
    )
    wrote_ts = any(c[0] == "set-option" and tmux.OPT_TIMESTAMP in c for c in mock.calls)
    wrote_reason = any(
        c[0] == "set-option" and tmux.OPT_WAIT_REASON in c and "question" in c for c in mock.calls
    )
    _check(wrote_state, "must write @cc-state")
    _check(wrote_ts, "must write @cc-timestamp")
    _check(wrote_reason, "must write @cc-wait-reason when waiting")


def _test_tmux_set_pane_git_identity_unsets_branch() -> None:
    calls: List[List[str]] = []

    def fake_run(args, *, check_available: bool = True):
        calls.append(list(args))
        if args and args[0] == "display-message":
            return "/tmp/somewhere"
        return ""

    saved_run = tmux._run_tmux
    saved_top = tmux._git_toplevel_name
    saved_branch = tmux._git_branch
    tmux._run_tmux = fake_run  # type: ignore[assignment]
    tmux._git_toplevel_name = lambda cwd: "proj"  # type: ignore[assignment]
    tmux._git_branch = lambda cwd: ""  # type: ignore[assignment]
    try:
        tmux.set_pane_git_identity("%7")
    finally:
        tmux._run_tmux = saved_run  # type: ignore[assignment]
        tmux._git_toplevel_name = saved_top  # type: ignore[assignment]
        tmux._git_branch = saved_branch  # type: ignore[assignment]

    wrote_project = any(
        c[0] == "set-option" and tmux.OPT_PROJECT in c and "proj" in c for c in calls
    )
    unset_branch = any(
        c[0] == "set-option" and "-u" in c and tmux.OPT_BRANCH in c for c in calls
    )
    set_branch = any(
        c[0] == "set-option" and "-u" not in c and tmux.OPT_BRANCH in c for c in calls
    )
    _check(wrote_project, "empty-branch resolution must still write @cc-project")
    _check(unset_branch, "empty-branch resolution must UNSET @cc-branch (stale-value bug)")
    _check(not set_branch, "empty-branch resolution must not SET @cc-branch")


def _test_set_pane_state_reassert_skips_timestamp() -> None:
    # Re-asserted state (idle -> idle): @cc-state may be rewritten but
    # @cc-timestamp must NOT be restamped — the inbox dismiss contract
    # (cli.cmd_inbox: "a fresh transition reappears") and priority ordering
    # read the timestamp as TRANSITION time.
    with _TmuxMock("idle") as mock:
        tmux.set_pane_state("%1", "idle", git_resolver=lambda _p: None)
    wrote_ts = any(c[0] == "set-option" and tmux.OPT_TIMESTAMP in c for c in mock.calls)
    _check(not wrote_ts, "re-assert must NOT restamp @cc-timestamp")

    # An explicit timestamp kwarg is a caller override: writes even on re-assert.
    with _TmuxMock("idle") as mock2:
        tmux.set_pane_state("%1", "idle", timestamp=123.0, git_resolver=lambda _p: None)
    wrote_override = any(
        c[0] == "set-option" and tmux.OPT_TIMESTAMP in c and "123.0" in c for c in mock2.calls
    )
    _check(wrote_override, "explicit timestamp kwarg must write even on re-assert")


# ---------------------------------------------------------------------------
# registry.py / cli.py title window-rename tests (tab naming)
# ---------------------------------------------------------------------------

def _test_registry_resolve_project_code() -> None:
    if registry.tomllib is None:
        # 3.10 interpreter (no stdlib tomllib): must fail open to "", never raise.
        _check(registry.resolve_project_code("/tmp/whatever") == "", "no tomllib -> fails open")
        return

    saved = os.environ.get("DOTFILES")
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-registry-test-")
    try:
        rel = "cc-tmux-test-project-zzz"
        os.makedirs(os.path.join(tmpdir, "home"), exist_ok=True)
        with open(os.path.join(tmpdir, "home", "projects.toml"), "w") as f:
            f.write(f'[[projects]]\ncode = "zz"\nname = "Test"\npath = "{rel}"\n')
        os.environ["DOTFILES"] = tmpdir

        nested = os.path.join(os.path.expanduser("~"), rel, "nested", "dir")
        _check(registry.resolve_project_code(nested) == "zz", "subdir must resolve to owning project's code")
        _check(registry.resolve_project_code("/definitely/not/tracked") == "", "unmatched cwd -> ''")
        _check(registry.resolve_project_code("") == "", "empty cwd -> ''")
    finally:
        if saved is None:
            os.environ.pop("DOTFILES", None)
        else:
            os.environ["DOTFILES"] = saved
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_registry_resolve_project_code_symlink_alias() -> None:
    """A registry entry whose ``path`` is a symlink alias must still resolve
    when queried by the symlink's REAL target — the exact shape of a real
    production bug: ``home/projects.toml``'s ``cc`` entry registers
    ``.claude`` (``~/.claude``), which is itself a symlink to ``~/dev/cc``.
    A tmux pane's shell cwd reports the REAL target path (``pwd`` resolves
    symlinks on ``cd``), so the un-resolved string-prefix match used to fail
    for any pane actually sitting in ``~/dev/cc``, silently blanking row 3
    (openspec/beads) for that entire project. Both
    :func:`registry._load_path_to_code` and :func:`registry.resolve_project_code`
    now resolve via ``realpath``, so either side's alias or real path
    resolves to the same registry code.
    """
    if registry.tomllib is None:
        return  # covered by the no-tomllib fail-open case in the sibling test above
    saved = os.environ.get("DOTFILES")
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-registry-symlink-test-")
    home = os.path.expanduser("~")
    alias_name = "cc-tmux-test-symlink-alias-zzz"
    real_target_dir = os.path.join(tmpdir, "real-target")
    alias_path = os.path.join(home, alias_name)
    os.makedirs(real_target_dir, exist_ok=True)
    try:
        os.symlink(real_target_dir, alias_path)
    except OSError:
        shutil.rmtree(tmpdir, ignore_errors=True)
        return  # no symlink permission on this filesystem -> skip, not a failure
    try:
        os.makedirs(os.path.join(tmpdir, "home"), exist_ok=True)
        with open(os.path.join(tmpdir, "home", "projects.toml"), "w") as f:
            f.write(f'[[projects]]\ncode = "zz"\nname = "Test"\npath = "{alias_name}"\n')
        os.environ["DOTFILES"] = tmpdir
        _check(
            registry.resolve_project_code(alias_path) == "zz",
            "querying via the registered alias path itself still resolves",
        )
        _check(
            registry.resolve_project_code(real_target_dir) == "zz",
            "querying via the symlink's REAL target resolves too (the ~/.claude -> ~/dev/cc bug)",
        )
        nested_real = os.path.join(real_target_dir, "nested", "dir")
        _check(
            registry.resolve_project_code(nested_real) == "zz",
            "a subdir of the real target also resolves",
        )
    finally:
        if saved is None:
            os.environ.pop("DOTFILES", None)
        else:
            os.environ["DOTFILES"] = saved
        try:
            os.unlink(alias_path)
        except OSError:
            pass
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_compose_title_name() -> None:
    # 20-char budget (cc-tmux-rename-fix-and-truncate; was 10): an 11-20 char
    # combined code+title now renders in full instead of truncating.
    combined_15 = cli.compose_title_name("if", "Fix ssh mesh")
    _check(combined_15 == "if·Fix ssh mesh", f"15-char combined must render in full, got {combined_15!r}")
    _check(len(combined_15) == 15, "15-char combined must not truncate at the 20-char budget")
    # Anything over 20 chars combined still truncates — now at 20, not 10.
    combined_over = cli.compose_title_name("if", "Fix ssh mesh auth flow long")
    _check(combined_over == "if·Fix ssh mesh auth", f"20-char truncation, got {combined_over!r}")
    _check(len(combined_over) == 20, "over-budget combined must truncate to exactly 20 chars")
    _check(cli.compose_title_name("if", "") == "if", "empty title falls back to code alone")
    _check(cli.compose_title_name("", "hello") == "hello", "empty code falls back to title alone")
    _check(cli.compose_title_name("", "", fallback="myproj") == "myproj", "both empty -> fallback")
    _check(len(cli.compose_title_name("if", "a very very long title indeed")) == 20, "always capped at 20")


def _test_maybe_rename_window_success_failure() -> None:
    """rename-fix-and-truncate: _maybe_rename_window reports actual tmux
    success/failure (``_run_tmux``'s ``None``-on-failure contract), not just
    "a rename-window call was issued"."""
    pane_line = tmux._FS.join(
        ["%1", "sess", "0", "idle", "100.0", "0.0", "", "", "proj", ""]
    )
    rename_result: List[Optional[str]] = [""]

    def fake_run(args: List[str], *, check_available: bool = True):
        if args and args[0] == "show-options" and args[-1] == cli._WINDOW_RENAME_OPT:
            return "on"  # @cc-window-rename enabled
        if args and args[0] == "list-panes":
            return pane_line  # tracked pane + itself as sibling
        if args and args[0] == "display-message":
            return "/tmp/myproject"  # pane cwd, for the default "state" format
        if args and args[0] == "rename-window":
            return rename_result[0]
        return ""  # @cc-window-rename-format (-> default "state"), set-window-option, etc.

    saved = tmux._run_tmux
    tmux._run_tmux = fake_run  # type: ignore[assignment]
    try:
        rename_result[0] = ""  # success: _run_tmux returns stripped stdout (possibly empty), never None
        _check(cli._maybe_rename_window("%1") is True, "successful rename-window must return True")

        rename_result[0] = None  # failure: _run_tmux's documented None-on-failure contract
        _check(cli._maybe_rename_window("%1") is False, "failed rename-window must return False")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_trace_register_rename_succeeded_field() -> None:
    """rename-fix-and-truncate: _trace_register's rename_succeeded field is
    distinct from the always-True rename_attempted and from rename_fired, and
    reflects whatever _maybe_rename_window's return value was threaded in."""
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-trace-test-")
    saved_config_dir = os.environ.get("CLAUDE_CONFIG_DIR")
    os.environ["CLAUDE_CONFIG_DIR"] = tmpdir
    try:
        trace_path = cli._cc_state_dir() / cli._REGISTER_TRACE_FILE
        cli._trace_register({"hook_event_name": "SessionStart"}, "%1", True, rename_succeeded=True)
        cli._trace_register({"hook_event_name": "PreToolUse"}, "%1", False, rename_succeeded=False)

        lines = trace_path.read_text(encoding="utf-8").splitlines()
        _check(len(lines) == 2, f"expected 2 trace lines, got {len(lines)}")
        entry_success = json.loads(lines[0])
        entry_failure = json.loads(lines[1])

        _check(entry_success["rename_attempted"] is True, "rename_attempted must always be True")
        _check(entry_success["rename_fired"] is True, "rename_fired must reflect the success call")
        _check(entry_success["rename_succeeded"] is True, "rename_succeeded must be True on success")

        _check(entry_failure["rename_attempted"] is True, "rename_attempted must always be True")
        _check(entry_failure["rename_fired"] is False, "rename_fired must reflect the failure call")
        _check(entry_failure["rename_succeeded"] is False, "rename_succeeded must be False on failure")
    finally:
        if saved_config_dir is None:
            os.environ.pop("CLAUDE_CONFIG_DIR", None)
        else:
            os.environ["CLAUDE_CONFIG_DIR"] = saved_config_dir
        shutil.rmtree(tmpdir, ignore_errors=True)


# ---------------------------------------------------------------------------
# render.py tests (pure presentation logic — Req-5 / Req-7)
# ---------------------------------------------------------------------------

def _test_render_format_duration() -> None:
    _check(render.format_duration(5) == "5s", "5s")
    _check(render.format_duration(59) == "59s", "59s")
    _check(render.format_duration(60) == "1m", "1m")
    _check(render.format_duration(3600) == "1h", "1h")
    _check(render.format_duration(90000) == "1d", "1d")
    _check(render.format_duration(-3) == "0s", "negative floors to 0s")



def _test_render_resolve_icons() -> None:
    # default when no override
    icons = render.resolve_icons(lambda _opt: "")
    _check(icons == render.DEFAULT_ICONS, "defaults when unset")
    # per-state override honored
    overrides = {"@cc-icon-waiting": "!!"}
    icons2 = render.resolve_icons(lambda opt: overrides.get(opt, ""))
    _check(icons2["waiting"] == "!!", "waiting override applied")
    _check(icons2["idle"] == render.DEFAULT_ICONS["idle"], "idle keeps default")


def _test_render_animated_icon() -> None:
    _check(render.animated_icon("idle", 0.0) == render.IDLE_GLYPH, "idle -> static glyph")
    _check(render.animated_icon("idle", 999.0) == render.IDLE_GLYPH, "idle never changes with time")
    _check(render.animated_icon("waiting", 0.0) == render.SHADE_FRAMES[0], "waiting frame 0")
    _check(render.animated_icon("waiting", 1.0) == render.SHADE_FRAMES[1], "waiting frame 1s later")
    n_shade = len(render.SHADE_FRAMES)
    _check(render.animated_icon("waiting", float(n_shade)) == render.SHADE_FRAMES[0], "waiting wraps around")
    _check(render.animated_icon("active", 0.0) == render.BLOCK_FRAMES[0], "active frame 0")
    _check(render.animated_icon("active", 2.0) == render.BLOCK_FRAMES[2], "active frame 2s later")
    _check(render.animated_icon("bogus-state", 0.0) == "", "unknown state -> ''")


# ---------------------------------------------------------------------------
# render.py idle-tab usage meter tests (cc-tmux-idle-tab-usage-meter)
# ---------------------------------------------------------------------------

def _test_render_idle_meter_ramp_sweep() -> None:
    scale = render.IDLE_METER_SCALE_TOKENS
    cases = [
        (0.0625, "⡀"),
        (0.5, "⣿"),
        (0.75, "⠛"),
        (0.9375, "⠈"),
        (1.0, "▓"),
        (1.5, "▓"),  # above 1.0 also clamps to the top glyph
    ]
    for ratio, expected_glyph in cases:
        idx = render._idle_meter_index(ratio)
        _check(
            render.IDLE_METER_RAMP[idx] == expected_glyph,
            f"ratio {ratio} -> idx {idx} -> {render.IDLE_METER_RAMP[idx]!r}, expected {expected_glyph!r}",
        )
        raw_tokens = ratio * scale
        glyph, _color = render.idle_usage_meter(raw_tokens, now=0.0)
        _check(
            glyph == expected_glyph,
            f"idle_usage_meter ratio={ratio} (raw_tokens={raw_tokens}) glyph {glyph!r}, expected {expected_glyph!r}",
        )


def _test_render_idle_meter_index0_flash() -> None:
    # raw_tokens near-zero -> ratio ~0.001 -> idx 0.
    raw_tokens = 1000
    _check(render._idle_meter_index(raw_tokens / render.IDLE_METER_SCALE_TOKENS) == 0, "sanity: lands in index 0")
    glyph_even, _ = render.idle_usage_meter(raw_tokens, now=0.0)  # int(0/1) % 2 == 0
    glyph_odd, _ = render.idle_usage_meter(raw_tokens, now=1.0)  # int(1/1) % 2 == 1
    _check(glyph_even == "⠀", f"even FRAME_PERIOD_SEC parity -> blank U+2800, got {glyph_even!r}")
    _check(glyph_odd == render.IDLE_METER_RAMP[0] == "░", f"odd parity -> ramp[0] '░', got {glyph_odd!r}")


def _test_render_idle_meter_none_fallback() -> None:
    for now in (0.0, 1.0, 42.5):
        glyph, color = render.idle_usage_meter(None, now)
        _check(glyph == render.IDLE_GLYPH, f"None raw_tokens -> IDLE_GLYPH at now={now}, got {glyph!r}")
        _check(color == "", f"None raw_tokens -> empty color at now={now}, got {color!r}")


def _test_render_idle_meter_color_matches_resolve_context_color() -> None:
    # One raw_tokens value per severity tier (design.md § Color + pulse),
    # including the >750k pulse tier checked at BOTH FRAME_PERIOD_SEC parities.
    tier_values = [
        50_000,   # DIM (<=100k)
        150_000,  # GREEN (>100k)
        250_000,  # YELLOW (>200k)
        400_000,  # ORANGE (>300k)
        550_000,  # RED steady (>500k)
        650_000,  # RED<->BRIGHT_RED pulse (>600k)
        800_000,  # DARK_RED<->RED pulse (>750k)
    ]
    for raw_tokens in tier_values:
        for now in (0.0, 1.0):
            _, color = render.idle_usage_meter(raw_tokens, now)
            expected = render.resolve_context_color(raw_tokens, now)
            _check(
                color == expected,
                f"raw_tokens={raw_tokens} now={now}: idle meter color {color!r} != resolve_context_color {expected!r}",
            )


def _test_render_resolve_tab_glyph_precedence() -> None:
    """resolve_tab_glyph precedence (task 4.2): waiting, active, fg=1 (foreground
    sub-agent), and bg=2 (background sub-agent) ALL return
    ``(resolve_tab_icon(state, now, fg_count, bg_count), "")`` byte-identical to
    calling resolve_tab_icon directly with an empty colour — the meter is never
    reached in those cases. ONLY the plain-idle case (fg=0, bg=0, state=="idle")
    routes to :func:`render.idle_usage_meter`."""
    # waiting state, no subagents -> plain resolve_tab_icon passthrough.
    icon = render.resolve_tab_icon("waiting", 0.0, 0, 0)
    _check(
        render.resolve_tab_glyph("waiting", 0.0, 0, 0) == (icon, ""),
        "waiting: byte-identical to resolve_tab_icon, empty colour",
    )

    # active state, no subagents -> plain resolve_tab_icon passthrough.
    icon = render.resolve_tab_icon("active", 2.0, 0, 0)
    _check(
        render.resolve_tab_glyph("active", 2.0, 0, 0) == (icon, ""),
        "active: byte-identical to resolve_tab_icon, empty colour",
    )

    # fg=1 (foreground sub-agent), even with state=="idle" -> subagent overlay
    # wins, never the meter.
    icon = render.resolve_tab_icon("idle", 0.0, 1, 0)
    _check(icon == render.SUBAGENT_FG_1, "sanity: fg=1 -> hollow ring")
    _check(
        render.resolve_tab_glyph("idle", 0.0, 1, 0) == (icon, ""),
        "fg=1: byte-identical to resolve_tab_icon, empty colour, not the meter",
    )

    # bg=2 (background sub-agents), fg=0, state=="idle" -> subagent overlay
    # wins, never the meter.
    icon = render.resolve_tab_icon("idle", 0.0, 0, 2)
    _check(icon == render.SUBAGENT_BG_2PLUS, "sanity: fg=0,bg=2 -> filled diamond")
    _check(
        render.resolve_tab_glyph("idle", 0.0, 0, 2) == (icon, ""),
        "bg=2: byte-identical to resolve_tab_icon, empty colour, not the meter",
    )

    # ONLY fg=0, bg=0, state=="idle" routes to the meter: with raw_tokens set,
    # the result diverges from resolve_tab_icon's static glyph and matches
    # idle_usage_meter directly.
    plain_icon = render.resolve_tab_icon("idle", 0.0, 0, 0)
    _check(plain_icon == render.IDLE_GLYPH, "sanity: fg=0,bg=0,idle -> static IDLE_GLYPH via resolve_tab_icon")
    meter_result = render.resolve_tab_glyph("idle", 0.0, 0, 0, raw_tokens=500_000)
    _check(
        meter_result == render.idle_usage_meter(500_000, 0.0),
        "plain idle + raw_tokens: routes to idle_usage_meter, not resolve_tab_icon",
    )
    _check(meter_result[0] != plain_icon, "plain idle + raw_tokens: meter glyph differs from resolve_tab_icon's static glyph")
    _check(meter_result[1] != "", "plain idle + raw_tokens: non-empty meter colour")


def _test_render_render_tabs_row_idle_meter_wiring() -> None:
    """render_tabs_row wiring (task 4.2): a plain-idle window with raw_tokens set
    renders the idle-usage-meter ramp glyph wrapped in its own #[fg=...] colour,
    with the segment's own label colour (CYAN-bold active / DIM inactive)
    restored immediately after — #[range=window|...] markup unchanged. The same
    window WITHOUT raw_tokens (None, or the attribute absent entirely) renders
    byte-identical to today's static-icon segment: no #[fg=] wrap around the
    icon at all."""
    idle_window = _FakeWindow(id="@1", index="1", name="work", state="idle")
    idle_window.raw_tokens = 500_000  # type: ignore[attr-defined]  # 500K/1M -> ratio 0.5 -> ramp idx 8

    expected_glyph, expected_color = render.idle_usage_meter(500_000, 0.0)
    _check(expected_glyph == "⣿", f"sanity: 500K/1M ratio 0.5 -> index 8 -> '⣿', got {expected_glyph!r}")
    _check(expected_color != "", "sanity: meter colour non-empty at 500K tokens")

    # Inactive window (DIM label colour): glyph wrapped in meter colour, DIM
    # restored immediately after for the trailing name.
    out = render.render_tabs_row([idle_window], "@2", now=0.0)
    _check(
        out == (
            f"#[fg={render.DIM}]#[range=window|1] "
            f"1 #[fg={expected_color}]{expected_glyph}#[fg={render.DIM}] work "
            f"#[norange]#[default]"
        ),
        f"inactive idle window w/ raw_tokens: meter glyph + colour, DIM restored, range markup intact: {out!r}",
    )

    # Active window (CYAN-bold label colour): same wiring, CYAN-bold restored
    # instead of DIM.
    active_colour = f"{render.CYAN},bold"
    out_active = render.render_tabs_row([idle_window], "@1", now=0.0)
    _check(
        out_active == (
            f"#[fg={active_colour}]#[range=window|1] "
            f"1 #[fg={expected_color}]{expected_glyph}#[fg={active_colour}] work "
            f"#[norange]#[default]"
        ),
        f"active idle window w/ raw_tokens: meter glyph + colour, CYAN-bold restored, range markup intact: {out_active!r}",
    )

    # Same window with raw_tokens explicitly None -> byte-identical to the
    # prior plain-icon rendering (no colour wrap around the icon at all).
    static_expected = f"#[fg={render.DIM}]#[range=window|1] 1 {render.IDLE_GLYPH} work #[norange]#[default]"
    idle_window_none = _FakeWindow(id="@1", index="1", name="work", state="idle")
    idle_window_none.raw_tokens = None  # type: ignore[attr-defined]
    out_none = render.render_tabs_row([idle_window_none], "@2", now=0.0)
    _check(out_none == static_expected, f"idle window, raw_tokens=None: byte-identical to static glyph, no wrap: {out_none!r}")
    _check(f"#[fg={expected_color}]" not in out_none, "raw_tokens=None: meter colour never appears")

    # Same window with the raw_tokens attribute absent entirely (getattr
    # default) -> identical to the explicit-None case — the real shape
    # cli._build_tabs_row emits for a window it never resolved tokens for.
    idle_window_no_attr = _FakeWindow(id="@1", index="1", name="work", state="idle")
    out_no_attr = render.render_tabs_row([idle_window_no_attr], "@2", now=0.0)
    _check(out_no_attr == static_expected, "getattr default (no raw_tokens attribute) matches explicit raw_tokens=None")


def _test_render_inbox_rows() -> None:
    panes = [
        _FakePaneFull("%1", "waiting", 100.0, "s1", "0", "proj", "main", "permission", "do X"),
        _FakePaneFull("%2", "idle", 90.0, "s1", "1", "proj2", "dev", "", "done"),
    ]
    rows = render.inbox_rows(panes, render.DEFAULT_ICONS, now=110.0)
    _check(len(rows) == 2, "one row per pane")
    _check(rows[0][1] == "%1" and rows[1][1] == "%2", "pane_id preserved as field 2")
    # session:window column present; time-in-state rendered.
    _check("s1:0" in rows[0][0], "session:window column")
    _check("10s" in rows[0][0], "duration column")
    # empty branch/reason render as '-'
    _check("-" in rows[1][0], "empty fields render as '-'")


@dataclass
class _FakePaneFull:
    """Full PaneInfo stand-in for render.inbox_rows tests."""

    id: str
    state: str
    timestamp: float
    session: str
    window: str
    project: str
    branch: str
    wait_reason: str
    task: str


# ---------------------------------------------------------------------------
# usage.py tests (pure presentation + fail-open, no nexus-agent — Req-8)
# ---------------------------------------------------------------------------

def _test_usage_color_thresholds() -> None:
    # absent -> DIM (only a genuinely missing field dims).
    _check(usage.color_for(None) == usage.DIM, "None -> DIM")
    # > 0.80 -> RED; the boundary 0.80 itself is NOT red (sh used strict >).
    _check(usage.color_for(0.81) == usage.RED, ">0.80 -> RED")
    _check(usage.color_for(0.80) == usage.YELLOW, "0.80 exactly -> YELLOW (not RED)")
    # >= 0.50 -> YELLOW; the boundary 0.50 IS yellow.
    _check(usage.color_for(0.50) == usage.YELLOW, "0.50 exactly -> YELLOW")
    _check(usage.color_for(0.49) == usage.CYAN, "<0.50 -> CYAN")
    # present 0.0 -> CYAN, not DIM.
    _check(usage.color_for(0.0) == usage.CYAN, "0.0 present -> CYAN, not DIM")


def _test_usage_pct_formatting() -> None:
    _check(usage.pct_for(None) == "--", "None -> --")
    _check(usage.pct_for(0.0) == "0%", "0.0 -> 0%")
    _check(usage.pct_for(0.5) == "50%", "0.5 -> 50%")
    _check(usage.pct_for(1.0) == "100%", "1.0 -> 100%")
    _check(usage.pct_for(0.807) == "81%", "0.807 -> 81% (rounds)")


def _test_usage_extract_util() -> None:
    cred = {"usage5hUsed": 50.0, "usage5hLimit": 100.0, "usage7dUsed": 0.0, "usage7dLimit": 200.0}
    _check(usage._extract_util(cred, "usage5hUsed", "usage5hLimit") == 0.5, "extract 0.5")
    _check(usage._extract_util(cred, "usage7dUsed", "usage7dLimit") == 0.0, "extract present 0")
    # missing / null used or limit, or a zero/negative limit -> None (not polled yet).
    _check(usage._extract_util({}, "usage5hUsed", "usage5hLimit") is None, "missing fields -> None")
    _check(
        usage._extract_util({"usage5hUsed": None, "usage5hLimit": None}, "usage5hUsed", "usage5hLimit")
        is None,
        "null used/limit -> None",
    )
    _check(
        usage._extract_util({"usage5hUsed": 10.0, "usage5hLimit": 0.0}, "usage5hUsed", "usage5hLimit")
        is None,
        "zero limit -> None",
    )


def _test_usage_account_label() -> None:
    # cc-tmux-status-bar-popup-polish task 4.1: _account_label's org suffix
    # is the first 8 characters of orgUuid (not the last character) --
    # unified with _account_identity's own org_short format.
    cred = {
        "accountName": "Leo",
        "accountEmail": "leo@x.dev",
        "orgUuid": "abc123f9-9999-9999-9999-999999999999",
    }
    _check(
        usage._account_label(cred) == "leo@x.dev·abc123f9",
        "prefers full email + first-8-char org-id suffix over accountName",
    )
    _, org_short = usage._account_identity(cred)
    _check(
        usage._account_label(cred) == f"leo@x.dev·{org_short}",
        "_account_label's org suffix matches _account_identity's org_short byte-for-byte",
    )
    _check(usage._account_label({"accountEmail": "leo@x.dev"}) == "leo@x.dev", "email alone when no org id")
    _check(usage._account_label({"accountName": "Leo"}) == "Leo", "falls back to accountName when no email")
    _check(usage._account_label({"name": "acct-abc123"}) == "acct-abc123", "falls back to raw name")
    _check(usage._account_label({}) == "", "nothing present -> ''")




def _test_usage_extract_active() -> None:
    _check(usage.extract_active({}) == ("", None, None), "empty payload -> empty triple")
    _check(usage.extract_active({"credentials": "x"}) == ("", None, None), "non-list -> empty")
    _check(
        usage.extract_active({"credentials": [{"isActive": False}]}) == ("", None, None),
        "no active credential -> empty",
    )
    label, u5, u7 = usage.extract_active(
        {"credentials": [
            {"isActive": False, "accountName": "other"},
            {"isActive": True, "accountName": "leo",
             "usage5hUsed": 1.0, "usage5hLimit": 4.0,
             "usage7dUsed": None, "usage7dLimit": None},
        ]}
    )
    _check(label == "leo", f"label from active credential: {label!r}")
    _check(u5 == 0.25, f"5h util extracted: {u5!r}")
    _check(u7 is None, "unpolled 7d -> None")

    # Regression (2026-07-13, row2/accounts-popup usage mismatch, Leo's
    # report): a STALE duplicate of the active identity earlier in list order
    # must not win just because it comes first. extract_active now runs
    # dedupe_credentials first (same freshest-by-usagePolledAt tie-break the
    # accounts-popup already used), so the two surfaces resolve the same row
    # instead of drifting — confirmed live: row2 showed 5H:90%/7D:64% (the
    # stale first row) while the popup's starred row showed 5H:36%/7D:71%
    # (the freshest one) for the SAME account.
    label2, u5_2, u7_2 = usage.extract_active(
        {"credentials": [
            {
                "accountEmail": "leo@x.dev", "orgUuid": "1", "isActive": True,
                "usage5hUsed": 90.0, "usage5hLimit": 100.0,
                "usage7dUsed": 64.0, "usage7dLimit": 100.0,
                "usagePolledAt": "2026-07-13T10:00:00Z",
            },
            {
                "accountEmail": "leo@x.dev", "orgUuid": "1", "isActive": True,
                "usage5hUsed": 36.0, "usage5hLimit": 100.0,
                "usage7dUsed": 71.0, "usage7dLimit": 100.0,
                "usagePolledAt": "2026-07-13T10:05:00Z",
            },
        ]}
    )
    _check(label2 == "leo@x.dev·1", f"label from deduped active row: {label2!r}")
    _check(u5_2 == 0.36, f"freshest (by usagePolledAt) row's 5H wins over the stale duplicate: {u5_2!r}")
    _check(u7_2 == 0.71, f"freshest row's 7D wins too: {u7_2!r}")

    # Regression (if-lh9u, confirmed live 2026-07-14): nx-agent's /credentials
    # payload can carry TWO simultaneous isActive: true rows for the SAME
    # email under DIFFERENT orgUuids -- dedupe_credentials groups by
    # (accountEmail, orgUuid), so it cannot collapse these two rows into one
    # (they're different groups, both survive dedupe as isActive: true). The
    # pre-fix extract_active picked the first isActive match in the deduped
    # list's order, not the most-recently-polled one, so a stale org's numbers
    # could silently win over the actually-current one. Same live shape: one
    # org stale since a prior day, one current.
    label3, u5_3, u7_3 = usage.extract_active(
        {"credentials": [
            {
                "accountEmail": "leo@x.dev", "orgUuid": "stale-org-1", "isActive": True,
                "usage5hUsed": 90.0, "usage5hLimit": 100.0,
                "usage7dUsed": 64.0, "usage7dLimit": 100.0,
                "usagePolledAt": "2026-07-13T09:00:00Z",
            },
            {
                "accountEmail": "leo@x.dev", "orgUuid": "current-org-2", "isActive": True,
                "usage5hUsed": 12.0, "usage5hLimit": 100.0,
                "usage7dUsed": 20.0, "usage7dLimit": 100.0,
                "usagePolledAt": "2026-07-14T09:00:00Z",
            },
        ]}
    )
    _check(
        label3 == "leo@x.dev·current-",
        f"label from freshest cross-org active row: {label3!r}",
    )
    _check(u5_3 == 0.12, f"freshest org's 5H wins over the stale org's isActive row: {u5_3!r}")
    _check(u7_3 == 0.20, f"freshest org's 7D wins too: {u7_3!r}")


def _test_usage_extract_reset_at() -> None:
    _check(usage._extract_reset_at({}, "usage5hResetAt") is None, "missing key -> None")
    _check(
        usage._extract_reset_at({"usage5hResetAt": None}, "usage5hResetAt") is None,
        "null -> None",
    )
    _check(
        usage._extract_reset_at({"usage5hResetAt": 123}, "usage5hResetAt") is None,
        "non-string -> None",
    )
    _check(
        usage._extract_reset_at({"usage5hResetAt": "not a date"}, "usage5hResetAt") is None,
        "unparseable string -> None",
    )
    epoch = usage._extract_reset_at({"usage5hResetAt": "2030-01-01T00:00:00Z"}, "usage5hResetAt")
    _check(
        epoch is not None and abs(epoch - 1893456000.0) < 1,
        f"Z-suffixed ISO parses to the right epoch: {epoch!r}",
    )
    epoch2 = usage._extract_reset_at(
        {"usage7dResetAt": "2030-01-01T00:00:00+00:00"}, "usage7dResetAt"
    )
    _check(epoch2 == epoch, "explicit +00:00 offset matches the Z-suffix equivalent")


def _test_usage_dedupe_credentials() -> None:
    _check(usage.dedupe_credentials("nope") == [], "non-list -> []")
    _check(usage.dedupe_credentials([]) == [], "empty list -> []")

    # Duplicate (accountEmail, orgUuid) rows collapse to one, most-recent
    # (by usagePolledAt) kept when BOTH entries carry a timestamp.
    creds = [
        {
            "accountEmail": "leo@x.dev", "orgUuid": "abc123f",
            "usagePolledAt": "2026-07-01T00:00:00Z", "usage5hUsed": 1.0, "usage5hLimit": 10.0,
        },
        {
            "accountEmail": "leo@x.dev", "orgUuid": "abc123f",
            "usagePolledAt": "2026-07-10T00:00:00Z", "usage5hUsed": 5.0, "usage5hLimit": 10.0,
        },
    ]
    out = usage.dedupe_credentials(creds)
    _check(len(out) == 1, f"duplicate identity collapses to one row: {len(out)}")
    _check(out[0]["usage5hUsed"] == 5.0, "most-recent (by usagePolledAt) row kept")

    # No usable usagePolledAt on either side -> last one wins (payload list
    # order presumed oldest -> newest, the common case on this machine since
    # usagePolledAt is frequently null).
    creds_no_ts = [
        {"accountEmail": "a@x.dev", "orgUuid": "1", "usage5hUsed": 1.0},
        {"accountEmail": "a@x.dev", "orgUuid": "1", "usage5hUsed": 9.0},
    ]
    out2 = usage.dedupe_credentials(creds_no_ts)
    _check(len(out2) == 1 and out2[0]["usage5hUsed"] == 9.0, "no timestamp -> last one wins")

    # Distinct orgUuid under the same email -> two separate accounts, not merged.
    creds_diff_org = [
        {"accountEmail": "a@x.dev", "orgUuid": "1"},
        {"accountEmail": "a@x.dev", "orgUuid": "2"},
    ]
    out3 = usage.dedupe_credentials(creds_diff_org)
    _check(len(out3) == 2, "distinct orgUuid under same email stays distinct")

    # No accountEmail -> falls back to the _account_label identity so distinct
    # unlabeled accounts don't collapse into one bucket.
    creds_no_email = [
        {"accountName": "personal", "orgUuid": "1"},
        {"accountName": "work", "orgUuid": "1"},
    ]
    out4 = usage.dedupe_credentials(creds_no_email)
    _check(len(out4) == 2, "distinct accountName fallback identities stay distinct")

    # Orphaned junk rows — no accountEmail AND status:refresh_failed — are
    # DROPPED outright, not grouped. Confirmed live 2026-07-13: 20 of 107 real
    # nexus-agent rows match this exact shape (auto-generated acct-XXXXXXXX
    # name, isActive:False, a distinct per-row duplicateGroupId that does NOT
    # merge them with each other), leaking through the popup as fake
    # "accounts" (if-lp8v/if-m5q6) because each row's own generated name is
    # its own fallback grouping key, so nothing ever collapsed them before.
    creds_junk = [
        {"name": "acct-aaa111", "status": "refresh_failed", "isActive": False},
        {"name": "acct-bbb222", "status": "refresh_failed", "isActive": False},
        {"accountEmail": "real@x.dev", "orgUuid": "1", "isActive": True},
    ]
    out8 = usage.dedupe_credentials(creds_junk)
    _check(len(out8) == 1, f"orphaned refresh_failed junk dropped, real account kept: {len(out8)}")
    _check(out8[0]["accountEmail"] == "real@x.dev", "surviving row is the real account")

    # A row WITH an accountEmail is NEVER dropped by the junk filter, even if
    # transiently refresh_failed — only genuinely identity-less rows qualify.
    creds_email_refresh_failed = [
        {"accountEmail": "flaky@x.dev", "orgUuid": "1", "status": "refresh_failed", "isActive": False},
    ]
    out9 = usage.dedupe_credentials(creds_email_refresh_failed)
    _check(len(out9) == 1, "refresh_failed row WITH an email is kept, not dropped")

    # Malformed rows (non-dict) are skipped, fail-open.
    out5 = usage.dedupe_credentials([1, "x", None, {"accountEmail": "b@x.dev"}])
    _check(out5 == [{"accountEmail": "b@x.dev"}], "non-dict rows skipped")

    # isActive:True MUST win over isActive:False even when the inactive
    # duplicate has a LATER usagePolledAt — confirmed live 2026-07-12 against
    # the real nexus-agent payload: a group can hold genuinely different
    # credential rows (distinct fingerprint/id, same accountEmail+orgUuid)
    # from token-swap history, and a stale sibling's poll timestamp can
    # lexically postdate the real active row's by milliseconds. Recency alone
    # would silently drop the one row the accounts-popup needs for SES.
    creds_active_vs_stale = [
        {
            "accountEmail": "c@x.dev", "orgUuid": "1", "fingerprint": "active-fp",
            "isActive": True, "usagePolledAt": "2026-07-12T21:31:17.119Z",
        },
        {
            "accountEmail": "c@x.dev", "orgUuid": "1", "fingerprint": "stale-fp",
            "isActive": False, "usagePolledAt": "2026-07-12T21:31:17.154Z",
        },
    ]
    out6 = usage.dedupe_credentials(creds_active_vs_stale)
    _check(len(out6) == 1, "still collapses to one row")
    _check(out6[0]["isActive"] is True, "isActive:True survives a later-timestamped inactive dup")
    _check(out6[0]["fingerprint"] == "active-fp", "the active row's own data is kept, not the stale dup's")

    # Same scenario, active row seen SECOND -> still wins (order-independent).
    out7 = usage.dedupe_credentials(list(reversed(creds_active_vs_stale)))
    _check(out7[0]["isActive"] is True, "isActive:True wins regardless of list order")

    # Regression (2026-07-13, real-payload-shaped): a real polled row (both
    # isActive:False) must NOT be overwritten by a LATER refresh_failed
    # duplicate that has no usagePolledAt. Confirmed live: nexus-agent's
    # actual payload interleaves genuinely-polled rows with all-null
    # refresh_failed junk for the SAME identity in no guaranteed order — the
    # prior tie-break's "either side missing a timestamp -> last one wins"
    # let a later null row silently erase real 5H/7D data, which is exactly
    # what the accounts popup showed for two real non-active accounts.
    creds_real_then_junk = [
        {
            "accountEmail": "d@x.dev", "orgUuid": "1", "isActive": False,
            "usage5hUsed": 25.0, "usage5hLimit": 100.0,
            "usagePolledAt": "2026-07-13T14:56:30.968Z",
        },
        {
            "accountEmail": "d@x.dev", "orgUuid": "1", "isActive": False,
            "usage5hUsed": None, "usage5hLimit": None, "usagePolledAt": None,
            "status": "refresh_failed",
        },
    ]
    out10 = usage.dedupe_credentials(creds_real_then_junk)
    _check(len(out10) == 1, "still collapses to one row")
    _check(out10[0]["usage5hUsed"] == 25.0, f"real polled data survives a later null junk dup: {out10[0]!r}")

    # Same scenario, junk seen FIRST then real data -> real data still wins
    # (the existing new-timestamp-vs-no-timestamp branch, not the bug).
    out11 = usage.dedupe_credentials(list(reversed(creds_real_then_junk)))
    _check(out11[0]["usage5hUsed"] == 25.0, f"real polled data wins regardless of list order: {out11[0]!r}")


def _test_render_accounts_popup() -> None:
    # cc-tmux-status-bar-popup-polish Decision 1: SES is dropped entirely --
    # every row (active or not) uses the uniform 20-cell 2-metric glyph over
    # 5H/7D only. render_accounts_popup no longer takes active_ses_pct/
    # active_raw_tokens at all.
    accounts = [
        ("leo@x.dev·abcd1234", 0.5, 0.85, None, None, "leo@x.dev", "abcd1234"),
        ("other@x.dev·efgh5678", 0.1, 0.2, None, None, "other@x.dev", "efgh5678"),
    ]
    out = render.render_accounts_popup(accounts, "leo@x.dev·abcd1234")
    lines = out.splitlines()
    # Summary + identity line + closing border rule per account (no reset
    # data on either account -> no reset lines): 3 lines * 2 accounts.
    _check(len(lines) == 6, f"summary + identity + border per account: {lines!r}")
    plain_lines = [_strip_ansi(l) for l in lines]
    active_line = next(l for l in plain_lines if "5H:50%" in l)
    other_summary = next(l for l in plain_lines if "5H:10%" in l)
    active_identity = next(l for l in plain_lines if "leo@x.dev" in l and "abcd1234" in l)
    other_identity = next(l for l in plain_lines if "other@x.dev" in l and "efgh5678" in l)
    _check(
        "5H:50%" in active_line and "7D:85%" in active_line,
        f"active row shows 5H/7D text: {active_line!r}",
    )
    # Active row carries the SAME 20-cell 2-metric glyph shape a non-active
    # row would for identical ratios -- no SES-shaped 4-bit-per-cell pattern,
    # no separate 3-metric encoding for the starred row.
    expected_active_glyph = render.render_usage_glyph_2metric(0.5, 0.85, n=20)
    _check(
        expected_active_glyph in active_line,
        f"active row carries the uniform 20-cell 2-metric glyph: {active_line!r}",
    )
    _check(
        "5H:10%" in other_summary and "7D:20%" in other_summary,
        f"non-active row shows 5H/7D text unchanged: {other_summary!r}",
    )
    expected_other_glyph = render.render_usage_glyph_2metric(0.1, 0.2, n=20)
    _check(
        expected_other_glyph in other_summary,
        f"non-active row carries the identical-shape 20-cell 2-metric glyph: {other_summary!r}",
    )
    # No SES-shaped dot pattern (a distinct 3-metric glyph) and no
    # token-count label (format_context_tokens output, e.g. "252.5k") appear
    # anywhere -- the popup is fully account-scoped now (Decision 1).
    _check(
        "k:" not in active_line and "k:" not in other_summary,
        "no SES token-count label anywhere",
    )
    _check("SES:" not in out, f"SES text is fully gone: {out!r}")
    _check(active_line.startswith("*"), f"active row is marked: {active_line!r}")
    _check(
        not other_summary.startswith("*"), f"non-active row is not marked: {other_summary!r}"
    )
    _check(
        active_identity.strip().startswith("leo@x.dev"),
        f"identity line under the active row's summary: {active_identity!r}",
    )
    _check(
        other_identity.strip().startswith("other@x.dev"),
        f"identity line under the non-active row's summary too: {other_identity!r}",
    )
    # No tmux status-format escaping leaks in (this popup uses real ANSI
    # instead — see the green checks below — never tmux's #[fg=...] tokens).
    _check("#[" not in out, "popup body carries no tmux #[...] style codes")
    # Every percentage is wrapped in the popup's green.
    _check(render._green("50%") in out, f"5H percentage is wrapped in green: {out!r}")
    _check(render._green("85%") in out, f"7D percentage is wrapped in green: {out!r}")
    # Each account block closes with a full-width '─' rule.
    border_lines = [l for l in lines if l and set(l) == {"─"}]
    _check(len(border_lines) == 2, f"one border rule per account: {lines!r}")

    # Unreachable nexus-agent / zero deduped credentials -> empty, fail-open.
    _check(render.render_accounts_popup([], "leo@x.dev") == "", "no accounts -> ''")

    # No account matches active_label -> no row gets the `*` marker (e.g. the
    # active credential had no usable label and was dropped by the caller).
    out_no_match = render.render_accounts_popup(accounts, "")
    plain_no_match = _strip_ansi(out_no_match)
    _check(
        not any(l.startswith("*") for l in plain_no_match.splitlines()),
        f"no active_label match -> no row marked: {out_no_match!r}",
    )


def _test_render_accounts_popup_reset_lines() -> None:
    """Indented per-account 5H/7D reset-time lines (Leo's ask, 2026-07-13),
    plus the same-day follow-up ask: short weekday instead of day-of-month,
    the "a" (am/pm) markers aligned between the 5H/7D lines, a border rule
    under each account block, and every number/datetime rendered green.

    ``now`` is injected (same DI pattern :func:`render.render_tabs_row` uses)
    so the countdown math is deterministic. The absolute clock text isn't
    asserted byte-for-byte since it renders in the test machine's local
    timezone (:func:`render._format_reset_line` uses ``time.localtime``); the
    expected weekday abbreviation is instead computed the SAME way
    production code does, so the assertion stays timezone-agnostic without
    hardcoding a day name.
    """
    now = 1_700_000_000.0
    five_h_reset = now + 2 * 3600 + 14 * 60  # 2h14m out
    seven_d_reset = now + 3 * 86400 + 14 * 3600 + 22 * 60  # 3d14h22m out
    expected_weekday = time.strftime("%a", time.localtime(seven_d_reset))

    accounts = [("leo@x.dev·8", 0.36, 0.71, five_h_reset, seven_d_reset, "leo@x.dev", "abcd1234")]
    out = render.render_accounts_popup(accounts, "leo@x.dev·8", now=now)
    lines = out.splitlines()
    # summary + identity + two reset lines + border.
    _check(len(lines) == 5, f"summary + identity + two reset lines + border: {lines!r}")
    identity = _strip_ansi(lines[1])
    reset_5h = _strip_ansi(lines[2])
    reset_7d = _strip_ansi(lines[3])
    _check(
        identity.strip().startswith("leo@x.dev") and "abcd1234" in identity,
        f"identity line under the summary: {identity!r}",
    )
    _check(
        reset_5h.startswith("   ") and "5H Resets at" in reset_5h and "in 02:14" in reset_5h,
        f"5H reset line indented, correct countdown: {reset_5h!r}",
    )
    _check(
        reset_7d.startswith("   ")
        and "7D Resets on" in reset_7d
        and f"{expected_weekday} " in reset_7d
        and "in 03:14:22" in reset_7d,
        f"7D reset line indented, short weekday (not day-of-month), correct dd:HH:mm countdown: {reset_7d!r}",
    )
    # "a" (am/pm) marker lines up in the same column on both lines — 5H's
    # blank day-slot is the same width as 7D's "<weekday> " prefix.
    am_pm_col_5h = max(reset_5h.find(" am"), reset_5h.find(" pm"))
    am_pm_col_7d = max(reset_7d.find(" am"), reset_7d.find(" pm"))
    _check(am_pm_col_5h >= 0 and am_pm_col_7d >= 0, "both lines carry an am/pm marker")
    _check(
        am_pm_col_5h == am_pm_col_7d,
        f"am/pm marker aligned between 5H and 7D lines: {reset_5h!r} vs {reset_7d!r}",
    )
    # Border rule closes the block.
    _check(
        len(lines[4]) > 0 and set(lines[4]) == {"─"},
        f"border rule under the account block: {lines[4]!r}",
    )
    # Countdowns are green.
    _check(render._green("02:14") in out, f"5H countdown is green: {out!r}")
    _check(render._green("03:14:22") in out, f"7D countdown is green: {out!r}")

    # Missing reset data (window not yet polled) -> line omitted, not a
    # placeholder — same fail-open convention as an absent 5H/7D percentage.
    accounts_missing = [("leo@x.dev·8", 0.36, 0.71, None, None, "leo@x.dev", "abcd1234")]
    out_missing = render.render_accounts_popup(accounts_missing, "leo@x.dev·8", now=now)
    _check(
        len(out_missing.splitlines()) == 3,
        f"summary + identity + border only, no reset lines: {out_missing!r}",
    )

    # Already-passed reset -> "now", not a negative/garbled countdown.
    accounts_past = [("leo@x.dev·8", 0.36, 0.71, now - 60, now - 60, "leo@x.dev", "abcd1234")]
    out_past = render.render_accounts_popup(accounts_past, "leo@x.dev·8", now=now)
    _check(render._green("now") in out_past, f"already-passed reset renders green 'now': {out_past!r}")

    # Reset lines render for a NON-active row too (Leo: "for both 5h and 7d
    # below each account", not just the starred one).
    accounts_two = [
        ("leo@x.dev·8", 0.36, 0.71, five_h_reset, seven_d_reset, "leo@x.dev", "abcd1234"),
        ("other@x.dev·2", 0.1, 0.2, five_h_reset, None, "other@x.dev", "efgh5678"),
    ]
    out_two = render.render_accounts_popup(accounts_two, "leo@x.dev·8", now=now)
    other_lines = [_strip_ansi(l) for l in out_two.splitlines()]
    other_idx = next(i for i, l in enumerate(other_lines) if "other@x.dev" in l)
    _check(
        "5H Resets at" in other_lines[other_idx + 1],
        f"non-active row also gets its 5H reset line: {other_lines[other_idx + 1]!r}",
    )
    _check(
        "7D Resets" not in other_lines[other_idx + 2],
        f"non-active row's missing 7D reset data omits that line only: {other_lines[other_idx + 2]!r}",
    )


def _test_context_bar_colors() -> None:
    """cc-tmux-context-bar: six-tier raw-token colour ramp + pulse pairs.

    Thresholds are strictly-greater-than (Leo's ask: "green > 100k" etc.), so
    a value exactly ON a boundary stays in the LOWER tier — checked
    explicitly at 100_000 to pin that edge down. Pulse tiers are checked at
    both frame parities (``now=0`` -> even frame -> base colour, ``now=
    FRAME_PERIOD_SEC`` -> odd frame -> pulse colour), the same wall-clock
    parity :func:`render.animated_icon` already uses.
    """
    _check(render.resolve_context_color(None, 0) == usage.DIM, "no data -> DIM")
    _check(render.resolve_context_color(50_000, 0) == usage.DIM, "<=100k -> DIM (safe zone)")
    _check(
        render.resolve_context_color(100_000, 0) == usage.DIM,
        "exactly 100k -> DIM (strictly-greater-than threshold, not >=)",
    )
    _check(render.resolve_context_color(150_000, 0) == usage.GREEN, ">100k -> GREEN")
    _check(render.resolve_context_color(250_000, 0) == usage.YELLOW, ">200k -> YELLOW")
    _check(render.resolve_context_color(350_000, 0) == usage.ORANGE, ">300k -> ORANGE")
    _check(render.resolve_context_color(550_000, 0) == usage.RED, ">500k -> RED, steady (no pulse pair)")
    _check(
        render.resolve_context_color(650_000, 0.0) == usage.RED,
        ">600k tier, even frame -> base RED",
    )
    _check(
        render.resolve_context_color(650_000, render.FRAME_PERIOD_SEC) == usage.BRIGHT_RED,
        ">600k tier, odd frame -> pulse BRIGHT_RED",
    )
    _check(
        render.resolve_context_color(800_000, 0.0) == usage.DARK_RED,
        ">750k tier, even frame -> base DARK_RED",
    )
    _check(
        render.resolve_context_color(800_000, render.FRAME_PERIOD_SEC) == usage.RED,
        ">750k tier, odd frame -> pulse RED (distinct pair from the 600k tier)",
    )


def _test_apply_metric_dots() -> None:
    """_apply_metric_dots (cc-tmux-braille-usage-glyph task 4.1): per-metric
    proportional dot-fill, in place, with per-metric degrade (``ratio is
    None`` -> no-op) proven not to clobber a sibling metric's already-OR'd
    bits in the same `cells` list."""
    # Representative ratio (0.5) with a 4-bit order (SES's) at a small n=4:
    # total budget = 4*4=16, dots=round(0.5*16)=8 -> the first two cells get
    # fully filled with the bit mask, the remaining two stay untouched.
    cells = [0, 0, 0, 0]
    render._apply_metric_dots(cells, 0.5, render._SES_BITS, 4)
    ses_mask = 0
    for b in render._SES_BITS:
        ses_mask |= 1 << b
    _check(cells == [ses_mask, ses_mask, 0, 0], f"0.5 ratio, n=4, 4-bit order: {cells!r}")

    # None is a no-op for its own metric AND leaves a sibling metric's
    # already-set bits in the same cells list untouched (per-metric degrade,
    # design.md § Staleness) -- metric A fills fully first, then metric B
    # (disjoint bit positions) gets a None ratio and cells must be
    # byte-identical after.
    cells2 = [0, 0]
    render._apply_metric_dots(cells2, 1.0, (0, 1), 2)
    before = list(cells2)
    render._apply_metric_dots(cells2, None, (2, 3), 2)
    _check(cells2 == before, f"None ratio is a no-op, sibling bits unaffected: {cells2!r}")
    _check(cells2 == [3, 3], f"metric A (ratio=1.0, bits (0,1)) fully filled first: {cells2!r}")

    # ratio=0.0 -> zero dots (no cells touched).
    cells3 = [0, 0, 0]
    render._apply_metric_dots(cells3, 0.0, (2, 5), 3)
    _check(cells3 == [0, 0, 0], f"ratio=0.0 -> zero dots: {cells3!r}")

    # ratio=1.0 -> every cell fully filled for that metric's bits.
    cells4 = [0, 0, 0]
    render._apply_metric_dots(cells4, 1.0, (2, 5), 3)
    full_mask = (1 << 2) | (1 << 5)
    _check(
        cells4 == [full_mask, full_mask, full_mask],
        f"ratio=1.0 -> every cell fully filled: {cells4!r}",
    )


def _test_render_usage_glyph() -> None:
    """render_usage_glyph (cc-tmux-braille-usage-glyph task 4.2): the
    validated /openspec:explore mockup anchor, plus n=10 (shipped row-2
    width) staleness edge cases."""
    # Concrete regression anchor -- if this ever changes unexpectedly, the
    # encoding broke (design.md § Encoding, proposal.md's bit-traced
    # example: SES=30%/5H=88%/7D=35% -> row3/5H extends to ~87.5%,
    # row4/7D to ~37.5%, rows1-2/SES to ~31%).
    anchor = render.render_usage_glyph(0.30, 0.88, 0.35, n=8)
    _check(anchor == "⣿⣿⣧⠤⠤⠤⠤⠀", f"validated mockup anchor byte-for-byte: {anchor!r}")

    # All-None at the shipped row-2 width -> fully blank glyph (every cell
    # is bare U+2800, no dots anywhere).
    blank = render.render_usage_glyph(None, None, None, n=10)
    _check(blank == chr(render._BRAILLE_BASE) * 10, f"all-None -> fully blank glyph: {blank!r}")

    # SES live, 5H/7D both None -> only rows 1-2 (SES bits) carry dots; rows
    # 3-4 (5H/7D bits) stay blank on every cell (per-metric degrade).
    ses_only = render.render_usage_glyph(0.5, None, None, n=10)
    cells = [ord(c) - render._BRAILLE_BASE for c in ses_only]
    h5_d7_mask = 0
    for b in render._H5_BITS + render._D7_BITS:
        h5_d7_mask |= 1 << b
    ses_mask = 0
    for b in render._SES_BITS:
        ses_mask |= 1 << b
    _check(
        all(c & h5_d7_mask == 0 for c in cells),
        f"5H/7D None -> no 5H/7D-shaped dots anywhere: {ses_only!r}",
    )
    _check(
        any(c & ses_mask for c in cells),
        f"SES live -> SES rows do carry dots: {ses_only!r}",
    )


def _test_render_usage_glyph_2metric() -> None:
    """render_usage_glyph_2metric (cc-tmux-braille-usage-glyph task 4.3):
    5H/7D at n=20, each independently proportional and given the full
    4-dot-per-cell budget (design.md § Non-active popup rows)."""
    out = render.render_usage_glyph_2metric(0.9, 0.3, n=20)
    _check(len(out) == 20, f"n=20 -> 20-cell glyph: {out!r}")

    cells = [ord(c) - render._BRAILLE_BASE for c in out]
    h5_mask = 0
    for b in render._H5_BITS_WIDE:
        h5_mask |= 1 << b
    d7_mask = 0
    for b in render._D7_BITS_WIDE:
        d7_mask |= 1 << b
    h5_dots = sum(bin(c & h5_mask).count("1") for c in cells)
    d7_dots = sum(bin(c & d7_mask).count("1") for c in cells)
    _check(
        h5_dots == round(0.9 * len(render._H5_BITS_WIDE) * 20),
        f"5H=0.9 dot count proportional to its own 4-dot/cell budget: {h5_dots}",
    )
    _check(
        d7_dots == round(0.3 * len(render._D7_BITS_WIDE) * 20),
        f"7D=0.3 dot count proportional to its own 4-dot/cell budget: {d7_dots}",
    )
    # Independently bit-traceable, same style as the 4.2 mockup anchor: 5H's
    # fill (0.9) extends further left-to-right than 7D's (0.3).
    _check(
        out == "⣿⣿⣿⣿⣿⣿⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠛⠀⠀",
        f"exact bit-traced glyph at 5H=0.9/7D=0.3, n=20: {out!r}",
    )


def _test_format_context_tokens() -> None:
    """format_context_tokens: the row-2 SES token-count label."""
    _check(render.format_context_tokens(None) == "--", "no tokens -> '--'")
    _check(render.format_context_tokens(252_500) == "252.5k", "252500 -> '252.5k'")
    _check(render.format_context_tokens(0) == "0.0k", "0 -> '0.0k'")


def _test_account_identity() -> None:
    """usage._account_identity: (email, org_id_short) for the popup's identity row."""
    _check(
        usage._account_identity(
            {"accountEmail": "leo@x.dev", "orgUuid": "37a74420-a010-462a-a938-6d1a4117830e"}
        )
        == ("leo@x.dev", "37a74420"),
        "email + first-8-chars org id",
    )
    _check(
        usage._account_identity({"accountEmail": "leo@x.dev"}) == ("leo@x.dev", ""),
        "no orgUuid -> empty org segment",
    )
    _check(
        usage._account_identity({"accountName": "Leo"}) == ("Leo", ""),
        "no email -> falls back to accountName, same chain as _account_label",
    )
    _check(usage._account_identity({}) == ("", ""), "nothing resolvable -> empty pair")


def _test_cli_resolve_ses_tokens() -> None:
    """cli._resolve_ses_tokens: used_pct * contextWindowSize, or None when
    either piece is missing (cc-tmux-context-bar)."""
    saved_get_pane_option = tmux.get_pane_option
    saved_session_context = nx_agent.session_context
    tmux.get_pane_option = lambda pane, opt: "sid-1"  # type: ignore[assignment]
    try:
        nx_agent.session_context = lambda *a, **k: {  # type: ignore[assignment]
            "usedPercentage": 61.0, "contextWindowSize": 200_000,
        }
        _check(
            cli._resolve_ses_tokens("%1") == 122_000.0,
            f"61% of 200k -> 122000: {cli._resolve_ses_tokens('%1')!r}",
        )

        # contextWindowSize missing -> tokens unresolvable even with a valid pct.
        nx_agent.session_context = lambda *a, **k: {"usedPercentage": 61.0}  # type: ignore[assignment]
        _check(
            cli._resolve_ses_tokens("%1") is None,
            "no contextWindowSize -> None, not a wrong count",
        )

        # nx unreachable -> None.
        nx_agent.session_context = lambda *a, **k: None  # type: ignore[assignment]
        _check(cli._resolve_ses_tokens("%1") is None, "nx-agent unreachable -> None")
    finally:
        tmux.get_pane_option = saved_get_pane_option  # type: ignore[assignment]
        nx_agent.session_context = saved_session_context  # type: ignore[assignment]


def _test_accounts_popup_click_dismiss_wiring() -> None:
    """``cc-tmux.tmux``'s accounts-popup binding uses fzf's real click-to-close
    mechanism, not the old any-keystroke ``read -n 1 -s`` dismiss.

    cc-tmux-accounts-popup-click-dismiss task 1.1's spike found tmux's
    ``display-popup`` has NO native mouse-click dismissal (tmux(1): only
    Escape/C-c or ``-k`` any-key) — real clickability comes from piping
    through fzf instead (``--bind 'click-header:abort'``, verified as a real
    bind action, not silently ignored), reusing the SAME ``supports_popup``
    fzf-gated branch the picker/inbox popups already use rather than
    inventing a new mechanism. ``--no-input`` hides/disables the query box
    so the popup genuinely cannot be typed into (stronger than the old
    single-keystroke dismiss, which still left a misleading blinking
    cursor). This is a static content-grep, not a live tmux/fzf exercise (no
    tty here) — the fzf bind-syntax validity itself was confirmed live at
    authoring time by comparing valid vs. an invalid bind action name's
    distinct parse error.

    2026-07-14 UPDATE (cc-tmux-status-bar-popup-polish task 3.4 follow-up,
    beads if-s1yu): the fzf pipeline (and all its click/dismiss flags) moved
    from a static string embedded in ``cc-tmux.tmux`` into
    ``cli.cmd_accounts_popup_launch``, which now builds it in Python and
    calls ``display-popup`` itself with a content-sized ``-h`` (fixing the
    "outer pane still 80%, fzf's box is small" gap the prior fix left open —
    see that function's docstring). The mechanism and every flag checked
    below are UNCHANGED, just relocated; this test now greps both files for
    where each piece actually lives instead of assuming everything is still
    in the ``.tmux`` shell string.
    """
    plugin_file = os.path.join(
        os.path.dirname(os.path.dirname(os.path.dirname(__file__))), "cc-tmux.tmux"
    )
    with open(plugin_file, "r", encoding="utf-8") as f:
        plugin_content = f.read()
    cli_file = os.path.join(os.path.dirname(__file__), "cli.py")
    with open(cli_file, "r", encoding="utf-8") as f:
        cli_content = f.read()

    _check(
        "accounts-popup-launch" in plugin_content,
        "cc-tmux.tmux delegates the supports_popup branch to accounts-popup-launch",
    )
    _check(
        "read -n 1 -s" in plugin_content,
        "static any-keystroke fallback retained for no-fzf/old-tmux case",
    )
    _check("accounts-popup | fzf" in cli_content, "accounts-popup piped through fzf (cmd_accounts_popup_launch)")
    _check("click-header:abort" in cli_content, "click-header:abort real click-to-close bind present")
    _check("--no-input" in cli_content, "--no-input present (popup cannot be typed into)")
    _check("--header-border" in cli_content, "--header-border present ([x] visually attached to the frame)")


def _test_cli_accounts_popup_no_session_state() -> None:
    """cc-tmux-status-bar-popup-polish task 3.1 (supersedes the retired
    if-hrbd ``ses_from_nx_agent`` case): the popup dropped SES entirely
    (Decision 1), so ``cmd_accounts_popup`` no longer resolves ANY
    per-session state at all -- no window/pane lookup
    (:func:`tmux.current_window_id`/:func:`cli._resolve_session_pane`), no
    nx-agent SES query (:func:`nx_agent.session_context`). The popup is
    fully account-scoped now, independent of the nx-agent session-context
    bugs (nx-22xz8 and the sibling context-push bug) filed the same session.

    Monkeypatches all three to raise if ever called, proving
    ``cmd_accounts_popup`` genuinely never touches them, and confirms the
    popup still renders correctly (uniform 2-metric glyph, real 5H/7D, no
    SES anywhere) from credential data alone.
    """
    saved_query = usage._query
    saved_current_window = tmux.current_window_id
    saved_resolve_pane = cli._resolve_session_pane
    saved_session_context = nx_agent.session_context

    def _must_not_be_called(*a, **k):
        raise AssertionError("cmd_accounts_popup must not resolve per-session state")

    usage._query = lambda *a, **k: {  # type: ignore[assignment]
        "credentials": [
            {
                "isActive": True,
                "accountEmail": "leo@x.dev",
                "orgUuid": "org12345999999999",
                "usage5hUsed": 50.0,
                "usage5hLimit": 100.0,
                "usage7dUsed": 85.0,
                "usage7dLimit": 100.0,
            },
        ]
    }
    tmux.current_window_id = _must_not_be_called  # type: ignore[assignment]
    cli._resolve_session_pane = _must_not_be_called  # type: ignore[assignment]
    nx_agent.session_context = _must_not_be_called  # type: ignore[assignment]
    try:
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            cli.cmd_accounts_popup(None)
        out = _strip_ansi(buf.getvalue())
        _check("5H:50%" in out and "7D:85%" in out, f"popup shows real 5H/7D from credential data: {out!r}")
        _check("SES:" not in out and "k:" not in out, f"no SES anywhere in the popup: {out!r}")
    finally:
        usage._query = saved_query  # type: ignore[assignment]
        tmux.current_window_id = saved_current_window  # type: ignore[assignment]
        cli._resolve_session_pane = saved_resolve_pane  # type: ignore[assignment]
        nx_agent.session_context = saved_session_context  # type: ignore[assignment]


def _test_cli_resolve_model_letter() -> None:
    """nx-yn6c2: ``_resolve_model_letter`` sources the row-2 model letter from
    nx-agent's ``GET /sessions/:id/context`` ``model`` field, keyed by the
    pane's ``@cc-session-id`` option — NOT the retired legacy per-pane
    ``session-context.<pane>.json`` file (since removed),
    mirroring the if-hrbd fix's technique for SES.
    """
    saved_get_pane_option = tmux.get_pane_option
    saved_session_context = nx_agent.session_context

    tmux.get_pane_option = lambda pane, opt: "sid-1"  # type: ignore[assignment]
    try:
        # NEW path: nx-agent's real value.
        nx_agent.session_context = lambda *a, **k: {"model": "O"}  # type: ignore[assignment]
        _check(
            cli._resolve_model_letter("%1") == "O",
            f"model letter comes from nx-agent: {cli._resolve_model_letter('%1')!r}",
        )

        # nx returns model: null (no model recorded) -> blank, not the legacy value.
        nx_agent.session_context = lambda *a, **k: {"model": None}  # type: ignore[assignment]
        _check(
            cli._resolve_model_letter("%1") == "",
            "model: null degrades to blank",
        )

        # nx-agent unreachable -> blank, NOT a silent fall-back to the legacy file.
        nx_agent.session_context = lambda *a, **k: None  # type: ignore[assignment]
        _check(
            cli._resolve_model_letter("%1") == "",
            "nx-agent unreachable -> blank, no legacy fallback",
        )

        # Malformed non-string model -> blank (fail-open on a wrong-shaped field).
        nx_agent.session_context = lambda *a, **k: {"model": 7}  # type: ignore[assignment]
        _check(cli._resolve_model_letter("%1") == "", "non-string model -> blank")
    finally:
        tmux.get_pane_option = saved_get_pane_option  # type: ignore[assignment]
        nx_agent.session_context = saved_session_context  # type: ignore[assignment]


def _test_usage_active_usage_ttl() -> None:
    # Cache round-trip + TTL + negative caching, with _query monkeypatched to count.
    calls = {"n": 0}
    saved_query = usage._query
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-usage-cache-test-")
    path = os.path.join(tmpdir, "cache.json")
    payload = {"credentials": [{"isActive": True, "accountName": "leo",
                                "usage5hUsed": 2.0, "usage5hLimit": 4.0,
                                "usage7dUsed": 1.0, "usage7dLimit": 10.0}]}
    try:
        def counting_query(url=usage.CREDENTIALS_URL, timeout=usage.TIMEOUT_SECS):
            calls["n"] += 1
            return payload
        usage._query = counting_query  # type: ignore[assignment]

        first = usage.active_usage(ttl=45.0, cache_path=path)
        _check(first == ("leo", 0.5, 0.1), f"miss fetches + extracts: {first!r}")
        _check(calls["n"] == 1, "first call hit the network once")
        _check(os.path.exists(path), "cache file written")

        second = usage.active_usage(ttl=45.0, cache_path=path)
        _check(second == first, "fresh cache returns same triple")
        _check(calls["n"] == 1, "fresh cache -> NO second fetch")

        os.utime(path, (time.time() - 3600, time.time() - 3600))
        third = usage.active_usage(ttl=45.0, cache_path=path)
        _check(third == first and calls["n"] == 2, "stale mtime -> refetch")

        # Corrupt cache fails open to a fetch.
        with open(path, "w") as f:
            f.write("not json")
        os.utime(path, None)
        fourth = usage.active_usage(ttl=45.0, cache_path=path)
        _check(fourth == first and calls["n"] == 3, "corrupt cache -> refetch")

        # Negative caching: failed fetch writes the empty triple; next call
        # within TTL serves it without re-querying.
        os.unlink(path)
        usage._query = lambda url=None, timeout=None: None  # type: ignore[assignment]
        down = usage.active_usage(ttl=45.0, cache_path=path)
        _check(down == ("", None, None), "fetch failure -> empty triple")
        usage._query = counting_query  # type: ignore[assignment]
        down2 = usage.active_usage(ttl=45.0, cache_path=path)
        _check(down2 == ("", None, None) and calls["n"] == 3,
               "negative cache served without refetch")
    finally:
        usage._query = saved_query  # type: ignore[assignment]
        shutil.rmtree(tmpdir, ignore_errors=True)


# ---------------------------------------------------------------------------
# Session / beads status-row tests (cc-tmux-session-usage-bars, task 4.1)
# ---------------------------------------------------------------------------

def _test_tmux_get_window_top_pane() -> None:
    # Priority-pick pane resolution — two tracked panes, waiting outranks idle.
    saved = tmux._run_tmux
    try:
        def fake_two_panes(args, *, check_available: bool = True):
            if args[:2] == ["list-panes", "-t"]:
                return "%1\x1fidle\n%2\x1fwaiting"
            return None
        tmux._run_tmux = fake_two_panes  # type: ignore[assignment]
        _check(tmux.get_window_top_pane("@1") == "%2", "waiting pane's id wins over idle")

        def fake_no_output(args, *, check_available: bool = True):
            return None
        tmux._run_tmux = fake_no_output  # type: ignore[assignment]
        _check(tmux.get_window_top_pane("@2") == "", "no tmux output -> ''")

        def fake_untracked(args, *, check_available: bool = True):
            return "%1\x1f"  # pane present, no @cc-state -> untracked
        tmux._run_tmux = fake_untracked  # type: ignore[assignment]
        _check(tmux.get_window_top_pane("@3") == "", "untracked pane -> ''")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_tmux_get_window_active_pane() -> None:
    # Fallback pane source for row 3 (plan 006 / BEADS-03): plain active pane,
    # no @cc-state required. Mirrors _test_tmux_get_window_top_pane's mocking.
    saved = tmux._run_tmux
    try:
        def fake_active(args, *, check_available: bool = True):
            if args[:1] == ["display-message"]:
                return "%5"
            return None
        tmux._run_tmux = fake_active  # type: ignore[assignment]
        _check(tmux.get_window_active_pane("@1") == "%5", "active pane id passes through")

        tmux._run_tmux = lambda args, *, check_available=True: None  # type: ignore[assignment]
        _check(tmux.get_window_active_pane("@1") == "", "no tmux -> '' (fail-open)")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_tmux_get_window_tabs() -> None:
    # Mirrors _test_tmux_get_window_top_pane's mocking convention: fake
    # tmux._run_tmux, branching on the leading args (list-windows vs list-panes).
    # The list-panes -s format now carries 4 fields (cc-tmux-subagent-tab-icon
    # task 1.2 added @cc-subagent-fg/@cc-subagent-bg columns) — pane_id/state
    # are followed by an fg count and a JSON bg-entries list per pane.
    saved = tmux._run_tmux
    try:
        def fake_two_windows(args, *, check_available: bool = True):
            if args[:1] == ["list-windows"]:
                return "@1\x1f1\x1feditor\n@2\x1f2\x1fshell"
            if args[:2] == ["list-panes", "-s"]:
                return (
                    "@1\x1fidle\x1f2\x1f[]\n"
                    "@2\x1fwaiting\x1f\x1f[10.0]\n"
                    "@2\x1factive\x1f1\x1f[20.0, 30.0]"
                )
            return None
        tmux._run_tmux = fake_two_windows  # type: ignore[assignment]
        windows = tmux.get_window_tabs()
        _check(len(windows) == 2, "two windows enumerated")
        by_id = {w.id: w for w in windows}
        _check(by_id["@1"].index == "1" and by_id["@1"].name == "editor", "window @1 id/index/name")
        _check(by_id["@1"].state == "idle", "window @1 top state (single pane)")
        _check(by_id["@1"].fg == 2, "window @1 fg count from its single pane")
        _check(by_id["@1"].bg == [], "window @1 bg entries empty")
        _check(by_id["@2"].name == "shell", "window @2 name")
        _check(by_id["@2"].state == "waiting", "window @2 top state (waiting beats active)")
        # fg SUMS across both of @2's panes (0 from the unparsable '' + 1 from
        # the active pane); bg UNIONS both panes' lists.
        _check(by_id["@2"].fg == 1, "window @2 fg summed across its panes")
        _check(by_id["@2"].bg == [10.0, 20.0, 30.0], "window @2 bg unioned across its panes")

        def fake_no_windows(args, *, check_available: bool = True):
            return None
        tmux._run_tmux = fake_no_windows  # type: ignore[assignment]
        _check(tmux.get_window_tabs() == [], "no tmux output -> []")

        def fake_windows_no_panes(args, *, check_available: bool = True):
            if args[:1] == ["list-windows"]:
                return "@1\x1f1\x1funtracked"
            return None  # list-panes read fails -> every window has no tracked state
        tmux._run_tmux = fake_windows_no_panes  # type: ignore[assignment]
        windows2 = tmux.get_window_tabs()
        _check(len(windows2) == 1 and windows2[0].state == "", "no pane data -> window state ''")
        _check(windows2[0].fg == 0 and windows2[0].bg == [], "no pane data -> fg/bg default to 0/[]")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_render_session_bar() -> None:
    # Full render: model + project + branch on the left (no leading
    # session-count glyph — cc-tmux-active-pane-resolution), SES/5H/7D
    # gauges + combined usage glyph on the right. The account-identity
    # segment moved off this row entirely to row 3 (render_beads_bar) --
    # render_session_bar no longer takes an account_label parameter
    # (cc-tmux-status-bar-popup-polish design.md § Decision 2).
    out = render.render_session_bar("O", "if", "main", 0.1, 0.5, 0.85)
    _check(f"#[fg={render.CYAN}]O" in out, "model letter rendered in CYAN")
    _check("if" in out, "project present on the left")
    _check(f"#[fg={render.BRANCH}]main" in out, "branch present in branch colour")
    _check("#[align=right]" in out, "left/right sides split via align=right")
    _check("#[range=user|accounts]" not in out, "no account-label range marker on row 2 (moved to row 3)")
    _check("SES:" not in out, "SES text is fully replaced by the context bar")
    _check("--:" in out and "5H:" in out and "7D:" in out, "context bar (no raw_tokens -> '--') + usage gauges render")
    # Combined usage glyph renders strictly AFTER the 7D: percentage, as the
    # LAST thing on the right side (design.md § Decision 2 — glyph moves to
    # the end) — the colour reset now lands before the unstyled glyph, not
    # at the very end of the string.
    usage_glyph = render.render_usage_glyph(0.1, 0.5, 0.85, n=10)
    _check(usage_glyph in out, f"combined usage glyph present: {out!r}")
    _check(
        out.index("7D:") < out.index(usage_glyph),
        f"glyph renders strictly after the 7D: percentage: {out!r}",
    )
    _check(out.endswith(usage_glyph), "the unstyled usage glyph is the final element on the right")

    # None percentages -> '--' rendered in DIM for every gauge.
    out_none = render.render_session_bar("", "if", "main", None, None, None)
    _check(out_none.count("--") == 3, "unpolled gauges all render '--'")

    # No model/project/branch -> fields fail-open, no leading glyph token either.
    out3 = render.render_session_bar("", "", "", None, None, None)
    _check("◉" not in out3 and "◌" not in out3, "no leading session-count glyph (removed)")
    _check(f"#[fg={render.CYAN}]" not in out3, "no model letter + no polled usage -> no CYAN segment (fail-open)")
    _check(render.BRANCH not in out3, "no branch -> no branch-colour segment")

    # cc-tmux-git-status-glyphs task 4.3: git_status= six-field glyph format —
    # each field at a representative nonzero count renders its exact glyph
    # string, in the fixed left-to-right order, with the spec-mandated color.
    gs_all = tmux.GitStatusCounts(modified=3, untracked=1, deleted=2, renamed=1, ahead=4, behind=1)
    out_all = render.render_session_bar("F", "if", "main", None, None, None, git_status=gs_all)
    _check(f"#[fg={render.BRANCH}]main" in out_all, "branch still renders with indicators present")
    expected_run = (
        f"#[fg={render.GREEN}]3M "
        f"#[fg={render.YELLOW}]1U "
        f"#[fg={render.RED}]2D "
        f"#[fg={render.BLUE}]1R "
        f"#[fg={render.DIM}]⇡4 "
        f"#[fg={render.DIM}]⇣1"
    )
    _check(expected_run in out_all, f"all-six-nonzero -> exact glyph run in fixed order: {out_all!r}")

    # Zero count for an individual field renders nothing for that field
    # specifically — proven by a partial case (only modified nonzero) so the
    # other five are confirmed cleanly omitted, not just the all-zero case.
    gs_partial = tmux.GitStatusCounts(modified=1)
    out_partial = render.render_session_bar("F", "if", "main", None, None, None, git_status=gs_partial)
    _check(f"#[fg={render.GREEN}]1M" in out_partial, "modified=1 -> GREEN '1M' marker")
    # Anchored on colour codes (not bare letters) since "7D:" from the usage
    # gauges always contains a literal 'D' regardless of git status —
    # RED/YELLOW/BLUE are otherwise unused when ses/5h/7d are all None
    # (color_for only returns DIM/RED/YELLOW/CYAN for gauges, never GREEN/BLUE,
    # and here all three are None so gauges render DIM only).
    _check(f"#[fg={render.YELLOW}]" not in out_partial, "untracked=0 -> no YELLOW 'U' marker")
    _check(f"#[fg={render.RED}]" not in out_partial, "deleted=0 -> no RED 'D' marker")
    _check(f"#[fg={render.BLUE}]" not in out_partial, "renamed=0 -> no BLUE 'R' marker")
    _check("⇡" not in out_partial, "ahead=0 -> no '⇡N' glyph")
    _check("⇣" not in out_partial, "behind=0 -> no '⇣N' glyph")

    # All-six-zero (explicit GitStatusCounts()) -> no working-tree-indicator
    # segment at all; branch renders alone with nothing appended after it.
    out_zero = render.render_session_bar(
        "F", "if", "main", None, None, None, git_status=tmux.GitStatusCounts()
    )
    _check(f"#[fg={render.BRANCH}]main#[default]" in out_zero, "all-zero -> branch renders with no indicator segment")
    _check(f"#[fg={render.GREEN}]" not in out_zero, "all-zero -> no GREEN 'M' marker")
    _check(f"#[fg={render.YELLOW}]" not in out_zero, "all-zero -> no YELLOW 'U' marker")
    _check(f"#[fg={render.RED}]" not in out_zero, "all-zero -> no RED 'D' marker")
    _check(f"#[fg={render.BLUE}]" not in out_zero, "all-zero -> no BLUE 'R' marker")
    _check("⇡" not in out_zero, "all-zero -> no '⇡N' glyph")
    _check("⇣" not in out_zero, "all-zero -> no '⇣N' glyph")

    # git_status=None renders identically to an explicit all-zero instance.
    out_none = render.render_session_bar("F", "if", "main", None, None, None, git_status=None)
    _check(out_none == out_zero, "git_status=None must match explicit GitStatusCounts() byte-for-byte")

    # No-kwargs call (git_status omitted entirely) -> same as explicit None.
    out_default = render.render_session_bar("F", "if", "main", None, None, None)
    _check(out_default == out_zero, "no-kwargs call must match explicit git_status=GitStatusCounts()")


def _test_render_beads_bar() -> None:
    # if-bqw.1 (cc commit b6b9a234 / cc-w83ov.4): render_beads_bar now takes
    # structured (openspec_open, openspec_in_progress, openspec_ua,
    # beads_open, beads_ready, beads_blocked) counts + independent per-half
    # ages, rendering the abbreviated `op:`/`bd:` format.
    D = render.DIM
    Y = render.YELLOW
    R = render.RED

    # No counts at all (no cache, or nothing parsed from either line) -> ''.
    _check(render.render_beads_bar(None, None, None, None, None, None) == "", "all None -> ''")

    # A half is "present" only when ALL THREE of its counts are non-None — a
    # partially-present half (one count set, the others None, e.g. from a
    # malformed line) renders as fully absent, same as fully-None (fail-open
    # contract: a broken half never leaks a placeholder value).
    out_partial = render.render_beads_bar(12, 1, None, 1, 5, 2)
    _check("op:" not in out_partial, "partial openspec half (ua=None) omitted entirely")
    _check(
        out_partial == f"#[fg={D}]bd: 1o 5r #[fg={Y}]2#[fg={D}]b#[default]",
        "partial openspec half omitted -> only the valid bd half renders",
    )

    # Openspec-only (beads half fully absent) -> single segment, no
    # separator, no bd text anywhere.
    out_openspec_only = render.render_beads_bar(12, 1, 0, None, None, None)
    _check(
        out_openspec_only == f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua#[default]",
        "openspec-only: single segment, zero ua -> DIM",
    )
    _check(
        "bd:" not in out_openspec_only and "|" not in out_openspec_only,
        "openspec-only: no bd segment, no separator",
    )

    # Beads-only (openspec half fully absent) -> single segment.
    out_beads_only = render.render_beads_bar(None, None, None, 1, 5, 0)
    _check(
        out_beads_only == f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b#[default]",
        "beads-only: single segment, zero blocked -> DIM",
    )
    _check("op:" not in out_beads_only, "beads-only: no openspec segment")

    # Both halves present, zero ua/blocked -> DIM throughout, joined by the
    # DIM ' | ' separator, single trailing reset (mirrors the old
    # multi-line-join shape, now built from two structured segments).
    out_both_zero = render.render_beads_bar(12, 1, 0, 1, 5, 0)
    _check(
        out_both_zero == (
            f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b#[default]"
        ),
        "both halves, zero ua/blocked -> DIM, joined by separator",
    )
    _check(out_both_zero.count("#[default]") == 1, "single trailing reset, not per-segment")

    # Nonzero-but-below-threshold ua/blocked -> YELLOW; open/in_progress/ready
    # counts always stay DIM regardless of their own value (informational,
    # not a health signal).
    out_yellow = render.render_beads_bar(12, 1, 3, 1, 5, 2)
    _check(
        out_yellow == (
            f"#[fg={D}]op: 12o 1ip #[fg={Y}]3#[fg={D}]ua"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={Y}]2#[fg={D}]b#[default]"
        ),
        "ua/blocked > 0 and < high threshold -> YELLOW",
    )

    # At/above the documented high-count threshold -> RED.
    hi_u, hi_b = render.BEADS_UNARCHIVED_HIGH, render.BEADS_BLOCKED_HIGH
    out_red = render.render_beads_bar(12, 1, hi_u, 1, 5, hi_b)
    _check(
        out_red == (
            f"#[fg={D}]op: 12o 1ip #[fg={R}]{hi_u}#[fg={D}]ua"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={R}]{hi_b}#[fg={D}]b#[default]"
        ),
        "ua/blocked >= documented high threshold -> RED",
    )
    _check(render._threshold_color(hi_u - 1, hi_u) == Y, "one below threshold -> still YELLOW, not RED")

    # Staleness markers (plan 006 / BEADS-01, extended to independent
    # per-segment ages): fresh/unknown age -> unchanged; age beyond
    # BEADS_STALE_AFTER_SEC on ONE half -> DIM trailing "(<duration>)" marker
    # on that segment only, the other segment unaffected.
    base = (
        f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua"
        f"{render._BEADS_SEP}"
        f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b#[default]"
    )
    _check(render.render_beads_bar(12, 1, 0, 1, 5, 0, None, None) == base, "both ages None -> no markers")
    _check(render.render_beads_bar(12, 1, 0, 1, 5, 0, 60.0, 60.0) == base, "both fresh -> no markers")
    _check(
        render.render_beads_bar(
            12, 1, 0, 1, 5, 0, render.BEADS_STALE_AFTER_SEC, render.BEADS_STALE_AFTER_SEC
        ) == base,
        "age exactly at threshold -> not yet stale (strict >)",
    )

    out_stale_openspec = render.render_beads_bar(12, 1, 0, 1, 5, 0, 901.0, None)
    _check(
        out_stale_openspec == (
            f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua (15m)"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b#[default]"
        ),
        "stale openspec age only -> (15m) marker on the op segment, bd segment unaffected",
    )

    out_stale_beads = render.render_beads_bar(12, 1, 0, 1, 5, 0, None, 7500.0)
    _check(
        out_stale_beads == (
            f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b (2h)#[default]"
        ),
        "stale beads age only -> (2h) marker on the bd segment, op segment unaffected",
    )


def _test_render_beads_bar_account_segment() -> None:
    """cc-tmux-status-bar-popup-polish task 4.4: render_beads_bar's new
    ``account_label`` parameter adds a third, independent segment carrying
    the active account's identity (design.md § Decision 3) -- the
    ``#[range=user|accounts]`` click marker relocated here from
    render_session_bar.
    """
    D = render.DIM
    label = "leo@x.dev·bc7da511"

    # (a) openspec + beads + account_label all present -> all three segments
    # appear, _BEADS_SEP-joined, account segment LAST, wrapped in the range
    # marker relocated from row 2. Openspec/beads values mirror
    # _test_render_beads_bar's "both halves, zero ua/blocked" case (12, 1, 0,
    # 1, 5, 0) — the new in_progress/open slots are just non-None
    # placeholders here, not what this test is about.
    out_all = render.render_beads_bar(12, 1, 0, 1, 5, 0, account_label=label)
    _check(
        out_all == (
            f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b"
            f"{render._BEADS_SEP}"
            f"#[range=user|accounts]#[fg={D}]{label}#[norange]#[default]"
        ),
        f"all three segments present, account segment last, range-marker-wrapped: {out_all!r}",
    )
    _check(out_all.count(render._BEADS_SEP) == 2, "two separators join three segments")

    # (b) openspec/beads BOTH absent (today's "no cache" case), account_label
    # present -> row shows ONLY the account segment, not "". All 6 count
    # positionals stay None here — the "absent" case doesn't need placeholders.
    out_account_only = render.render_beads_bar(
        None, None, None, None, None, None, account_label=label
    )
    _check(
        out_account_only == f"#[range=user|accounts]#[fg={D}]{label}#[norange]#[default]",
        f"no cache, account present -> account-only row, not empty: {out_account_only!r}",
    )
    _check("op:" not in out_account_only and "bd:" not in out_account_only, "no count segments leak in")

    # (c) account_label absent, openspec/beads present -> unchanged
    # two-segment behavior (regression guard for today's existing contract).
    out_two_segment = render.render_beads_bar(12, 1, 0, 1, 5, 0)
    _check(
        out_two_segment == (
            f"#[fg={D}]op: 12o 1ip #[fg={D}]0#[fg={D}]ua"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]bd: 1o 5r #[fg={D}]0#[fg={D}]b#[default]"
        ),
        f"account_label omitted -> unchanged two-segment behavior: {out_two_segment!r}",
    )
    _check("range=user|accounts" not in out_two_segment, "no account segment when account_label is empty")

    # (d) all three absent -> "" (unchanged empty-row contract).
    _check(render.render_beads_bar(None, None, None, None, None, None) == "", "all three absent -> ''")
    _check(
        render.render_beads_bar(None, None, None, None, None, None, account_label="") == "",
        "explicit empty account_label -> ''",
    )


def _test_render_tabs_row() -> None:
    # Empty window list -> '' (nothing to show).
    _check(render.render_tabs_row([], "@1", 0.0) == "", "empty window list -> ''")

    windows = [
        _FakeWindow(id="@1", index="1", name="editor", state="waiting"),
        _FakeWindow(id="@2", index="2", name="shell", state=""),
    ]

    out = render.render_tabs_row(windows, "@2", now=1.0)

    # Active window (id == active_window_id) renders bold CYAN; the animated
    # icon for its tracked state is reused verbatim from animated_icon, not
    # re-derived (window @1 is NOT active here — inactive windows still get
    # their icon, only the colour differs). Index/icon/name are space-
    # separated (not colon-separated), matching the old #I #(icon)#W
    # convention, and each segment is wrapped in #[range=window|N]/#[norange]
    # so the default MouseDown1Status binding can route clicks.
    icon_waiting = render.animated_icon("waiting", 1.0)
    _check(
        f"#[fg={render.DIM}]#[range=window|1] 1 {icon_waiting} editor #[norange]#[default]" in out,
        "inactive window: DIM, icon present, range=window|1, space-separated",
    )

    # Untracked window (state == '') renders no icon at all — bare
    # 'index name', matching resolve_tab_icon's untracked contract —
    # and IS styled as the active window here (bold CYAN).
    _check(
        f"#[fg={render.CYAN},bold]#[range=window|2] 2 shell #[norange]#[default]" in out,
        "active + untracked: CYAN bold, no icon glyph, range=window|2",
    )
    _check("2:  shell" not in out, "untracked window never renders a double space where the icon would be")
    _check(":" not in out, "no colon separator anywhere in the rendered row")

    # Swap which window is active -> the CYAN/DIM assignment flips accordingly.
    out2 = render.render_tabs_row(windows, "@1", now=1.0)
    _check(
        f"#[fg={render.CYAN},bold]#[range=window|1] 1 {icon_waiting} editor #[norange]#[default]" in out2,
        "window @1 active -> CYAN bold, range=window|1",
    )
    _check(f"#[fg={render.DIM}]#[range=window|2] 2 shell #[norange]#[default]" in out2, "window @2 inactive -> DIM")

    # No matching active_window_id (e.g. tmux.current_window_id() failed and
    # returned '') -> every window renders DIM, none crash on the empty-string
    # comparison (fail-open). Range markup still present per-window.
    out3 = render.render_tabs_row(windows, "", now=1.0)
    _check(f"#[fg={render.CYAN}" not in out3, "no active id -> no window rendered as active")
    _check("#[range=window|1]" in out3 and "#[range=window|2]" in out3, "range markup present regardless of active id")


def _test_cli_read_roadmap_pulse_fail_open() -> None:
    # No pane id -> ('', None) without ever touching tmux.
    _check(cli._read_roadmap_pulse("") == ("", None), "empty pane -> ('', None)")

    saved_run = tmux._run_tmux
    try:
        # tmux returns no cwd -> ('', None) (nothing to resolve).
        tmux._run_tmux = lambda args, *, check_available=True: ""  # type: ignore[assignment]
        _check(cli._read_roadmap_pulse("%1") == ("", None), "no cwd -> ('', None)")

        # cwd present but registry resolves no code -> ('', None).
        tmux._run_tmux = (  # type: ignore[assignment]
            lambda args, *, check_available=True: "/definitely/not/tracked"
        )
        _check(cli._read_roadmap_pulse("%1") == ("", None), "untracked cwd -> ('', None)")
    finally:
        tmux._run_tmux = saved_run  # type: ignore[assignment]

    if registry.tomllib is None:
        # 3.10 interpreter: the resolved-code branches below need tomllib.
        return

    saved_dotfiles = os.environ.get("DOTFILES")
    saved_cfg = os.environ.get("CLAUDE_CONFIG_DIR")
    saved_run2 = tmux._run_tmux
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-pulse-test-")
    try:
        rel = "cc-tmux-pulse-project-zzz"
        os.makedirs(os.path.join(tmpdir, "home"), exist_ok=True)
        with open(os.path.join(tmpdir, "home", "projects.toml"), "w") as f:
            f.write(f'[[projects]]\ncode = "zz"\nname = "T"\npath = "{rel}"\n')
        os.environ["DOTFILES"] = tmpdir

        cfg = os.path.join(tmpdir, "cfg")
        state_dir = os.path.join(cfg, "scripts", "state")
        os.makedirs(state_dir, exist_ok=True)
        os.environ["CLAUDE_CONFIG_DIR"] = cfg

        cwd = os.path.join(os.path.expanduser("~"), rel, "sub")
        tmux._run_tmux = lambda args, *, check_available=True: cwd  # type: ignore[assignment]
        pulse_file = os.path.join(state_dir, "roadmap-pulse.zz.line")

        # Positive read proves the plumbing actually reaches the resolved file
        # (so the fail-open cases below aren't passing vacuously).
        with open(pulse_file, "w") as f:
            f.write("next: /apply zz-thing 1o 2u\n")
        content, age = cli._read_roadmap_pulse("%1")
        _check(content == "next: /apply zz-thing 1o 2u", "reads + strips the resolved pulse line")
        _check(isinstance(age, float) and 0.0 <= age < 60.0, "fresh write -> small non-negative float age")

        # Missing file -> ('', None) (fail-open, never raises).
        os.unlink(pulse_file)
        _check(cli._read_roadmap_pulse("%1") == ("", None), "missing pulse file -> ('', None)")

        # A directory where the file should be -> unreadable -> ('', None) (never raises).
        os.makedirs(pulse_file)
        _check(cli._read_roadmap_pulse("%1") == ("", None), "unreadable pulse path -> ('', None)")
    finally:
        tmux._run_tmux = saved_run2  # type: ignore[assignment]
        for key, val in (("DOTFILES", saved_dotfiles), ("CLAUDE_CONFIG_DIR", saved_cfg)):
            if val is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = val
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_cli_read_roadmap_pulse_radar_strip() -> None:
    # Defensive radar: line strip (cc-tmux-row3-openspec-beads-format task
    # 2.1): a stray radar:-prefixed line (e.g. a stale cache file written
    # before ~/dev/cc commit 88d0558e removed it from --line mode) is
    # dropped; content with none is unaffected.
    if registry.tomllib is None:
        return  # 3.10 interpreter: the resolved-code path below needs tomllib.

    saved_dotfiles = os.environ.get("DOTFILES")
    saved_cfg = os.environ.get("CLAUDE_CONFIG_DIR")
    saved_run = tmux._run_tmux
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-radar-test-")
    try:
        rel = "cc-tmux-radar-project-zzz"
        os.makedirs(os.path.join(tmpdir, "home"), exist_ok=True)
        with open(os.path.join(tmpdir, "home", "projects.toml"), "w") as f:
            f.write(f'[[projects]]\ncode = "zr"\nname = "T"\npath = "{rel}"\n')
        os.environ["DOTFILES"] = tmpdir

        cfg = os.path.join(tmpdir, "cfg")
        state_dir = os.path.join(cfg, "scripts", "state")
        os.makedirs(state_dir, exist_ok=True)
        os.environ["CLAUDE_CONFIG_DIR"] = cfg

        cwd = os.path.join(os.path.expanduser("~"), rel, "sub")
        tmux._run_tmux = lambda args, *, check_available=True: cwd  # type: ignore[assignment]
        pulse_file = os.path.join(state_dir, "roadmap-pulse.zr.line")

        # A stray radar: line is stripped entirely; the other lines survive.
        with open(pulse_file, "w") as f:
            f.write("radar:stale (1d)\nop: 12o 3ip 1ua\nbd: 4o 5r 2b\n")
        content, _age = cli._read_roadmap_pulse("%1")
        _check(
            content == "op: 12o 3ip 1ua\nbd: 4o 5r 2b",
            "radar: line stripped, other lines preserved",
        )
        _check("radar:" not in content, "no radar: content survives the read")

        # Content with no radar: line at all is unaffected.
        with open(pulse_file, "w") as f:
            f.write("op: 12o 3ip 1ua\nbd: 4o 5r 2b\n")
        content2, _age2 = cli._read_roadmap_pulse("%1")
        _check(
            content2 == "op: 12o 3ip 1ua\nbd: 4o 5r 2b",
            "content without a radar: line is unaffected",
        )

        # A radar:-only cache file strips down to empty content (fail-open,
        # matching the row's existing 'nothing to show' contract).
        with open(pulse_file, "w") as f:
            f.write("radar:stale (1d)\n")
        content3, _age3 = cli._read_roadmap_pulse("%1")
        _check(content3 == "", "radar:-only content -> '' after stripping")
    finally:
        tmux._run_tmux = saved_run  # type: ignore[assignment]
        for key, val in (("DOTFILES", saved_dotfiles), ("CLAUDE_CONFIG_DIR", saved_cfg)):
            if val is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = val
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_cli_parse_roadmap_pulse_counts() -> None:
    # if-bqw.1 (cc commit b6b9a234 / cc-w83ov.4): parse the abbreviated
    # "op: {N}o {M}ip {K}ua" / "bd: {N}o {M}r {K}b" cache format into
    # structured counts, each half independent and fail-open.
    both = "op: 12o 3ip 1ua\nbd: 4o 5r 2b"
    _check(
        cli._parse_roadmap_pulse_counts(both) == (12, 3, 1, 4, 5, 2),
        "well-formed two-line content -> both halves parsed",
    )

    # Real-world sample verified live against
    # ~/.claude/scripts/state/roadmap-pulse.if.line (if-bqw.1 task text).
    live_sample = "op: 1o 0ip 0ua\nbd: 1o 1r 0b"
    _check(
        cli._parse_roadmap_pulse_counts(live_sample) == (1, 0, 0, 1, 1, 0),
        "live-sample two-line content -> both halves parsed exactly",
    )

    reordered = "bd: 4o 5r 2b\nop: 12o 3ip 1ua"
    _check(
        cli._parse_roadmap_pulse_counts(reordered) == (12, 3, 1, 4, 5, 2),
        "line order doesn't affect parsing",
    )

    openspec_only = "op: 12o 3ip 1ua"
    _check(
        cli._parse_roadmap_pulse_counts(openspec_only) == (12, 3, 1, None, None, None),
        "missing bd line -> beads half absent (None, None, None), openspec half intact",
    )

    malformed_beads = "op: 12o 3ip 1ua\nbd: garbage"
    _check(
        cli._parse_roadmap_pulse_counts(malformed_beads) == (12, 3, 1, None, None, None),
        "malformed bd line -> beads half absent, openspec half intact",
    )

    beads_only = "bd: 4o 5r 2b"
    _check(
        cli._parse_roadmap_pulse_counts(beads_only) == (None, None, None, 4, 5, 2),
        "missing op line -> openspec half absent (None, None, None), beads half intact",
    )

    malformed_openspec = "op: not-a-number ip 1ua\nbd: 4o 5r 2b"
    _check(
        cli._parse_roadmap_pulse_counts(malformed_openspec) == (None, None, None, 4, 5, 2),
        "malformed op line -> openspec half absent, beads half intact",
    )

    _check(
        cli._parse_roadmap_pulse_counts("") == (None, None, None, None, None, None),
        "empty content -> both halves absent",
    )
    _check(
        cli._parse_roadmap_pulse_counts("some unrelated line") == (None, None, None, None, None, None),
        "unrelated content -> both halves absent",
    )


def _test_cli_beads_pane_fallback() -> None:
    # Row 3 pane resolution (plan 006 / BEADS-03): tracked pane wins; with no
    # tracked pane the window's active pane is used; both empty -> ''.
    saved_top = tmux.get_window_top_pane
    saved_active = tmux.get_window_active_pane
    try:
        tmux.get_window_top_pane = lambda w: "%2"  # type: ignore[assignment]
        tmux.get_window_active_pane = lambda w: "%9"  # type: ignore[assignment]
        _check(cli._beads_pane("@1") == "%2", "tracked pane preferred over active pane")

        tmux.get_window_top_pane = lambda w: ""  # type: ignore[assignment]
        _check(cli._beads_pane("@1") == "%9", "no tracked pane -> active-pane fallback")

        tmux.get_window_active_pane = lambda w: ""  # type: ignore[assignment]
        _check(cli._beads_pane("@1") == "", "no pane at all -> '' (fail-open)")
    finally:
        tmux.get_window_top_pane = saved_top  # type: ignore[assignment]
        tmux.get_window_active_pane = saved_active  # type: ignore[assignment]


def _test_cli_beads_pane_active_tracked_preferred() -> None:
    """Row 3 pane resolution mirrors row 2's tmux-active-pane-first
    preference (found + fixed 2026-07-13): a window with TWO tracked panes
    must use the tmux-ACTIVE one, not `get_window_top_pane`'s priority pick
    — the exact bug this regression test locks in had row 3 silently
    reading a different (and stale) pane's project on a multi-tracked-pane
    window, even though :func:`cli._resolve_session_pane` (row 2) had
    already been fixed for this same class of bug.
    """
    saved_active = tmux.get_window_active_pane
    saved_option = tmux.get_pane_option
    saved_top = tmux.get_window_top_pane
    try:
        tmux.get_window_active_pane = lambda w: "%5"  # type: ignore[assignment]
        tmux.get_pane_option = lambda pane, opt: "idle"  # type: ignore[assignment]
        tmux.get_window_top_pane = lambda w: "%2"  # type: ignore[assignment]  # the OLD (wrong) winner
        _check(
            cli._beads_pane("@1") == "%5",
            "tmux-active tracked pane must win over the priority-pick top pane",
        )
    finally:
        tmux.get_window_active_pane = saved_active  # type: ignore[assignment]
        tmux.get_pane_option = saved_option  # type: ignore[assignment]
        tmux.get_window_top_pane = saved_top  # type: ignore[assignment]


def _test_cli_resolve_session_pane_active_tracked() -> None:
    # Row 2 pane resolution (cc-tmux-active-pane-resolution): the window's
    # tmux-active pane wins outright when it is a tracked Claude pane (its
    # @cc-state is in VALID_STATES) — the priority-pick fallback is never
    # consulted for its return value.
    saved_active = tmux.get_window_active_pane
    saved_option = tmux.get_pane_option
    saved_top = tmux.get_window_top_pane
    try:
        tmux.get_window_active_pane = lambda w: "%5"  # type: ignore[assignment]
        tmux.get_pane_option = lambda pane, opt: "idle"  # type: ignore[assignment]
        tmux.get_window_top_pane = lambda w: "%99"  # type: ignore[assignment]
        _check(cli._resolve_session_pane("@1") == "%5", "tracked active pane must win")
    finally:
        tmux.get_window_active_pane = saved_active  # type: ignore[assignment]
        tmux.get_pane_option = saved_option  # type: ignore[assignment]
        tmux.get_window_top_pane = saved_top  # type: ignore[assignment]


def _test_cli_resolve_session_pane_active_untracked_fallback() -> None:
    # Active pane resolves but is untracked (no/invalid @cc-state, e.g. a
    # plain shell pane focused next to a background Claude pane) -> falls
    # back to the priority-pick winner from get_window_top_pane.
    saved_active = tmux.get_window_active_pane
    saved_option = tmux.get_pane_option
    saved_top = tmux.get_window_top_pane
    try:
        tmux.get_window_active_pane = lambda w: "%5"  # type: ignore[assignment]
        tmux.get_pane_option = lambda pane, opt: ""  # type: ignore[assignment]  # untracked
        tmux.get_window_top_pane = lambda w: "%2"  # type: ignore[assignment]
        _check(cli._resolve_session_pane("@1") == "%2", "untracked active pane -> top-pane fallback")
    finally:
        tmux.get_window_active_pane = saved_active  # type: ignore[assignment]
        tmux.get_pane_option = saved_option  # type: ignore[assignment]
        tmux.get_window_top_pane = saved_top  # type: ignore[assignment]


def _test_cli_resolve_session_pane_no_active_fallback() -> None:
    # No active pane resolvable at all (get_window_active_pane falsy, e.g.
    # tmux.display-message failed) -> falls back cleanly to
    # get_window_top_pane, fail-open, no exception. get_pane_option must
    # never be invoked here (short-circuit on the falsy `active`).
    saved_active = tmux.get_window_active_pane
    saved_option = tmux.get_pane_option
    saved_top = tmux.get_window_top_pane
    try:
        tmux.get_window_active_pane = lambda w: ""  # type: ignore[assignment]

        def _boom(pane, opt):
            raise AssertionError("get_pane_option must not be called when active pane is falsy")

        tmux.get_pane_option = _boom  # type: ignore[assignment]
        tmux.get_window_top_pane = lambda w: "%3"  # type: ignore[assignment]
        _check(cli._resolve_session_pane("@1") == "%3", "no active pane -> top-pane fallback, fail-open")
    finally:
        tmux.get_window_active_pane = saved_active  # type: ignore[assignment]
        tmux.get_pane_option = saved_option  # type: ignore[assignment]
        tmux.get_window_top_pane = saved_top  # type: ignore[assignment]


def _test_render_session_bar_no_glyph() -> None:
    # session_count was removed as a render_session_bar parameter entirely
    # (cc-tmux-active-pane-resolution) — the leading session-count glyph
    # (◉/◌) must never appear, regardless of how many tracked panes exist
    # for the project, since the function no longer takes a pane count at
    # all. Left side is now purely model_letter/project/branch composition.
    out_full = render.render_session_bar("O", "if", "main", 0.1, 0.5, 0.85)
    _check("◉" not in out_full and "◌" not in out_full, "populated call -> no glyph token")
    _check("#[range=user|accounts]" not in out_full, "no account-label range marker (moved to row 3)")

    out_empty = render.render_session_bar("", "", "", None, None, None)
    _check("◉" not in out_empty and "◌" not in out_empty, "empty call -> no glyph token")


def _test_render_session_bar_usage_glyph_wiring() -> None:
    """render_session_bar (row 2, cc-tmux-braille-usage-glyph task 4.4): the
    combined 3-metric braille glyph (n=10) renders alongside the unchanged
    SES token-count label + 5H/7D text, replacing the former shade-block
    bar (tasks 3.1/3.3); the SES label is severity-coloured, not DIM (task
    3.4 correction)."""
    out = render.render_session_bar(
        "O", "if", "main", 0.30, 0.88, 0.35, raw_tokens=252_500
    )
    expected_glyph = render.render_usage_glyph(0.30, 0.88, 0.35, n=10)
    _check(expected_glyph in out, f"row 2 carries the 10-cell 3-metric glyph: {out!r}")
    _check("252.5k:" in out, f"row 2 still carries the unchanged SES token-count label: {out!r}")
    # 5H:/7D: labels and their percentages are separately-coloured segments
    # (not a contiguous "5H:88%" string -- see the f-string in
    # render_session_bar), same convention _test_render_session_bar already
    # asserts on.
    _check(
        "5H:" in out and "88%" in out and "7D:" in out and "35%" in out,
        f"row 2 still carries the unchanged 5H/7D text: {out!r}",
    )
    # 252_500 raw tokens falls in the >200k/<=300k tier -> steady YELLOW (no
    # pulse tier, so this assertion isn't wall-clock-flaky).
    _check(
        f"#[fg={usage.YELLOW}]252.5k:" in out,
        f"SES label wrapped in its severity colour (task 3.4), not DIM: {out!r}",
    )
    _check(
        f"#[fg={render.DIM}]252.5k:" not in out, f"SES label no longer plain DIM: {out!r}"
    )
    # cc-tmux-status-bar-popup-polish: no account-label text/marker anywhere,
    # and the glyph renders strictly AFTER the 7D: percentage (order
    # assertion, design.md § Decision 2).
    _check("#[range=user|accounts]" not in out, "no account-label range marker on row 2")
    _check(
        out.index("7D:") < out.index(expected_glyph),
        f"glyph renders strictly after the 7D: percentage: {out!r}",
    )


def _test_conductor_attach_command() -> None:
    import shlex as _shlex

    _check(
        conductor._attach_command("conductor") == "tmux attach-session -t conductor",
        "plain name must pass through unquoted-equivalent",
    )
    hostile = "bad name; rm -rf /"
    _check(
        _shlex.split(conductor._attach_command(hostile))
        == ["tmux", "attach-session", "-t", hostile],
        "hostile name must survive shell splitting as ONE argv token",
    )


def _test_conductor_send_prompt_refusal() -> None:
    _check(conductor._send_prompt_refusal("idle", False) is None, "idle must be sendable")
    _check(conductor._send_prompt_refusal("waiting", False) is None, "waiting must be sendable")
    active = conductor._send_prompt_refusal("active", False)
    _check(active is not None and "active" in active, "active must refuse with busy reason")
    untracked = conductor._send_prompt_refusal(None, False)
    _check(untracked is not None and "not a tracked" in untracked, "None state must refuse")
    _check(conductor._send_prompt_refusal(None, True) is None, "--force overrides untracked")
    _check(conductor._send_prompt_refusal("active", True) is None, "--force overrides busy")


def _test_conductor_resolve_dir() -> None:
    saved = os.environ.get("CC_TMUX_CONDUCTOR")
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-conductor-test-")
    try:
        os.environ.pop("CC_TMUX_CONDUCTOR", None)

        # Explicit valid target -> resolved, no error.
        got, err = conductor._resolve_dir(tmpdir)
        _check(got == os.path.abspath(tmpdir) and err == "", "valid explicit target must resolve")

        # Explicit invalid target -> (None, reason); NEVER a silent fallback.
        got, err = conductor._resolve_dir(os.path.join(tmpdir, "nope"))
        _check(got is None and "not a directory" in err, "invalid explicit target must refuse")

        # Conductor context without a target -> (None, reason requiring --target).
        os.environ["CC_TMUX_CONDUCTOR"] = "1"
        got, err = conductor._resolve_dir(None)
        _check(got is None and "explicit --target" in err, "conductor context must require --target")
    finally:
        if saved is None:
            os.environ.pop("CC_TMUX_CONDUCTOR", None)
        else:
            os.environ["CC_TMUX_CONDUCTOR"] = saved
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_conductor_worktree_slot() -> None:
    top = "/repo"
    # Free slot -> bare stamp, no suffix.
    branch, path = conductor._worktree_slot(top, "20260711-120000", exists=lambda _p: False)
    _check(branch == "conductor/20260711-120000", f"bare branch wrong: {branch}")
    _check(path == "/repo/.worktrees/conductor-20260711-120000", f"bare path wrong: {path}")

    # First two taken -> -3 suffix on BOTH branch and path.
    taken = {
        "/repo/.worktrees/conductor-20260711-120000",
        "/repo/.worktrees/conductor-20260711-120000-2",
    }
    branch, path = conductor._worktree_slot(top, "20260711-120000", exists=lambda p: p in taken)
    _check(branch == "conductor/20260711-120000-3", f"suffixed branch wrong: {branch}")
    _check(path == "/repo/.worktrees/conductor-20260711-120000-3", f"suffixed path wrong: {path}")


def _test_conductor_pane_ready() -> None:
    _check(conductor._pane_ready(None) is False, "None capture -> not ready")
    _check(conductor._pane_ready("") is False, "empty capture -> not ready")
    _check(conductor._pane_ready("   \n\n  ") is False, "whitespace capture -> not ready")
    _check(conductor._pane_ready("Welcome to Claude Code") is True, "painted -> ready")


def _test_conductor_wait_for_pane_ready() -> None:
    # Ready on the first capture: no sleeps consumed.
    sleeps: List[float] = []
    ok = conductor._wait_for_pane_ready(
        "%1", timeout=5.0, interval=0.25,
        capture=lambda: "hello", sleep=sleeps.append, clock=lambda: 0.0,
    )
    _check(ok is True and sleeps == [], "immediately-ready pane must not sleep")

    # Never ready: fake clock advances past the deadline -> False, bounded.
    ticks = iter([0.0, 1.0, 2.0, 3.0])
    ok = conductor._wait_for_pane_ready(
        "%1", timeout=2.5, interval=0.25,
        capture=lambda: "", sleep=lambda _s: None, clock=lambda: next(ticks),
    )
    _check(ok is False, "never-ready pane must time out False")


def _test_cli_evaluate_plugin_listing() -> None:
    raw = ('[{"id": "cc-tmux@cc-tmux", "version": "0.1.1", '
           '"enabled": true, "installPath": "/snap/0.1.1"}]')
    rows, install = cli._evaluate_plugin_listing(raw, "0.1.1")
    _check(install == "/snap/0.1.1", f"installPath not extracted: {install!r}")
    _check(rows[0] == ("PASS", "claude plugin enabled", "enabled (v0.1.1)"),
           f"enabled entry should PASS: {rows[0]!r}")
    _check(rows[1][0] == "PASS" and rows[1][1] == "plugin snapshot version",
           f"matching versions should PASS: {rows[1]!r}")


def _test_cli_evaluate_plugin_listing_degraded() -> None:
    raw_disabled = ('[{"id": "cc-tmux@cc-tmux", "version": "0.1.1", '
                    '"enabled": false, "installPath": "/snap/0.1.1"}]')
    rows, _ = cli._evaluate_plugin_listing(raw_disabled, "0.1.1")
    _check(rows[0][0] == "FAIL", f"disabled plugin must FAIL: {rows[0]!r}")

    raw_stale = ('[{"id": "cc-tmux@cc-tmux", "version": "0.1.0", '
                 '"enabled": true, "installPath": "/snap/0.1.0"}]')
    rows, _ = cli._evaluate_plugin_listing(raw_stale, "0.1.1")
    _check(rows[1][0] == "WARN", f"version mismatch must WARN: {rows[1]!r}")

    rows, install = cli._evaluate_plugin_listing("[]", "0.1.1")
    _check(rows[0][0] == "WARN" and install == "",
           f"missing entry must WARN with empty install: {rows!r}")

    rows, install = cli._evaluate_plugin_listing("not json", "0.1.1")
    _check(rows[0][0] == "WARN" and install == "",
           f"garbage input must WARN, never raise: {rows!r}")


def _test_cli_evaluate_hook_liveness() -> None:
    _check(cli._evaluate_hook_liveness(0, None, 1000.0)[0] == "INFO",
           "no live panes -> INFO")
    _check(cli._evaluate_hook_liveness(2, None, 1000.0)[0] == "FAIL",
           "live panes + no register evidence -> FAIL")
    _check(cli._evaluate_hook_liveness(2, 0.0, 1000.0)[0] == "FAIL",
           "zero/invalid ts counts as no evidence -> FAIL")


def _test_cli_evaluate_hook_liveness_ages() -> None:
    now = 100000.0
    _check(cli._evaluate_hook_liveness(1, now - 60.0, now)[0] == "PASS",
           "1-min-old register -> PASS")
    _check(cli._evaluate_hook_liveness(1, now - 1800.0, now)[0] == "PASS",
           "exactly at threshold -> PASS (boundary inclusive)")
    _check(cli._evaluate_hook_liveness(1, now - 3600.0, now)[0] == "WARN",
           "1-hour-old register with live panes -> WARN")


def _test_cli_trace_needs_trim() -> None:
    _check(cli.trace_needs_trim(0) is False, "empty file must not trim")
    _check(cli.trace_needs_trim(cli._REGISTER_TRACE_TRIM_BYTES) is False, "at threshold must not trim")
    _check(cli.trace_needs_trim(cli._REGISTER_TRACE_TRIM_BYTES + 1) is True, "past threshold must trim")
    _check(cli.trace_needs_trim(100, threshold=10) is True, "explicit threshold honored")


def _test_cli_accounts_popup_max_line_width() -> None:
    """cc-tmux-status-bar-popup-polish task 3.4 follow-up (2026-07-14,
    beads if-s1yu): :func:`cli._accounts_popup_max_line_width` must measure
    the ANSI-STRIPPED visual width, not the raw ``len()`` of a colour-coded
    line — a green-wrapped reset-time line carries
    ``\\x1b[38;2;0;172;58m`` + ``\\x1b[0m`` bytes that render as zero columns
    but would otherwise inflate the computed popup width far past what the
    content actually needs.
    """
    _check(cli._accounts_popup_max_line_width("") == 0, "empty body -> 0")
    _check(cli._accounts_popup_max_line_width("abc") == 3, "single plain line")
    _check(
        cli._accounts_popup_max_line_width("short\nlongest line here\nmid") == 17,
        "widest of several plain lines wins",
    )
    ansi_line = f"{render._ANSI_GREEN}12:34 pm{render._ANSI_RESET}"
    _check(
        cli._accounts_popup_max_line_width(ansi_line) == len("12:34 pm"),
        f"ANSI escapes must not inflate the measured width: {ansi_line!r}",
    )
    _check(
        cli._accounts_popup_max_line_width(f"{ansi_line}\nplain-but-longer-line") == len("plain-but-longer-line"),
        "a plain line can still win over a shorter ANSI-decorated one",
    )


def _test_cli_hook_freshness() -> None:
    _check(cli.hook_freshness([], 1000.0) == "none", "no panes -> none")
    _check(cli.hook_freshness([0.0], 1000.0) == "none", "zero timestamps -> none")
    _check(cli.hook_freshness([900.0], 1000.0) == "fresh", "recent -> fresh")
    _check(cli.hook_freshness([100.0], 10000.0) == "stale", "old -> stale")
    _check(cli.hook_freshness([100.0, 9990.0], 10000.0) == "fresh", "newest wins")
    _check(cli.hook_freshness([100.0], 1000.0, stale_after=899.0) == "stale", "custom window")


# ---------------------------------------------------------------------------
# Sub-agent tab-icon overlay tests (cc-tmux-subagent-tab-icon, task 4.1)
# ---------------------------------------------------------------------------

def _test_tmux_subagent_fg_increment_decrement() -> None:
    """@cc-subagent-fg increment/decrement pair, including the floored-at-0
    stray-stop case (a stop with no matching start must not go negative)."""
    state = {"fg": ""}

    def fake_run(args, *, check_available: bool = True):
        if args and args[0] == "show-options" and tmux.OPT_SUBAGENT_FG in args:
            return state["fg"]
        if args and args[0] == "set-option" and tmux.OPT_SUBAGENT_FG in args:
            state["fg"] = args[-1]
            return ""
        return ""

    saved = tmux._run_tmux
    tmux._run_tmux = fake_run  # type: ignore[assignment]
    try:
        _check(tmux.get_subagent_fg("%1") == 0, "unset @cc-subagent-fg reads as 0")
        _check(tmux.increment_subagent_fg("%1") == 1, "first increment -> 1")
        _check(tmux.increment_subagent_fg("%1") == 2, "second concurrent increment -> 2")
        _check(tmux.decrement_subagent_fg("%1") == 1, "decrement -> 1")
        _check(tmux.decrement_subagent_fg("%1") == 0, "decrement to 0")
        # Stray stop with no matching start (hook-ordering race): must not go negative.
        _check(tmux.decrement_subagent_fg("%1") == 0, "floored-at-0 stray-stop stays 0")
        _check(tmux.decrement_subagent_fg("%1") == 0, "repeated stray-stop still stays 0")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_tmux_subagent_bg_append() -> None:
    """@cc-subagent-bg read-modify-write append (task 2.2)."""
    state = {"bg": ""}

    def fake_run(args, *, check_available: bool = True):
        if args and args[0] == "show-options" and tmux.OPT_SUBAGENT_BG in args:
            return state["bg"]
        if args and args[0] == "set-option" and tmux.OPT_SUBAGENT_BG in args:
            state["bg"] = args[-1]
            return ""
        return ""

    saved = tmux._run_tmux
    tmux._run_tmux = fake_run  # type: ignore[assignment]
    try:
        _check(tmux.get_subagent_bg("%1") == [], "unset @cc-subagent-bg reads as []")
        tmux.append_subagent_bg("%1", 100.0)
        _check(tmux.get_subagent_bg("%1") == [100.0], "first append")
        tmux.append_subagent_bg("%1", 200.0)
        _check(tmux.get_subagent_bg("%1") == [100.0, 200.0], "second append preserves the first")
        # Corrupt/garbage option value -> fail-open to [].
        state["bg"] = "not json"
        _check(tmux.get_subagent_bg("%1") == [], "corrupt JSON -> [] (fail-open)")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_cli_prune_background_entries() -> None:
    """prune_background_entries: fresh kept, stale pruned, mixed, and empty input."""
    now = 1000.0
    timeout = 300.0
    _check(cli.prune_background_entries([], now, timeout) == [], "empty input -> empty output")
    _check(cli.prune_background_entries([now - 1], now, timeout) == [now - 1], "fresh entry kept")
    _check(cli.prune_background_entries([now - 300], now, timeout) == [now - 300], "exactly-at-timeout kept")
    _check(cli.prune_background_entries([now - 301], now, timeout) == [], "just-past-timeout pruned")
    _check(cli.prune_background_entries([now - 1000], now, timeout) == [], "far-stale entry pruned")
    mixed = [now - 10, now - 299, now - 300, now - 301, now - 1000]
    _check(
        cli.prune_background_entries(mixed, now, timeout) == [now - 10, now - 299, now - 300],
        "mixed fresh+stale -> only fresh survive",
    )


def _test_render_resolve_tab_icon() -> None:
    """Resolved glyph mapping (tasks.md task 1.1): fg=1 -> hollow ring
    regardless of bg; fg=2+ -> filled circle regardless of bg; fg=0 with
    bg=1/2+ -> hollow/filled diamond; fg=0 and bg=0 (or fully pruned) falls
    through to the existing state-based animated_icon result."""
    _check(render.resolve_tab_icon("idle", 0.0, 1, 0) == render.SUBAGENT_FG_1, "fg=1 -> hollow ring")
    _check(render.resolve_tab_icon("idle", 0.0, 2, 0) == render.SUBAGENT_FG_2PLUS, "fg=2 -> filled circle")
    _check(render.resolve_tab_icon("idle", 0.0, 5, 0) == render.SUBAGENT_FG_2PLUS, "fg=5 -> filled circle")
    # Foreground takes precedence over background whenever fg is nonzero.
    _check(render.resolve_tab_icon("idle", 0.0, 1, 9) == render.SUBAGENT_FG_1, "fg=1 wins over any bg count")
    _check(render.resolve_tab_icon("idle", 0.0, 2, 9) == render.SUBAGENT_FG_2PLUS, "fg=2+ wins over any bg count")
    # fg=0 falls through to the background heuristic.
    _check(render.resolve_tab_icon("idle", 0.0, 0, 1) == render.SUBAGENT_BG_1, "fg=0,bg=1 -> hollow diamond")
    _check(render.resolve_tab_icon("idle", 0.0, 0, 2) == render.SUBAGENT_BG_2PLUS, "fg=0,bg=2 -> filled diamond")
    _check(render.resolve_tab_icon("idle", 0.0, 0, 7) == render.SUBAGENT_BG_2PLUS, "fg=0,bg=7 -> filled diamond")
    # Neither active (or bg fully pruned by the caller before this call) -> falls
    # through to the plain state-based animated_icon result, not a subagent glyph.
    _check(render.resolve_tab_icon("idle", 0.0, 0, 0) == render.IDLE_GLYPH, "fg=0,bg=0 -> idle glyph (fallthrough)")
    _check(
        render.resolve_tab_icon("waiting", 0.0, 0, 0) == render.SHADE_FRAMES[0],
        "fg=0,bg=0 -> waiting animation frame preserved",
    )
    _check(
        render.resolve_tab_icon("active", 2.0, 0, 0) == render.BLOCK_FRAMES[2],
        "fg=0,bg=0 -> active animation frame preserved",
    )
    _check(render.resolve_tab_icon("", 0.0, 0, 0) == "", "no tracked pane, no subagents -> ''")


def _test_cli_register_subagent_start_stop_branching() -> None:
    """cmd_register --subagent-start/--subagent-stop wiring (task 2.1/2.2): a
    foreground start increments @cc-subagent-fg; a background start
    (tool_input.run_in_background: true) appends to @cc-subagent-bg instead;
    any stop unconditionally decrements @cc-subagent-fg (floored at 0) since
    only a foreground start ever incremented it in the first place — see
    cmd_register's own docstring note on the resolved stop-side ambiguity."""
    calls: List[str] = []

    def fake_increment(pane_id):
        calls.append(f"inc:{pane_id}")
        return 1

    def fake_decrement(pane_id):
        calls.append(f"dec:{pane_id}")
        return 0

    def fake_append(pane_id, ts):
        calls.append(f"append:{pane_id}")

    saved_inc = tmux.increment_subagent_fg
    saved_dec = tmux.decrement_subagent_fg
    saved_append = tmux.append_subagent_bg
    saved_set_pane_state = tmux.set_pane_state
    saved_notify_react = cli.notify.react
    saved_maybe_rename = cli._maybe_rename_window
    saved_trace = cli._trace_register
    saved_read_stdin = cli._read_hook_stdin
    tmux.increment_subagent_fg = fake_increment  # type: ignore[assignment]
    tmux.decrement_subagent_fg = fake_decrement  # type: ignore[assignment]
    tmux.append_subagent_bg = fake_append  # type: ignore[assignment]
    tmux.set_pane_state = lambda *a, **k: False  # type: ignore[assignment]
    cli.notify.react = lambda *a, **k: None  # type: ignore[assignment]
    cli._maybe_rename_window = lambda pane: False  # type: ignore[assignment]
    cli._trace_register = lambda *a, **k: None  # type: ignore[assignment]
    try:
        args_start = argparse.Namespace(
            state="active", task=None, reason=None, pane="%1",
            subagent_start=True, subagent_stop=False,
        )

        cli._read_hook_stdin = lambda: {  # type: ignore[assignment]
            "hook_event_name": "PreToolUse", "tool_input": {},
        }
        cli.cmd_register(args_start)
        _check("inc:%1" in calls, "foreground start must increment fg")
        _check(not any(c.startswith("append:") for c in calls), "foreground start must not touch bg")

        calls.clear()
        cli._read_hook_stdin = lambda: {  # type: ignore[assignment]
            "hook_event_name": "PreToolUse",
            "tool_input": {"run_in_background": True},
        }
        cli.cmd_register(args_start)
        _check("append:%1" in calls, "background start must append to bg")
        _check(not any(c.startswith("inc:") for c in calls), "background start must not increment fg")

        calls.clear()
        args_stop = argparse.Namespace(
            state="active", task=None, reason=None, pane="%1",
            subagent_start=False, subagent_stop=True,
        )
        cli._read_hook_stdin = lambda: {"hook_event_name": "PostToolUse"}  # type: ignore[assignment]
        cli.cmd_register(args_stop)
        _check("dec:%1" in calls, "stop must unconditionally decrement fg")
        _check(not any(c.startswith("append:") for c in calls), "stop must never append to bg")
    finally:
        tmux.increment_subagent_fg = saved_inc  # type: ignore[assignment]
        tmux.decrement_subagent_fg = saved_dec  # type: ignore[assignment]
        tmux.append_subagent_bg = saved_append  # type: ignore[assignment]
        tmux.set_pane_state = saved_set_pane_state  # type: ignore[assignment]
        cli.notify.react = saved_notify_react  # type: ignore[assignment]
        cli._maybe_rename_window = saved_maybe_rename  # type: ignore[assignment]
        cli._trace_register = saved_trace  # type: ignore[assignment]
        cli._read_hook_stdin = saved_read_stdin  # type: ignore[assignment]


# ---------------------------------------------------------------------------
# nx_agent.py caching tests (cache hit skips HTTP, miss fetches + writes,
# failure negatively caches — cc-tmux-adopt-nx-context-and-git-status task 4.1).
# Mirrors _test_usage_active_usage_ttl's shape: usage._query is monkeypatched to
# count / control the fetch, cache_path + now injected so no real network or
# filesystem clock is touched.
# ---------------------------------------------------------------------------

def _test_nx_agent_session_context_cache() -> None:
    calls = {"n": 0}
    saved_query = usage._query
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-nx-ctx-test-")
    path = os.path.join(tmpdir, "ctx.json")
    payload = {"usedPercentage": 42.0, "contextWindowSize": 200000, "sessionId": "s1"}
    try:
        # --- cache HIT: a fresh, well-formed cache file skips the HTTP fetch ---
        with open(path, "w", encoding="utf-8") as f:
            json.dump(payload, f)

        def boom(url=None, timeout=None):
            raise AssertionError("fetch must not run on a fresh cache hit")

        usage._query = boom  # type: ignore[assignment]
        hit = nx_agent.session_context("s1", ttl=45.0, cache_path=path, now=time.time())
        _check(hit == payload, f"fresh cache hit returns cached dict unchanged: {hit!r}")

        # --- cache MISS: live fetch, and the result is written to the cache file ---
        os.unlink(path)

        def counting_query(url=None, timeout=None):
            calls["n"] += 1
            return payload

        usage._query = counting_query  # type: ignore[assignment]
        miss = nx_agent.session_context("s1", ttl=45.0, cache_path=path, now=time.time())
        _check(miss == payload, f"miss fetches the payload: {miss!r}")
        _check(calls["n"] == 1, "miss hit the network exactly once")
        with open(path, "r", encoding="utf-8") as f:
            _check(json.load(f) == payload, "fetched payload written back to the cache file")

        # fresh cache now suppresses a second fetch
        second = nx_agent.session_context("s1", ttl=45.0, cache_path=path, now=time.time())
        _check(second == payload and calls["n"] == 1, "fresh cache -> NO second fetch")

        # --- fetch FAILURE: None returned AND negatively cached (no refetch in TTL) ---
        os.unlink(path)
        calls["n"] = 0

        def failing_query(url=None, timeout=None):
            calls["n"] += 1
            return None

        usage._query = failing_query  # type: ignore[assignment]
        down = nx_agent.session_context("s1", ttl=45.0, cache_path=path, now=time.time())
        _check(down is None, "fetch failure -> None")
        _check(calls["n"] == 1, "failure fetched exactly once")
        _check(os.path.exists(path), "negative result cached to disk")
        down2 = nx_agent.session_context("s1", ttl=45.0, cache_path=path, now=time.time())
        _check(down2 is None and calls["n"] == 1, "negative cache served without refetch")

        # empty session_id short-circuits to None, never touches the network
        usage._query = boom  # type: ignore[assignment]
        _check(nx_agent.session_context("", cache_path=path) is None, "empty session_id -> None, no fetch")
    finally:
        usage._query = saved_query  # type: ignore[assignment]
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_nx_agent_project_git_status_cache() -> None:
    calls = {"n": 0}
    saved_query = usage._query
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-nx-status-test-")
    path = os.path.join(tmpdir, "status.json")
    git_obj = {"branch": "main", "dirty": {"modified": 1, "untracked": 2}, "detached": False}
    payload = {"code": "if", "git": git_obj}
    try:
        # --- cache HIT: fresh full-status cache -> git sub-object, no fetch ---
        with open(path, "w", encoding="utf-8") as f:
            json.dump(payload, f)

        def boom(url=None, timeout=None):
            raise AssertionError("fetch must not run on a fresh cache hit")

        usage._query = boom  # type: ignore[assignment]
        hit = nx_agent.project_git_status("if", ttl=45.0, cache_path=path, now=time.time())
        _check(hit == git_obj, f"fresh cache hit returns the git sub-object: {hit!r}")

        # --- cache MISS: fetch full /status dict, write it, extract git ---
        os.unlink(path)

        def counting_query(url=None, timeout=None):
            calls["n"] += 1
            return payload

        usage._query = counting_query  # type: ignore[assignment]
        miss = nx_agent.project_git_status("if", ttl=45.0, cache_path=path, now=time.time())
        _check(miss == git_obj and calls["n"] == 1, f"miss fetches + extracts git: {miss!r}")
        with open(path, "r", encoding="utf-8") as f:
            _check(json.load(f) == payload, "FULL /status dict cached (not just the git sub-object)")

        # --- response present but WITHOUT a git field -> None, full dict still cached ---
        os.unlink(path)
        calls["n"] = 0

        def nogit_query(url=None, timeout=None):
            calls["n"] += 1
            return {"code": "if"}

        usage._query = nogit_query  # type: ignore[assignment]
        nogit = nx_agent.project_git_status("if", ttl=45.0, cache_path=path, now=time.time())
        _check(nogit is None, "response without a git field -> None")
        _check(calls["n"] == 1, "no-git response fetched once")
        nogit2 = nx_agent.project_git_status("if", ttl=45.0, cache_path=path, now=time.time())
        _check(nogit2 is None and calls["n"] == 1, "cached no-git dict served without refetch")

        # --- unreachable / malformed -> None, negatively cached ---
        os.unlink(path)
        calls["n"] = 0

        def failing_query(url=None, timeout=None):
            calls["n"] += 1
            return None

        usage._query = failing_query  # type: ignore[assignment]
        down = nx_agent.project_git_status("if", ttl=45.0, cache_path=path, now=time.time())
        _check(down is None and calls["n"] == 1, "unreachable -> None, fetched once")
        down2 = nx_agent.project_git_status("if", ttl=45.0, cache_path=path, now=time.time())
        _check(down2 is None and calls["n"] == 1, "negative cache served without refetch")

        # empty code short-circuits to None, never touches the network
        usage._query = boom  # type: ignore[assignment]
        _check(nx_agent.project_git_status("", cache_path=path) is None, "empty code -> None, no fetch")
    finally:
        usage._query = saved_query  # type: ignore[assignment]
        shutil.rmtree(tmpdir, ignore_errors=True)


# ---------------------------------------------------------------------------
# tmux._git_status against real throwaway git repos (cc-tmux-git-status-glyphs
# task 4.1, replacing the prior spec's tmux._git_dirty/_git_ahead fixture —
# those two functions no longer exist, merged into one _git_status parse of
# `git status --porcelain=v2 --branch`). A real fixture repo is simpler + more
# realistic than mocking _run_git's porcelain stdout by hand.
# ---------------------------------------------------------------------------

def _init_git_status_fixture(tmpdir: str) -> Callable[..., None]:
    """Init a throwaway repo at tmpdir with one committed ``tracked.txt``.

    Returns a ``git(*args)`` runner bound to ``tmpdir`` so callers can keep
    building on the same fixture without repeating the subprocess plumbing.
    """
    def git(*args: str) -> None:
        subprocess.run(
            ["git", "-C", tmpdir, *args],
            check=True,
            capture_output=True,
            text=True,
        )

    git("init", "-q")
    git("config", "user.email", "self-test@cc-tmux.local")
    git("config", "user.name", "cc-tmux self-test")
    git("config", "commit.gpgsign", "false")
    with open(os.path.join(tmpdir, "tracked.txt"), "w", encoding="utf-8") as f:
        f.write("hello\n")
    git("add", "tracked.txt")
    git("commit", "-q", "-m", "initial")
    return git


def _test_tmux_git_status_clean_no_upstream() -> None:
    if shutil.which("git") is None:
        return  # no git binary -> nothing to exercise; _git_status already fails open to None

    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-git-status-clean-")
    try:
        _init_git_status_fixture(tmpdir)
        status = tmux._git_status(tmpdir)
        _check(status is not None, "clean tree with git available must not return None")
        _check(
            status == tmux.GitStatusCounts(),
            f"clean tree -> all-zero GitStatusCounts, got {status!r}",
        )
        _check(
            status.ahead == 0 and status.behind == 0,
            f"no upstream configured -> ahead/behind both 0, got {status!r}",
        )
    finally:
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_tmux_git_status_dirty_counts() -> None:
    if shutil.which("git") is None:
        return

    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-git-status-dirty-")
    try:
        git = _init_git_status_fixture(tmpdir)

        # a second committed file, to be unstaged-deleted below
        with open(os.path.join(tmpdir, "deleted.txt"), "w", encoding="utf-8") as f:
            f.write("bye\n")
        git("add", "deleted.txt")
        git("commit", "-q", "-m", "add deleted.txt")

        # 1 staged-modified file
        with open(os.path.join(tmpdir, "tracked.txt"), "w", encoding="utf-8") as f:
            f.write("changed\n")
        git("add", "tracked.txt")

        # 1 unstaged-deleted file
        os.remove(os.path.join(tmpdir, "deleted.txt"))

        # 1 untracked file
        with open(os.path.join(tmpdir, "new.txt"), "w", encoding="utf-8") as f:
            f.write("new\n")

        status = tmux._git_status(tmpdir)
        _check(status is not None, "dirty tree must not return None")
        _check(status.modified == 1, f"1 staged-modified file -> modified=1, got {status!r}")
        _check(status.deleted == 1, f"1 unstaged-deleted file -> deleted=1, got {status!r}")
        _check(status.untracked == 1, f"1 untracked file -> untracked=1, got {status!r}")
        _check(status.renamed == 0, f"no rename in this fixture -> renamed=0, got {status!r}")
    finally:
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_tmux_git_status_renamed() -> None:
    if shutil.which("git") is None:
        return

    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-git-status-renamed-")
    try:
        git = _init_git_status_fixture(tmpdir)
        with open(os.path.join(tmpdir, "orig.txt"), "w", encoding="utf-8") as f:
            f.write("stable content for rename detection\n")
        git("add", "orig.txt")
        git("commit", "-q", "-m", "add orig.txt")

        # `git mv` stages the rename directly — no extra `add` needed for
        # porcelain=v2 to report it as a "2 <XY> ..." rename/copy entry.
        git("mv", "orig.txt", "renamed.txt")

        status = tmux._git_status(tmpdir)
        _check(status is not None, "renamed tree must not return None")
        _check(status.renamed == 1, f"staged rename -> renamed=1, got {status!r}")
    finally:
        shutil.rmtree(tmpdir, ignore_errors=True)


def _test_tmux_git_status_ahead() -> None:
    if shutil.which("git") is None:
        return

    remote_dir = tempfile.mkdtemp(prefix="cc-tmux-git-status-remote-")
    work_dir = tempfile.mkdtemp(prefix="cc-tmux-git-status-ahead-")
    try:
        subprocess.run(
            ["git", "init", "-q", "--bare", remote_dir], check=True, capture_output=True, text=True,
        )
        git = _init_git_status_fixture(work_dir)
        git("remote", "add", "origin", remote_dir)
        branch = subprocess.run(
            ["git", "-C", work_dir, "rev-parse", "--abbrev-ref", "HEAD"],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        git("push", "-q", "-u", "origin", f"{branch}:{branch}")

        # one more local commit, never pushed -> local HEAD is ahead of upstream
        with open(os.path.join(work_dir, "tracked.txt"), "w", encoding="utf-8") as f:
            f.write("ahead commit\n")
        git("add", "tracked.txt")
        git("commit", "-q", "-m", "ahead commit")

        status = tmux._git_status(work_dir)
        _check(status is not None, "ahead-of-upstream tree must not return None")
        _check(status.ahead == 1, f"one unpushed commit -> ahead=1, got {status!r}")
        _check(status.behind == 0, f"nothing on the remote we lack -> behind=0, got {status!r}")
    finally:
        shutil.rmtree(remote_dir, ignore_errors=True)
        shutil.rmtree(work_dir, ignore_errors=True)


def _test_tmux_git_status_behind() -> None:
    if shutil.which("git") is None:
        return

    remote_dir = tempfile.mkdtemp(prefix="cc-tmux-git-status-remote-")
    work_dir = tempfile.mkdtemp(prefix="cc-tmux-git-status-behind-")
    other_dir = tempfile.mkdtemp(prefix="cc-tmux-git-status-other-")
    try:
        subprocess.run(
            ["git", "init", "-q", "--bare", remote_dir], check=True, capture_output=True, text=True,
        )
        git = _init_git_status_fixture(work_dir)
        git("remote", "add", "origin", remote_dir)
        branch = subprocess.run(
            ["git", "-C", work_dir, "rev-parse", "--abbrev-ref", "HEAD"],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        git("push", "-q", "-u", "origin", f"{branch}:{branch}")

        # A second clone pushes a commit work_dir hasn't seen yet.
        subprocess.run(
            ["git", "clone", "-q", remote_dir, other_dir], check=True, capture_output=True, text=True,
        )

        def other_git(*args: str) -> None:
            subprocess.run(["git", "-C", other_dir, *args], check=True, capture_output=True, text=True)

        other_git("config", "user.email", "self-test@cc-tmux.local")
        other_git("config", "user.name", "cc-tmux self-test")
        other_git("config", "commit.gpgsign", "false")
        with open(os.path.join(other_dir, "tracked.txt"), "w", encoding="utf-8") as f:
            f.write("upstream moved on\n")
        other_git("add", "tracked.txt")
        other_git("commit", "-q", "-m", "upstream-only commit")
        other_git("push", "-q", "origin", f"{branch}:{branch}")

        # work_dir fetches the remote-tracking ref WITHOUT merging it into
        # HEAD, so @{upstream} now points past local HEAD -> behind, not ahead.
        git("fetch", "-q", "origin")

        status = tmux._git_status(work_dir)
        _check(status is not None, "behind-upstream tree must not return None")
        _check(status.behind == 1, f"one un-merged upstream commit -> behind=1, got {status!r}")
        _check(status.ahead == 0, f"no local-only commits -> ahead=0, got {status!r}")
    finally:
        shutil.rmtree(remote_dir, ignore_errors=True)
        shutil.rmtree(work_dir, ignore_errors=True)
        shutil.rmtree(other_dir, ignore_errors=True)


def _test_tmux_git_status_failure() -> None:
    # Nonexistent directory -> `git -C <dir>` exits non-zero -> _run_git returns
    # None -> _git_status returns None (works even without a git binary present,
    # since _run_git's own shutil.which guard fails open the same way).
    _check(
        tmux._git_status("/nonexistent/cc-tmux-self-test-path-zzz") is None,
        "nonexistent directory -> _git_status returns None",
    )

    # Explicit _run_git failure (monkeypatched) -> None regardless of cwd.
    saved_run_git = tmux._run_git
    tmux._run_git = lambda cwd, args: None  # type: ignore[assignment]
    try:
        _check(tmux._git_status(".") is None, "_run_git failure -> _git_status returns None")
    finally:
        tmux._run_git = saved_run_git  # type: ignore[assignment]


# ---------------------------------------------------------------------------
# cmd_register session_id capture + _build_session_bar dual-source composition
# (cc-tmux-adopt-nx-context-and-git-status task 4.2). Mirrors
# _test_cli_register_subagent_start_stop_branching's monkeypatch style for the
# cmd_register path, and the module-level nx_agent monkeypatch + fake
# get_pane_option store the row-2 tests already use for the session-bar path.
# ---------------------------------------------------------------------------

def _test_cli_register_captures_session_id() -> None:
    """cmd_register captures session_id -> @cc-session-id on SessionStart only
    (task 1.4). A non-SessionStart event with a session_id leaves the option
    untouched; a SessionStart with no session_id writes nothing for it. Mirrors
    the SessionStart-scoped session_title capture on the same code path."""
    set_opts: List[Tuple[str, str]] = []

    saved_set_pane_state = tmux.set_pane_state
    saved_set_opt = tmux._set_opt
    saved_set_pane_title = tmux.set_pane_title
    saved_notify_react = cli.notify.react
    saved_maybe_rename = cli._maybe_rename_window
    saved_trace = cli._trace_register
    saved_read_stdin = cli._read_hook_stdin
    tmux.set_pane_state = lambda *a, **k: False  # type: ignore[assignment]
    tmux._set_opt = lambda pane, opt, val: set_opts.append((opt, val))  # type: ignore[assignment]
    tmux.set_pane_title = lambda *a, **k: None  # type: ignore[assignment]
    cli.notify.react = lambda *a, **k: None  # type: ignore[assignment]
    cli._maybe_rename_window = lambda pane: False  # type: ignore[assignment]
    cli._trace_register = lambda *a, **k: None  # type: ignore[assignment]
    try:
        args = argparse.Namespace(
            state="idle", task=None, reason=None, pane="%1",
            subagent_start=False, subagent_stop=False,
        )

        # SessionStart + session_id -> @cc-session-id set to that value.
        cli._read_hook_stdin = lambda: {  # type: ignore[assignment]
            "hook_event_name": "SessionStart", "session_id": "abc123",
        }
        set_opts.clear()
        cli.cmd_register(args)
        _check(
            (tmux.OPT_SESSION_ID, "abc123") in set_opts,
            f"SessionStart session_id must set @cc-session-id: {set_opts!r}",
        )

        # Non-SessionStart event carrying a session_id -> @cc-session-id untouched.
        cli._read_hook_stdin = lambda: {  # type: ignore[assignment]
            "hook_event_name": "UserPromptSubmit", "session_id": "abc123",
        }
        set_opts.clear()
        cli.cmd_register(args)
        _check(
            not any(opt == tmux.OPT_SESSION_ID for opt, _ in set_opts),
            f"non-SessionStart event must NOT set @cc-session-id: {set_opts!r}",
        )

        # SessionStart with session_id absent -> no @cc-session-id write, no crash.
        cli._read_hook_stdin = lambda: {"hook_event_name": "SessionStart"}  # type: ignore[assignment]
        set_opts.clear()
        cli.cmd_register(args)
        _check(
            not any(opt == tmux.OPT_SESSION_ID for opt, _ in set_opts),
            f"SessionStart without session_id must not write @cc-session-id: {set_opts!r}",
        )
    finally:
        tmux.set_pane_state = saved_set_pane_state  # type: ignore[assignment]
        tmux._set_opt = saved_set_opt  # type: ignore[assignment]
        tmux.set_pane_title = saved_set_pane_title  # type: ignore[assignment]
        cli.notify.react = saved_notify_react  # type: ignore[assignment]
        cli._maybe_rename_window = saved_maybe_rename  # type: ignore[assignment]
        cli._trace_register = saved_trace  # type: ignore[assignment]
        cli._read_hook_stdin = saved_read_stdin  # type: ignore[assignment]


def _test_cli_resolve_git_status_dual_source() -> None:
    """_resolve_git_status (task 2.1) resolves branch + all six GitStatusCounts
    fields INDEPENDENTLY: nx's value wins per-field only when nx's response
    actually carries that key (presence-gated via _nx_field's _MISSING
    sentinel, not truthiness), else the corresponding field falls back to the
    local ``@cc-git-status`` JSON blob (decoded by _local_git_status).

    Local fallback values are set DIFFERENT from the nx values throughout so a
    wrong-source bug is visible in the resolved counts (mirrors the retired
    cli.build_session_bar_dual_source test's technique). get_pane_option is a
    fake per-option store; tmux._run_tmux / registry.resolve_project_code /
    nx_agent.project_git_status are monkeypatched at the module level
    (project_git_status is the one _resolve_git_status consults)."""
    local_status_json = json.dumps({
        "modified": 99, "untracked": 98, "deleted": 2,
        "renamed": 1, "ahead": 4, "behind": 1,
    })
    store = {
        tmux.OPT_BRANCH: "local-branch",       # nx value differs -> proves source
        tmux.OPT_GIT_STATUS: local_status_json,
    }
    local_expected = tmux.GitStatusCounts(modified=99, untracked=98, deleted=2, renamed=1, ahead=4, behind=1)

    saved_project_git_status = nx_agent.project_git_status
    saved_get_pane_option = tmux.get_pane_option
    saved_run_tmux = tmux._run_tmux
    saved_resolve_code = registry.resolve_project_code
    tmux.get_pane_option = lambda pane, opt: store.get(opt, "")  # type: ignore[assignment]
    tmux._run_tmux = lambda args, *, check_available=True: "/some/cwd"  # type: ignore[assignment]
    registry.resolve_project_code = lambda cwd: "if"  # type: ignore[assignment]
    try:
        # --- nx carries ONLY modified/untracked (nested under `dirty`) ---
        # deleted/renamed/ahead/behind have no nx source -> fall back to local.
        nx_agent.project_git_status = lambda *a, **k: {  # type: ignore[assignment]
            "branch": "nx-branch", "dirty": {"modified": 3, "untracked": 1},
        }
        branch, counts = cli._resolve_git_status("%1")
        _check(branch == "nx-branch", f"partial nx -> nx branch used: {branch!r}")
        expected_partial = tmux.GitStatusCounts(modified=3, untracked=1, deleted=2, renamed=1, ahead=4, behind=1)
        _check(counts == expected_partial, f"partial nx -> modified/untracked from nx, rest from local: {counts!r}")

        # --- nx UNREACHABLE (returns None) -> branch + all six fields fall to local ---
        nx_agent.project_git_status = lambda *a, **k: None  # type: ignore[assignment]
        branch, counts = cli._resolve_git_status("%1")
        _check(branch == "local-branch", f"nx unreachable -> local branch used: {branch!r}")
        _check(counts == local_expected, f"nx unreachable -> all six fields from local: {counts!r}")

        # --- SIMULATED future nx response carrying all six keys (branch +
        # dirty.{modified,untracked} + top-level deleted/renamed/ahead/behind)
        # -> all six prefer nx, proving the forward-compatible per-field rule
        # needs no future code change. ---
        nx_agent.project_git_status = lambda *a, **k: {  # type: ignore[assignment]
            "branch": "nx-branch-full",
            "dirty": {"modified": 10, "untracked": 11},
            "deleted": 12, "renamed": 13, "ahead": 14, "behind": 15,
        }
        branch, counts = cli._resolve_git_status("%1")
        _check(branch == "nx-branch-full", f"full nx -> nx branch used: {branch!r}")
        expected_full = tmux.GitStatusCounts(modified=10, untracked=11, deleted=12, renamed=13, ahead=14, behind=15)
        _check(counts == expected_full, f"full nx -> all six fields from nx: {counts!r}")

        # --- A legitimate nx `0` still wins over a nonzero local value
        # (presence-check, not truthiness — the _MISSING sentinel's job). ---
        nx_agent.project_git_status = lambda *a, **k: {  # type: ignore[assignment]
            "branch": "nx-branch-zero", "dirty": {"modified": 0, "untracked": 1},
        }
        branch, counts = cli._resolve_git_status("%1")
        _check(counts.modified == 0, f"nx modified=0 wins over local modified=99 (presence, not truthiness): {counts!r}")
        _check(counts.untracked == 1, f"nx untracked=1 still used alongside the 0 field: {counts!r}")
    finally:
        nx_agent.project_git_status = saved_project_git_status  # type: ignore[assignment]
        tmux.get_pane_option = saved_get_pane_option  # type: ignore[assignment]
        tmux._run_tmux = saved_run_tmux  # type: ignore[assignment]
        registry.resolve_project_code = saved_resolve_code  # type: ignore[assignment]


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

_TESTS: List[Tuple[str, Callable[[], None]]] = [
    ("priority.constants", _test_priority_constants),
    ("priority.sort_order", _test_sort_order),
    ("priority.group_by_state", _test_group_by_state),
    ("priority.cycle_priority_mode", _test_cycle_priority_mode),
    ("priority.cycle_flat_mode", _test_cycle_flat_mode),
    ("priority.cycle_bad_mode_fallback", _test_cycle_bad_mode_falls_back),
    ("priority.recency_tiebreak", _test_recency_tiebreak_within_group),
    ("priority.group_order_unchanged_by_visits", _test_group_order_unchanged_by_visits),
    ("priority.missing_visited_fallback", _test_missing_visited_timestamp_fallback),
    ("tmux.reconcile_rate_limit", _test_reconcile_rate_limit),
    ("priority.select_next", _test_select_next),
    ("tmux.is_real_transition", _test_is_real_transition_pure),
    ("tmux.set_pane_state_change", _test_set_pane_state_returns_change),
    ("tmux.set_pane_state_hot_path", _test_set_pane_state_hot_path_skips_git),
    ("tmux.set_pane_state_unknown", _test_set_pane_state_unknown_state),
    ("tmux.set_pane_state_writes", _test_set_pane_state_writes_state_and_timestamp),
    ("tmux.set_pane_git_identity_unsets_branch", _test_tmux_set_pane_git_identity_unsets_branch),
    ("tmux.set_pane_state_reassert_ts", _test_set_pane_state_reassert_skips_timestamp),
    ("registry.resolve_project_code", _test_registry_resolve_project_code),
    ("registry.resolve_project_code_symlink_alias", _test_registry_resolve_project_code_symlink_alias),
    ("cli.compose_title_name", _test_compose_title_name),
    ("cli.maybe_rename_window_success_failure", _test_maybe_rename_window_success_failure),
    ("cli.trace_register_rename_succeeded_field", _test_trace_register_rename_succeeded_field),
    ("render.format_duration", _test_render_format_duration),
    ("render.resolve_icons", _test_render_resolve_icons),
    ("render.animated_icon", _test_render_animated_icon),
    ("render.idle_meter_ramp_sweep", _test_render_idle_meter_ramp_sweep),
    ("render.idle_meter_index0_flash", _test_render_idle_meter_index0_flash),
    ("render.idle_meter_none_fallback", _test_render_idle_meter_none_fallback),
    ("render.idle_meter_color_matches_resolve_context_color", _test_render_idle_meter_color_matches_resolve_context_color),
    ("render.resolve_tab_glyph_precedence", _test_render_resolve_tab_glyph_precedence),
    ("render.tabs_row_idle_meter_wiring", _test_render_render_tabs_row_idle_meter_wiring),
    ("render.inbox_rows", _test_render_inbox_rows),
    ("usage.color_thresholds", _test_usage_color_thresholds),
    ("usage.pct_formatting", _test_usage_pct_formatting),
    ("usage.extract_util", _test_usage_extract_util),
    ("usage.account_label", _test_usage_account_label),
    ("usage.extract_active", _test_usage_extract_active),
    ("usage.extract_reset_at", _test_usage_extract_reset_at),
    ("usage.dedupe_credentials", _test_usage_dedupe_credentials),
    ("render.accounts_popup", _test_render_accounts_popup),
    ("render.accounts_popup_reset_lines", _test_render_accounts_popup_reset_lines),
    ("render.context_bar_colors", _test_context_bar_colors),
    ("render.apply_metric_dots", _test_apply_metric_dots),
    ("render.usage_glyph", _test_render_usage_glyph),
    ("render.usage_glyph_2metric", _test_render_usage_glyph_2metric),
    ("render.format_context_tokens", _test_format_context_tokens),
    ("usage.account_identity", _test_account_identity),
    ("cli.resolve_ses_tokens", _test_cli_resolve_ses_tokens),
    ("accounts_popup.click_dismiss_wiring", _test_accounts_popup_click_dismiss_wiring),
    ("accounts_popup.no_session_state", _test_cli_accounts_popup_no_session_state),
    ("usage.active_usage_ttl", _test_usage_active_usage_ttl),
    ("tmux.get_window_top_pane", _test_tmux_get_window_top_pane),
    ("tmux.get_window_active_pane", _test_tmux_get_window_active_pane),
    ("tmux.get_window_tabs", _test_tmux_get_window_tabs),
    ("render.session_bar", _test_render_session_bar),
    ("render.beads_bar", _test_render_beads_bar),
    ("render.beads_bar_account_segment", _test_render_beads_bar_account_segment),
    ("render.tabs_row", _test_render_tabs_row),
    ("cli.read_roadmap_pulse_fail_open", _test_cli_read_roadmap_pulse_fail_open),
    ("cli.read_roadmap_pulse_radar_strip", _test_cli_read_roadmap_pulse_radar_strip),
    ("cli.parse_roadmap_pulse_counts", _test_cli_parse_roadmap_pulse_counts),
    ("cli.beads_pane_fallback", _test_cli_beads_pane_fallback),
    ("cli.beads_pane_active_tracked_preferred", _test_cli_beads_pane_active_tracked_preferred),
    ("cli.resolve_session_pane_active_tracked", _test_cli_resolve_session_pane_active_tracked),
    ("cli.resolve_session_pane_active_untracked_fallback", _test_cli_resolve_session_pane_active_untracked_fallback),
    ("cli.resolve_session_pane_no_active_fallback", _test_cli_resolve_session_pane_no_active_fallback),
    ("render.session_bar_no_glyph", _test_render_session_bar_no_glyph),
    ("render.session_bar_usage_glyph_wiring", _test_render_session_bar_usage_glyph_wiring),
    ("cli.evaluate_plugin_listing", _test_cli_evaluate_plugin_listing),
    ("cli.evaluate_plugin_listing_degraded", _test_cli_evaluate_plugin_listing_degraded),
    ("cli.evaluate_hook_liveness", _test_cli_evaluate_hook_liveness),
    ("cli.evaluate_hook_liveness_ages", _test_cli_evaluate_hook_liveness_ages),
    ("cli.trace_needs_trim", _test_cli_trace_needs_trim),
    ("cli.accounts_popup_max_line_width", _test_cli_accounts_popup_max_line_width),
    ("cli.hook_freshness", _test_cli_hook_freshness),
    ("conductor.attach_command", _test_conductor_attach_command),
    ("conductor.send_prompt_refusal", _test_conductor_send_prompt_refusal),
    ("conductor.resolve_dir", _test_conductor_resolve_dir),
    ("conductor.worktree_slot", _test_conductor_worktree_slot),
    ("conductor.pane_ready", _test_conductor_pane_ready),
    ("conductor.wait_for_pane_ready", _test_conductor_wait_for_pane_ready),
    ("tmux.subagent_fg_increment_decrement", _test_tmux_subagent_fg_increment_decrement),
    ("tmux.subagent_bg_append", _test_tmux_subagent_bg_append),
    ("cli.prune_background_entries", _test_cli_prune_background_entries),
    ("render.resolve_tab_icon", _test_render_resolve_tab_icon),
    ("cli.register_subagent_start_stop_branching", _test_cli_register_subagent_start_stop_branching),
    ("nx_agent.session_context_cache", _test_nx_agent_session_context_cache),
    ("nx_agent.project_git_status_cache", _test_nx_agent_project_git_status_cache),
    ("tmux.git_status_clean_no_upstream", _test_tmux_git_status_clean_no_upstream),
    ("tmux.git_status_dirty_counts", _test_tmux_git_status_dirty_counts),
    ("tmux.git_status_renamed", _test_tmux_git_status_renamed),
    ("tmux.git_status_ahead", _test_tmux_git_status_ahead),
    ("tmux.git_status_behind", _test_tmux_git_status_behind),
    ("tmux.git_status_failure", _test_tmux_git_status_failure),
    ("cli.register_captures_session_id", _test_cli_register_captures_session_id),
    ("cli.resolve_git_status_dual_source", _test_cli_resolve_git_status_dual_source),
    ("cli.resolve_model_letter", _test_cli_resolve_model_letter),
]


def run_self_test(verbose: bool = False) -> int:
    """Run every test; print a summary; return the failure count (0 = all pass)."""
    passed = 0
    failed: List[Tuple[str, str]] = []
    for name, fn in _TESTS:
        try:
            fn()
        except Exception as exc:  # noqa: BLE001 - report, don't crash the runner
            failed.append((name, str(exc)))
            if verbose:
                print(f"FAIL {name}: {exc}")
        else:
            passed += 1
            if verbose:
                print(f"ok   {name}")

    total = len(_TESTS)
    if failed:
        print(f"cc-tmux self-test: {passed}/{total} passed, {len(failed)} FAILED")
        for name, msg in failed:
            print(f"  FAIL {name}: {msg}")
    else:
        print(f"cc-tmux self-test: {passed}/{total} passed")
    return len(failed)
