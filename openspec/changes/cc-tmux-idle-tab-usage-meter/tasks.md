<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-d4uv -->

# Tasks: cc-tmux-idle-tab-usage-meter

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = ramp table + index function + scale constant; API = `idle_usage_meter` /
> `resolve_tab_glyph` pure functions; UI = `render_tabs_row` styling + `_build_tabs_row`
> plumbing; E2E = self-tests + live verification). Owner: general-purpose engineer agents (no
> dedicated api/ui roles for this Python tmux plugin). Full design rationale (ramp table,
> round-to-nearest indexing, colour reuse lock, flash mechanics, None fallback, cost bounds) in
> `design.md` — do not re-derive here.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: add module-level `IDLE_METER_SCALE_TOKENS = 1_000_000`, the 17-glyph `IDLE_METER_RAMP` tuple exactly as listed in `design.md` § The 17-state ramp (`░ ⡀ ⣀ ⣄ ⣤ ⣦ ⣶ ⣷ ⣿ ⢿ ⠿ ⠻ ⠛ ⠙ ⠉ ⠈ ▓`), and `_idle_meter_index(ratio: float) -> int` returning `round(max(0.0, min(1.0, ratio)) * 16)`. Cite the design.md section in a comment rather than re-deriving the table inline. [owner:general-purpose] [type:api] [beads:if-i8eq]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: add `idle_usage_meter(raw_tokens: Optional[float], now: float) -> Tuple[str, str]` returning `(glyph, color)`. `raw_tokens is None` -> `(IDLE_GLYPH, "")` (byte-identical fallback to today's idle rendering, empty color = emit no styling — see design.md § None fallback; NEVER return the flash for missing data). Otherwise `ratio = raw_tokens / IDLE_METER_SCALE_TOKENS`, `idx = _idle_meter_index(ratio)`; at `idx == 0` alternate `IDLE_METER_RAMP[0]` with `"⠀"` (U+2800, same column width) on `int(now / FRAME_PERIOD_SEC) % 2` (design.md § Flash); else `IDLE_METER_RAMP[idx]`. Color is ALWAYS `resolve_context_color(raw_tokens, now)` reused verbatim — no meter-specific colour logic (locked decision, design.md § Color). [owner:general-purpose] [type:api] [beads:if-hmnz]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: add `resolve_tab_glyph(state: str, now: float, fg_count: int, bg_count: int, raw_tokens: Optional[float] = None) -> Tuple[str, str]` — when `fg_count == 0 and bg_count == 0 and state == "idle"` return `idle_usage_meter(raw_tokens, now)`; every other case returns `(resolve_tab_icon(state, now, fg_count, bg_count), "")` so waiting/active animations and the sub-agent overlay precedence stay byte-identical. Do NOT modify `resolve_tab_icon` or `animated_icon` themselves (legacy `cmd_window_icon` still calls them — design.md § API shape). [owner:general-purpose] [type:api] [beads:if-vslh]

## UI Batch

- [x] [3.1] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_tabs_row`, read `raw_tokens = getattr(w, "raw_tokens", None)` (duck-typed with default, same convention as `fg`/`bg`) and switch the icon call from `resolve_tab_icon(...)` to `resolve_tab_glyph(state, now, fg_count, bg_count, raw_tokens)`. When the returned color is non-empty, compose the icon part as `#[fg={color}]{glyph}#[fg={colour}] ` (meter colour on the glyph only, then restore the segment's existing label `colour` — CYAN,bold active / DIM inactive — before index/name); when color is empty, keep today's exact `{icon} ` composition so a colorless render is byte-identical to the current output. Update the docstring's contract note. [owner:general-purpose] [type:ui] [beads:if-gwta]
- [x] [3.2] `apps/cc-tmux/src/cc_tmux/cli.py`: in `_build_tabs_row`, after the existing `w.bg` prune loop, populate `w.raw_tokens` ONLY for windows where `state == "idle"` and `w.fg` is 0 and the pruned `w.bg` is empty: resolve the representative pane via `_resolve_session_pane(w.id)` and set `w.raw_tokens = _resolve_ses_tokens(pane)` (`None` on any failure — fail-open, matching this function's conventions); all other windows get `w.raw_tokens = None` (or simply never set, relying on 3.1's getattr default — pick one and be consistent). Do not add any new HTTP or cache layer — `_resolve_ses_tokens` -> `nx_agent.session_context` is already disk+negative cached per session-id (design.md § Data plumbing). Legacy `cmd_window_icon` stays completely unchanged. [owner:general-purpose] [type:ui] [beads:if-qr07]

## E2E Batch

- [x] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `_idle_meter_index` + `idle_usage_meter`: ratio sweep hits Leo's nominal glyphs exactly (0.0625 -> `⡀`, 0.5 -> `⣿`, 0.75 -> `⠛`, 0.9375 -> `⠈`, 1.0 and above-1.0-clamped -> `▓`); index-0 flash alternates `░`/`⠀` across two `now` values of opposite parity; `None` -> `(IDLE_GLYPH, "")` exactly; colour equals `resolve_context_color(raw_tokens, now)` for one raw_tokens value in each of the 6 severity tiers, including a >750k case checked at both parities (pulse pair comes through verbatim). Run `cc-tmux self-test` and paste the passing stdout. [owner:general-purpose] [type:testing] [beads:if-7sfa]
- [x] [4.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `resolve_tab_glyph` precedence (waiting/active/fg=1/bg=2 cases return `(resolve_tab_icon(...), "")` byte-identical; only plain idle routes to the meter) and `render_tabs_row` wiring (an idle window object with `raw_tokens=500_000` renders the `⣿` glyph wrapped in a `#[fg=` colour that then restores the label colour; the same window with `raw_tokens` absent renders today's exact segment string; active-window CYAN-bold highlighting and `#[range=window|...]` markup unchanged). Run `cc-tmux self-test` and paste the passing stdout showing zero failures overall (update any pre-existing tab-row assertions that legitimately changed shape rather than deleting or skipping them). [owner:general-purpose] [type:testing] [beads:if-au6q]
- [x] [4.3] Live verification: re-register the plugin bindings/format in the running server via `tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux`, then capture the REAL rendered tabs row (interrogate the status line, e.g. capture-pane on the status area or read the evaluated `status-format[0]` — NOT `display-message -F` alone, which never runs `#()` jobs). Attempt real data first; the known `session_context() -> None` gap on this machine (design.md § None fallback) means idle tabs likely render the `█` fallback — that fallback IS a required observation. Then inject an illustrative value by writing a synthetic nx-agent context cache entry for a tracked session (same on-disk cache `nx_agent._fetch_cached` reads) with e.g. `usedPercentage`/`contextWindowSize` yielding ~500k raw tokens, confirm the live tab shows a coloured `⣿`, and afterwards delete the synthetic cache entry. Paste both captured outputs (fallback + illustrative) and note which data path each exercised. [owner:general-purpose] [type:testing] [beads:if-m8ds]

  **Verified live 2026-07-14** (apply/20260714-1609-b4e1cf3e). Real-data captures (no
  synthetic input) hit the colored-glyph branch directly, superseding the design doc's stated
  `session_context() -> None` gap — nx-agent now returns real `usedPercentage` for every tracked
  session on this machine (confirmed via direct `curl localhost:7400/sessions/:id/context`), so
  the `█` fallback was NOT observed live (nothing to fall back from) — noted, not treated as a
  failure per the task brief. Real render-all captures against genuinely idle windows during this
  session showed the meter firing correctly end-to-end: nx window (56% used -> 560,000 raw
  tokens) rendered ramp idx 9 `⢿` colored RED (`#E61F44`, matches the `>500_000` tier); cc window
  (25% -> ~250,000 tokens) rendered idx 4 `⣤` colored ORANGE/YELLOW-tier `#FAC760`. For the
  illustrative ~500k-token case, the live session's panes were changing state too rapidly
  (multiple concurrent agent dispatches) to reliably catch and inject against one of Leo's real
  panes mid-render, so the injection was done against an isolated scratch tmux window (`tmux
  new-window`, never touching any of Leo's 5 existing windows/6 panes) registered idle via the
  real `cc-tmux register --state idle` CLI path and given `@cc-session-id` reused from a real
  tracked session (`dbc555f4-...`). Writing `{"usedPercentage": 50, "contextWindowSize":
  1000000}` to that session's on-disk nx-agent cache file
  (`/tmp/cc-tmux-nx-context-cache.1000.dbc555f4....json`) and running the worktree's `bin/cc-tmux
  render-all <scratch-window>` produced `#[fg=#FF8C00]\u{28ff}` — ramp idx 8, `⣿`, colored ORANGE
  (exactly 500,000 tokens is not `>500_000` so it falls in the `>300_000` ORANGE tier rather than
  RED — correct per `_context_color_pair`). Synthetic cache file deleted immediately after
  capture; scratch window killed; live server's keybinding registration restored to the
  main-checkout plugin (`tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux`), confirmed via
  `tmux list-keys -T prefix` showing `bind-key -T prefix o run-shell
  ".../installfest/apps/cc-tmux/bin/cc-tmux cycle"` (main checkout path, not the worktree). The
  `~/.tmux/plugins/cc-tmux` symlink itself was never modified — only the keybinding rebind (via
  `run-shell` on `cc-tmux.tmux`) and direct `render-all` invocations against the worktree binary
  were used to exercise worktree code, exactly as task step 2 specified.
