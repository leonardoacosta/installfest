"""Presentation-layer pure functions for cc-tmux views (Req-5, Req-7).

Everything here is a *pure* function of its inputs — no tmux dependency — so the
status-format, duration, and inbox-column rendering are unit-testable without a
live server (design.md invariant 2: views derive, they do not store). The CLI
handlers in :mod:`cc_tmux.cli` read tmux options and hand the values in.

State icons are functional status-bar glyphs (Geometric Shapes block, not
emoji) with sensible defaults, each overridable via ``@cc-icon-<state>``.
"""

from __future__ import annotations

import re
import time
from typing import Callable, Dict, List, Optional, Sequence, Tuple

from . import tmux
from .usage import BLUE, CYAN, DIM, GREEN, RED, YELLOW, color_for, pct_for

# Default state glyphs. Geometric Shapes (U+25CF/25CB/25D0), NOT emoji — plain
# monospace-friendly marks that render in any terminal and are overridable.
DEFAULT_ICONS: Dict[str, str] = {
    "waiting": "●",  # ● filled — needs attention
    "idle": "○",     # ○ hollow — done / ready
    "active": "◐",   # ◐ half — working
}

# Default @cc-status-format: "icon count" per state, highest attention first.
DEFAULT_STATUS_FORMAT = "{waiting:icon} {idle:icon} {active:icon}"

_TOKEN_RE = re.compile(r"\{(\w+):icon\}")

# ---------------------------------------------------------------------------
# Animated window-tab icon (Req: animated tab icon)
#
# The literal window NAME (set via `rename-window`) only changes on discrete
# Claude Code hook events — irregular, sometimes minutes apart, sometimes
# bursty — so it cannot drive a believable animation on its own. Real motion
# needs a wall-clock-driven re-render, which tmux already provides for free
# via `window-status-format`/`window-status-current-format`: those are
# re-evaluated on every status-bar refresh (`status-interval`), independent of
# hook activity. `cli.cmd_window_icon` is invoked FROM that format string
# (`#(cc-tmux window-icon #{window_id})`), so :func:`animated_icon` picks a
# frame purely from the caller-supplied wall-clock time — no timer, no
# background process, same "daemon-free" invariant as the rest of this
# plugin (tmux.py's own docstring).
#
# Frame family per state (distinct motion language, not just distinct icons):
#   waiting (needs a decision: permission/question/plan/elicitation) -> a
#     rising/falling shade pulse, reads as "needs attention".
#   active (Claude mid-turn) -> a rotating block edge, reads as "in motion".
#   idle (turn ended, nothing pending) -> a single static glyph, deliberately
#     NOT animated — nothing is happening, so nothing should move.
# ---------------------------------------------------------------------------

SHADE_FRAMES: Tuple[str, ...] = ("░", "▒", "▓", "█", "▓", "▒", "░")
BLOCK_FRAMES: Tuple[str, ...] = ("▁", "▏", "▔", "▕")
IDLE_GLYPH = "█"

# Seconds per frame. Matches the (default 1s-floor) status-interval driving
# re-renders — a shorter period than the actual refresh cadence would just
# mean some frames are silently skipped, which is harmless.
FRAME_PERIOD_SEC = 1.0


def animated_icon(state: str, now: float) -> str:
    """The tab-icon glyph for ``state`` at wall-clock ``now``.

    Pure function of its inputs (testable without a live clock or tmux) —
    :func:`cc_tmux.cli.cmd_window_icon` supplies the real ``time.time()``.
    ``waiting``/``active`` cycle their frame tuple by ``now // FRAME_PERIOD_SEC``;
    ``idle`` always returns the same static glyph. Any other state (or an
    empty string, meaning no tracked pane) falls back to :data:`DEFAULT_ICONS`,
    then to ``""`` — callers should treat an empty result as "print nothing".
    """
    if state == "waiting":
        return SHADE_FRAMES[int(now / FRAME_PERIOD_SEC) % len(SHADE_FRAMES)]
    if state == "active":
        return BLOCK_FRAMES[int(now / FRAME_PERIOD_SEC) % len(BLOCK_FRAMES)]
    if state == "idle":
        return IDLE_GLYPH
    return DEFAULT_ICONS.get(state, "")


