---
status: draft
---

# Proposal: cc-tmux-adopt-nx-context-and-git-status

## Why

`docs/explainers/nx-session-context-api-migration.md` (2026-07-13) documents that nx
(`~/dev/personal/nexus`) retired the pane-keyed `session-context.<pane_id>.json` file
`cc_tmux/cli.py`'s `_read_session_context` reads for row 2's `context_used_pct`, `branch`,
`dirty`, and `ahead` fields — "clean replacement, not a compat shim," per the doc. In its place,
nx shipped two HTTP surfaces on nx-agent (port 7400, already the credentials-endpoint host
`usage.py` queries):

- `GET /sessions/:id/context` — session-id-keyed, in-memory, 600s TTL. Carries
  `usedPercentage`/`contextWindowSize`. No model tag.
- `GET /projects/:id/status` — project-code-keyed, backed by a 60s-polling git-observer. Its
  optional `git` object carries `branch`/`dirty: {modified, untracked}`/`detached`/`observedAt`.
  No ahead/behind-vs-upstream field anywhere in the payload or the underlying `git_events` table.

User ask: **"We want nx to be the sole source of our data for these integrations."** Refined via
clarifying questions into four concrete decisions this proposal implements:

1. **branch/dirty**: nx (`/projects/:id/status`'s `git` object) is the PRIMARE source; the
   existing local `git` shell-out (`tmux.set_pane_git_identity`) remains as a silent fallback for
   when nx-agent is unreachable — not the strict zero-fallback reading of "sole source." This is
   a resilience-over-purity call the user made explicitly when asked.
2. **ahead**: nx has no field for this today (confirmed absent from both the live payload and the
   `git_events` schema). Rather than block on nx-side work outside this repo, cc-tmux computes it
   locally via a NEW `git rev-list --count @{upstream}..HEAD`-style shell-out, mirroring the
   existing `_git_branch` pattern in `tmux.py`. This is a genuinely local-only field today, not a
   "primary nx / fallback local" split like branch/dirty — there is nothing on the nx side to
   prefer. A bead is filed for nx to add ahead/behind to `git-observer` as a future follow-up;
   this field switches to the nx-primary/local-fallback shape once that lands.
3. **model_letter**: no source exists anywhere right now — not local, not nx. The retired file was
   the only place this ever came from, and nx's new session-context payload does not carry a model
   tag (confirmed absent from `packages/core/src/types/session-context.ts`). A prior attempt to
   source it from the SessionStart hook payload was already tried and rejected (`cli.py`'s own
   comment: "confirmed empty on every live pane... also misses mid-session `/model` switches").
   Per the user's explicit call, this proposal does NOT reintroduce that rejected mechanism or
   invent a new one — the `model_letter` code path in `_read_session_context` is left untouched,
   and the pre-existing (already happening, independent of this proposal) degradation to a blank
   segment once nx stops writing the legacy file is accepted and documented, not fixed here. A
   bead is filed asking nx to add a model field to `/sessions/:id/context`.
4. **dirty display**: nx's `git` object gives real `{modified, untracked}` counts instead of the
   old boolean. The user chose to surface the richer counts (`render_session_bar`'s `dirty` param
   changes shape) rather than collapse them back to a single boolean `*`.

**Project-code join risk, checked, not assumed:** `/projects/:id/status`'s `:id` is looked up
against nx's OWN project registry (`~/.claude/scripts/config/projects.json`, loaded by
`config-loader.ts`) — a DIFFERENT file from the one `cc_tmux/registry.py` reads
(`home/projects.toml`, this repo's own registry; see `project_repo_relocation_if_to_installfest`
memory — these two registries have drifted before). Verified directly: both registries currently
assign the SAME code (`if` → `~/dev/personal/installfest`, `nx` → `~/dev/personal/nexus`) for
every project this machine actively runs cc-tmux against. This is not guaranteed to hold
fleet-wide forever (two independently hand-maintained files), so the design treats an
unknown-project 404 from nx exactly like an unreachable nx-agent — fail open to the local
fallback — rather than assuming the join always succeeds.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/nx_agent.py`** (new): HTTP client for nx-agent's two new endpoints.
  `session_context(session_id)` queries `GET /sessions/:id/context`; `project_git_status(code)`
  queries `GET /projects/:id/status` and extracts the `git` sub-object. Both reuse `usage._query()`
  for the actual fetch (1s timeout, fail-open `None`) rather than duplicating urllib boilerplate,
  and both are backed by a short-TTL (~5s) on-disk cache mirroring `usage.py`'s
  `_read_usage_cache`/`_write_usage_cache` atomic-write pattern — a fresh generic cache pair here
  rather than generalizing `usage.py`'s (payload-shape-specific, `_`-private, and already shipping
  in production — not worth the regression risk of a cross-cutting refactor for this scope).
- **`apps/cc-tmux/src/cc_tmux/tmux.py`**: `set_pane_git_identity` gains local `dirty`/`ahead`
  resolution (`_git_dirty`, a new `git status --porcelain` counter; `_git_ahead`, a new
  `git rev-list --count` shell-out) alongside the existing `_git_branch` call, on the same
  waiting/idle-only cadence (Invariant 4 — never on the hot `active` path). Two new pane options,
  `@cc-dirty` (JSON `[modified, untracked]`) and `@cc-ahead` (stringified int), join `@cc-branch`
  in `_ALL_OPTS` so `clear_pane_state` retires them with everything else on `SessionEnd`. A third
  new option, `@cc-session-id`, is captured in `cmd_register`'s existing `SessionStart` branch
  (same block that already captures `session_title`) from the hook payload's `session_id` field.
- **`apps/cc-tmux/src/cc_tmux/cli.py`**: `_build_session_bar` sources `context_used_pct` from
  `nx_agent.session_context(@cc-session-id)`; sources `branch`/`dirty` from
  `nx_agent.project_git_status(@cc-project's registry code)` when present, falling back to
  `@cc-branch`/`@cc-dirty` pane options when nx returns nothing (unreachable, 404, or no `git`
  field yet); sources `ahead` from the local-only `@cc-ahead` pane option. `model_letter` sourcing
  is untouched — still `_read_session_context`'s read of the (now-defunct) legacy file, degrading
  to blank per the existing freshness-cutoff fail-open path, exactly as the explainer doc predicts.
- **`apps/cc-tmux/src/cc_tmux/render.py`**: `render_session_bar`'s `dirty` parameter changes from
  `bool` to `Optional[Tuple[int, int]]` (modified, untracked); renders `*<modified>+<untracked>`
  when the total is nonzero, nothing when `None`/`(0, 0)`. `ahead`'s existing `int` param and
  `^N` rendering are unchanged.

## Non-Goals

- No change to `@cc-project` resolution — that stays sourced from `registry.py`'s own
  `home/projects.toml` lookup (cc-tmux's project-registry concern), not from nx. Only the
  git-working-tree fields (branch/dirty) and the two nx-only fields (context %, model letter)
  are in scope for "these integrations."
- No fix for `model_letter`'s blank-after-migration state — see Why item 3. A bead tracks the
  nx-side ask; this proposal explicitly does not reintroduce the already-rejected SessionStart-hook
  mechanism.
- No nx-side change of any kind (no `ahead`/`behind` field added to `git-observer`, no `model`
  field added to session-context) — both are filed as beads against nx's own tracker, out of this
  repo's scope.
- No removal of the legacy `_read_session_context`/`SESSION_CONTEXT_MAX_AGE_SECS` machinery — it
  keeps running exactly as today for `model_letter` (Non-Goal above); only the pieces of it that
  fed `context_used_pct`/`branch`/`dirty`/`ahead` are superseded by the new sources.
- No reconciliation of the `home/projects.toml` vs `~/.claude/scripts/config/projects.json`
  registry-drift risk itself — flagged and fail-open-guarded (see Why), not fixed; a fleet-wide
  registry unification is a separate, larger concern outside this proposal.

## Context

- Extends: `apps/cc-tmux/src/cc_tmux/usage.py` (`_query()` reused, unmodified)
- Extends: `apps/cc-tmux/src/cc_tmux/tmux.py` (`set_pane_git_identity`, `_ALL_OPTS`, the
  `_git_branch`-sibling shell-out pattern)
- Extends: `apps/cc-tmux/src/cc_tmux/cli.py` (`cmd_register`, `_read_session_context`,
  `_build_session_bar`)
- Related: `docs/explainers/nx-session-context-api-migration.md` — the awareness doc this
  proposal directly resolves (its own closing line: "a decision for a future `/feature` in this
  repo — not decided by this file").
- Related: nx bead `nx-yn6c2` ("cc-tmux: capture session_id from CC hook payload, query nx's new
  `/sessions/:id/context` endpoint"), filed in nx's own tracker, explicitly scoped as living
  entirely in this repo — this proposal is that work.
- Related: `project_repo_relocation_if_to_installfest` memory (prior fleet-registry path-drift
  incident) — motivates the fail-open join-risk guard in Why.
- touches: `apps/cc-tmux/src/cc_tmux/nx_agent.py`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/testing.py`, `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| `nx_agent.session_context()` / `project_git_status()` (HTTP + cache, injectable path/urlopen) | `cc-tmux self-test` cases: cache hit skips fetch, unreachable nx-agent -> `None` (fail open), well-formed payload extracts correctly — task 4.1 |
| `tmux._git_dirty()` / `_git_ahead()` (pure-ish, git subprocess) | `cc-tmux self-test` cases against a fixture repo: clean tree -> `None`/`0`, dirty tree -> correct counts, no upstream -> `0` (fail open) — task 4.1 |
| `cli.cmd_register` session_id capture | `cc-tmux self-test` + register-trace inspection: SessionStart payload with `session_id` sets `@cc-session-id`; non-SessionStart events leave it untouched — task 4.2 |
| `cli._build_session_bar` dual-source composition (nx primary, local fallback for branch/dirty; local-only ahead; untouched model_letter) | `cc-tmux self-test` cases: nx reachable -> nx values used; nx unreachable/404 -> local fallback values used; both absent -> fail-open blank — task 4.2 |
| `render.render_session_bar` dirty-counts format | `cc-tmux self-test` case: `(3, 2)` renders `*3+2`; `(0, 0)`/`None` renders nothing — task 4.3 |
| End-to-end row-2 render against a live pane + live nx-agent | Live verification: real pane, real nx-agent, confirm SES%/branch/dirty-counts/ahead render correctly and model letter degrades to blank without erroring — paste observed output — task 4.4 |
