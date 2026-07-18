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

## Empty-field sentinel (`-`)

`encode()` substitutes the single-character SENTINEL (`-`) for every empty
field, and `decode()` maps it back to `""`. This is NOT cosmetic: the cmux
sidebar's Swift interpreter silently ignores
`.split(separator:"|", omittingEmptySubsequences: false)` and always collapses
empty subsequences (live-verified, task 3.2), so a string with empty middle
fields (the common case — most fields are empty most of the time) yields fewer
than 8 segments and positional field indexing breaks. Sentinelling every empty
field guarantees the encoded string has ZERO empty segments, so the reader's
`.split(separator: "|")` reliably returns all 8 and can index positionally
(task 3.3). A raw field value that is *literally* `-` is not distinguishable
from empty after decode — acceptable because no producer emits a bare `-`
(openspec/beads summaries are count strings, usage is digits, state/wait_reason
are controlled vocab, epoch is numeric). Old-format strings written before this
scheme (real empty segments, `CC1|idle||123||||`) still decode correctly in
Python (str.split does not collapse) and self-heal to the sentinel form on the
first re-write.
"""

from __future__ import annotations

MAGIC = "CC1"
FIELD_COUNT = 8  # magic + 7 real fields
SENTINEL = "-"  # stands in for an empty field so the Swift .split never collapses

_EMPTY_FIELDS = {
    "state": "",
    "wait_reason": "",
    "epoch": "",
    "openspec": "",
    "beads": "",
    "usage_5h": "",
    "usage_7d": "",
}


def _desentinel(value: str) -> str:
    """Map the empty-field SENTINEL back to an empty string (see module docstring)."""
    return "" if value == SENTINEL else value


def _sentinel(value: str) -> str:
    """Substitute SENTINEL for an empty field so the Swift .split never collapses it."""
    return SENTINEL if value == "" else value


def decode(description: str) -> dict:
    """Best-effort decode; unknown/malformed input -> all-empty dict, never raises.

    Maps the empty-field SENTINEL (`-`) back to `""`, and tolerates the pre-sentinel
    old format (real empty segments) transparently — an empty segment is already `""`.
    """
    empty = dict(_EMPTY_FIELDS)
    if not description or not description.startswith(f"{MAGIC}|"):
        return empty
    parts = description.split("|")
    if len(parts) != FIELD_COUNT:
        return empty
    _, state, wait_reason, epoch, openspec, beads, u5, u7 = parts
    return {
        "state": _desentinel(state),
        "wait_reason": _desentinel(wait_reason),
        "epoch": _desentinel(epoch),
        "openspec": _desentinel(openspec),
        "beads": _desentinel(beads),
        "usage_5h": _desentinel(u5),
        "usage_7d": _desentinel(u7),
    }


def encode(fields: dict) -> str:
    """Re-serialize a full field dict (after a caller has merged its owned fields in).

    Every empty field is emitted as the SENTINEL (`-`) so the encoded string has no
    empty segments — required for the Swift reader's positional split (module docstring).
    """
    return "|".join(
        [
            MAGIC,
            _sentinel(fields.get("state", "") or ""),
            _sentinel(fields.get("wait_reason", "") or ""),
            _sentinel(fields.get("epoch", "") or ""),
            _sentinel(_sanitize(fields.get("openspec", ""))),
            _sentinel(_sanitize(fields.get("beads", ""))),
            _sentinel(fields.get("usage_5h", "") or ""),
            _sentinel(fields.get("usage_7d", "") or ""),
        ]
    )


def _sanitize(value: str) -> str:
    """Strip delimiter + newlines from a free-text field before encoding (see § Sanitization)."""
    if not isinstance(value, str):
        return ""
    return value.replace("|", "/").replace("\n", " ").replace("\r", "")
