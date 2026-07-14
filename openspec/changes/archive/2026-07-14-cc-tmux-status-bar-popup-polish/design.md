# Design: cc-tmux-status-bar-popup-polish

## Context

This follows directly from a live `/openspec:explore` session (screenshots of the running popup
+ row 2) that found three independent, verified bugs plus one deliberate UI restructure Leo
asked for. All four land together since they touch the same three functions
(`render_session_bar`, `render_accounts_popup`, `render_beads_bar`) and the same handful of
call sites (`cmd_accounts_popup`, `_build_beads_bar`).

**Row-numbering correction, resolved during exploration**: cc-tmux's own code/spec vocabulary
calls the session-identity+usage row "row 2" (`status-format[1]`) and the openspec/beads row
"row 3" (`status-format[2]`). Leo's own mental model numbers them one lower — his "row 2" is the
code's row 3. This proposal uses the CODE's numbering throughout (row 2 = session identity +
usage, row 3 = openspec/beads) to stay consistent with the shipped spec text; the account
identity segment is moving from row 2 to row 3 in code terms.

## Decision 1: Popup drops SES entirely — 2-metric glyph for every row, no exceptions

**What changes**: `render_accounts_popup`'s `is_active` branch currently builds a SES
token-count label + 3-metric glyph (`render_usage_glyph(active_ses_pct, five_h, seven_d, n=20)`)
for the starred row only. This is deleted. Every row — active or not — uses
`render_usage_glyph_2metric(five_h, seven_d, n=20)` and shows only `5H:xx% 7D:xx%` text. The
`*` marker stays as the sole active-account indicator (Leo confirmed this is the correct/wanted
signal, not something to remove).

**Why**: SES (context-window-used %) is a property of the currently-focused pane's session, not
of an account in the abstract — the popup is meant to show account-level usage. Confirmed live
2026-07-14 that the active row's SES was rendering `--` in production because nx-agent's
session-context store is currently unpopulated (separate, already-filed bugs `nx-22xz8` and the
sibling context-push bug, both nexus-repo, both out of scope here) — but even once those land,
mixing a session-scoped metric into an account-scoped popup is the wrong shape. Popup becomes
"global usage" only, matching Leo's exact framing.

**Cascading simplification**: once no popup row needs SES, `cmd_accounts_popup` no longer needs
to resolve a window/pane at all — the `tmux.current_window_id()` / `_resolve_session_pane` /
`_resolve_ses_pct` / `_resolve_ses_tokens` calls in that handler are dead code and are removed
(not just left unused — Reader Gate). `render_accounts_popup`'s `active_ses_pct` /
`active_raw_tokens` parameters are removed from its signature entirely. This also means the
popup becomes fully independent of the nx session-context bugs filed last turn — it will render
correctly even before those land.

**Breaking change vs. shipped spec**: `openspec/specs/cc-tmux/spec.md`'s "Clicking the row-2
account label opens a read-only accounts popup" Requirement has a committed scenario ("the
active account's row includes SES") that this directly contradicts. Per the Breaking Changes
Policy, this is a clean-replacement call — Leo asked for this exact change directly during
exploration, so this proposal treats it as an explicit approved breaking change, not something
requiring a fresh confirmation loop. The spec delta below MODIFIES that requirement and REMOVES
the SES scenario.

## Decision 2: Row 2 composition — drop the account label, reorder to `SES 5H 7D glyph`

**What changes**: `render_session_bar`'s `right` string currently is
`{label_seg}{ses_label}:{glyph} 5H:{c5} 7D:{c7}`. Becomes `{ses_label}:{glyph} 5H:{c5} 7D:{c7}
{glyph}` — wait, precisely: `ses_label` text, then `5H:xx%`, then `7D:xx%`, then the glyph LAST.
`label_seg` (the account identity + its `#[range=user|accounts]` click wrapper) is removed from
this function entirely.

**Spec/code drift this incidentally fixes**: the shipped spec Requirement's prose already
described the glyph as the LAST element (`"...5H:xx%/7D:xx% text, and a combined ... glyph"`) —
current code puts the glyph immediately after the SES label, before 5H/7D. This proposal's
reorder makes code match what the spec always said, not just what Leo asked for this session.

## Decision 3: Row 3 gains the account identity segment (not a new physical status line)

