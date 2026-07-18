# cmux Smuggled-Field Encoding Scheme

> Single source of truth for the `description`-field encoding shared between the Python writers
> (`apps/cc-tmux/src/cc_tmux/tmux.py`'s hook dual-write, task `[2.1]`; the periodic
> openspec/beads/usage writer, task `[2.4]`) and the Swift reader (`claude-sessions.swift.tmpl`,
> tasks `[3.2]`/`[3.3]`). Do not reinvent field order, delimiters, or parsing rules per-caller —
> change this doc first, then update both ends to match.

## Why not JSON

cmux's custom-sidebar SwiftUI interpreter is a "growing subset" with no confirmed structured
JSON decoder in scope for sidebar files — only string primitives (`.split(separator:)`,
`.hasPrefix`, `.contains`, `.uppercased`/`.lowercased`). The encoding below is designed to be
fully parseable with those primitives alone: a fixed-order, single-character-delimited string,
no nesting, no escaping beyond one sanitization rule (see § Sanitization).

## Format

```
CC1|<state>|<wait_reason>|<epoch>|<openspec>|<beads>|<usage_5h>|<usage_7d>
```

Eight `|`-delimited segments, always present in this exact order. A field with no value is an
**empty segment** (two consecutive delimiters), never an omitted one — the reader always expects
exactly 8 segments after splitting.

| # | Field | Values | Empty means |
|---|-------|--------|--------------|
| 0 | Magic + version | literal `CC1` | n/a — always present, always exactly `CC1` |
| 1 | `state` | `idle` \| `active` \| `waiting` | state not yet known (fresh workspace, no hook fire yet) |
| 2 | `wait_reason` | `question` \| `plan` \| `permission` \| `elicitation` | not currently `waiting`, or reason unknown |
| 3 | `epoch` | integer epoch seconds (string) | last-transition time unknown |
| 4 | `openspec` | short sanitized summary string | no openspec data written yet / not applicable to this workspace |
| 5 | `beads` | short sanitized summary string | no beads data written yet / not applicable to this workspace |
| 6 | `usage_5h` | integer percent `0`-`100` (string) | this workspace is not the usage carrier (see § Usage Carrier), or usage unpolled |
| 7 | `usage_7d` | integer percent `0`-`100` (string) | same as `usage_5h` |

`state` mirrors `@cc-state` in `apps/cc-tmux/src/cc_tmux/tmux.py` (`OPT_STATE`); `wait_reason`
mirrors `@cc-wait-reason` (only 4 values, matching `tmux.py`'s existing vocabulary — the
sidebar spec only gives `permission` a distinct visual treatment today, but the encoding carries
all 4 so a future UI change doesn't require a format bump). `epoch` mirrors `@cc-timestamp`'s
"only real transitions restamp, re-asserts do not" semantics.

### Magic + version prefix (`CC1`)

The literal `CC1` segment lets the Swift reader reject anything that isn't this scheme *before*
attempting to split further — cmux's own default `description` value, a stale field from a
different tool, or plain garbage all fail a `.hasPrefix("CC1|")` check and render as "no state
indicator" (the spec's unparseable-description scenario), never a crash.

**Version bump rule**: bump to `CC2` (and so on) only on a breaking shape change — a field added,
removed, or reordered. Adding a *new value* to an existing field's vocabulary (e.g. a 5th
`wait_reason`) does NOT require a bump; the reader's per-field checks already treat an unrecognized
value the same as absent (see § Swift Reader Contract). A reader that only recognizes `CC1` and
sees `CC2|...` MUST treat it as unparseable (fail closed on version skew), not attempt to parse it
positionally — this is what makes the version bump meaningful instead of decorative.

## Field ownership and read-modify-write contract

No single writer owns all 8 fields, and a workspace's `description` is one shared string — two
writers touching the same field on the same workspace is a real (if occasional) situation, not
a hypothetical:

