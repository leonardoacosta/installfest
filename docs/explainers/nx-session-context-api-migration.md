---
status: current
updated: 2026-07-13
---

# nx session-context API migration — impact on cc-tmux

## What changed

nx (`~/dev/personal/nexus`) shipped and merged `add-session-context-api` on
2026-07-13 (archived at
`nexus/openspec/changes/archive/2026-07-13-add-session-context-api/`). It
consolidated nexus-statusline's two overlapping per-render context-window
caches into one session-id-keyed source, exposed over HTTP on nx-agent
instead of a pane-keyed file on disk.

**The pane-keyed file `session-context.<pane_id>.json` is gone — clean
replacement, not a compat shim.** nx-agent stays deliberately pane-agnostic;
it has zero tmux awareness anywhere in its codebase, by design.

The same day, two more nx specs landed (`add-project-status-snapshots`,
`add-git-status-orbit`, both archived under
`nexus/openspec/changes/archive/2026-07-13-*`) that add a related-but-separate
git-status surface, described below since cc-tmux reads git fields too.

## Why this breaks cc-tmux today

`apps/cc-tmux/src/cc_tmux/cli.py`'s `_read_session_context(pane_id)` reads
`session-context.<pane_id>.json` for `(model_letter, context_used_pct,
branch, dirty, ahead)`, feeding `_build_session_bar`'s row-2 status line. That
file is no longer written by nx as of this merge. Effects, gated by the
existing `SESSION_CONTEXT_MAX_AGE_SECS = 900` (15 min) freshness cutoff in
`_read_session_context`:

| Field | What happens | Why |
|---|---|---|
| `context_used_pct` | Goes to `None` (row hides the SES gauge) ~15 min after the last real write | No other source — was nx-only |
| `model_letter` | Goes to `""` (blank) ~15 min after the last write | No other source in cc-tmux today |
| `dirty` | Goes to `False` permanently once stale | No fallback in `_build_session_bar` |
| `ahead` | Goes to `0` permanently once stale | No fallback in `_build_session_bar` |
| `branch` | **Keeps working** | Falls back to `@cc-branch` tmux pane option (hook-coupled, set independently of the session-context file) — see `_build_session_bar`'s `ctx_branch or tmux.get_pane_option(pane, tmux.OPT_BRANCH)` |

Net: the row-2 session bar silently degrades to project + branch only within
about 15 minutes of this nx deploy reaching a machine. Nothing crashes — the
whole path is fail-open by design — but it's a real, live regression versus
current cc-tmux behavior, not a hypothetical.

## The new nx surface (context-window)

```
GET  /sessions/:id/context
PATCH /sessions/:id/context
```

Keyed by **`session_id`**, not pane id or project code. In-memory, 600s TTL
on the nx-agent side (not Postgres).

Response shape (`~/dev/personal/nexus/packages/core/src/types/session-context.ts`):

```ts
{
  sessionId: string,
  usedPercentage: number,       // 0-100, same convention as context_used_pct
  contextWindowSize: number | null,
  updatedAt: string,            // ISO 8601
}
```

**cc-tmux has zero session-id state today** (confirmed: pane-only tracking
throughout `cli.py`/`registry.py`). To use this endpoint, cc-tmux needs to:

1. Capture `session_id` from Claude Code's hook payload in `cmd_register`
   (`cli.py`) — the SessionStart hook entrypoint, same place `@cc-project`
   and other pane options are currently set from hook data.
2. Store it as a new tmux pane option (e.g. `@cc-session-id`).
3. Query `GET http://localhost:7400/sessions/:id/context` by that id from
   `usage.py` (or a new module), same pattern as the existing
   `CREDENTIALS_URL` query in `usage.py` — 1s timeout, fail-open, on-disk
   cache to bound fetch frequency (mirror the precedent `usage.py` already
   documents for its 5h/7d credentials query).

This does **not** cover `model_letter` — nothing in the new nx surface
carries a model tag. That's an open gap, not solved by this endpoint.

## The new nx surface (git status) — a partial `dirty` replacement

```
GET /projects/:id/status        # includes an optional `git` object
GET /projects/:id/git-events?days=<n>
```

Keyed by **project code**, not session id or pane id — cc-tmux already
resolves `@cc-project` per pane, so this is the easier of the two to wire.

The `git` object (`~/dev/personal/nexus/packages/core/src/types/git-status.ts`), folded
into `GET /projects/:id/status` and omitted entirely when nx's git-observer
hasn't polled that project yet:

```ts
{
  branch: string | null,   // null on detached HEAD
  headSha: string,
  detached: boolean,
  dirty: { modified: number, untracked: number },  // richer than the old boolean
  observedAt: string,      // ISO 8601, last poll time
}
```

This could replace the `dirty` boolean with real counts ("N changed, M new"
instead of a flag) — nx's git-observer polls every 60s. **It does not carry
an `ahead`/`behind` vs-upstream count** — no such field exists anywhere in
the new payload or the underlying `git_events` table. `ahead` has no current
nx-side replacement; it'd need either a local `git rev-list --count` shell-out
in cc-tmux (which may already exist elsewhere as `@cc-branch`'s sibling — not
verified here) or a future nx change to add it to git-observer.

## Reference

- nx bead `nx-yn6c2` — "cc-tmux: capture session_id from CC hook payload,
  query nx's new /sessions/:id/context endpoint" (filed in nx's tracker,
  explicitly scoped as "lives entirely in the cc-tmux repo/plugin, not nx").
- `nexus/openspec/changes/archive/2026-07-13-add-session-context-api/` —
  design rationale for dropping the pane-keyed file.
- `nexus/openspec/changes/archive/2026-07-13-add-project-status-snapshots/`
  and `.../2026-07-13-add-git-status-orbit/` — the git-status surface.
- `~/dev/personal/nexus/packages/core/src/types/session-context.ts` and
  `~/dev/personal/nexus/packages/core/src/types/git-status.ts` — authoritative
  Zod contracts; prefer these over this doc if they've drifted.

## Not covered here

This is an awareness doc, not a design. Whether cc-tmux adopts both new
endpoints, one, or neither (and how `model_letter`/`ahead` get resolved) is
a decision for a future `/feature` in this repo — not decided by this file.
