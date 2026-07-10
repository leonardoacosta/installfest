"""Pure priority / ordering logic for tracked Claude panes.

This module is deliberately free of any tmux dependency: every function operates
on plain :class:`~cc_tmux.tmux.PaneInfo`-shaped objects (anything with ``state``
and ``timestamp`` attributes) so the ordering rules are unit-testable without a
live tmux server (design.md invariant: views derive, they do not store).

Priority model (Req-4):
    waiting (0)  >  idle (1)  >  active (2)
Lower number == higher attention priority. Within a group, the most-recently
*visited* pane surfaces first (``@cc-visited`` desc), falling back to the
most-recent state change (``timestamp`` desc) for never-visited panes
(cc-tmux-scout-adoptions Decision 2 — recency is a within-group tiebreak only,
the group order itself is unchanged).

Only ``waiting`` and ``idle`` panes are *pending* (cyclable / dismissable);
``active`` panes are shown for overview but never cycled to.
"""

from __future__ import annotations

from typing import Dict, List, Sequence, TypeVar

# Attention ordering. Lower == higher priority (surfaces first).
STATE_PRIORITY: Dict[str, int] = {
    "waiting": 0,
    "idle": 1,
    "active": 2,
}

# Panes the user might need to act on — the set that cycling / dismissal target.
PENDING_STATES = frozenset({"waiting", "idle"})

# Every state cc-tmux understands. A pane whose @cc-state is not in here is
# treated as untracked and excluded from views.
VALID_STATES = frozenset(STATE_PRIORITY.keys())

# Cycle modes selectable via @cc-cycle-mode.
VALID_CYCLE_MODES: List[str] = ["priority", "flat"]

# States ordered high-to-low priority (waiting, idle, active). Used to walk
# groups deterministically.
_STATES_BY_PRIORITY: List[str] = sorted(STATE_PRIORITY, key=lambda s: STATE_PRIORITY[s])

T = TypeVar("T")


def _priority_of(pane: object) -> int:
    """Priority rank of a pane's state; unknown states sort last."""
    state = getattr(pane, "state", None)
    return STATE_PRIORITY.get(state, len(STATE_PRIORITY) + 1)


def _timestamp_of(pane: object) -> float:
    """Numeric timestamp for newest-first ordering; missing/garbage -> 0."""
    ts = getattr(pane, "timestamp", None)
    try:
        return float(ts)
    except (TypeError, ValueError):
        return 0.0


def _visited_of(pane: object) -> float:
    """Numeric last-visited epoch for the recency tiebreak; missing/garbage -> 0.

    Sibling of :func:`_timestamp_of`: a never-visited pane reads 0.0 and thus
    falls back to timestamp ordering within its group.
    """
    v = getattr(pane, "visited", None)
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


def group_by_state(panes: Sequence[T]) -> Dict[str, List[T]]:
    """Bucket panes by state, each bucket sorted newest-first.

    Returns a dict keyed by every state in ``STATE_PRIORITY`` (empty lists for
    absent states) so callers can index without KeyError. Panes with an unknown
    state are dropped.
    """
    groups: Dict[str, List[T]] = {state: [] for state in STATE_PRIORITY}
    for pane in panes:
        state = getattr(pane, "state", None)
        if state in groups:
            groups[state].append(pane)
    for state in groups:
        # visited desc, then timestamp desc (recency tiebreak within the group).
        groups[state].sort(key=lambda p: (-_visited_of(p), -_timestamp_of(p)))
    return groups


def sort_panes(panes: Sequence[T]) -> List[T]:
    """All panes ordered by (state priority asc, visited desc, timestamp desc).

    The canonical global ordering used by inbox / status views: waiting first,
    then idle, then active — and within each group the most-recently-visited pane
    first (recency tiebreak), falling back to newest state change. Unknown-state
    panes sort to the very end.
    """
    return sorted(panes, key=lambda p: (_priority_of(p), -_visited_of(p), -_timestamp_of(p)))


def pending_panes(panes: Sequence[T]) -> List[T]:
    """Only cyclable/dismissable panes (waiting + idle), in priority order."""
    return [p for p in sort_panes(panes) if getattr(p, "state", None) in PENDING_STATES]


def cycle_order(panes: Sequence[T], mode: str = "priority") -> List[T]:
    """The ordered ring of panes that ``cc-tmux cycle`` walks.

    ``priority`` mode: only the single highest-priority NON-EMPTY pending group
        (all waiting, else all idle) — you stay within the most-urgent bucket.
    ``flat`` mode: every pending pane across both groups, in full priority order
        (waiting newest-first, then idle newest-first).

    ``active`` panes are never in the cycle ring in either mode. An unknown mode
    falls back to ``priority`` (fail-open: never raise on a bad @cc-cycle-mode).
    """
    if mode not in VALID_CYCLE_MODES:
        mode = "priority"

    if mode == "flat":
        return pending_panes(panes)

    # priority mode: highest-priority non-empty pending group only.
    groups = group_by_state(panes)
    for state in _STATES_BY_PRIORITY:
        if state not in PENDING_STATES:
            continue
        if groups[state]:
            return list(groups[state])
    return []


def select_next(
    panes: Sequence[T],
    current_id: str | None,
    mode: str = "priority",
    *,
    id_attr: str = "id",
) -> T | None:
    """Pick the pane to hop to next, given the currently-focused pane id.

    Advances one step through :func:`cycle_order`. If the current pane is in the
    ring, returns the next one (wrapping around); otherwise returns the ring's
    head. Returns ``None`` when the ring is empty.
    """
    ring = cycle_order(panes, mode)
    if not ring:
        return None

    if current_id is None:
        return ring[0]

    ids = [getattr(p, id_attr, None) for p in ring]
    try:
        idx = ids.index(current_id)
    except ValueError:
        return ring[0]

    return ring[(idx + 1) % len(ring)]
