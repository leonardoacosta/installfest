---
status: draft
---

# Proposal: cc-tmux-status-bar-popup-polish

## Why

A live `/openspec:explore` session (screenshots of the running accounts popup + row 2) surfaced
four issues, three of them confirmed live bugs and one a deliberate restructure Leo asked for:

1. **Popup active-row SES is broken and shouldn't be there at all.** The starred/active row
   renders a SES token-count label + 3-metric glyph that currently shows `--` in production
   (nx-agent's session-context store is unpopulated — separate nexus-repo bugs `nx-22xz8` and a
   sibling context-push bug, both already filed, both out of scope here). But even independent of
   that outage, SES is session-scoped, not account-scoped — it never belonged in an
   account-usage popup. Leo: "this should be global usage, not session based."
2. **Row 2's composition needs reordering, and the account label needs to move off it.** Current
   right side is `{account label} {SES label}:{glyph} 5H: 7D:`. Target:
   `85.0K 5H:50% 7D:9% [glyph]` — label gone, glyph moved to the end (after 5H/7D, not before).
3. **The account identity belongs on row 3 (the openspec/beads row), not row 2.** Confirmed
   during exploration: Leo's own "row 2" refers to the openspec/beads row in his mental model —
   the code's row 3. No new physical status line is needed; that row already exists.
4. **Row 2's org-id suffix doesn't match the popup's.** `usage._account_label` uses
   `org_uuid[-1]` (the last character) while the popup's `_account_identity` uses `org_uuid[:8]`
   (first 8 characters) — two different slices of the same field, confirmed via live code read.
   Fix: unify on the popup's 8-char format.
5. **The popup's fzf box is genuinely truncating the account list**, using roughly half the
   popup pane's actual height (`display-popup -h 80%`) despite the outer popup being tall
   enough — Leo confirmed this is real truncation, not a spacing preference.

Full root-cause detail, the exact code locations, and the chosen fix for each: `design.md`.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/usage.py`**: `_account_label`'s org suffix changes from
  `org_uuid[-1]` to `org_uuid[:8]`, matching `_account_identity`'s existing format. Both
  row-2/row-3 display text and the internal active-account matching key update together
  (same function, no cross-surface drift).
- **`apps/cc-tmux/src/cc_tmux/render.py`**:
  - `render_accounts_popup`: removes the active-row SES/3-metric-glyph branch entirely; every
    row (active or not) uses `render_usage_glyph_2metric` — the `active_ses_pct`/
    `active_raw_tokens` parameters are removed from the function signature. The `*` active
    marker is unchanged.
  - `render_session_bar`: removes the account-label segment from the right side; reorders the
    remaining right-side content to `{ses_label}:{5H} {7D} {glyph}` (glyph last), which also
    corrects a pre-existing spec/code drift (the shipped spec text already said "glyph last").
  - `render_beads_bar`: gains a new parameter carrying the active account's identity string,
    appended as a third independent, fail-open `_BEADS_SEP`-joined segment.
- **`apps/cc-tmux/src/cc_tmux/cli.py`**:
  - `cmd_accounts_popup`: removes the now-dead window/pane/SES resolution code
    (`tmux.current_window_id()`, `_resolve_session_pane`, `_resolve_ses_pct`,
    `_resolve_ses_tokens`) — the popup no longer needs any per-session state.
  - `_build_beads_bar`: calls the existing cached `_active_usage()` (same 45s-TTL cache row 2
    already warms — no new network cost) to source the account-identity string for row 3.
- **`apps/cc-tmux/cc-tmux.tmux`**: fixes the accounts-popup fzf box so it actually uses the full
  popup pane height instead of truncating the account list (see `design.md` Decision 5 for the
  investigation approach and candidate fixes).
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED deltas for "A dedicated tmux status row shows
  session identity and usage" (row 2 — drops the account label, corrects glyph ordering), "A
  dedicated tmux status row surfaces open/ready beads and proposals" (row 3 — adds the account
  identity segment), and "Clicking the row-2 account label opens a read-only accounts popup"
  (drops the active-row SES scenario, corrects "row-2" references to "row-3" since the
  clickable label moves there, documents the height fix).

## Non-Goals

- No fix to the nx-agent session-context bugs (`nx-22xz8`, and the sibling context-push bug
  filed the same session) — separate repo (`~/dev/personal/nexus`), separate beads, unaffected
  by this proposal's popup simplification (the popup no longer depends on that data at all).
- No fix to the stale-`isActive`-credential-order bug (`if-lh9u`, filed same session) —
  orthogonal to this proposal's rendering changes; tracked separately.
- No change to row 2's model/project/branch/git-status left side.
- No change to row 3's openspec/beads count logic, thresholds, or staleness-age rendering.
- No change to the `5H:xx%`/`7D:xx%` color-threshold logic (`usage.color_for`) anywhere.
- No new physical tmux status line — the account identity relocates to the EXISTING row 3, no
  `status` bump, no theme-file changes.

## Context

- Related: `openspec/changes/archive/2026-07-14-cc-tmux-braille-usage-glyph/` — most recent
  prior change to both `render_session_bar` and `render_accounts_popup`, established the
  glyph functions this proposal reuses (`render_usage_glyph`, `render_usage_glyph_2metric`) and
  the DB/API/UI/E2E batch-mapping convention for this Python-plugin, no-traditional-layers repo
  (used as this proposal's tasks.md structural template).
- Related: `openspec/changes/archive/2026-07-13-cc-tmux-accounts-popup-click-dismiss/` and
  `2026-07-12-cc-tmux-account-switcher-popup/` — established the popup's click/dismiss
  mechanism and the `#[range=user|accounts]`/`MouseDown1Status` binding this proposal relocates
  but does not rewire.
