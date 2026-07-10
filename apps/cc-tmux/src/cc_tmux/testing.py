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

from . import paths, priority, tmux


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
