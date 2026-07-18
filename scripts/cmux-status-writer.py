#!/usr/bin/env python3
"""cmux-status-writer — periodic writer for the openspec/beads/usage smuggled fields.

Task [2.4] of `openspec/changes/add-cmux-sidebar-widgets/`. Populates the
`openspec`, `beads`, `usage_5h`, `usage_7d` segments of the CC1 encoding
(`docs/cmux-sidebar-encoding.md`) on every cmux workspace's `description`, via
`cmux workspace-action --action set-description` (confirmed in
`docs/cmux-workspace-action-api.md`).

Runs on Darwin only in practice — `cmux` is a macOS-native CLI
(`cmux-workspaces.sh`'s own Darwin gate documents this: cmux is the GUI app,
only reachable on the Mac it runs on). This script does not hard-block on
`uname` though; it fails open if the `cmux` binary just isn't on PATH, which
has the same effect. A workspace's PROJECT files may live on a different host
than the one running this writer (an SSH-backed workspace's
`current_directory` is a path on `remote.destination`, not on this Mac) —
openspec/beads status for such a workspace is gathered by shelling a read-only
command over `ssh <destination>` instead of assuming a local cwd.

Field ownership (`docs/cmux-sidebar-encoding.md` § Field ownership and
read-modify-write contract): this writer owns ONLY `openspec`, `beads`,
`usage_5h`, `usage_7d`. It read-modify-writes every description — decode,
overwrite only its own 4 fields, re-encode all 8 — so task [2.1]'s hook
writer's `state`/`wait_reason`/`epoch` fields are always preserved untouched
even though both writers target the same shared string.

Usage carrier (`docs/cmux-sidebar-encoding.md` § Usage Carrier, explicitly
deferred to this task): the workspace `cmux workspace list --json` marks
`selected: true` (the one currently focused in the cmux window) is the
carrier — real 5H/7D percent values are written only there; every OTHER
workspace gets its usage fields actively cleared to empty (never left as a
stale prior carrier's numbers once focus moves on). If no workspace is
selected (shouldn't normally happen), the first workspace in the list is the
fallback carrier so usage is never silently dropped everywhere.

Invocation:
    cmux-status-writer.py                    # sweep every workspace (periodic/default mode)
    cmux-status-writer.py --workspace <ref>   # update exactly one workspace (on-demand)
    cmux-status-writer.py --all               # force a full sweep even if $CMUX_WORKSPACE_ID is set

With no `--workspace`/`--all` flag and `$CMUX_WORKSPACE_ID` set in the
environment (i.e. invoked from inside a live cmux-bound shell, mirroring task
[2.1]'s hook dual-write gating convention), this restricts to that one
workspace automatically — the same script is both the periodic timer's full
sweep and a cheap on-demand single-workspace refresh.

Fail-open throughout (matches cc-tmux's own invariant 5, tmux.py's module
docstring): a missing `cmux` binary, an unreachable nx-agent, a dead SSH host,
or a single workspace's openspec/beads command failing never aborts the run or
raises — the affected field(s) are just left/written empty and the writer
moves on to the next workspace. Exit 0 always, idempotent, no interactive
input — safe for both on-demand and timer invocation
(`home/Library/LaunchAgents/com.leonardoacosta.cmux-status-writer.plist`).
"""

from __future__ import annotations

import argparse
import json
import os
import shlex
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Optional

INSTALLFEST_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(INSTALLFEST_ROOT / "scripts" / "lib"))
sys.path.insert(0, str(INSTALLFEST_ROOT / "apps" / "cc-tmux" / "src"))

from cmux_status_encoding import decode, encode  # noqa: E402

try:
    # Reuse cc-tmux's own nx-agent /credentials client (cached, deduped,
    # fail-open) rather than re-deriving the fetch — same repo, stdlib-only,
    # zero extra dependency. See usage.py's module docstring for the payload
    # shape and its dedupe rationale.
    from cc_tmux import usage as cc_usage  # noqa: E402
except Exception:  # noqa: BLE001 - fail open if cc-tmux isn't importable here
    cc_usage = None