# ---------------------------------------------------------------------------
# Sub-agent tab-icon overlay (cc-tmux-subagent-tab-icon)
#
# Resolved 6-way glyph mapping (Leo, 2026-07-12, tasks.md task 1.1): foreground
# (exact, hook-verified via the Task tool's own PreToolUse/PostToolUse pair)
# and background (heuristic, timeout-aged) sub-agent activity get DISTINCT
# shape families — circle for foreground, diamond for background — so the two
# are visually distinguishable at a glance rather than colliding on the same
# two marks. Within each family, hollow=1 / filled=2+ mirrors the "hollow=one,
# filled=multiple" language DEFAULT_ICONS already uses elsewhere in this
# module. Foreground always takes precedence over background when both are
# nonzero (foreground is the exact signal; background is only a heuristic
# fallback) — see :func:`resolve_tab_icon`.
# ---------------------------------------------------------------------------

SUBAGENT_FG_1 = "◎"       # 1 foreground sub-agent running
SUBAGENT_FG_2PLUS = "◉"   # 2+ foreground sub-agents running
SUBAGENT_BG_1 = "◇"       # 0 foreground, 1 unexpired background sub-agent
SUBAGENT_BG_2PLUS = "◆"   # 0 foreground, 2+ unexpired background sub-agents


def resolve_tab_icon(state: str, now: float, fg_count: int, bg_count: int) -> str:
    """The tab-icon glyph, sub-agent-aware (cc-tmux-subagent-tab-icon overlay).

    Pure function of its inputs — ``bg_count`` MUST already be the caller's
    PRUNED count (:func:`cc_tmux.cli.prune_background_entries`); this function
    has no clock-aging logic of its own, it only branches on counts. Foreground
    takes precedence over background whenever ``fg_count`` is nonzero (it is
    the exact signal; background is only a time-boxed heuristic). Falls
    through to the plain :func:`animated_icon` state-based glyph
    (waiting/active/idle) when neither is active — this is an ADDITIVE overlay
    on top of the existing ``@cc-state`` animation, not a replacement for it
    (proposal.md Non-Goals).
    """
    if fg_count >= 2:
        return SUBAGENT_FG_2PLUS
    if fg_count == 1:
        return SUBAGENT_FG_1
    if bg_count >= 2:
        return SUBAGENT_BG_2PLUS
    if bg_count == 1:
        return SUBAGENT_BG_1
    return animated_icon(state, now)


def resolve_icons(get_option: Callable[[str], str]) -> Dict[str, str]:
    """Icon map with per-state ``@cc-icon-<state>`` overrides applied.

    ``get_option`` is injected (``tmux.get_global_option`` in production, a stub
    in tests) so this stays pure and testable.
    """
    icons = dict(DEFAULT_ICONS)
    for state in DEFAULT_ICONS:
        try:
            override = get_option(f"@cc-icon-{state}")
        except Exception:  # noqa: BLE001 - fail open to defaults
            override = ""
        if override:
            icons[state] = override
    return icons


def format_duration(seconds: float) -> str:
    """Compact human duration: ``5s`` / ``3m`` / ``2h`` / ``1d`` (floored)."""
    try:
        s = int(seconds)
    except (TypeError, ValueError):
        return "0s"
    if s < 0:
        s = 0
    if s < 60:
        return f"{s}s"
    m = s // 60
    if m < 60:
        return f"{m}m"
    h = m // 60
    if h < 24:
        return f"{h}h"
    return f"{h // 24}d"


def render_status(fmt: str, counts: Dict[str, int], icons: Dict[str, str]) -> str:
    """Render ``@cc-status-format`` — ``{state:icon}`` -> "icon count" when > 0.

    A state with a zero count renders empty (the token drops out); leftover
    whitespace is collapsed so ``"● 2  ◐ 1"`` never has ragged gaps.
    """
    def _repl(match: "re.Match[str]") -> str:
        state = match.group(1)
        count = counts.get(state, 0)
        if count <= 0:
            return ""
        return f"{icons.get(state, state)} {count}"

    out = _TOKEN_RE.sub(_repl, fmt or "")
    return re.sub(r"\s+", " ", out).strip()