| Fields | Owner | Write frequency |
|--------|-------|------------------|
| `state`, `wait_reason`, `epoch` | cc-tmux hook dual-write (`[2.1]`) | Every hook-driven state transition (`SessionStart`, prompt-submit, `permission_prompt` notification, `Stop`) |
| `openspec`, `beads` | Periodic writer (`[2.4]`) | Polling cadence TBD by `[2.4]` |
| `usage_5h`, `usage_7d` | Periodic writer (`[2.4]`), carrier workspace only | Polling cadence TBD by `[2.4]` |

Because both writers can target the same workspace's `description`, **every writer MUST
read-modify-write, never blind-overwrite the whole string**:

1. Fetch the workspace's current `description` (via whatever `cmux workspace-action` read
   confirmed in task `[1.3]` exposes).
2. Attempt to decode it per this scheme. If it doesn't match (wrong prefix, wrong segment count,
   or first invocation ever) — start from an all-empty field set (`CC1||||||||` is NOT correct;
   see worked example below for the true all-empty form).
3. Overwrite only the fields this writer owns; leave every other field exactly as decoded.
4. Re-encode all 8 segments in order and write the full string back.

This is a plain read-then-write, not an atomic compare-and-swap — a race between the hook writer
and the periodic writer landing within the same instant can drop one side's update. This is
accepted as consistent with the existing fail-open posture (`tmux.py`'s own invariant 5: a failed
or lost write never blocks Claude, and self-heals on the next tick/transition) — do not add
locking to close it.

## Usage Carrier

The usage figures (`usage_5h`/`usage_7d`) are **account-global**, not per-workspace, but this
encoding has no separate global channel — only per-workspace `description` fields exist. The
periodic writer (`[2.4]`) therefore designates exactly one workspace as the **usage carrier** and
writes real values into `usage_5h`/`usage_7d` only on that workspace's description; every other
workspace's `description` carries empty segments for those two fields. Carrier-selection strategy
(current-focused workspace, a fixed workspace, etc.) is `[2.4]`'s decision, out of scope here —
this doc only fixes the wire format both ends must agree on.

The Swift sidebar's compact usage footer (`[3.3]`) is rendered once for the whole panel (not per
row): it scans all decoded workspace rows and renders from whichever one has non-empty
`usage_5h`/`usage_7d` — see the spec's "missing usage data renders no footer" scenario for the
no-carrier-found case.

## Sanitization

`openspec` and `beads` are free-text summaries produced by other tooling, not hand-authored — the
writer MUST sanitize before encoding:

- Strip/replace any literal `|` in the value (e.g. replace with `/`) — an unescaped delimiter in a
  free-text field would shift every subsequent field out of position.
- Strip newlines and control characters.
- Recommended (not enforced by the reader): keep each summary under ~20 characters. This is a
  sidebar row, not a detail panel — the Swift reader does not truncate, so an overlong value is a
  writer-side rendering concern, not a parse error.

## Worked examples

All-empty (fresh workspace, no writer has ever run):

```
CC1|||||||
```

(8 segments: `CC1`, then 7 empty strings — 7 delimiters after `CC1`.)

Idle, no wait reason, no openspec/beads/usage data yet:

```
CC1|idle||1737158765||||
```

Active, mid-transition, still no openspec/beads/usage data:

```
CC1|active||1737158812||||
```

Waiting on a permission prompt, openspec/beads populated, NOT the usage carrier:

```
CC1|waiting|permission|1737159001|3 open, 1 approved|12 ready, 2 blocked||
```

Idle, full data including usage (this workspace IS the carrier):

```
CC1|idle||1737159050|3 open, 1 approved|12 ready, 2 blocked|68|47
```

Field-by-field breakdown of the last example:

| Segment index | Raw value | Meaning |
|---|---|---|
| 0 | `CC1` | Magic/version — confirms this is our scheme |
| 1 | `idle` | Claude session is idle |
| 2 | `` (empty) | Not waiting, so no wait-reason |
| 3 | `1737159050` | Epoch seconds of the last real state transition |
| 4 | `3 open, 1 approved` | openspec-status summary (sanitized, no `\|`) |
| 5 | `12 ready, 2 blocked` | beads-status summary (sanitized, no `\|`) |
| 6 | `68` | 5-hour usage, 68% (this workspace is the usage carrier) |
| 7 | `47` | 7-day usage, 47% |

