"""cc-tmux: Claude Code + tmux plugin.

Tracks every Claude Code pane's state (waiting/idle/active) in tmux pane options
— the single source of truth — and provides priority cycling, jump-back, a
notification inbox, OS notifications, a status segment, and a dispatch conductor.

Stdlib-only, Python 3.10+. The public surface used across modules:
  * :mod:`cc_tmux.tmux`     — PaneInfo, get_hop_panes(), set_pane_state(), ...
  * :mod:`cc_tmux.priority` — STATE_PRIORITY, cycle selection, sort ordering
  * :mod:`cc_tmux.cli`      — main() entrypoint
"""

from __future__ import annotations

__version__ = "0.1.0"