def inbox_rows(
    panes: Sequence[object],
    icons: Dict[str, str],
    now: float,
) -> List[Tuple[str, str]]:
    """Aligned ``(label, pane_id)`` rows for the inbox / picker (Req-5 columns).

    Columns: state icon | ``session:window`` | project | branch | time-in-state
    | wait reason | task. Every column except the trailing task is left-padded to
    a common width so fzf/menu rows line up. Each pane needs ``id``/``session``/
    ``window``/``state``/``timestamp``/``project``/``branch``/``wait_reason``/
    ``task`` attributes (a :class:`~cc_tmux.tmux.PaneInfo`).
    """
    cells: List[List[str]] = []
    for p in panes:
        state = getattr(p, "state", "")
        cells.append(
            [
                icons.get(state, state or "?"),
                f"{getattr(p, 'session', '')}:{getattr(p, 'window', '')}",
                getattr(p, "project", "") or "-",
                getattr(p, "branch", "") or "-",
                format_duration(now - float(getattr(p, "timestamp", 0.0) or 0.0)),
                getattr(p, "wait_reason", "") or "-",
                getattr(p, "task", "") or "-",
                getattr(p, "id", ""),  # machine field, not aligned into the label
            ]
        )

    # Pad columns 0..5 (leave the trailing task at index 6 unpadded).
    pad_cols = 6
    widths = [0] * pad_cols
    for row in cells:
        for i in range(pad_cols):
            widths[i] = max(widths[i], len(row[i]))

    out: List[Tuple[str, str]] = []
    for row in cells:
        padded = [row[i].ljust(widths[i]) for i in range(pad_cols)]
        label = "  ".join(padded + [row[6]]).rstrip()
        out.append((label, row[7]))
    return out


# ---------------------------------------------------------------------------
# Session / beads status rows (row 2 + row 3 — cc-tmux-session-usage-bars,
# corrected post cc-tmux-bar-cleanup)
#
# Both are *pure* composition functions. They emit tmux status-format strings
# using the same ``#[fg=…]``/``#[default]`` escaping convention as
# :func:`cc_tmux.usage.render_usage`, reusing that module's ``CYAN``/``DIM``
# colour constants. The CLI handlers (``cmd_session_bar``/``cmd_beads_bar``)
# read tmux/cache state and hand plain values in — nothing here touches tmux or
# a subprocess.
#
# Claude usage stats (account label, SES/5H/7D gauges) render on row 2's
# right side, alongside the left-side session/model/git identity. Only row 1
# (the window-tabs `status-right`) stays usage-free — that part of
# cc-tmux-bar-cleanup was correct and stays; cleanup's removal of usage from
# row 2 itself was a live-testing regression, reverted here.
# ---------------------------------------------------------------------------

# Branch-name colour (purple), distinct from usage.py's util palette. Model
# letter, project, and gauge labels reuse DIM/CYAN from usage.py.
BRANCH = "#B267E6"


