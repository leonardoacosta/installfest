"""Built-in pure-function test suite for ``cc-tmux self-test`` (Req-13).

These tests exercise the logic that MUST be correct without a live tmux server:
the priority sort/cycle rules, the ``set_pane_state`` transition-detection
decision (with tmux calls mocked), and path detection. No external test runner —
stdlib only, so the suite runs anywhere ``python3`` does.

Run via ``cc-tmux self-test`` (exit 0 = pass, non-zero = failure count).
"""

from __future__ import annotations

import os
import shutil
import tempfile
import time
from dataclasses import dataclass
from typing import Callable, List, Tuple

from . import cli, paths, priority, registry, render, tmux, usage


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
    _check(cli.compose_title_name("if", "Fix ssh mesh auth") == "if·Fix ssh", "10-char combined truncation")
    _check(cli.compose_title_name("if", "") == "if", "empty title falls back to code alone")
    _check(cli.compose_title_name("", "hello") == "hello", "empty code falls back to title alone")
    _check(cli.compose_title_name("", "", fallback="myproj") == "myproj", "both empty -> fallback")
    _check(len(cli.compose_title_name("if", "a very very long title indeed")) == 10, "always capped at 10")


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

def _test_render_session_glyph() -> None:
    # The raw count -> glyph mapping cc-tmux's session bar composes from
    # (design.md Testing: 0/1/2+ -> '◌'/'◉'/'◉ N').
    _check(render._session_glyph(0) == render.SESSION_GLYPH_HOLLOW, "0 -> hollow")
    _check(render._session_glyph(1) == render.SESSION_GLYPH_FILLED, "1 -> filled")
    _check(render._session_glyph(2) == f"{render.SESSION_GLYPH_FILLED} 2", "2 -> filled + count")
    _check(render._session_glyph(7) == f"{render.SESSION_GLYPH_FILLED} 7", "7 -> filled + count")
    _check(render._session_glyph(-1) == render.SESSION_GLYPH_HOLLOW, "negative floors to hollow")


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


def _test_tmux_get_window_tabs() -> None:
    # Mirrors _test_tmux_get_window_top_pane's mocking convention: fake
    # tmux._run_tmux, branching on the leading args (list-windows vs list-panes).
    saved = tmux._run_tmux
    try:
        def fake_two_windows(args, *, check_available: bool = True):
            if args[:1] == ["list-windows"]:
                return "@1\x1f1\x1feditor\n@2\x1f2\x1fshell"
            if args[:2] == ["list-panes", "-s"]:
                return "@1\x1fidle\n@2\x1fwaiting\n@2\x1factive"
            return None
        tmux._run_tmux = fake_two_windows  # type: ignore[assignment]
        windows = tmux.get_window_tabs()
        _check(len(windows) == 2, "two windows enumerated")
        by_id = {w.id: w for w in windows}
        _check(by_id["@1"].index == "1" and by_id["@1"].name == "editor", "window @1 id/index/name")
        _check(by_id["@1"].state == "idle", "window @1 top state (single pane)")
        _check(by_id["@2"].name == "shell", "window @2 name")
        _check(by_id["@2"].state == "waiting", "window @2 top state (waiting beats active)")

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
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]


