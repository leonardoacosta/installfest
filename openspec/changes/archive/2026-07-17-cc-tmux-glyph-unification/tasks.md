---
stack: cc-meta
---
<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-u2nz -->

# Tasks: cc-tmux-glyph-unification

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no UI batch
> (no `tmux.conf.tmpl`/theme-file changes; everything lives in `render.py`/`cli.py`). Owner:
> general-purpose engineer agents (no dedicated api/ui roles for this Python tmux plugin).
> Verification is `python -c` function-level stdout + live `cc-tmux render-all` capture — no
> pytest harness exists in this plugin.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: unify the idle no-data fallback onto the ramp. In `idle_usage_meter`, replace the `raw_tokens is None` -> solid-block return with the ramp state-0 glyph `░` rendered STATIC in DIM styling (no flash, no meter colour). Remove the solid-block constant from tab-state rendering entirely (grep first: if other call sites still consume it, leave the constant defined but unused by tabs and note the citation). The genuinely-fresh state-0 flash (`░` <-> blank) and all other ramp states are unchanged. [beads:if-j44c]
- [x] [1.2] `apps/cc-tmux/src/cc_tmux/render.py`: ramp-adjacent active pulse. Extend the active branch (currently a fixed two-glyph braille pair in `animated_icon`) to accept the session's raw token count: compute meter state `i` via the existing index helper and flash between ramp glyphs `i` and `min(i+1, 16)` on the existing wall-clock parity; at state 16 flash 15 <-> 16. `None` tokens -> flash `░` <-> `⡀` uncoloured. Reuse `resolve_context_color` for the data-present pulse colour exactly as the idle meter does — no new colour logic. [beads:if-w7iy]
- [x] [1.3] `apps/cc-tmux/src/cc_tmux/render.py`: collapse the sub-agent overlay to one diamond pair. In `resolve_tab_icon`, replace the four fg/bg count-keyed braille flash pairs with a single `◇` <-> `◆` wall-clock swap for ANY nonzero tracked sub-agent activity. Delete the now-dead four pair constants (grep-verified no other references). Precedence and bg pruning logic untouched. [beads:if-jwq8]
- [x] [1.4] Function-level verification (paste stdout): `python -c` calls exercising `idle_usage_meter` (500000 -> `⣿`; None -> DIM `░` static), the active pair (70000 -> `⡀`/`⣀`; state-16 tokens -> `⠈`/`▓`; None -> `░`/`⡀`), and `resolve_tab_icon` (fg=1 and bg=2 both -> `◇`/`◆` by parity; fg=0/bg=0 -> state glyph). [beads:if-5v0o]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/cli.py`: widen raw-token resolution in `_build_tabs_row` from idle-only to idle AND active windows, feeding the new active pulse. Rely on the existing nx-agent short-TTL disk cache — no new caching layer, no extra per-tick network beyond the cache contract. Waiting/sub-agent windows keep `raw_tokens=None` (their glyphs don't consume it). [beads:if-1l0u]
- [x] [2.2] Verification (paste stdout): with a live active session, run `cc-tmux render-all <window_id> <w> <h>` twice at opposite wall-clock parities and paste both captures showing two DISTINCT adjacent ramp frames for the active window. [beads:if-duk8]

## E2E Batch

- [x] [3.1] Live acceptance sweep (paste captures): (a) idle window with data -> single static ramp glyph, correct colour tier; (b) simulate no-data (unset the pane's session id option on a scratch pane or point nx-agent URL at a dead port for one render) -> DIM `░`, no solid block anywhere in the row; (c) dispatch a foreground sub-agent (any Task call in a tracked pane) -> `◇`/`◆` alternation across two parities; (d) grep-negative over `render.py` for the removed braille pair constants. [beads:if-77ik]
- [x] [3.2] Reload the live tmux plugin (`tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux` — bindings/config changes need re-registration per repo memory) and confirm the status row renders the new glyph language in the real session bar (screenshot or `tmux capture-pane` paste). [beads:if-safu]