def render_session_bar(
    model_letter: str,
    project: str,
    branch: str,
    account_label: str,
    ses_pct: Optional[float],
    five_h_pct: Optional[float],
    seven_d_pct: Optional[float],
    *,
    git_status: Optional["tmux.GitStatusCounts"] = None,
) -> str:
    """Row-2 status-format string: model/project/git on the left, usage on the right.

    Left side: model letter + project + git branch, followed by up to six
    working-tree indicator segments (cc-tmux-git-status-glyphs task 3.1),
    each entirely omitted — no glyph, no stray separator — when its count is
    0. ``git_status`` is a :class:`cc_tmux.tmux.GitStatusCounts` (or ``None``,
    treated identically to an all-zero instance). In this fixed left-to-right
    order, space-separated: ``<N>M`` (GREEN) if ``modified > 0``, ``<N>U``
    (YELLOW) if ``untracked > 0``, ``<N>D`` (RED) if ``deleted > 0``, ``<N>R``
    (BLUE) if ``renamed > 0``, ``⇡<N>`` if ``ahead > 0``, ``⇣<N>`` if
    ``behind > 0`` — the ahead/behind glyphs are unstyled/DIM, matching the
    branch segment's own styling rather than getting a distinct colour. The
    whole indicator run is dropped (fail-open) when ``branch`` is empty, so a
    marker never appears without the branch it describes — same fail-open
    contract the prior ``dirty``/``ahead`` params had. Right
    side: account label + SES:/5H:/7D: gauges, each coloured via color_for and
    formatted via pct_for. The two sides are joined with a #[align=right]
    directive so tmux fills the gap between them. ses_pct / five_h_pct /
    seven_d_pct are utilization ratios in 0..1 (or None when unpolled -> '--'
    in DIM).

    Pure function of its inputs (no tmux/subprocess). Empty model_letter /
    project / branch fields drop out of the left side (fail-open).

    The account-label token on the right side is wrapped in
    ``#[range=user|accounts]``/``#[norange]`` (cc-tmux-account-switcher-popup
    task 3.1) — the same range-marker mechanism :func:`cmd_status_inbox`
    already uses for its ``#[range=pane|<id>]`` badges, confirmed via task
    1.1's spike to be the only way to bind a NON-default ``MouseDown1Status``
    action to a specific status-bar segment on this tmux version (3.6a): all
    ranges share one ``MouseDown1Status`` key, distinguished at click time via
    ``#{mouse_status_range}`` — see ``cc-tmux.tmux``'s override. Dropped
    entirely (no range wrapper) when ``account_label`` is empty, so an
    unlabeled right side never emits a dead click target.
    """
    left_parts: List[str] = []
    if model_letter:
        left_parts.append(f"#[fg={CYAN}]{model_letter}")
    if project:
        left_parts.append(f"#[fg={DIM}]{project}")
    if branch:
        left_parts.append(f"#[fg={DIM}]>")
        seg = f"#[fg={BRANCH}]{branch}"
        gs = git_status or tmux.GitStatusCounts()
        indicators: List[str] = []
        if gs.modified > 0:
            indicators.append(f"#[fg={GREEN}]{gs.modified}M")
        if gs.untracked > 0:
            indicators.append(f"#[fg={YELLOW}]{gs.untracked}U")
        if gs.deleted > 0:
            indicators.append(f"#[fg={RED}]{gs.deleted}D")
        if gs.renamed > 0:
            indicators.append(f"#[fg={BLUE}]{gs.renamed}R")
        if gs.ahead > 0:
            indicators.append(f"#[fg={DIM}]⇡{gs.ahead}")
        if gs.behind > 0:
            indicators.append(f"#[fg={DIM}]⇣{gs.behind}")
        if indicators:
            seg += " " + " ".join(indicators)
        left_parts.append(seg)
    left = " ".join(left_parts) + "#[default]"

    cs, c5, c7 = color_for(ses_pct), color_for(five_h_pct), color_for(seven_d_pct)
    ps, p5, p7 = pct_for(ses_pct), pct_for(five_h_pct), pct_for(seven_d_pct)
    label_seg = (
        f"#[range=user|accounts]#[fg={DIM}]{account_label} #[norange]"
        if account_label
        else ""
    )
    right = (
        f"{label_seg}"
        f"#[fg={DIM}]SES:#[fg={cs}]{ps}#[default] "
        f"#[fg={DIM}]5H:#[fg={c5}]{p5}#[default] "
        f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default]"
    )
    return f"{left}#[align=right]{right}"


# ---------------------------------------------------------------------------
# Accounts popup (cc-tmux-account-switcher-popup)
#
# Renders the multi-line body shown when the row-2 account-label segment is
# clicked (the #[range=user|accounts] marker above). This body reaches a
# REAL terminal — either fzf (``--ansi``, see ``cc-tmux.tmux``'s
# accounts_popup_cmd) or the plain ``display-popup -E "... ; read -n 1 -s"``
# fallback for pre-3.2 tmux/no-fzf — never tmux's own status-format renderer,
# so genuine ANSI SGR escapes (:func:`_green`) are the right mechanism for
# colour here, unlike :func:`render_session_bar`'s ``#[fg=...]`` tokens
# (which WOULD show up as literal garbage in this popup — the two renderers
# target two different consumers, not interchangeable escaping).
# ---------------------------------------------------------------------------

# Truecolor ANSI green matching this module's own tmux-hex GREEN (#00ac3a,
# usage.py) — Leo's ask (2026-07-13): every number/datetime in the popup is
# uniformly this green, NOT severity-escalated the way color_for's RED/
# YELLOW/CYAN colors the tmux status-format segments elsewhere in this file.
_ANSI_GREEN = "\x1b[38;2;0;172;58m"
_ANSI_RESET = "\x1b[0m"