OPENSPEC_STATUS_BIN = str(Path.home() / ".claude" / "scripts" / "bin" / "openspec-status")
CMD_TIMEOUT_SECS = 20
# Matches docs/cmux-sidebar-encoding.md's "~20 characters" recommendation for
# a sidebar-row summary, with a little slack for a two-state count string.
SUMMARY_MAX_CHARS = 24


def _run(args: list[str], *, cwd: Optional[str] = None, timeout: int = CMD_TIMEOUT_SECS) -> Optional[str]:
    """Run a command, return stripped stdout, or None on any failure (fail-open)."""
    try:
        proc = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    return proc.stdout.strip()


def _remote_run(host: str, remote_cwd: str, args: list[str], *, timeout: int = CMD_TIMEOUT_SECS) -> Optional[str]:
    """Run `args` on `host` inside `remote_cwd` over ssh. None on any failure."""
    command = " ".join(shlex.quote(a) for a in args)
    full = f"cd {shlex.quote(remote_cwd)} && {command}"
    return _run(
        ["ssh", "-o", "ConnectTimeout=8", "-o", "BatchMode=yes", host, full],
        timeout=timeout,
    )


def list_workspaces(cmux: str) -> list[dict]:
    """Every workspace in the current cmux window, or [] on any failure (fail-open)."""
    out = _run([cmux, "workspace", "list", "--json"])
    if not out:
        return []
    try:
        data = json.loads(out)
    except (json.JSONDecodeError, TypeError):
        return []
    workspaces = data.get("workspaces") if isinstance(data, dict) else None
    return workspaces if isinstance(workspaces, list) else []


def _truncate(value: str) -> str:
    if len(value) <= SUMMARY_MAX_CHARS:
        return value
    return value[: SUMMARY_MAX_CHARS - 1].rstrip(", ") + "…"


def openspec_summary(directory: str, *, ssh_host: Optional[str]) -> str:
    """Short 'N open, M in-progress'-style summary of unarchived proposals, or '' on failure."""
    args = [OPENSPEC_STATUS_BIN, "--json", "--no-enrich"]
    out = _remote_run(ssh_host, directory, args) if ssh_host else _run(args, cwd=directory)
    if not out:
        return ""
    try:
        specs = json.loads(out)
    except (json.JSONDecodeError, TypeError):
        return ""
    if not isinstance(specs, list) or not specs:
        return ""
    counts: dict[str, int] = {}
    for spec in specs:
        if not isinstance(spec, dict):
            continue
        state = spec.get("state") or "unknown"
        counts[state] = counts.get(state, 0) + 1
    if not counts:
        return ""
    # Busiest-state-first, name as tiebreak (deterministic); cap at 2 states
    # to stay a sidebar-row-sized summary, not a full report.
    ordered = sorted(counts.items(), key=lambda kv: (-kv[1], kv[0]))
    return ", ".join(f"{n} {state}" for state, n in ordered[:2])


def _json_list_len(raw: Optional[str]) -> Optional[int]:
    if raw is None:
        return None
    try:
        data = json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return None
    return len(data) if isinstance(data, list) else None


def beads_summary(directory: str, *, ssh_host: Optional[str]) -> str:
    """Short 'N ready, M blocked'-style summary, or '' on failure/no beads DB here."""
    ready_args = ["bd", "ready", "--json", "-n", "0"]
    blocked_args = ["bd", "list", "--status", "blocked", "--json", "-n", "0"]
    if ssh_host:
        ready_out = _remote_run(ssh_host, directory, ready_args)
        blocked_out = _remote_run(ssh_host, directory, blocked_args)
    else:
        ready_out = _run(ready_args, cwd=directory)
        blocked_out = _run(blocked_args, cwd=directory)

    ready = _json_list_len(ready_out)
    blocked = _json_list_len(blocked_out)
    if ready is None and blocked is None:
        return ""
    parts = []
    if ready is not None:
        parts.append(f"{ready} ready")
    if blocked is not None:
        parts.append(f"{blocked} blocked")
    return ", ".join(parts)


def active_usage_pct() -> tuple[str, str]:
    """('', '') on any failure — else (usage_5h, usage_7d) as integer-percent strings."""
    if cc_usage is None:
        return "", ""
    try:
        _label, u5, u7 = cc_usage.active_usage()
    except Exception:  # noqa: BLE001 - fail open, mirrors cc_tmux's own invariant 5
        return "", ""
    u5_str = "" if u5 is None else str(round(u5 * 100))
    u7_str = "" if u7 is None else str(round(u7 * 100))
    return u5_str, u7_str


