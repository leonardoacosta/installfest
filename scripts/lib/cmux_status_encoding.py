"""Shared CC1 smuggled-field encode/decode helpers.

Single source of truth for the `description`-field wire format documented in
`docs/cmux-sidebar-encoding.md` — do not reimplement this per-caller. Both
Python writers share it:

* `scripts/cmux-status-writer.py` (task [2.4]) — owns `openspec`, `beads`,
  `usage_5h`, `usage_7d`.
* `apps/cc-tmux/src/cc_tmux/tmux.py`'s future hook dual-write (task [2.1]) —
  owns `state`, `wait_reason`, `epoch`. Not yet landed; when it is, it should
  import this module rather than duplicating `decode`/`encode`.

Every function is pure and fail-open: `decode()` never raises (malformed or
absent input -> the all-empty field dict), and `encode()` always produces a
well-formed 8-segment string. See the doc's "Python writer contract" section —
this file IS that contract, not a re-derivation of it.
"""

from __future__ import annotations

MAGIC = "CC1"
FIELD_COUNT = 8  # magic + 7 real fields

_EMPTY_FIELDS = {
    "state": "",
    "wait_reason": "",
    "epoch": "",
    "openspec": "",
    "beads": "",
    "usage_5h": "",
    "usage_7d": "",
}


def decode(description: str) -> dict:
    """Best-effort decode; unknown/malformed input -> all-empty dict, never raises."""
    empty = dict(_EMPTY_FIELDS)
    if not description or not description.startswith(f"{MAGIC}|"):
        return empty
    parts = description.split("|")
    if len(parts) != FIELD_COUNT:
        return empty
    _, state, wait_reason, epoch, openspec, beads, u5, u7 = parts
    return {
        "state": state,
        "wait_reason": wait_reason,
        "epoch": epoch,
        "openspec": openspec,
        "beads": beads,
        "usage_5h": u5,
        "usage_7d": u7,
    }


def encode(fields: dict) -> str:
    """Re-serialize a full field dict (after a caller has merged its owned fields in)."""
    return "|".join(
        [
            MAGIC,
            fields.get("state", ""),
            fields.get("wait_reason", ""),
            fields.get("epoch", ""),
            _sanitize(fields.get("openspec", "")),
            _sanitize(fields.get("beads", "")),
            fields.get("usage_5h", ""),
            fields.get("usage_7d", ""),
        ]
    )


def _sanitize(value: str) -> str:
    """Strip delimiter + newlines from a free-text field before encoding (see § Sanitization)."""
    if not isinstance(value, str):
        return ""
    return value.replace("|", "/").replace("\n", " ").replace("\r", "")
