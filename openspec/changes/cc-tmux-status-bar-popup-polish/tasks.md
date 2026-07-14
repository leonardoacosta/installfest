<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-z55e -->

# Tasks: cc-tmux-status-bar-popup-polish

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = shared org-suffix primitive; API = pure `render_accounts_popup`/
> `render_session_bar`/`render_beads_bar` encoding changes; UI = CLI wiring + tmux config; E2E =
> tests + live verification). Owner: general-purpose engineer agents (no dedicated api/ui roles
> for this Python tmux plugin, matching the prior `cc-tmux-braille-usage-glyph` precedent). Full
> design rationale (why each decision, breaking-change framing, height-fix investigation
> approach) in `design.md` — do not re-derive here.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/usage.py`: in `_account_label`, change the org suffix from [beads:if-dm3t]
  `org_uuid[-1]` (last character) to `org_uuid[:8]` (first 8 characters) — matching
  `_account_identity`'s existing `org_short` format exactly. This function is the single source
  for BOTH the printed row-2/row-3 label text and the internal `active_label` matching key used
  by `_active_usage()`/`extract_active()` and `cmd_accounts_popup` — changing it here updates
  both consistently. Update the docstring's own example (currently shows `leo@x.dev·7`) to the
  new 8-char format. [owner:general-purpose] [type:api]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_accounts_popup`, delete the [beads:if-dg45]
  `is_active` branch's SES/3-metric-glyph construction (`bar_label`, `bar_color`, `bar_glyphs`,
  `bar_str_plain`, `bar_str`, and the `tail_plain`/`tail` prefixing that used them). Every row
  (active and non-active alike) now falls through to the existing `else` branch's logic:
  `render_usage_glyph_2metric(five_h, seven_d, n=20)` prepended to `5H:xx% 7D:xx%` text. The `*`
  marker (`marker = "* " if is_active else "  "`) is UNCHANGED — it remains the sole active-row
  indicator. Remove the `active_ses_pct: Optional[float]` and `active_raw_tokens: Optional[float]
  = None` parameters from the function signature entirely (no longer referenced anywhere in the
  body) — update the docstring accordingly (delete the SES-related paragraphs, keep the
  `*`-marker and per-account-identity-row documentation). [owner:general-purpose] [type:api]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_session_bar`, remove `label_seg` [beads:if-w0tc]
  (the `#[range=user|accounts]...#[norange]` account-label segment) and its construction from the
  `right` string entirely. Reorder the remaining right-side content so the combined usage glyph
  renders LAST, after `7D:xx%` — i.e. `right = f"#[fg={ses_color}]{ses_label}:#[default] "
  f"#[fg={DIM}]5H:#[fg={c5}]{p5}#[default] " f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default]{usage_glyph}"`
  (exact spacing/separator placement is an implementation detail — match the existing style of a
  single space between segments; confirm against the target composition `85.0K 5H:50% 7D:9%
  [glyph]` from the design doc). The `account_label: str` parameter becomes unused by this
  function's body — remove it from the signature (it moves to `render_beads_bar` per task 2.3;
  do NOT leave a vestigial unused parameter). Update the docstring to drop every SES-label-glyph
  ordering claim and the account-label paragraph, and to state the new right-side order and the
  removed parameter. [owner:general-purpose] [type:api]
