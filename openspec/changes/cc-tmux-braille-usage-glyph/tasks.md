<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-4lxh -->

# Tasks: cc-tmux-braille-usage-glyph

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = shared braille bit-order constants + per-metric dot-fill helper; API =
> render_usage_glyph / render_usage_glyph_2metric pure encoding functions; UI =
> render_session_bar/render_accounts_popup wiring; E2E = tests + live verification). Owner:
> general-purpose engineer agents (no dedicated api/ui roles for this Python tmux plugin). Full
> design rationale (encoding choice, width table, popup asymmetry, color, staleness) in
> `design.md` — do not re-derive here.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: add module-level braille bit-order constants: [beads:if-bz1f]
  `_BRAILLE_BASE = 0x2800`, `_SES_BITS = (0, 1, 3, 4)` (rows 1-2, 4 dots/cell), `_H5_BITS = (2, 5)`
  (row 3, 2 dots/cell), `_D7_BITS = (6, 7)` (row 4, 2 dots/cell), `_H5_BITS_WIDE = (0, 1, 3, 4)`
  (rows 1-2, 4 dots/cell, used only by the 2-metric popup encoding), `_D7_BITS_WIDE = (2, 5, 6, 7)`
  (rows 3-4, 4 dots/cell, ditto). Values and rationale per `design.md` § Encoding — cite that
  section in a comment rather than re-deriving the bit math inline.
  [owner:general-purpose] [type:api]
- [x] [1.2] `apps/cc-tmux/src/cc_tmux/render.py`: add `_apply_metric_dots(cells: List[int], ratio: [beads:if-cvfn]
  Optional[float], bits: Tuple[int, ...], n: int) -> None` — mutates `cells` (a list of `n` ints,
  one per braille cell, each accumulating OR'd bits) in place. `ratio` is 0..1 (matching the
  existing `_resolve_ses_pct`/`_extract_util` convention — NOT 0..100). `ratio is None` -> no-op
  (per-metric degrade: zero dots for this metric only, per `design.md` § Staleness). Otherwise:
  `total = len(bits) * n`, `dots = round(max(0.0, min(1.0, ratio)) * total)`, fill cells
  sequentially left to right, each cell taking `min(remaining, len(bits))` dots from `bits` in
  order, stopping once `remaining` reaches 0. This is the exact algorithm validated in the
  `/openspec:explore` mockup — reuse verbatim, do not redesign the fill order.
  [owner:general-purpose] [type:api]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_usage_glyph(ses_ratio: [beads:if-22w6]
  Optional[float], h5_ratio: Optional[float], d7_ratio: Optional[float], n: int = 10) -> str` —
  builds an `n`-length `cells` list, calls `_apply_metric_dots` for SES (`_SES_BITS`), 5H
  (`_H5_BITS`), 7D (`_D7_BITS`) in that order (order doesn't matter for correctness since each
  writes disjoint bits, but keep this order for readability), then returns
  `"".join(chr(_BRAILLE_BASE + c) for c in cells)`. Docstring cites the validated mockup example
  (SES=0.30/5H=0.88/7D=0.35 at n=8 -> `⣿⣿⣧⠤⠤⠤⠤⠀`) as a worked example, and states the
  per-metric-degrade contract for `None` inputs. [owner:general-purpose] [type:api]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_usage_glyph_2metric(h5_ratio: [beads:if-jpwo]
  Optional[float], d7_ratio: Optional[float], n: int = 20) -> str` — same shape as 2.1 but calls
  `_apply_metric_dots` for 5H (`_H5_BITS_WIDE`) and 7D (`_D7_BITS_WIDE`) only, giving each metric
  the full 4-dot-per-cell budget. Used exclusively by the popup's non-active account rows (per
  `design.md` § Non-active popup rows). [owner:general-purpose] [type:api]

## UI Batch