def _test_render_session_bar() -> None:
    # Full render: 2+ sessions, model + project + branch on the left, usage
    # gauges (account label + SES/5H/7D) on the right (restored post
    # cc-tmux-bar-cleanup regression).
    out = render.render_session_bar(2, "O", "if", "main", "leo@x.dev", 0.1, 0.5, 0.85)
    _check("◉ 2" in out, "2+ sessions -> filled + count glyph")
    _check(f"#[fg={render.CYAN}]O" in out, "model letter rendered in CYAN")
    _check("if" in out, "project present on the left")
    _check(f"#[fg={render.BRANCH}]main" in out, "branch present in branch colour")
    _check("#[align=right]" in out, "left/right sides split via align=right")
    _check("leo@x.dev" in out, "account label present on the right")
    _check("SES:" in out and "5H:" in out and "7D:" in out, "usage gauges render on the session bar")
    _check(out.endswith("#[default]"), "resets colour at end")

    # None percentages -> '--' rendered in DIM for every gauge.
    out_none = render.render_session_bar(1, "", "if", "main", "leo@x.dev", None, None, None)
    _check(out_none.count("--") == 3, "unpolled gauges all render '--'")

    # Single session -> bare filled glyph (not the 2+ '◉ N' form).
    out2 = render.render_session_bar(1, "S", "oo", "dev", "", None, None, None)
    _check("◉" in out2 and "◉ 1" not in out2, "1 session -> bare filled glyph")

    # 0 sessions + no model/project/branch -> hollow glyph only; fields fail-open.
    out3 = render.render_session_bar(0, "", "", "", "", None, None, None)
    _check("◌" in out3 and "◉" not in out3, "0 sessions -> hollow glyph only")
    _check(f"#[fg={render.CYAN}]" not in out3, "no model letter + no polled usage -> no CYAN segment (fail-open)")
    _check(render.BRANCH not in out3, "no branch -> no branch-colour segment")

    # dirty=True, ahead>0 -> both YELLOW markers render alongside the branch.
    out_dirty = render.render_session_bar(1, "F", "if", "main", "", None, None, None, dirty=True, ahead=2)
    _check(f"#[fg={render.BRANCH}]main" in out_dirty, "branch still renders with markers present")
    _check(f"#[fg={render.YELLOW}]*" in out_dirty, "dirty=True -> YELLOW '*' marker")
    _check(f"#[fg={render.YELLOW}]^2" in out_dirty, "ahead=2 -> YELLOW '^2' marker")

    # dirty=False, ahead=0 -> no markers at all.
    out_clean = render.render_session_bar(1, "F", "if", "main", "", None, None, None, dirty=False, ahead=0)
    _check("*" not in out_clean, "dirty=False -> no '*' marker")
    _check("^" not in out_clean, "ahead=0 -> no '^N' marker")

    # Empty branch + dirty=True, ahead=5 -> markers gated on branch, neither appears.
    out_nobranch = render.render_session_bar(1, "F", "if", "", "", None, None, None, dirty=True, ahead=5)
    _check("*" not in out_nobranch, "no branch -> dirty marker suppressed (gated on branch)")
    _check("^" not in out_nobranch, "no branch -> ahead marker suppressed (gated on branch)")

    # No-kwargs call -> byte-identical to explicit dirty=False, ahead=0 (backward compat).
    out_default = render.render_session_bar(1, "F", "if", "main", "", None, None, None)
    out_explicit_default = render.render_session_bar(
        1, "F", "if", "main", "", None, None, None, dirty=False, ahead=0
    )
    _check(out_default == out_explicit_default, "no-kwargs call must match explicit dirty=False, ahead=0")


