---
stack: t3
---
<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-8oi8 -->

# Implementation Tasks

## UI Batch

- [x] [1.1] In `apps/cc-tmux/src/cc_tmux/usage.py`: extend `extract_active` to also return the active credential's `usage5hResetAt` epoch (reuse `_extract_reset_at` — do not re-derive ISO parsing), widen the cache payload (`_read_usage_cache`/`_write_usage_cache`/`active_usage`) from the `(label, u5, u7)` triple to include `r5` (float epoch or None; a cache file missing the key reads as None — fail-open, no cache-version bump needed). In `render.py`, append `·<countdown>` in DIM to the row-2 5H segment (line ~710) ONLY when `u5 >= 0.80` and `r5` is a future epoch: minutes form `47m` under 60 minutes, `1h12m` at/above; past/absent `r5` or `u5 < 0.80` renders the segment byte-identical to today. Add `testing.py` self-test cases: formatting (m / h+m / past -> absent), threshold gate (0.79 no countdown, 0.80 countdown), cache round-trip of `r5`, absent-field fail-open. Run `python -m cc_tmux` self-test from `apps/cc-tmux` and paste PASS output. [beads:if-d979]

  **Scope note (Leo decision, 2026-07-20)**: the literal file list above (usage.py/render.py/testing.py
  only) would either crash `cli.py`'s two hardcoded 3-tuple unpacks of `active_usage()`/`_active_usage()`,
  or ship a countdown that never renders on the real deployed status line — the primary production data
  path is `_resolve_session_usage(pane)` -> `nx_agent.session_usage()`, with `active_usage()` only a
  fallback. Leo chose full end-to-end wiring (option B) over a backward-compatible opt-in shim; `cli.py`
  was added to this task's scope. See the wave-plan decision log
  (`docs/apply/apply-2026-07-19-001/wave-plan.json`) for the full tradeoff writeup.

  **Implementation**: `usage.py`'s `extract_active`/`active_usage`/`_read_usage_cache`/`_write_usage_cache`
  widened to the real 4-element `(label, u5, u7, r5)` shape; `r5` reuses `_extract_reset_at` (no
  re-derived ISO parsing). Cache read treats a missing/null `r5` key as `None` — fail-open, no
  cache-version bump. `cli.py`: `_active_usage()` (~line 1005/1061) re-typed and returns the 4-tuple;
  `_resolve_session_usage(pane)` returns `(five_h, seven_d, five_h_reset)`, sourcing `five_h_reset`
  primarily from `nx_agent.session_usage()`'s real `fiveHour.resetsAt` field (confirmed to genuinely
  exist in nx's SessionStatusResponse), falling back per-field to `active_usage()`'s global `r5`
  exactly like `u5`/`u7` already do. `_build_session_bar` threads `five_h_reset` into
  `render.render_session_bar`. The other pre-existing 3-tuple unpack site (`_build_beads_bar`,
  ~line 1844) was also widened to 4 (extra value unused, matching the file's existing underscore
  convention). `render.py`: new `_ROW2_COUNTDOWN_THRESHOLD` (0.80), `_format_row2_countdown`
  (`47m` / `1h12m` form), `_row2_reset_countdown` (threshold + future-epoch gate);
  `render_session_bar` gained `five_h_reset`/`now` kwargs (both default `None`, unwired-safe) — the
  5H segment grows a DIM `·<countdown>` suffix immediately after the percentage when gated in,
  otherwise byte-identical to before. `testing.py`: fixed every pre-existing 3-tuple assumption about
  `extract_active`/`active_usage`/`_resolve_session_usage`/`_active_usage` (would otherwise raise
  `ValueError` at runtime with the widened signature), plus 4 new cases: countdown formatting
  (m/h+m/rounding), threshold gate (0.79 vs 0.80, absent/past `r5`, absent `u5`), cache round-trip of
  `r5` + old-cache-file/null fail-open, and full `render_session_bar` wiring including the
  below-threshold/no-reset byte-identical assertions.

  **Self-test** (independently re-run by the orchestrator, not just trusted from the implementing
  agent): `cd apps/cc-tmux && PYTHONPATH=src python3 -m cc_tmux self-test` -> `cc-tmux self-test:
  120/120 passed` (116 baseline + 4 new cases).