def _green(text: str) -> str:
    """Wrap ``text`` in the popup's ANSI truecolor green + reset."""
    return f"{_ANSI_GREEN}{text}{_ANSI_RESET}"


def _format_reset_countdown(remaining_secs: float, *, with_days: bool) -> str:
    """``HH:mm`` (5H) or ``dd:HH:mm`` (7D) countdown to a reset, or ``"now"``.

    ``remaining_secs`` <= 0 (already reset, or a clock skew edge case) ->
    ``"now"``, matching the convention nexus-statusline's own
    ``formatCountdown`` (``apps/nexus-statusline/src/render.ts``) uses for the
    identical "reset already passed" case.
    """
    if remaining_secs <= 0:
        return "now"
    total = int(remaining_secs)
    if with_days:
        days, rem = divmod(total, 86400)
        hours, rem = divmod(rem, 3600)
        minutes = rem // 60
        return f"{days:02d}:{hours:02d}:{minutes:02d}"
    hours, rem = divmod(total, 3600)
    minutes = rem // 60
    return f"{hours:02d}:{minutes:02d}"


# Width of the 7D line's day slot: a 3-letter weekday abbreviation + one
# separating space (``"Sat "``). The 5H line has no day of its own (its
# reset always lands the same calendar day), but pads with a same-width
# blank so BOTH lines' ``HH:mm a`` clock — and therefore both lines' "a"
# am/pm markers — start in the same column (Leo's 2026-07-13 alignment ask).
_DAY_SLOT_WIDTH = 4


def _format_reset_line(
    window_label: str,
    verb: str,
    reset_epoch: Optional[float],
    now: float,
    *,
    with_day: bool,
) -> Tuple[str, str]:
    """``(plain, colored)`` reset-time line pair, or ``("", "")`` when unresolved.

    ``reset_epoch`` absent (nexus-agent hasn't polled this window yet) ->
    ``("", "")``, so the caller omits the line entirely — fail-open, matching
    :func:`render_accounts_popup`'s own "nothing to show" convention rather
    than rendering a placeholder. Absolute time renders in LOCAL time
    (``time.localtime``, matching this module's ``now: float`` epoch-seconds
    convention elsewhere — :func:`resolve_tab_icon`/:func:`inbox_rows`) since
    that's what a human reading a terminal popup expects.

    ``with_day`` prefixes the short weekday name (``%a`` — ``Sat``/``Mon``/
    ``Thu``; 7D's reset can land on a different calendar day than "today", so
    "which day" matters more than "which date"; 5H always resolves same-day,
    so its slot is left blank — see :data:`_DAY_SLOT_WIDTH`) and switches the
    countdown from ``HH:mm`` to ``dd:HH:mm`` (see
    :func:`_format_reset_countdown`).

    Returns BOTH a plain-text line (for width/alignment math — ANSI escapes
    are zero-width visually but non-zero in ``len()``, so alignment MUST be
    computed off the plain variant) and a colour-decorated one (for actual
    display, :func:`_green` around the "numbers and datetimes" — the clock,
    weekday, and countdown, not the ``"Resets at/on"``/``"in"`` label text).
    """
    if reset_epoch is None:
        return "", ""
    lt = time.localtime(reset_epoch)
    clock = f"{time.strftime('%I:%M', lt)} {time.strftime('%p', lt).lower()}"
    day_slot = f"{time.strftime('%a', lt):<3} " if with_day else " " * _DAY_SLOT_WIDTH
    when = f"{day_slot}{clock}"
    countdown = _format_reset_countdown(reset_epoch - now, with_days=with_day)
    plain = f"{window_label} Resets {verb} {when}  in {countdown}"
    colored = f"{window_label} Resets {verb} {_green(when)}  in {_green(countdown)}"
    return plain, colored