def _test_render_beads_bar() -> None:
    # Empty pulse line -> '' (row 3 shows nothing).
    _check(render.render_beads_bar("") == "", "empty pulse -> ''")

    # A pulse line that is ONLY a 'next:' line -> '' — that content is dropped
    # entirely (row 3 never shows the next: segment, regardless of ordering),
    # so with nothing else to show the row renders nothing.
    _check(render.render_beads_bar("next: /apply foo 2o 3u") == "", "next:-only pulse -> ''")

    # Plain (non-next:) line -> entirely DIM, single trailing reset.
    out2 = render.render_beads_bar("12 open / 24 waiting")
    _check(
        out2 == f"#[fg={render.DIM}]12 open / 24 waiting#[default]",
        "plain pulse line -> all DIM",
    )

    # Two-line cache content (next: line + counts line) -> the next: line is
    # dropped entirely and only the counts line renders, DIM, with no
    # separator artifact from the discarded line (the cc-tmux-bar-cleanup
    # regression this fixed: row 3 must never show next: content).
    out3 = render.render_beads_bar("next: /apply foo 2o 3u\n12 open, 3 unarchived")
    _check(
        out3 == f"#[fg={render.DIM}]12 open, 3 unarchived#[default]",
        "two-line: next: line dropped, only counts line remains",
    )
    _check("next:" not in out3, "two-line: next: content never appears in the rendered row")

    # Multiple non-next: lines -> joined with the DIM ' | ' separator, single
    # trailing reset (the separator only matters when there's more than one
    # surviving line).
    out4 = render.render_beads_bar("12 open / 24 waiting\n3 blocked")
    _check(
        out4 == (
            f"#[fg={render.DIM}]12 open / 24 waiting"
            f"#[fg={render.DIM}] | #[fg={render.DIM}]3 blocked#[default]"
        ),
        "multi-line: both lines joined with a DIM pipe separator",
    )
    _check(out4.count("#[default]") == 1, "multi-line: single trailing reset, not per-line")

    # Blank-line-only content (e.g. a stray trailing newline) -> ''.
    _check(render.render_beads_bar("\n\n") == "", "blank-only content -> ''")


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
    # No pane id -> '' without ever touching tmux.
    _check(cli._read_roadmap_pulse("") == "", "empty pane -> ''")

    saved_run = tmux._run_tmux
    try:
        # tmux returns no cwd -> '' (nothing to resolve).
        tmux._run_tmux = lambda args, *, check_available=True: ""  # type: ignore[assignment]
        _check(cli._read_roadmap_pulse("%1") == "", "no cwd -> ''")

        # cwd present but registry resolves no code -> ''.
        tmux._run_tmux = (  # type: ignore[assignment]
            lambda args, *, check_available=True: "/definitely/not/tracked"
        )
        _check(cli._read_roadmap_pulse("%1") == "", "untracked cwd -> ''")
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
        _check(
            cli._read_roadmap_pulse("%1") == "next: /apply zz-thing 1o 2u",
            "reads + strips the resolved pulse line",
        )

        # Missing file -> '' (fail-open, never raises).
        os.unlink(pulse_file)
        _check(cli._read_roadmap_pulse("%1") == "", "missing pulse file -> ''")

        # A directory where the file should be -> unreadable -> '' (never raises).
        os.makedirs(pulse_file)
        _check(cli._read_roadmap_pulse("%1") == "", "unreadable pulse path -> ''")
    finally:
        tmux._run_tmux = saved_run2  # type: ignore[assignment]
        for key, val in (("DOTFILES", saved_dotfiles), ("CLAUDE_CONFIG_DIR", saved_cfg)):
            if val is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = val
        shutil.rmtree(tmpdir, ignore_errors=True)


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
    ("usage.active_usage_ttl", _test_usage_active_usage_ttl),
    ("render.session_glyph", _test_render_session_glyph),
    ("tmux.get_window_top_pane", _test_tmux_get_window_top_pane),
    ("tmux.get_window_tabs", _test_tmux_get_window_tabs),
    ("render.session_bar", _test_render_session_bar),
    ("render.beads_bar", _test_render_beads_bar),
    ("render.tabs_row", _test_render_tabs_row),
    ("cli.read_roadmap_pulse_fail_open", _test_cli_read_roadmap_pulse_fail_open),
    ("cli.read_session_context", _test_cli_read_session_context),
    ("cli.evaluate_plugin_listing", _test_cli_evaluate_plugin_listing),
    ("cli.evaluate_plugin_listing_degraded", _test_cli_evaluate_plugin_listing_degraded),
    ("cli.evaluate_hook_liveness", _test_cli_evaluate_hook_liveness),
    ("cli.evaluate_hook_liveness_ages", _test_cli_evaluate_hook_liveness_ages),
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