**What changes**: `render_beads_bar` gains a new parameter carrying the active account's
identity string, appended as a third independent segment (same fail-open, `_BEADS_SEP`-joined
convention already used for the openspec/beads pair) — e.g.:
`openspec: 2 open 1 unarchived (2m) | beads: 3 ready 0 blocked (2m) | leo@priceless.dev·bc7da511`.
The segment is independent of the other two: if roadmap-pulse has no cache at all (today's
"empty row" case) but an account is active, row 3 shows *just* the account segment, not nothing.
The `#[range=user|accounts]` click wrapper moves here too — the `MouseDown1Status` binding in
`cc-tmux.tmux` keys off `#{mouse_status_range}` globally across the whole status line (not
per-row), confirmed via the existing code comment describing that mechanism, so moving which row
*emits* the range marker requires zero binding changes.

**Where the label string comes from**: `_build_beads_bar` calls the SAME cached
`_active_usage()` row 2 already calls (45s TTL, shared cache file — no new network cost; two
calls in the same render-all tick just both hit the warm cache). It only needs the `label`
return value, not 5H/7D (those stay exclusively on row 2).

**No new physical status line**: Leo's "row 2" in his own numbering is what the code calls row
3 — the openspec/beads row already exists (`status-format[2]`, `status 3` in
`tmux.conf.tmpl`). No `status` bump, no new theme wiring required.

## Decision 4: Org-suffix format — unify on the popup's 8-char format

**What changes**: `usage._account_label`'s org suffix changes from `org_uuid[-1]` (last
character — confirmed via live code read, NOT `org_uuid[1]`, the second character, which is what
"are we using [1] or [-1]" was asking to disambiguate) to `org_uuid[:8]` (first 8 characters),
matching `_account_identity`'s existing `org_short` format used in the popup's identity rows.

**Why this is safe**: `_account_label` serves double duty — it's both the row-2/row-3 PRINTED
text and the internal string-equality matching key both `_active_usage()`/`extract_active()` and
`cmd_accounts_popup` use to find `active_label`. Both call sites use the same function, so
changing its format changes both consistently; no cross-surface drift risk. This also means that
after Decision 3 lands, row 3's account segment IS the label string as-is — no separate
email/org lookup needed, `_account_label`'s own return value is exactly the display format
wanted (`email·orgid8char`).

## Decision 5: Popup height — real truncation, not a spacing preference

Leo confirmed (correcting my initial read of the screenshot): the fzf box *inside* the popup
pane is only using about half the popup's actual height and cutting off the account list, even
though `display-popup` is already invoked with `-h 80%`. The outer tmux popup dimension is not
the bottleneck — something about the inner `fzf` invocation is capping its own rendered height
below what its container actually offers.

**Approach**: `cc-tmux.tmux`'s `accounts_popup_cmd` pipes to plain `fzf --ansi --no-input
--header-border ...` with no explicit `--height` flag. fzf defaults to filling its controlling
terminal when it can detect the real size, but a `display-popup -E`-spawned pty is a case where
that detection is known to be unreliable in some tmux/fzf version combinations (stale
`$LINES`/`$COLUMNS`, or an interaction with `--header-border`'s own reserved rows). This
proposal does not assume a single fix — the E2E batch task investigates
(`stty size` / `$LINES` inside the popup vs. the outer `-h 80%` pane) and applies whichever of
these actually closes the gap, verified live:
1. Explicit `fzf --height=100%` (redundant with a full-screen popup, but forces fzf to stop
   auto-detecting and just fill 100% of what it's given).
2. `display-popup`'s own `-h` bumped higher (e.g. `-h 95%` or a fixed line count) if the popup
   pane itself is the actual bottleneck, not fzf's internal sizing.
3. Both, if the investigation shows they're two independent partial causes.

Whichever fix is applied, the acceptance bar is: with the real payload's typical 2-3 deduped
accounts (5 lines each), zero rows are cut off, and the popup does not leave excess dead space
disproportionate to fzf's own header/border overhead (i.e., not literally "grow the box bigger
without checking why it doesn't already fill it").

## Non-Goals

- No fix to the nx-agent session-context bugs (`nx-22xz8` and the sibling context-push bug) —
  separate repo, separate beads, unaffected by Decision 1's popup simplification.
- No fix to the stale-`isActive`-credential-order bug (`if-lh9u`) — filed separately, orthogonal
  to this proposal's rendering changes.
- No change to row 2's model/project/branch/git-status left side, or to row 3's
  openspec/beads-count logic itself — only the account-identity addition.
- No change to the `5H:xx%`/`7D:xx%` color-threshold logic (`usage.color_for`) anywhere.