def render_accounts_popup(
    accounts: Sequence[
        Tuple[str, Optional[float], Optional[float], Optional[float], Optional[float]]
    ],
    active_label: str,
    active_ses_pct: Optional[float],
    now: Optional[float] = None,
) -> str:
    """Aligned, ANSI-green-accented popup body: one row per deduped account.

    ``accounts`` is every deduped account as an already-extracted
    ``(label, five_h_pct, seven_d_pct, five_h_reset_epoch, seven_d_reset_epoch)``
    5-tuple — the CLI handler (:func:`cc_tmux.cli.cmd_accounts_popup`) builds
    these via :func:`cc_tmux.usage.dedupe_credentials` +
    :func:`cc_tmux.usage._account_label`/:func:`cc_tmux.usage._extract_util`/
    :func:`cc_tmux.usage._extract_reset_at`, so this function stays pure with
    no credential-dict shape knowledge. Every row renders ``5H:xx% 7D:xx%``;
    the row whose label equals ``active_label`` (exact match) is additionally
    prefixed with ``SES:xx%`` (from ``active_ses_pct``) and marked with a
    leading ``*`` — SES is a property of the currently-focused pane, not of a
    credential in the abstract (proposal's "SES is not an account-level
    metric"), so it is supplied by the caller rather than looked up
    per-account here. Every percentage is wrapped in :func:`_green`.

    Below EVERY account's summary row (active or not — Leo's ask, not just
    the starred one) sit up to two indented, aligned, green-accented
    reset-time lines, one per window, each omitted independently when that
    window's reset time hasn't been polled yet (see :func:`_format_reset_line`),
    followed by a full-width ``─`` rule separating this account's block from
    the next (Leo's 2026-07-13 readability ask):

        * leo@x.dev·8   SES:36% 5H:36% 7D:71%
             5H Resets at      03:45 pm  in 02:14
             7D Resets on  Sat 03:45 pm  in 03:14:22
        ──────────────────────────────────────────

    Percent formatting reuses :func:`pct_for` (``--`` for an absent/unpolled
    value); :func:`color_for` (severity RED/YELLOW/CYAN) is deliberately NOT
    used here — Leo wants every number/datetime uniformly green, not
    escalated by utilization (contrast :func:`render_session_bar`'s tmux
    status-format segment, which DOES escalate). ``now`` is the
    caller-supplied wall-clock epoch (``time.time()`` in production,
    injectable for deterministic tests) — same DI pattern
    :func:`render_tabs_row` already uses for its own ``now`` param.

    Pure function of its inputs. Empty ``accounts`` -> ``""`` (fail-open: an
    unreachable nexus-agent, or a payload with zero deduped/labelled
    credentials, renders nothing rather than an empty/garbled popup).
    """
    if not accounts:
        return ""
    t = time.time() if now is None else now

    rows: List[Tuple[str, str, str, bool, str, str, str, str]] = []
    for label, five_h, seven_d, five_h_reset, seven_d_reset in accounts:
        is_active = bool(active_label) and label == active_label
        five_h_str, seven_d_str = pct_for(five_h), pct_for(seven_d)
        tail_plain = f"5H:{five_h_str} 7D:{seven_d_str}"
        tail = f"5H:{_green(five_h_str)} 7D:{_green(seven_d_str)}"
        if is_active:
            ses_str = pct_for(active_ses_pct)
            tail_plain = f"SES:{ses_str} {tail_plain}"
            tail = f"SES:{_green(ses_str)} {tail}"
        reset_5h_plain, reset_5h = _format_reset_line("5H", "at", five_h_reset, t, with_day=False)
        reset_7d_plain, reset_7d = _format_reset_line("7D", "on", seven_d_reset, t, with_day=True)
        rows.append((label, tail_plain, tail, is_active, reset_5h_plain, reset_5h, reset_7d_plain, reset_7d))

    label_width = max(len(label) for label, *_rest in rows)
    lines: List[str] = []
    for label, tail_plain, tail, is_active, reset_5h_plain, reset_5h, reset_7d_plain, reset_7d in rows:
        marker = "* " if is_active else "  "
        summary_plain = f"{marker}{label.ljust(label_width)}  {tail_plain}"
        lines.append(f"{marker}{label.ljust(label_width)}  {tail}")
        block_width = len(summary_plain)
        if reset_5h_plain:
            lines.append(f"   {reset_5h}")
            block_width = max(block_width, len(reset_5h_plain) + 3)
        if reset_7d_plain:
            lines.append(f"   {reset_7d}")
            block_width = max(block_width, len(reset_7d_plain) + 3)
        lines.append("─" * block_width)
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Window-tabs row (cc-tmux-tabs-and-rename-fix)
#
# The per-window `window-status-format`/`window-status-current-format`
# mechanism never re-evaluates its nested `#()` job on this tmux version (3.6a)
# — confirmed via /openspec:explore runtime evidence — so the animated tab
# icon it was meant to drive never actually moves. This renders the ENTIRE
# tabs row as one string from a single top-level status-format slot instead
# (the same slot class row 2/row 3 already use), which DOES re-evaluate its
# `#()` job on every status-bar refresh. Same daemon-free, status-interval-
# driven cadence as animated_icon/render_session_bar/render_beads_bar — no
# background process, no timer of its own.
# ---------------------------------------------------------------------------