- [x] [2.3] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_beads_bar`, add a new parameter [beads:if-wgrj]
  (e.g. `account_label: str = ""`) and, when non-empty, append a THIRD independent segment to
  `segments` (same `_BEADS_SEP`-joined convention already used for the openspec/beads pair) —
  wrap it in the SAME `#[range=user|accounts]...#[norange]` click marker `render_session_bar`
  used to carry (relocated here, not duplicated — the tmux `MouseDown1Status` binding in
  `cc-tmux.tmux` keys off `#{mouse_status_range}` globally, so no binding change is needed, only
  moving which row emits the marker). This segment is independent of the openspec/beads pair:
  when BOTH of those are absent (today's "no cache -> empty row" case) but `account_label` is
  non-empty, the row renders ONLY the account segment, not `""`. When `account_label` is empty
  too, behavior is unchanged from today (empty row when nothing is available). Update the
  docstring accordingly. [owner:general-purpose] [type:api]

## UI Batch

- [x] [3.1] `apps/cc-tmux/src/cc_tmux/cli.py`: in `cmd_accounts_popup`, remove the now-dead [beads:if-l6xg]
  window/pane/SES resolution block (`window = tmux.current_window_id()`, `pane =
  _resolve_session_pane(window) if window else ""`, `active_ses_pct =
  _resolve_ses_pct(pane) if pane else None`, `active_raw_tokens = _resolve_ses_tokens(pane) if
  pane else None`) and the now-removed keyword arguments in the `render.render_accounts_popup(...)`
  call (`active_ses_pct`, `active_raw_tokens`). Update the function's docstring to remove the
  "SES ... is resolved via `_resolve_session_pane` + ..." paragraph — the popup no longer touches
  any per-session state at all. [owner:general-purpose] [type:ui]
- [x] [3.2] `apps/cc-tmux/src/cc_tmux/cli.py`: in `render_session_bar`'s call site inside [beads:if-rrfx]
  `_build_session_bar`, drop the now-removed `account_label` positional argument (task 2.2 removed
  it from the function signature) — `_build_session_bar` still computes `account_label,
  five_h_pct, seven_d_pct = _active_usage()` (5H/7D still needed for row 2's own gauge), it just
  stops passing `account_label` through to `render_session_bar`. [owner:general-purpose] [type:ui]
- [x] [3.3] `apps/cc-tmux/src/cc_tmux/cli.py`: in `_build_beads_bar`, call the existing cached [beads:if-wa9q]
  `_active_usage()` (same function/cache `_build_session_bar` already calls — 45s TTL, shared
  on-disk cache file, so calling it a second time in the same render tick is a cache hit, not a
  new network fetch) to get `account_label` (5H/7D from this call are unused here — row 3 needs
  only the label), and pass it through to `render.render_beads_bar(...)` as the new
  `account_label` argument from task 2.3. Update the docstring. [owner:general-purpose] [type:ui]
- [x] [3.4] `apps/cc-tmux/cc-tmux.tmux`: investigate why the `accounts_popup_cmd`'s fzf box [beads:if-s1yu]
  truncates the account list to roughly half the popup pane's actual height despite `-h 80%` on
  the outer `display-popup`. Per `design.md` Decision 5, try (in order, keep whichever actually
  closes the gap, verified live per task 4.5 — do not guess-and-ship without live confirmation):
  (a) add an explicit `--height=100%` to the `fzf` invocation to stop it from relying on
  possibly-stale terminal-size auto-detection inside the `display-popup -E`-spawned pty, (b) if
  that alone doesn't close the gap, increase `display-popup`'s own `-h` (e.g. `-h 95%` or a fixed
  line count sized to the realistic max account count), (c) apply both if the investigation shows
  they're two independent partial causes. Document which fix(es) were needed and why, inline as a
  comment near the `accounts_popup_cmd` definition (mirroring this file's existing convention of
  dated rationale comments for prior fixes to this same line). [owner:general-purpose] [type:ui]

## E2E Batch

- [x] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test case for `_account_label`'s [beads:if-nd28]
  new org-suffix format — a credential with a known `orgUuid` produces a label ending in the
  first 8 characters of that UUID, and this matches `_account_identity`'s `org_short` for the
  identical credential byte-for-byte. Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
- [x] [4.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-9y0t]
  `render_accounts_popup`'s uniform 2-metric glyph — the active row's rendered line contains no
  SES-shaped 4-bit-per-cell dot pattern and no token-count label (`format_context_tokens`
  output), only `5H:xx% 7D:xx%` text plus the 20-cell 2-metric glyph identical in shape to a
  non-active row's glyph; the `*` marker is still present on exactly the active row and absent
  from every other row. Update/replace any PRE-EXISTING test asserting the old 3-metric
  active-row shape (do not delete without replacing — same discipline as the prior
  `cc-tmux-braille-usage-glyph` proposal's task 4.4 correction). Run `cc-tmux self-test` and
  paste the passing stdout with the full pass count (0 failures, no regressions from this batch).
  [owner:general-purpose] [type:testing]
- [x] [4.3] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-k3uz]
  `render_session_bar`'s new right-side composition — the rendered right side contains no
  account-label text and no `#[range=user|accounts]` marker; the combined glyph's characters
  appear strictly AFTER the `7D:` percentage substring in the rendered output (order assertion,
  not just presence). Update any PRE-EXISTING test asserting the old
  label-first/glyph-before-5H-7D order. Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
- [x] [4.4] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-iphi]
  `render_beads_bar`'s new account segment — (a) openspec/beads counts both present, account
  label present -> all three segments appear, `_BEADS_SEP`-joined, account segment last and
  wrapped in the range marker; (b) openspec/beads both absent (no cache), account label present
  -> row shows ONLY the account segment, not `""`; (c) account label absent, openspec/beads
  present -> unchanged two-segment behavior (regression guard for today's existing contract);
  (d) all three absent -> `""` (unchanged empty-row contract). Run `cc-tmux self-test` and paste
  the passing stdout. [owner:general-purpose] [type:testing]
- [x] [4.5] Live verification, real popup height fix: with the real `/credentials` payload on [beads:if-1ctd]
  this machine (currently 2-3 deduped accounts per the last `/openspec:explore` session), open
  the actual accounts popup via its tmux keybinding/click and confirm every account's full block
  (summary + identity + reset lines + separator) is visible with no truncation — this is the
  acceptance test for task 3.4, not a source-read confirmation. Paste the observed popup output
  (or a description of exactly what rendered, row by row, if a screenshot isn't captured inline).
  [owner:general-purpose] [type:testing]

  DONE, verified live via 3 real popup triggers on Leo's attached tmux client (window 0:8, then
  0:5, then 0:2 as panes rotated) against real `/credentials` data (3 deduped accounts): first
  pass confirmed truncation gone but surfaced two follow-up gaps (fzf highlight/gutter making
  rows look selectable, and the outer `display-popup` pane not shrinking to match fzf's own
  content-sized box) — both fixed live (`98ea328`, `3433c6c`, `e2519f8`, the last two adding a
  new `cc-tmux accounts-popup-launch` subcommand that computes height AND width from real
  content). Final live confirmation from Leo: "perfect" — box now fits height and width to the
  actual 3-account content with zero truncation, zero dead space, and no fake selectability.
- [x] [4.6] Live verification, full end-to-end render: with a real tracked pane in this repo, [beads:if-ao78]
  confirm row 2's actual on-screen render shows the new composition
  (`85.0K 5H:50% 7D:9% [glyph]`, no account label anywhere on that row) and row 3 shows the
  account identity segment (`email·orgid8char`) alongside whatever openspec/beads counts are
  cached. Click the account-identity segment on row 3 and confirm the popup still opens
  correctly (range marker successfully relocated) and shows global-usage-only data (no `--`
  SES artifact anywhere in the popup). Run `cc-tmux self-test` one final time and paste the
  full passing stdout (0 failures). [owner:general-purpose] [type:testing]

  DONE. Live output captured directly (worktree binary against real tmux/nx-agent state, cache
  cleared first to avoid a stale-TTL read): row 2 (`bin/cc-tmux session-bar 0:8`) ->
  `#[fg=#454D54]mesh #[fg=#454D54]> #[fg=#B267E6]main#[default]#[align=right]#[fg=#454D54]--:#[default] #[fg=#454D54]5H:#[fg=#FAC760]64%#[default] #[fg=#454D54]7D:#[fg=#5BD1B9]32%#[default]⣤⣤⣤⠤⠤⠤⠄⠀⠀⠀`
  — no account label anywhere, glyph strictly after `7D:`. Row 3
  (`bin/cc-tmux beads-bar 0:8`) -> `openspec: 1 open 0 unarchived | beads: 19 ready 0 blocked | leo@priceless.dev·bc7da511`
  — three segments, account identity last, correct 8-char org suffix, wrapped in the
  `#[range=user|accounts]` marker. Popup click-through confirmed via the 3 live triggers in
  task 4.5 above (uniform 2-metric glyph, `*` marker only on the active row, zero SES anywhere).
  `cc-tmux self-test`: 100/100 passed (final run, post all follow-up fixes).