- [x] [3.1] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_session_bar` (row 2), the current [beads:if-dxvy]
  right-side line is `f"{render_context_bar(raw_tokens, ses_pct, t)} " f"#[fg={DIM}]5H:#[fg={c5}]{p5}#[default] " f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default]"`
  (`render_context_bar` = SES token-count label `f"#[fg={DIM}]{label}:#[fg={color}]{bar}#[default]"`
  via `_context_bar_parts`). Replace ONLY the `bar` (shade-block ▓/░ glyphs) portion — keep the
  `label:` token-count text and its color exactly as-is (reuse `format_context_tokens`/
  `resolve_context_color`, both already used internally by `_context_bar_parts` — do not
  duplicate that logic, call them directly or keep going through `_context_bar_parts` and simply
  discard its `bar` return value). Append `render_usage_glyph(ses_ratio, h5_ratio, d7_ratio,
  n=10)` after the label instead of the shade-block bar, in a neutral/unstyled color (no
  `#[fg=...]` wrapper, or an explicit DIM/default if the surrounding tmux format string requires
  one — check how the existing unstyled `⇡`/`⇣` segments handle this and mirror it). The
  `5H:xx% 7D:xx%` text stays completely unchanged. Do NOT remove any existing text — the glyph is
  additive, appended after the SES label and before (or after — pick one, document it) the 5H/7D
  text. [owner:general-purpose] [type:ui]
- [x] [3.2] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_accounts_popup`, the active account's [beads:if-s6uh]
  row currently builds `bar_plain = f"{bar_label}:{bar_glyphs}"` / `bar_colored =
  f"{bar_label}:{_hex_to_ansi_fg(bar_color)}{bar_glyphs}{_ANSI_RESET}"` via `_context_bar_parts`
  (see the `is_active` branch). Replace `bar_glyphs` with `render_usage_glyph(ses_ratio, h5_ratio,
  d7_ratio, n=20)` (neutral/unstyled — do not wrap it in `_hex_to_ansi_fg`), keeping `bar_label:`
  (the token-count label) and the existing `5H:xx% 7D:xx%` tail completely unchanged. Each
  non-active account's row (the `else` case, no bar today) gets
  `render_usage_glyph_2metric(h5_ratio, d7_ratio, n=20)` appended alongside its existing
  `5H:xx% 7D:xx%` text (no SES field, unchanged) — this is NEW for non-active rows, which
  currently render no glyph at all. [owner:general-purpose] [type:ui]
- [x] [3.3] `apps/cc-tmux/src/cc_tmux/render.py`: remove `render_context_bar` and its [beads:if-kl8p]
  `CONTEXT_BAR_WIDTH`/`_BAR_FILLED`/`_BAR_EMPTY` constants once nothing calls it (confirm via
  grep across `apps/cc-tmux/src/cc_tmux/` before deleting — retired the shade-block bar itself;
  see task 3.4 below for a correction on where its severity color goes).
  [owner:general-purpose] [type:ui]
- [x] [3.4] CORRECTION (found during UI batch verification, not new scope): tasks 3.1/3.2 and [beads:if-4lxh.1]
  design.md's original Color section both mistakenly assumed `_context_color_pair`'s 6-tier
  severity ramp was already applied to the SES token-count label — it was NEVER on the label,
  only on the now-retired shade-bar's fill color (`render_context_bar`'s `#[fg={color}]{bar}`).
  The label was always plain DIM. As landed by tasks 3.1/3.2, this means the severity ramp
  (including the pulsing-red near-context-exhaustion warning) currently renders NOWHERE — a real
  regression, not this proposal's intent (design.md's stated intent was always "color stays on
  text, glyph stays neutral" — the label IS that text, it just wasn't wired to the ramp before).
  Fix in `apps/cc-tmux/src/cc_tmux/render.py`: in `render_session_bar`, change
  `f"#[fg={DIM}]{ses_label}:#[default]{usage_glyph} "` to wrap `ses_label` in
  `resolve_context_color(raw_tokens, t)` instead of `DIM` (reuse the existing function — `t` is
  already computed in this function for the glyph/pulse timing, confirm it's in scope at this
  point or hoist it earlier if needed). In `render_accounts_popup`'s active-row branch, change
  `bar_label = format_context_tokens(active_raw_tokens)` + the plain `bar_str = f"{bar_label}:
  {bar_glyphs}"` to wrap `bar_label` in `_hex_to_ansi_fg(resolve_context_color(active_raw_tokens,
  t))` (ANSI, matching this function's escaping convention, NOT tmux `#[fg=...]`) — do not wrap
  `bar_glyphs` (the glyph itself stays neutral, unchanged). Verify by running both functions with
  a `raw_tokens` value in each of the 6 severity tiers (per `_context_color_pair`'s docstring:
  <=100k, >100k, >200k, >300k, >500k, >600k, >750k) and confirming the label's color changes
  tier-to-tier while the glyph stays unstyled. Run `cc-tmux self-test` and confirm still 92/94 (no
  new regressions from this fix; the 2 pre-existing failures from tasks 3.1/3.2 are owned by task
  4.4, not this one). Also apply the `design.md` § Color correction already written (the
  "Correction found during UI batch verification" paragraph) — no further edit needed there,
  just confirm it reads consistently with what you implement.
  [owner:general-purpose] [type:ui]

## E2E Batch

- [ ] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-3ch1]
  `_apply_metric_dots`: a representative ratio (e.g. 0.5) with a 4-bit order produces the correct
  dot-bit set across a small `n` (e.g. n=4); `None` produces an all-zero `cells` list (no dots
  set) while a sibling call for a different metric on the same `cells` list is unaffected (proves
  per-metric degrade doesn't clobber other metrics' bits); ratio=0.0 -> zero dots; ratio=1.0 ->
  every cell fully filled for that metric's bits. Run `cc-tmux self-test` and paste the passing
  stdout. [owner:general-purpose] [type:testing]
