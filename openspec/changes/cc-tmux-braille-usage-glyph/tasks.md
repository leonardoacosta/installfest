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

- [ ] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: add module-level braille bit-order constants: [beads:if-bz1f]
  `_BRAILLE_BASE = 0x2800`, `_SES_BITS = (0, 1, 3, 4)` (rows 1-2, 4 dots/cell), `_H5_BITS = (2, 5)`
  (row 3, 2 dots/cell), `_D7_BITS = (6, 7)` (row 4, 2 dots/cell), `_H5_BITS_WIDE = (0, 1, 3, 4)`
  (rows 1-2, 4 dots/cell, used only by the 2-metric popup encoding), `_D7_BITS_WIDE = (2, 5, 6, 7)`
  (rows 3-4, 4 dots/cell, ditto). Values and rationale per `design.md` § Encoding — cite that
  section in a comment rather than re-deriving the bit math inline.
  [owner:general-purpose] [type:api]
- [ ] [1.2] `apps/cc-tmux/src/cc_tmux/render.py`: add `_apply_metric_dots(cells: List[int], ratio: [beads:if-cvfn]
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

- [ ] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_usage_glyph(ses_ratio: [beads:if-22w6]
  Optional[float], h5_ratio: Optional[float], d7_ratio: Optional[float], n: int = 10) -> str` —
  builds an `n`-length `cells` list, calls `_apply_metric_dots` for SES (`_SES_BITS`), 5H
  (`_H5_BITS`), 7D (`_D7_BITS`) in that order (order doesn't matter for correctness since each
  writes disjoint bits, but keep this order for readability), then returns
  `"".join(chr(_BRAILLE_BASE + c) for c in cells)`. Docstring cites the validated mockup example
  (SES=0.30/5H=0.88/7D=0.35 at n=8 -> `⣿⣿⣧⠤⠤⠤⠤⠀`) as a worked example, and states the
  per-metric-degrade contract for `None` inputs. [owner:general-purpose] [type:api]
- [ ] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_usage_glyph_2metric(h5_ratio: [beads:if-jpwo]
  Optional[float], d7_ratio: Optional[float], n: int = 20) -> str` — same shape as 2.1 but calls
  `_apply_metric_dots` for 5H (`_H5_BITS_WIDE`) and 7D (`_D7_BITS_WIDE`) only, giving each metric
  the full 4-dot-per-cell budget. Used exclusively by the popup's non-active account rows (per
  `design.md` § Non-active popup rows). [owner:general-purpose] [type:api]

## UI Batch

- [ ] [3.1] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_session_bar` (row 2), replace the [beads:if-dxvy]
  `render_context_bar` call + separate 5H/7D text-only rendering with: unchanged `SES:xx%
  5H:xx% 7D:xx%` text (still color-coded via existing `usage.color_for`/`_context_color_pair`),
  followed by `render_usage_glyph(ses_ratio, h5_ratio, d7_ratio, n=10)` rendered in a
  neutral/unstyled color (no `#[fg=...]` wrapper, or an explicit DIM/default if the surrounding
  tmux format string requires one — check how the existing unstyled `⇡`/`⇣` segments handle this
  and mirror it). Do NOT remove the text percentages — the glyph is additive.
  [owner:general-purpose] [type:ui]
- [ ] [3.2] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_accounts_popup`, the active account's [beads:if-s6uh]
  row calls `render_usage_glyph(ses_ratio, h5_ratio, d7_ratio, n=20)` alongside its existing
  `SES:xx% 5H:xx% 7D:xx%` text; each non-active account's row calls
  `render_usage_glyph_2metric(h5_ratio, d7_ratio, n=20)` alongside its existing `5H:xx% 7D:xx%`
  text (no SES field, unchanged). Both glyphs render neutral/unstyled, same as row 2.
  [owner:general-purpose] [type:ui]
- [ ] [3.3] `apps/cc-tmux/src/cc_tmux/render.py`: remove `render_context_bar` and its [beads:if-kl8p]
  `CONTEXT_BAR_WIDTH`/`_BAR_FILLED`/`_BAR_EMPTY` constants once nothing calls it (confirm via
  grep across `apps/cc-tmux/src/cc_tmux/` before deleting — `_context_color_pair`'s 6-tier ramp
  is UNCHANGED and still used for the text label's color, only the shade-block bar itself is
  retired). [owner:general-purpose] [type:ui]

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
  unchanged `SES:xx% 5H:xx% 7D:xx%` text AND the 10-cell glyph; the popup's active row contains
  both its text and the 20-cell 3-metric glyph; a non-active popup row contains its text (no SES)
  and the 20-cell 2-metric glyph, with no SES-shaped dots present. Run `cc-tmux self-test` and
  paste the passing stdout. [owner:general-purpose] [type:testing]
- [ ] [4.5] Live verification: with a real tracked pane in this repo, confirm row 2's actual [beads:if-1r71]
  on-screen render shows both the text and the new glyph, using real 5H/7D from nexus-agent (per
  `design.md` § Live-data verification gap, SES may need to stay illustrative if
  `nx_agent.session_context()` still returns `None` for every tracked session on this machine —
  attempt the real pull first, only fall back to illustrative if it's still unavailable, and note
  which happened). Click the account label and confirm the popup's active-row and non-active-row
  glyphs render as expected. Paste observed output (both row 2 and the popup).
  [owner:general-purpose] [type:testing]