def render_tabs_row(windows: Sequence[object], active_window_id: str, now: float) -> str:
    """Row-1 status-format string: one ``index:icon name`` segment per window.

    ``windows`` is any sequence of objects with ``id``/``index``/``name``/
    ``state`` attributes (duck-typed via ``getattr``, matching this module's
    other pane/window-consuming functions — see :func:`inbox_rows`); the
    canonical source is :func:`cc_tmux.tmux.get_window_tabs`. ``state`` is the
    window's highest-priority tracked ``@cc-state``, or ``""`` for a window
    with no tracked Claude pane — that window renders with no icon (matches
    :func:`cmd_window_icon`'s existing "untracked window -> no icon" contract),
    just its bare ``index:name``. ``now`` is the caller-supplied wall-clock
    time (``time.time()`` in production) handed straight to
    :func:`resolve_tab_icon` (which falls through to :func:`animated_icon` for
    the animation frame when no sub-agent is active — cc-tmux-subagent-tab-icon)
    — same invocation pattern :func:`cc_tmux.cli.cmd_window_icon` already uses,
    reused here per window rather than re-deriving the state->glyph mapping.
    ``fg``/``bg`` (duck-typed via ``getattr``, defaulting to ``0``/``[]``) are
    the window's sub-agent counts; ``bg`` MUST already be pruned by the caller
    (:func:`cc_tmux.cli._build_tabs_row`) before this is called — same
    contract :func:`resolve_tab_icon` documents.

    The active window (``id == active_window_id``) renders bold CYAN; every
    other window renders DIM — the same semantic colour pair
    :func:`render_session_bar` uses for emphasis vs. identity text, reused
    here rather than inventing a third convention. No wrapping bg colour is
    applied (theme ``.conf`` files wrap the whole row, same as
    ``status-format[1]``/``[2]`` — see :func:`render_session_bar`).

    Each segment is wrapped in ``#[range=window|<index>]``/``#[norange]`` —
    the same range markup tmux's native window-status rendering emits, which
    is what makes the default ``MouseDown1Status`` binding (``switch-client
    -t =``) know which window a click landed on. Replacing tmux's native
    per-window rendering with this custom job (see module docstring) means we
    must emit that markup ourselves or clicks land nowhere.

    Pure function of its inputs (no tmux/subprocess). Empty ``windows`` ->
    ``""`` (nothing to show).
    """
    segments: List[str] = []
    for w in windows:
        state = getattr(w, "state", "") or ""
        fg_count = getattr(w, "fg", 0) or 0
        bg_count = len(getattr(w, "bg", None) or [])
        icon = resolve_tab_icon(state, now, fg_count, bg_count)
        icon_part = f"{icon} " if icon else ""
        index = getattr(w, "index", "")
        name = getattr(w, "name", "")
        label = f"{index} {icon_part}{name}"

        is_active = active_window_id and getattr(w, "id", None) == active_window_id
        colour = f"{CYAN},bold" if is_active else DIM
        segments.append(
            f"#[fg={colour}]#[range=window|{index}] {label} #[norange]#[default]"
        )
    return "".join(segments)


# Row 3 stale threshold: the roadmap-pulse cache is written under a ~5-minute
# SWR contract (rules/TOOLING.md Ambient Surfacing); 15 minutes = three missed
# refresh cycles, at which point the counts get a trailing age marker so stale
# data never masquerades as current (plan 006 / BEADS-01).
BEADS_STALE_AFTER_SEC = 900.0