- [ ] [4.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test case reproducing the [beads:if-i9mh]
  `/openspec:explore` mockup's validated example exactly: `render_usage_glyph(0.30, 0.88, 0.35,
  n=8)` produces `⣿⣿⣧⠤⠤⠤⠤⠀` byte-for-byte (this is the concrete regression anchor for the
  whole encoding — if this ever changes unexpectedly, the encoding broke). Additional cases at
  n=10 (the shipped row-2 width): all-`None` -> fully blank glyph; SES live + 5H/7D both `None` ->
  only rows 1-2 show dots, rows 3-4 blank. Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
- [ ] [4.3] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test case for [beads:if-6v3f]
  `render_usage_glyph_2metric` at n=20: 5H and 7D at distinct representative ratios (e.g. 0.9 and
  0.3) produce rows-1-2-fill and rows-3-4-fill proportional to each, independently verifiable by
  bit-tracing the returned glyph the same way the 4.2 case does. Run `cc-tmux self-test` and paste
  the passing stdout. [owner:general-purpose] [type:testing]
- [ ] [4.4] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the [beads:if-hczj]
  `render_session_bar`/`render_accounts_popup` wiring: row 2's rendered output contains both the
  unchanged SES token-count label (now severity-colored per task 3.4, not DIM) + `5H:xx% 7D:xx%`
  text AND the 10-cell glyph; the popup's active row contains both its text and the 20-cell
  3-metric glyph; a non-active popup row contains its text (no SES) and the 20-cell 2-metric
  glyph, with no SES-shaped dots present. ALSO REQUIRED (not optional cleanup): the UI batch left
  the self-test suite at 92/94 — two PRE-EXISTING tests now fail because they assert the old
  shade-bar shape: `render.accounts_popup` (asserts a non-active row's text starts with a bare
  `5H:` percentage, which no longer holds now that the 2-metric glyph is prepended) and
  `render.context_bar_format` (calls `render.render_context_bar` directly, which task 3.3
  retired). Update BOTH to assert the new correct shape instead of deleting or skipping them —
  this task is not complete until `cc-tmux self-test` reports 94/94 (the original 92 plus
  whatever new cases 4.1-4.4 add, zero failures). Run `cc-tmux self-test` and paste the passing
  stdout showing 0 failures. [owner:general-purpose] [type:testing]
- [ ] [4.5] Live verification: with a real tracked pane in this repo, confirm row 2's actual [beads:if-1r71]
  on-screen render shows both the text and the new glyph, using real 5H/7D from nexus-agent (per
  `design.md` § Live-data verification gap, SES may need to stay illustrative if
  `nx_agent.session_context()` still returns `None` for every tracked session on this machine —
  attempt the real pull first, only fall back to illustrative if it's still unavailable, and note
  which happened). Click the account label and confirm the popup's active-row and non-active-row
  glyphs render as expected. Paste observed output (both row 2 and the popup).
  [owner:general-purpose] [type:testing]
