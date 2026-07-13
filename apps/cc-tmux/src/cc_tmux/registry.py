"""Project short-code resolution from the dotfiles project registry.

Used only by the opt-in ``title`` window-rename format (see
:func:`cc_tmux.cli._title_window_name`) to prefix the tmux window name with the
same short code ``home/projects.toml`` already assigns each project — the same
registry ``scripts/lib/registry.sh`` resolves for the shell-side consumers
(Raycast, cmux, mux-remote).

Stdlib-only, matching the rest of cc-tmux (see pyproject.toml). ``tomllib`` is
3.11+; on a stray 3.10 interpreter, or when this plugin runs standalone outside
the personal dotfiles repo (no registry file at all), resolution fails open to
no codes — never an exception (invariant 5, tmux.py).
"""

from __future__ import annotations

import os
from typing import Dict

try:
    import tomllib
except ModuleNotFoundError:  # Python 3.10 — see module docstring
    tomllib = None  # type: ignore[assignment]

_DEFAULT_DOTFILES = os.path.expanduser("~/dev/personal/installfest")


def _registry_path() -> str:
    dotfiles = os.environ.get("DOTFILES") or _DEFAULT_DOTFILES
    return os.path.join(dotfiles, "home", "projects.toml")


def _load_path_to_code() -> Dict[str, str]:
    """``{absolute project path: code}`` from the registry. ``{}`` on any failure."""
    if tomllib is None:
        return {}
    path = _registry_path()
    if not os.path.isfile(path):
        return {}
    try:
        with open(path, "rb") as f:
            data = tomllib.load(f)
    except (OSError, ValueError):
        return {}
    home = os.path.expanduser("~")
    out: Dict[str, str] = {}
    for entry in data.get("projects", []):
        if not isinstance(entry, dict):
            continue
        code, rel_path = entry.get("code"), entry.get("path")
        if code and rel_path:
            # realpath, not normpath: several registry entries (e.g. "cc" ->
            # ".claude") are symlink aliases — ~/.claude -> ~/dev/cc. A pane's
            # shell cwd reports the REAL target path (`pwd` resolves symlinks
            # on cd), so matching on the un-resolved alias path silently
            # failed for any pane actually sitting in ~/dev/cc, blanking
            # row 3 (openspec/beads) for that whole project. realpath here
            # collapses both the registry entry and (in resolve_project_code)
            # the queried cwd to the same physical path regardless of which
            # alias either side used.
            out[os.path.realpath(os.path.join(home, rel_path))] = code
    return out


def resolve_project_code(cwd: str) -> str:
    """The registry short code owning ``cwd`` (longest-prefix match), or ``""``.

    ``cwd`` need not be the project root — any subdirectory resolves to its
    owning project's code, same as the shell consumers of this registry.
    Resolved via ``realpath`` (matching :func:`_load_path_to_code`'s own
    realpath-normalized keys) so a pane sitting in a symlink's real target
    still matches a registry entry recorded under the symlink alias — see
    that function's docstring for the concrete ``~/.claude`` -> ``~/dev/cc``
    case this fixes.
    """
    if not cwd:
        return ""
    norm_cwd = os.path.realpath(cwd)
    best_code, best_len = "", -1
    for proj_path, code in _load_path_to_code().items():
        if norm_cwd != proj_path and not norm_cwd.startswith(proj_path + os.sep):
            continue
        if len(proj_path) > best_len:
            best_code, best_len = code, len(proj_path)
    return best_code