- [x] [1.2] Live render verification: with the deployed symlinked plugin (`~/.tmux/plugins/cc-tmux -> apps/cc-tmux`), delete the usage cache file (`/tmp/cc-tmux-usage-cache.<uid>.json`), re-render row 2 against the real nexus-agent payload, and paste the rendered string showing either the countdown (if 5H >= 80% right now) or the unchanged segment plus a forced-cache test (write a synthetic cache with `u5: 0.94, r5: now+47m` and paste the rendered `5H:94%·47m`). Reload live bindings via `tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux` if entrypoint changed (it should not). [beads:if-d8um]
  - depends on: 1.1

  **Live-verified through the real `cli.py` path** (`cli._build_session_bar('@171', pane='%240')`, a
  genuinely tracked live pane in this session — not a synthetic direct `render_session_bar()` call):
  1. Deleted `/tmp/cc-tmux-usage-cache.1000.json`, re-rendered -> fresh live fetch confirmed (new
     cache written with real `r5`); row rendered `...5H:32%... 7D:...62%...` (below 80%, correctly no
     countdown). `curl localhost:7400/statusline?sessionId=...` confirmed this session's own
     `fiveHour` is `null` server-side, so the fallback-to-`active_usage()` path is what's actually
     exercising `r5` live right now — expected/by-design, not a gap (nexus-agent doesn't always carry
     a per-session reset timestamp; the fallback exists for exactly this case).
  2. Forced a synthetic cache write (`u5: 0.94, r5: now+47m`), re-ran the same real
     `_build_session_bar` call -> `...5H:94%·47m 7D:...` — the exact `5H:94%·47m`-shaped output, RED
     percentage (94% > 80% via `color_for`) with the DIM countdown suffix rendered.
  3. Deleted the synthetic cache, re-rendered once more — reverted cleanly to live data (32%, no
     countdown); machine left in its genuine state, nothing fake left behind.

  No `tmux run-shell` reload was needed at any point, as expected for a pure render/usage/cli change
  (no entrypoint/binding change).

## E2E Batch

- [x] [2.1] Targeted `git add apps/cc-tmux/src/cc_tmux/usage.py apps/cc-tmux/src/cc_tmux/render.py apps/cc-tmux/src/cc_tmux/testing.py` (no `git add -A`/`.`); commit `feat(cc-tmux): render 5H reset countdown on row 2 near session limit`; push. Paste `git log -1 --stat` output. [beads:if-w9ke]
  - depends on: 1.1, 1.2

  **Scope note**: `cli.py` was added to the targeted add per task 1.1's scope-note (option B,
  end-to-end wiring) — the feature commit below adds 4 files, not the originally-listed 3.
  **Push note**: per `/apply:all`'s architecture, phase agents commit only; the orchestrator is the
  single push point per wave (not this task's literal "push" instruction) — the actual `git push`
  happens once, after the wave's gate passes, alongside the sibling `add-cmux-sidebar-widgets`
  work already pushed this run.

  Feature commit `abf0866`:
  ```
  commit abf0866b60d48801a43fcfb44cef0b9551c87b39
  Author: leonardoacosta <leo@leonardoacosta.dev>
      feat(cc-tmux): render 5H reset countdown on row 2 near session limit

   apps/cc-tmux/src/cc_tmux/cli.py     |  66 +++++++----
   apps/cc-tmux/src/cc_tmux/render.py  |  68 +++++++++++-
   apps/cc-tmux/src/cc_tmux/testing.py | 214 +++++++++++++++++++++++++++++++-----
   apps/cc-tmux/src/cc_tmux/usage.py   |  46 +++++---
  ```
