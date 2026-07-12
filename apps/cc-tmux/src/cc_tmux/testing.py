"""Built-in pure-function test suite for ``cc-tmux self-test`` (Req-13).

These tests exercise the logic that MUST be correct without a live tmux server:
the priority sort/cycle rules, the ``set_pane_state`` transition-detection
decision (with tmux calls mocked), and path detection. No external test runner —
stdlib only, so the suite runs anywhere ``python3`` does.

Run via ``cc-tmux self-test`` (exit 0 = pass, non-zero = failure count).
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import tempfile
import time
from dataclasses import dataclass
from typing import Callable, List, Optional, Tuple

from . import cli, conductor, paths, priority, registry, render, tmux, usage


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
# paths.py tests
# ---------------------------------------------------------------------------

def _test_tmux_conf_candidates() -> None:
    cands = [str(p) for p in paths.tmux_conf_candidates()]
    _check(any(c.endswith("tmux/tmux.conf") for c in cands), "XDG tmux.conf missing from candidates")
    _check(any(c.endswith(".tmux.conf") for c in cands), "~/.tmux.conf missing from candidates")


def _test_find_tmux_conf_override() -> None:
    saved = os.environ.get("TMUX_CONF")
    with tempfile.NamedTemporaryFile(prefix="cc-tmux-test-", suffix=".conf", delete=False) as tf:
        tmp_path = tf.name
    try:
        os.environ["TMUX_CONF"] = tmp_path
        found = paths.find_tmux_conf()
        _check(found is not None and str(found) == tmp_path, "TMUX_CONF override not honored")
    finally:
        if saved is None:
            os.environ.pop("TMUX_CONF", None)
        else:
            os.environ["TMUX_CONF"] = saved
        os.unlink(tmp_path)


def _test_find_plugin_dir() -> None:
    # Running from the source tree, the plugin dir is derivable and contains src/cc_tmux.
    found = paths.find_plugin_dir()
    _check(found is not None, "plugin dir not found from source tree")
    _check((found / "src" / "cc_tmux").is_dir(), "plugin dir missing src/cc_tmux")


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


def _test_render_status() -> None:
    icons = {"waiting": "W", "idle": "I", "active": "A"}
    fmt = "{waiting:icon} {idle:icon} {active:icon}"
    # zero-count states drop out and whitespace collapses.
    out = render.render_status(fmt, {"waiting": 2, "idle": 0, "active": 1}, icons)
    _check(out == "W 2 A 1", f"status render wrong: {out!r}")
    # all zero -> empty string.
    _check(render.render_status(fmt, {}, icons) == "", "empty counts -> empty status")


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


def _test_tmux_get_window_top_state() -> None:
    saved = tmux._run_tmux
    try:
        def fake_two_panes(args, *, check_available: bool = True):
            if args[:2] == ["list-panes", "-t"]:
                return "%1\x1fidle\n%2\x1fwaiting"
            return None
        tmux._run_tmux = fake_two_panes  # type: ignore[assignment]
        _check(tmux.get_window_top_state("@1") == "waiting", "waiting outranks idle")

        def fake_no_output(args, *, check_available: bool = True):
            return None
        tmux._run_tmux = fake_no_output  # type: ignore[assignment]
        _check(tmux.get_window_top_state("@2") == "", "no tmux output -> ''")

        def fake_untracked(args, *, check_available: bool = True):
            return "%1\x1f"  # pane present, no @cc-state -> untracked
        tmux._run_tmux = fake_untracked  # type: ignore[assignment]
        _check(tmux.get_window_top_state("@3") == "", "untracked pane -> ''")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


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
    _check(
        usage._account_label({"accountName": "Leo", "accountEmail": "leo@x.dev", "orgUuid": "abc123f"})
        == "leo@x.dev·f",
        "prefers full email + org-id last char over accountName",
    )
    _check(usage._account_label({"accountEmail": "leo@x.dev"}) == "leo@x.dev", "email alone when no org id")
    _check(usage._account_label({"accountName": "Leo"}) == "Leo", "falls back to accountName when no email")
    _check(usage._account_label({"name": "acct-abc123"}) == "acct-abc123", "falls back to raw name")
    _check(usage._account_label({}) == "", "nothing present -> ''")


def _test_usage_render_segment() -> None:
    payload = {
        "credentials": [
            {"isActive": False, "accountName": "personal", "usage5hUsed": 10.0, "usage5hLimit": 100.0},
            {
                "isActive": True,
                "accountName": "work",
                "usage5hUsed": 50.0,
                "usage5hLimit": 100.0,
                "usage7dUsed": 85.0,
                "usage7dLimit": 100.0,
            },
        ],
        "activeFingerprint": "abc",
    }
    out = usage.render_usage(payload)
    expected = (
        f"#[fg={usage.DIM}]work "
        f"#[fg={usage.DIM}]5H:#[fg={usage.YELLOW}]50%#[default] "
        f"#[fg={usage.DIM}]7D:#[fg={usage.RED}]85%#[default]"
    )
    _check(out == expected, f"segment mismatch: {out!r}")
    # no trailing newline in the rendered segment.
    _check(not out.endswith("\n"), "segment must not carry a trailing newline")


def _test_usage_fail_open() -> None:
    # Every "would render nothing" case -> ''.
    _check(usage.render_usage({}) == "", "no credentials key -> ''")
    _check(usage.render_usage({"credentials": "nope"}) == "", "non-list credentials -> ''")
    _check(
        usage.render_usage({"credentials": [{"isActive": False, "accountName": "x"}]}) == "",
        "no isActive credential -> ''",
    )
    _check(
        usage.render_usage({"credentials": [{"isActive": True, "accountName": ""}]}) == "",
        "active credential with no usable label -> ''",
    )
    # active-but-unpolled account (usage5h/7d absent) renders '--' pcts and dim colours —
    # the expected nexus-agent state before it has ever polled that account.
    out = usage.render_usage({"credentials": [{"isActive": True, "accountName": "a"}]})
    _check("5H:" in out and "--" in out, "unpolled windows -> '--' pct")
    _check(f"#[fg={usage.DIM}]5H:#[fg={usage.DIM}]--" in out, "missing 5H window -> DIM")


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


def _test_render_accounts_popup() -> None:
    accounts = [
        ("leo@x.dev·f", 0.5, 0.85),
        ("other@x.dev·2", 0.1, 0.2),
    ]
    out = render.render_accounts_popup(accounts, "leo@x.dev·f", 0.42)
    lines = out.splitlines()
    _check(len(lines) == 2, f"one line per account: {lines!r}")
    active_line = next(l for l in lines if "leo@x.dev" in l)
    other_line = next(l for l in lines if "other@x.dev" in l)
    _check("SES:42%" in active_line, f"active row shows SES: {active_line!r}")
    _check(
        "5H:50%" in active_line and "7D:85%" in active_line,
        f"active row shows 5H/7D too: {active_line!r}",
    )
    _check("SES:" not in other_line, f"non-active row has no SES: {other_line!r}")
    _check(
        "5H:10%" in other_line and "7D:20%" in other_line,
        f"non-active row shows 5H/7D: {other_line!r}",
    )
    _check(active_line.startswith("*"), f"active row is marked: {active_line!r}")
    _check(not other_line.startswith("*"), f"non-active row is not marked: {other_line!r}")
    # Plain text popup: no tmux status-format escaping leaks in.
    _check("#[" not in out, "popup body carries no tmux #[...] style codes")

    # Unreachable nexus-agent / zero deduped credentials -> empty, fail-open.
    _check(render.render_accounts_popup([], "leo@x.dev", 0.5) == "", "no accounts -> ''")

    # No account matches active_label -> no row gets SES (e.g. the active
    # credential had no usable label and was dropped by the caller).
    out_no_match = render.render_accounts_popup(accounts, "", None)
    _check("SES:" not in out_no_match, "no active_label match -> no SES anywhere")


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
    # Pane-id analogue of get_window_top_state — mirror that test's fixture shape.
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
    # session-count glyph — cc-tmux-active-pane-resolution), usage gauges
    # (account label + SES/5H/7D) on the right (restored post
    # cc-tmux-bar-cleanup regression).
    out = render.render_session_bar("O", "if", "main", "leo@x.dev", 0.1, 0.5, 0.85)
    _check(f"#[fg={render.CYAN}]O" in out, "model letter rendered in CYAN")
    _check("if" in out, "project present on the left")
    _check(f"#[fg={render.BRANCH}]main" in out, "branch present in branch colour")
    _check("#[align=right]" in out, "left/right sides split via align=right")
    _check("leo@x.dev" in out, "account label present on the right")
    _check("SES:" in out and "5H:" in out and "7D:" in out, "usage gauges render on the session bar")
    _check(out.endswith("#[default]"), "resets colour at end")

    # None percentages -> '--' rendered in DIM for every gauge.
    out_none = render.render_session_bar("", "if", "main", "leo@x.dev", None, None, None)
    _check(out_none.count("--") == 3, "unpolled gauges all render '--'")

    # No model/project/branch -> fields fail-open, no leading glyph token either.
    out3 = render.render_session_bar("", "", "", "", None, None, None)
    _check("◉" not in out3 and "◌" not in out3, "no leading session-count glyph (removed)")
    _check(f"#[fg={render.CYAN}]" not in out3, "no model letter + no polled usage -> no CYAN segment (fail-open)")
    _check(render.BRANCH not in out3, "no branch -> no branch-colour segment")

    # dirty=True, ahead>0 -> both YELLOW markers render alongside the branch.
    out_dirty = render.render_session_bar("F", "if", "main", "", None, None, None, dirty=True, ahead=2)
    _check(f"#[fg={render.BRANCH}]main" in out_dirty, "branch still renders with markers present")
    _check(f"#[fg={render.YELLOW}]*" in out_dirty, "dirty=True -> YELLOW '*' marker")
    _check(f"#[fg={render.YELLOW}]^2" in out_dirty, "ahead=2 -> YELLOW '^2' marker")

    # dirty=False, ahead=0 -> no markers at all.
    out_clean = render.render_session_bar("F", "if", "main", "", None, None, None, dirty=False, ahead=0)
    _check("*" not in out_clean, "dirty=False -> no '*' marker")
    _check("^" not in out_clean, "ahead=0 -> no '^N' marker")

    # Empty branch + dirty=True, ahead=5 -> markers gated on branch, neither appears.
    out_nobranch = render.render_session_bar("F", "if", "", "", None, None, None, dirty=True, ahead=5)
    _check("*" not in out_nobranch, "no branch -> dirty marker suppressed (gated on branch)")
    _check("^" not in out_nobranch, "no branch -> ahead marker suppressed (gated on branch)")

    # No-kwargs call -> byte-identical to explicit dirty=False, ahead=0 (backward compat).
    out_default = render.render_session_bar("F", "if", "main", "", None, None, None)
    out_explicit_default = render.render_session_bar(
        "F", "if", "main", "", None, None, None, dirty=False, ahead=0
    )
    _check(out_default == out_explicit_default, "no-kwargs call must match explicit dirty=False, ahead=0")


def _test_render_beads_bar() -> None:
    # cc-tmux-row3-openspec-beads-format task 2.3: render_beads_bar now takes
    # structured (openspec_open, openspec_unarchived, beads_ready,
    # beads_blocked) counts + independent per-half ages, rather than a raw
    # pulse-line string.
    D = render.DIM
    Y = render.YELLOW
    R = render.RED

    # No counts at all (no cache, or nothing parsed from either line) -> ''.
    _check(render.render_beads_bar(None, None, None, None) == "", "all None -> ''")

    # A half is "present" only when BOTH its counts are non-None — a
    # partially-present half (one count set, the other None, e.g. from a
    # malformed line) renders as fully absent, same as fully-None (task 2.2's
    # fail-open contract: a broken half never leaks a placeholder value).
    out_partial = render.render_beads_bar(12, None, 5, 2)
    _check("openspec:" not in out_partial, "partial openspec half (unarchived=None) omitted entirely")
    _check(
        out_partial == f"#[fg={D}]beads: 5 ready #[fg={Y}]2#[fg={D}] blocked#[default]",
        "partial openspec half omitted -> only the valid beads half renders",
    )

    # Openspec-only (beads half fully absent) -> single segment, no
    # separator, no beads text anywhere.
    out_openspec_only = render.render_beads_bar(12, 0, None, None)
    _check(
        out_openspec_only == f"#[fg={D}]openspec: 12 open #[fg={D}]0#[fg={D}] unarchived#[default]",
        "openspec-only: single segment, zero unarchived -> DIM",
    )
    _check(
        "beads:" not in out_openspec_only and "|" not in out_openspec_only,
        "openspec-only: no beads segment, no separator",
    )

    # Beads-only (openspec half fully absent) -> single segment.
    out_beads_only = render.render_beads_bar(None, None, 5, 0)
    _check(
        out_beads_only == f"#[fg={D}]beads: 5 ready #[fg={D}]0#[fg={D}] blocked#[default]",
        "beads-only: single segment, zero blocked -> DIM",
    )
    _check("openspec:" not in out_beads_only, "beads-only: no openspec segment")

    # Both halves present, zero unarchived/blocked -> DIM throughout, joined
    # by the DIM ' | ' separator, single trailing reset (mirrors the old
    # multi-line-join shape, now built from two structured segments).
    out_both_zero = render.render_beads_bar(12, 0, 5, 0)
    _check(
        out_both_zero == (
            f"#[fg={D}]openspec: 12 open #[fg={D}]0#[fg={D}] unarchived"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]beads: 5 ready #[fg={D}]0#[fg={D}] blocked#[default]"
        ),
        "both halves, zero unarchived/blocked -> DIM, joined by separator",
    )
    _check(out_both_zero.count("#[default]") == 1, "single trailing reset, not per-segment")

    # Nonzero-but-below-threshold unarchived/blocked -> YELLOW; open/ready
    # counts always stay DIM regardless of their own value (informational,
    # not a health signal).
    out_yellow = render.render_beads_bar(12, 3, 5, 2)
    _check(
        out_yellow == (
            f"#[fg={D}]openspec: 12 open #[fg={Y}]3#[fg={D}] unarchived"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]beads: 5 ready #[fg={Y}]2#[fg={D}] blocked#[default]"
        ),
        "unarchived/blocked > 0 and < high threshold -> YELLOW",
    )

    # At/above the documented high-count threshold -> RED.
    hi_u, hi_b = render.BEADS_UNARCHIVED_HIGH, render.BEADS_BLOCKED_HIGH
    out_red = render.render_beads_bar(12, hi_u, 5, hi_b)
    _check(
        out_red == (
            f"#[fg={D}]openspec: 12 open #[fg={R}]{hi_u}#[fg={D}] unarchived"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]beads: 5 ready #[fg={R}]{hi_b}#[fg={D}] blocked#[default]"
        ),
        "unarchived/blocked >= documented high threshold -> RED",
    )
    _check(render._threshold_color(hi_u - 1, hi_u) == Y, "one below threshold -> still YELLOW, not RED")

    # Staleness markers (plan 006 / BEADS-01, extended to independent
    # per-segment ages by task 2.3): fresh/unknown age -> unchanged; age
    # beyond BEADS_STALE_AFTER_SEC on ONE half -> DIM trailing "(<duration>)"
    # marker on that segment only, the other segment unaffected.
    base = (
        f"#[fg={D}]openspec: 12 open #[fg={D}]0#[fg={D}] unarchived"
        f"{render._BEADS_SEP}"
        f"#[fg={D}]beads: 5 ready #[fg={D}]0#[fg={D}] blocked#[default]"
    )
    _check(render.render_beads_bar(12, 0, 5, 0, None, None) == base, "both ages None -> no markers")
    _check(render.render_beads_bar(12, 0, 5, 0, 60.0, 60.0) == base, "both fresh -> no markers")
    _check(
        render.render_beads_bar(12, 0, 5, 0, render.BEADS_STALE_AFTER_SEC, render.BEADS_STALE_AFTER_SEC) == base,
        "age exactly at threshold -> not yet stale (strict >)",
    )

    out_stale_openspec = render.render_beads_bar(12, 0, 5, 0, 901.0, None)
    _check(
        out_stale_openspec == (
            f"#[fg={D}]openspec: 12 open #[fg={D}]0#[fg={D}] unarchived (15m)"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]beads: 5 ready #[fg={D}]0#[fg={D}] blocked#[default]"
        ),
        "stale openspec age only -> (15m) marker on the openspec segment, beads segment unaffected",
    )

    out_stale_beads = render.render_beads_bar(12, 0, 5, 0, None, 7500.0)
    _check(
        out_stale_beads == (
            f"#[fg={D}]openspec: 12 open #[fg={D}]0#[fg={D}] unarchived"
            f"{render._BEADS_SEP}"
            f"#[fg={D}]beads: 5 ready #[fg={D}]0#[fg={D}] blocked (2h)#[default]"
        ),
        "stale beads age only -> (2h) marker on the beads segment, openspec segment unaffected",
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
    # 'index name', matching cmd_window_icon's existing untracked contract —
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
            f.write("radar:stale (1d)\nopenspec: 12 open, 3 unarchived\nbeads: 5 ready, 2 blocked\n")
        content, _age = cli._read_roadmap_pulse("%1")
        _check(
            content == "openspec: 12 open, 3 unarchived\nbeads: 5 ready, 2 blocked",
            "radar: line stripped, other lines preserved",
        )
        _check("radar:" not in content, "no radar: content survives the read")

        # Content with no radar: line at all is unaffected.
        with open(pulse_file, "w") as f:
            f.write("openspec: 12 open, 3 unarchived\nbeads: 5 ready, 2 blocked\n")
        content2, _age2 = cli._read_roadmap_pulse("%1")
        _check(
            content2 == "openspec: 12 open, 3 unarchived\nbeads: 5 ready, 2 blocked",
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
    # cc-tmux-row3-openspec-beads-format task 2.2: parse the two-line
    # "openspec: N open, M unarchived" / "beads: N ready, M blocked" cache
    # format into structured counts, each half independent and fail-open.
    both = "openspec: 12 open, 3 unarchived\nbeads: 5 ready, 2 blocked"
    _check(
        cli._parse_roadmap_pulse_counts(both) == (12, 3, 5, 2),
        "well-formed two-line content -> both halves parsed",
    )

    reordered = "beads: 5 ready, 2 blocked\nopenspec: 12 open, 3 unarchived"
    _check(
        cli._parse_roadmap_pulse_counts(reordered) == (12, 3, 5, 2),
        "line order doesn't affect parsing",
    )

    openspec_only = "openspec: 12 open, 3 unarchived"
    _check(
        cli._parse_roadmap_pulse_counts(openspec_only) == (12, 3, None, None),
        "missing beads line -> beads half absent (None, None), openspec half intact",
    )

    malformed_beads = "openspec: 12 open, 3 unarchived\nbeads: garbage"
    _check(
        cli._parse_roadmap_pulse_counts(malformed_beads) == (12, 3, None, None),
        "malformed beads line -> beads half absent, openspec half intact",
    )

    beads_only = "beads: 5 ready, 2 blocked"
    _check(
        cli._parse_roadmap_pulse_counts(beads_only) == (None, None, 5, 2),
        "missing openspec line -> openspec half absent (None, None), beads half intact",
    )

    malformed_openspec = "openspec: not-a-number open, 3 unarchived\nbeads: 5 ready, 2 blocked"
    _check(
        cli._parse_roadmap_pulse_counts(malformed_openspec) == (None, None, 5, 2),
        "malformed openspec line -> openspec half absent, beads half intact",
    )

    _check(
        cli._parse_roadmap_pulse_counts("") == (None, None, None, None),
        "empty content -> both halves absent",
    )
    _check(
        cli._parse_roadmap_pulse_counts("some unrelated line") == (None, None, None, None),
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
    out_full = render.render_session_bar("O", "if", "main", "leo@x.dev", 0.1, 0.5, 0.85)
    _check("◉" not in out_full and "◌" not in out_full, "populated call -> no glyph token")

    out_empty = render.render_session_bar("", "", "", "", None, None, None)
    _check("◉" not in out_empty and "◌" not in out_empty, "empty call -> no glyph token")


def _test_cli_read_session_context() -> None:
    # No pane id -> ('', None, '', False, 0) without ever touching the filesystem.
    _check(
        cli._read_session_context("") == ("", None, "", False, 0),
        "empty pane -> ('', None, '', False, 0)",
    )

    saved_cfg = os.environ.get("CLAUDE_CONFIG_DIR")
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-session-context-test-")
    try:
        state_dir = os.path.join(tmpdir, "scripts", "state")
        os.makedirs(state_dir, exist_ok=True)
        os.environ["CLAUDE_CONFIG_DIR"] = tmpdir

        fixture = os.path.join(state_dir, "session-context.%9.json")
        with open(fixture, "w") as f:
            f.write(f'{{"context_used_pct": 42, "model": "F", "ts": {time.time()}}}')

        letter, pct, branch, dirty, ahead = cli._read_session_context("%9")
        _check(letter == "F", f"model letter not read from fixture: {letter!r}")
        _check(pct == 0.42, f"context pct not read/scaled from fixture: {pct!r}")
        _check(
            (branch, dirty, ahead) == ("", False, 0),
            f"legacy payload without git keys -> ('', False, 0): got {(branch, dirty, ahead)!r}",
        )

        # Missing file -> fail-open ('', None, '', False, 0), never raises.
        os.unlink(fixture)
        _check(
            cli._read_session_context("%9") == ("", None, "", False, 0),
            "missing file -> ('', None, '', False, 0)",
        )

        # Malformed JSON -> fail-open, never raises.
        with open(fixture, "w") as f:
            f.write("not json")
        _check(
            cli._read_session_context("%9") == ("", None, "", False, 0),
            "malformed JSON -> ('', None, '', False, 0)",
        )

        # Stale ts (older than the freshness cutoff) -> fail-open, including git fields.
        with open(fixture, "w") as f:
            f.write(
                f'{{"context_used_pct": 42, "model": "F", "ts": {time.time() - 3600}, '
                f'"branch": "main", "dirty": true, "ahead": 3}}'
            )
        _check(
            cli._read_session_context("%9") == ("", None, "", False, 0),
            "stale ts -> ('', None, '', False, 0), git fields included in the fail-open",
        )

        # Missing ts -> unverifiable, treated as stale -> fail-open.
        with open(fixture, "w") as f:
            f.write('{"context_used_pct": 42, "model": "F"}')
        _check(
            cli._read_session_context("%9") == ("", None, "", False, 0),
            "missing ts -> ('', None, '', False, 0)",
        )

        # Boolean ts -> non-numeric, treated as stale -> fail-open.
        with open(fixture, "w") as f:
            f.write('{"context_used_pct": 42, "model": "F", "ts": true}')
        _check(
            cli._read_session_context("%9") == ("", None, "", False, 0),
            "boolean ts -> ('', None, '', False, 0)",
        )

        # Fresh ts + multi-char model -> letter clamps to one char.
        with open(fixture, "w") as f:
            f.write(f'{{"context_used_pct": 42, "model": "Fable", "ts": {time.time()}}}')
        letter, pct, branch, dirty, ahead = cli._read_session_context("%9")
        _check(letter == "F", f"multi-char model should clamp to one letter: {letter!r}")
        _check(pct == 0.42, f"context pct not read/scaled from fixture: {pct!r}")

        # Full payload with fresh ts + valid git fields -> parsed through.
        with open(fixture, "w") as f:
            f.write(
                f'{{"context_used_pct": 42, "model": "F", "ts": {time.time()}, '
                f'"branch": "main", "dirty": true, "ahead": 3}}'
            )
        _check(
            cli._read_session_context("%9") == ("F", 0.42, "main", True, 3),
            "full fresh payload -> ('F', 0.42, 'main', True, 3)",
        )

        # Garbage-typed git fields -> defaults; bool ahead -> 0 (bool-is-int guard).
        with open(fixture, "w") as f:
            f.write(
                f'{{"context_used_pct": 42, "model": "F", "ts": {time.time()}, '
                f'"branch": 5, "dirty": "yes", "ahead": -2}}'
            )
        _, _, branch, dirty, ahead = cli._read_session_context("%9")
        _check(
            (branch, dirty, ahead) == ("", False, 0),
            f"garbage-typed git fields -> ('', False, 0): got {(branch, dirty, ahead)!r}",
        )

        with open(fixture, "w") as f:
            f.write(
                f'{{"context_used_pct": 42, "model": "F", "ts": {time.time()}, '
                f'"branch": "main", "dirty": true, "ahead": true}}'
            )
        _, _, _, _, ahead = cli._read_session_context("%9")
        _check(ahead == 0, f"boolean ahead must not be treated as int 1: got {ahead!r}")
    finally:
        if saved_cfg is None:
            os.environ.pop("CLAUDE_CONFIG_DIR", None)
        else:
            os.environ["CLAUDE_CONFIG_DIR"] = saved_cfg
        shutil.rmtree(tmpdir, ignore_errors=True)


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
    ("cli.compose_title_name", _test_compose_title_name),
    ("cli.maybe_rename_window_success_failure", _test_maybe_rename_window_success_failure),
    ("cli.trace_register_rename_succeeded_field", _test_trace_register_rename_succeeded_field),
    ("paths.tmux_conf_candidates", _test_tmux_conf_candidates),
    ("paths.find_tmux_conf_override", _test_find_tmux_conf_override),
    ("paths.find_plugin_dir", _test_find_plugin_dir),
    ("render.format_duration", _test_render_format_duration),
    ("render.render_status", _test_render_status),
    ("render.resolve_icons", _test_render_resolve_icons),
    ("render.animated_icon", _test_render_animated_icon),
    ("tmux.get_window_top_state", _test_tmux_get_window_top_state),
    ("render.inbox_rows", _test_render_inbox_rows),
    ("usage.color_thresholds", _test_usage_color_thresholds),
    ("usage.pct_formatting", _test_usage_pct_formatting),
    ("usage.extract_util", _test_usage_extract_util),
    ("usage.account_label", _test_usage_account_label),
    ("usage.render_segment", _test_usage_render_segment),
    ("usage.fail_open", _test_usage_fail_open),
    ("usage.extract_active", _test_usage_extract_active),
    ("usage.dedupe_credentials", _test_usage_dedupe_credentials),
    ("render.accounts_popup", _test_render_accounts_popup),
    ("usage.active_usage_ttl", _test_usage_active_usage_ttl),
    ("tmux.get_window_top_pane", _test_tmux_get_window_top_pane),
    ("tmux.get_window_active_pane", _test_tmux_get_window_active_pane),
    ("tmux.get_window_tabs", _test_tmux_get_window_tabs),
    ("render.session_bar", _test_render_session_bar),
    ("render.beads_bar", _test_render_beads_bar),
    ("render.tabs_row", _test_render_tabs_row),
    ("cli.read_roadmap_pulse_fail_open", _test_cli_read_roadmap_pulse_fail_open),
    ("cli.read_roadmap_pulse_radar_strip", _test_cli_read_roadmap_pulse_radar_strip),
    ("cli.parse_roadmap_pulse_counts", _test_cli_parse_roadmap_pulse_counts),
    ("cli.beads_pane_fallback", _test_cli_beads_pane_fallback),
    ("cli.resolve_session_pane_active_tracked", _test_cli_resolve_session_pane_active_tracked),
    ("cli.resolve_session_pane_active_untracked_fallback", _test_cli_resolve_session_pane_active_untracked_fallback),
    ("cli.resolve_session_pane_no_active_fallback", _test_cli_resolve_session_pane_no_active_fallback),
    ("render.session_bar_no_glyph", _test_render_session_bar_no_glyph),
    ("cli.read_session_context", _test_cli_read_session_context),
    ("cli.evaluate_plugin_listing", _test_cli_evaluate_plugin_listing),
    ("cli.evaluate_plugin_listing_degraded", _test_cli_evaluate_plugin_listing_degraded),
    ("cli.evaluate_hook_liveness", _test_cli_evaluate_hook_liveness),
    ("cli.evaluate_hook_liveness_ages", _test_cli_evaluate_hook_liveness_ages),
    ("cli.trace_needs_trim", _test_cli_trace_needs_trim),
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
