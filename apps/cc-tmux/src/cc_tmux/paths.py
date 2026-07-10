"""Filesystem path detection for cc-tmux.

Two jobs:

1. Locate the user's ``tmux.conf`` across the common layouts (XDG config home,
   the legacy ``~/.tmux.conf``, and an explicit ``$TMUX_CONF`` override) so the
   install/entrypoint layer can find where to wire the plugin.
2. Locate this plugin's own clone directory (the ``apps/cc-tmux`` tree, or its
   deployed copy at ``~/.tmux/plugins/cc-tmux``) so sibling assets
   (``cc-tmux.tmux``, ``hooks/``, ``skills/``) can be resolved relative to the
   installed package regardless of how it was invoked.

All detection is pure and side-effect free: it never creates files. Functions
return the first existing candidate, or ``None`` when nothing is found, so a
caller can fail open.
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import List, Optional


def _home() -> Path:
    return Path(os.path.expanduser("~"))


def _xdg_config_home() -> Path:
    xdg = os.environ.get("XDG_CONFIG_HOME", "").strip()
    if xdg:
        return Path(xdg)
    return _home() / ".config"


def tmux_conf_candidates() -> List[Path]:
    """Ordered list of every place a tmux.conf might live.

    Order reflects precedence tmux itself uses / the community convention:
    an explicit env override, then the XDG location, then the legacy dotfile.
    """
    candidates: List[Path] = []

    override = os.environ.get("TMUX_CONF", "").strip()
    if override:
        candidates.append(Path(override))

    # XDG: ~/.config/tmux/tmux.conf (the layout this repo deploys).
    candidates.append(_xdg_config_home() / "tmux" / "tmux.conf")

    # Legacy: ~/.tmux.conf
    candidates.append(_home() / ".tmux.conf")

    return candidates


def find_tmux_conf() -> Optional[Path]:
    """First existing tmux.conf, or None. Never raises."""
    for cand in tmux_conf_candidates():
        try:
            if cand.is_file():
                return cand
        except OSError:
            continue
    return None


def plugin_dir_candidates() -> List[Path]:
    """Ordered list of places the cc-tmux plugin tree might be found.

    The most reliable answer is derived from this module's own location
    (``__file__`` -> ``src/cc_tmux/paths.py`` -> package root two levels up),
    since that is where the code is actually running from. The deployed clone
    path is included as a fallback for callers resolving assets by convention.
    """
    candidates: List[Path] = []

    # Derived from this file: src/cc_tmux/paths.py -> apps/cc-tmux/
    try:
        pkg_root = Path(__file__).resolve().parents[2]
        candidates.append(pkg_root)
    except (OSError, IndexError):
        pass

    # Explicit override (set by the tmux entrypoint / install script).
    override = os.environ.get("CC_TMUX_PLUGIN_DIR", "").strip()
    if override:
        candidates.append(Path(override))

    # Conventional manual-clone deploy target (mirrors tmux-which-key).
    candidates.append(_home() / ".tmux" / "plugins" / "cc-tmux")

    # De-duplicate while preserving order.
    seen: set[str] = set()
    unique: List[Path] = []
    for c in candidates:
        key = str(c)
        if key not in seen:
            seen.add(key)
            unique.append(c)
    return unique


def find_plugin_dir() -> Optional[Path]:
    """First plugin dir that looks real (contains ``src/cc_tmux``). Never raises."""
    for cand in plugin_dir_candidates():
        try:
            if (cand / "src" / "cc_tmux").is_dir():
                return cand
        except OSError:
            continue
    return None
