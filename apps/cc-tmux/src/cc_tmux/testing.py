"""Built-in pure-function test suite for ``cc-tmux self-test`` (Req-13).

These tests exercise the logic that MUST be correct without a live tmux server:
the priority sort/cycle rules, the ``set_pane_state`` transition-detection
decision (with tmux calls mocked), and path detection. No external test runner —
stdlib only, so the suite runs anywhere ``python3`` does.

Run via ``cc-tmux self-test`` (exit 0 = pass, non-zero = failure count).
"""

from __future__ import annotations

import os
import tempfile
from dataclasses import dataclass
from typing import Callable, List, Tuple

from . import paths, priority, render, tmux, usage


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
    """Minimal stand-in for PaneInfo — priority.py only reads state/timestamp/id."""

    id: str
    state: str
    timestamp: float


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
    acct = {"five_hour": {"utilization": 0.5}, "seven_day": {"utilization": 0}}
    _check(usage._extract_util(acct, "five_hour") == 0.5, "extract 0.5")
    _check(usage._extract_util(acct, "seven_day") == 0.0, "extract present 0")
    # missing window / missing utilization / null -> None.
    _check(usage._extract_util({}, "five_hour") is None, "missing window -> None")
    _check(usage._extract_util({"five_hour": {}}, "five_hour") is None, "missing util -> None")
    _check(
        usage._extract_util({"five_hour": {"utilization": None}}, "five_hour") is None,
        "null util -> None",
    )


def _test_usage_render_segment() -> None:
    payload = {
        "active_account": "work",
        "accounts": [
            {"name": "personal", "five_hour": {"utilization": 0.1}},
            {"name": "work", "five_hour": {"utilization": 0.5}, "seven_day": {"utilization": 0.85}},
        ],
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
    # Every "sh would exit 0 with no output" case -> ''.
    _check(usage.render_usage({}) == "", "no active_account -> ''")
    _check(usage.render_usage({"active_account": ""}) == "", "empty active_account -> ''")
    _check(
        usage.render_usage({"active_account": "x", "accounts": "nope"}) == "",
        "non-list accounts -> ''",
    )
    _check(
        usage.render_usage({"active_account": "x", "accounts": [{"name": "y"}]}) == "",
        "active not found in accounts -> ''",
    )
    # missing-both-windows account renders '--' pcts and dim colours.
    out = usage.render_usage({"active_account": "a", "accounts": [{"name": "a"}]})
    _check("5H:" in out and "--" in out, "missing windows -> '--' pct")
    _check(f"#[fg={usage.DIM}]5H:#[fg={usage.DIM}]--" in out, "missing 5H window -> DIM")


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
    ("priority.select_next", _test_select_next),
    ("tmux.is_real_transition", _test_is_real_transition_pure),
    ("tmux.set_pane_state_change", _test_set_pane_state_returns_change),
    ("tmux.set_pane_state_hot_path", _test_set_pane_state_hot_path_skips_git),
    ("tmux.set_pane_state_unknown", _test_set_pane_state_unknown_state),
    ("tmux.set_pane_state_writes", _test_set_pane_state_writes_state_and_timestamp),
    ("paths.tmux_conf_candidates", _test_tmux_conf_candidates),
    ("paths.find_tmux_conf_override", _test_find_tmux_conf_override),
    ("paths.find_plugin_dir", _test_find_plugin_dir),
    ("render.format_duration", _test_render_format_duration),
    ("render.render_status", _test_render_status),
    ("render.resolve_icons", _test_render_resolve_icons),
    ("render.inbox_rows", _test_render_inbox_rows),
    ("usage.color_thresholds", _test_usage_color_thresholds),
    ("usage.pct_formatting", _test_usage_pct_formatting),
    ("usage.extract_util", _test_usage_extract_util),
    ("usage.render_segment", _test_usage_render_segment),
    ("usage.fail_open", _test_usage_fail_open),
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