- Related beads (filed same session, different repo/root-cause, not fixed here): `if-lh9u`
  (installfest — stale isActive credential ordering), `nx-22xz8` (nexus — `ccSessionId` never
  populated), and the sibling nexus context-push bug (filed same turn, same session).
- touches: `apps/cc-tmux/src/cc_tmux/usage.py`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/cc-tmux.tmux`,
  `apps/cc-tmux/src/cc_tmux/testing.py`, `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| `_account_label` org-suffix format | `cc-tmux self-test` case: a credential's label uses the first 8 chars of `orgUuid`, matching `_account_identity`'s `org_short` byte-for-byte for the same credential |
| `render_accounts_popup` — uniform 2-metric glyph, no SES anywhere | `cc-tmux self-test` cases: active row's output contains no SES-shaped dots and no token-count label; active AND non-active rows both use the 20-cell 2-metric encoding; `*` marker still present on the active row only |
| `render_session_bar` — label removed, reordered right side | `cc-tmux self-test` case: right-side output contains no account-label/range-marker text; glyph appears strictly after the `7D:` percentage in the rendered string |
| `render_beads_bar` — account segment, independent fail-open | `cc-tmux self-test` cases: with openspec/beads counts absent but an account label present, the row shows only the account segment; with all three present, all three appear `_BEADS_SEP`-joined; with the account label absent, the row is unchanged from today's two-segment behavior |
| `cmd_accounts_popup` dead-code removal | `cc-tmux self-test`/lint: no reference to `tmux.current_window_id`/`_resolve_session_pane`/`_resolve_ses_pct`/`_resolve_ses_tokens` remains in `cmd_accounts_popup` |
| Popup height fix | Live verification: real popup open with the actual multi-account payload, confirm every account row is visible (no truncation) — task 4.5 |
| End-to-end live render | Live verification: real pane, row 2 shows the reordered composition with no label, row 3 shows the account identity segment, popup click still opens and shows correct global-usage-only data — paste observed output for both rows and the popup |