## Swift reader contract

Pseudocode for the decode step (`[3.2]`/`[3.3]` implement this in the actual `.swift` sidebar
file):

```swift
guard description.hasPrefix("CC1|") else {
    // Not our scheme (empty, garbage, or a version we don't understand yet) —
    // render no state indicator / no openspec-beads row / no usage footer.
    return nil
}

// omittingEmptySubsequences MUST be false — Swift's default is `true`, which
// silently collapses empty fields (e.g. an empty wait_reason) and shifts every
// subsequent field out of position. This is the single most important gotcha
// on the Swift side of this contract.
let segments = description.split(separator: "|", omittingEmptySubsequences: false)

guard segments.count == 8 else {
    // Wrong shape — treat as unparseable, don't index positionally into a
    // short/long array.
    return nil
}

let state = String(segments[1])
guard ["idle", "active", "waiting"].contains(state) else {
    // Unknown/garbage state value — no crash, no fallback icon (per spec).
    return nil
}

let waitReason = String(segments[2])  // "" unless state == "waiting"
let epoch = Int(segments[3])          // nil if empty or non-numeric — handle both
let openspecSummary = String(segments[4])
let beadsSummary = String(segments[5])
let usage5h = Int(segments[6])        // nil => this row is not the usage carrier
let usage7d = Int(segments[7])
```

Every guard fails closed (returns `nil` / renders nothing) rather than crashing or falling back to
a default icon — matching the spec's "unparseable or absent description renders no state
indicator" and "missing usage data renders no footer" scenarios.

## Python writer contract

Both Python writers (`[2.1]`'s hook dual-write, `[2.4]`'s periodic writer) share the same
encode/decode helpers — implement these once (e.g. alongside `apps/cc-tmux/src/cc_tmux/tmux.py`
or a small shared module both writers import) rather than duplicating the join/split logic:

```python
MAGIC = "CC1"
FIELD_COUNT = 8  # magic + 7 real fields

def decode(description: str) -> dict:
    """Best-effort decode; unknown/malformed input -> all-empty dict, never raises."""
    empty = {"state": "", "wait_reason": "", "epoch": "", "openspec": "",
             "beads": "", "usage_5h": "", "usage_7d": ""}
    if not description or not description.startswith(f"{MAGIC}|"):
        return empty
    parts = description.split("|")
    if len(parts) != FIELD_COUNT:
        return empty
    _, state, wait_reason, epoch, openspec, beads, u5, u7 = parts
    return {"state": state, "wait_reason": wait_reason, "epoch": epoch,
            "openspec": openspec, "beads": beads, "usage_5h": u5, "usage_7d": u7}


def encode(fields: dict) -> str:
    """Re-serialize a full field dict (after a caller has merged its owned fields in)."""
    return "|".join([
        MAGIC,
        fields.get("state", ""),
        fields.get("wait_reason", ""),
        fields.get("epoch", ""),
        _sanitize(fields.get("openspec", "")),
        _sanitize(fields.get("beads", "")),
        fields.get("usage_5h", ""),
        fields.get("usage_7d", ""),
    ])


def _sanitize(value: str) -> str:
    """Strip delimiter + newlines from a free-text field before encoding (see § Sanitization)."""
    return value.replace("|", "/").replace("\n", " ").replace("\r", "")
```

A writer's own call site is then: fetch current `description` -> `decode()` -> overwrite only the
fields it owns -> `encode()` the merged dict -> write back. This is exactly the read-modify-write
contract from § Field ownership above, expressed as code.

## Deferred to other tasks (not decided here)

- **Exact `cmux workspace-action` CLI param names / `cmux()` action method** for reading and
  writing a workspace's `description` — task `[1.3]`.
- **Usage carrier-workspace selection strategy** — task `[2.4]`.
- **openspec/beads summary content and polling cadence** — task `[2.4]`.