# Row 3 "high count" thresholds for the unarchived-proposal / blocked-bead
# halves (cc-tmux-row3-openspec-beads-format task 2.3): 5 is roughly a day's
# worth of shipped-but-unarchived specs, or blocked beads piling up, before
# it stops being "a couple things to clean up next session" and becomes a
# RED alarm. Any count > 0 is already YELLOW; these constants only gate the
# YELLOW -> RED escalation, mirroring BEADS_STALE_AFTER_SEC's
# documented-constant convention above.
BEADS_UNARCHIVED_HIGH = 5
BEADS_BLOCKED_HIGH = 5

_BEADS_SEP = f"#[fg={DIM}] | "


def _threshold_color(n: int, high: int) -> str:
    """DIM at ``0`` (healthy), YELLOW above ``0``, RED at/above ``high``."""
    if n <= 0:
        return DIM
    if n >= high:
        return RED
    return YELLOW


def _pulse_segment(
    label: str,
    n1: int,
    word1: str,
    n2: int,
    word2: str,
    age_sec: Optional[float],
    high: int,
) -> str:
    """One ``"label: N word1 M word2 (age)"`` segment, ``n2`` threshold-colored.

    ``n1`` (open/ready) is purely informational and stays DIM. ``n2``
    (unarchived/blocked) is a health signal, colored via
    :func:`_threshold_color`. ``age_sec`` beyond ``BEADS_STALE_AFTER_SEC``
    appends a DIM trailing ``" (<duration>)"`` marker, independent per segment.
    """
    n2_color = _threshold_color(n2, high)
    seg = (
        f"#[fg={DIM}]{label}: {n1} {word1} "
        f"#[fg={n2_color}]{n2}#[fg={DIM}] {word2}"
    )
    if age_sec is not None and age_sec > BEADS_STALE_AFTER_SEC:
        seg += f" ({format_duration(age_sec)})"
    return seg


def render_beads_bar(
    openspec_open: Optional[int],
    openspec_unarchived: Optional[int],
    beads_ready: Optional[int],
    beads_blocked: Optional[int],
    openspec_age_sec: Optional[float] = None,
    beads_age_sec: Optional[float] = None,
) -> str:
    """Row-3 status-format string from parsed roadmap-pulse counts, or ``''``.

    Renders up to two ``|``-separated segments:
    ``openspec: {open} open {unarchived} unarchived ({age})`` and
    ``beads: {ready} ready {blocked} blocked ({age})`` (cc-tmux-row3-openspec-
    beads-format task 2.3), replacing the prior raw-pulse-line passthrough.
    Each half is independent and fail-open: a half whose pair of counts is not
    BOTH present (``None`` from an absent/malformed cache line — see
    :func:`cc_tmux.cli._parse_roadmap_pulse_counts`) is omitted entirely
    rather than rendered with a placeholder, so a broken ``beads:`` line never
    blanks a valid ``openspec:`` half and vice versa. Both halves omitted (no
    cache, or nothing parsed) -> ``""``, matching the row's original
    "no cache -> empty" contract.

    ``unarchived``/``blocked`` are colored by semantic threshold
    (:func:`_threshold_color`; DIM healthy, YELLOW above 0, RED at/above
    :data:`BEADS_UNARCHIVED_HIGH`/:data:`BEADS_BLOCKED_HIGH`); ``open``/
    ``ready`` stay DIM (informational, not a health signal).
    ``openspec_age_sec``/``beads_age_sec`` are each independent cache-file
    ages in seconds — both halves read the SAME cache file's single mtime
    today (so callers typically pass the same value for both), but the
    per-segment marker is forward-compatible with a future per-half cache
    split (plan 006 / BEADS-01) with no further render.py change needed.
    Ages beyond ``BEADS_STALE_AFTER_SEC`` append a DIM trailing
    ``" (<duration>)"`` marker via :func:`format_duration`, independently per
    segment.

    Pure function of its inputs (no tmux/subprocess).
    """
    segments = []
    if openspec_open is not None and openspec_unarchived is not None:
        segments.append(
            _pulse_segment(
                "openspec", openspec_open, "open", openspec_unarchived, "unarchived",
                openspec_age_sec, BEADS_UNARCHIVED_HIGH,
            )
        )
    if beads_ready is not None and beads_blocked is not None:
        segments.append(
            _pulse_segment(
                "beads", beads_ready, "ready", beads_blocked, "blocked",
                beads_age_sec, BEADS_BLOCKED_HIGH,
            )
        )

    if not segments:
        return ""
    return _BEADS_SEP.join(segments) + "#[default]"