def gather_status(ws: dict) -> tuple[str, str]:
    """(openspec_summary, beads_summary) for one workspace, '' / '' if not resolvable."""
    directory = ws.get("current_directory")
    if not isinstance(directory, str) or not directory:
        return "", ""

    remote = ws.get("remote") or {}
    ssh_host = None
    if isinstance(remote, dict) and remote.get("enabled") and remote.get("connected"):
        dest = remote.get("destination")
        if isinstance(dest, str) and dest:
            ssh_host = dest

    if ssh_host:
        return (
            _truncate(openspec_summary(directory, ssh_host=ssh_host)),
            _truncate(beads_summary(directory, ssh_host=ssh_host)),
        )

    # Local (or remote-but-disconnected, or a remote path that doesn't exist
    # on THIS host): only resolve if the directory is actually present here.
    # A disconnected/foreign path just yields empty fields (fail-open — "not
    # applicable to this workspace" per the encoding doc, not an error).
    if not Path(directory).is_dir():
        return "", ""
    return (
        _truncate(openspec_summary(directory, ssh_host=None)),
        _truncate(beads_summary(directory, ssh_host=None)),
    )


def update_workspace(cmux: str, ws: dict, *, is_carrier: bool, usage_5h: str, usage_7d: str) -> bool:
    """Read-modify-write one workspace's description. Returns True on success/no-op."""
    ref = ws.get("ref")
    if not isinstance(ref, str) or not ref:
        return False

    openspec_val, beads_val = gather_status(ws)

    current = decode(ws.get("description") or "")
    current["openspec"] = openspec_val
    current["beads"] = beads_val
    current["usage_5h"] = usage_5h if is_carrier else ""
    current["usage_7d"] = usage_7d if is_carrier else ""
    new_description = encode(current)

    if new_description == (ws.get("description") or ""):
        return True  # already up to date, nothing to write

    result = _run(
        [
            cmux,
            "workspace-action",
            "--workspace",
            ref,
            "--action",
            "set-description",
            "--description",
            new_description,
        ]
    )
    return result is not None


def main(argv: Optional[list[str]] = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--workspace", metavar="REF", help="update only this workspace ref (e.g. workspace:3)")
    parser.add_argument(
        "--all", action="store_true", help="force a full sweep even if $CMUX_WORKSPACE_ID is set"
    )
    args = parser.parse_args(argv)

    cmux = shutil.which("cmux")
    if not cmux:
        print("cmux-status-writer: cmux CLI not found on PATH, nothing to do", file=sys.stderr)
        return 0

    all_workspaces = list_workspaces(cmux)
    if not all_workspaces:
        print("cmux-status-writer: no workspaces (or cmux unreachable)", file=sys.stderr)
        return 0

    target_ref = args.workspace
    if not target_ref and not args.all:
        target_ref = os.environ.get("CMUX_WORKSPACE_ID") or None

    if target_ref:
        targets = [w for w in all_workspaces if w.get("ref") == target_ref]
        if not targets:
            print(f"cmux-status-writer: workspace {target_ref} not found", file=sys.stderr)
            return 0
    else:
        targets = all_workspaces

    usage_5h, usage_7d = active_usage_pct()

    carrier_ref = None
    for w in all_workspaces:
        if w.get("selected") is True:
            carrier_ref = w.get("ref")
            break
    if carrier_ref is None and all_workspaces:
        carrier_ref = all_workspaces[0].get("ref")

    ok = 0
    for ws in targets:
        try:
            if update_workspace(cmux, ws, is_carrier=(ws.get("ref") == carrier_ref), usage_5h=usage_5h, usage_7d=usage_7d):
                ok += 1
        except Exception as exc:  # noqa: BLE001 - one workspace's failure must not abort the sweep
            print(f"cmux-status-writer: {ws.get('ref')} failed: {exc}", file=sys.stderr)

    print(f"cmux-status-writer: updated {ok}/{len(targets)} workspace(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
