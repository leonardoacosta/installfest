---
order: 0719a
---

# Proposal: Prune Pass for Raycast + cmux-workspaces Generators

## Change ID
`add-launcher-registry-prune-pass`

## Summary
Add an auto-delete prune step to `scripts/generate-raycast.sh` and `scripts/cmux-workspaces.sh`
so both generators remove launcher artifacts for project codes no longer present in
`home/projects.toml`, instead of leaving orphans behind (`es`/`gd`/`pp`/`sj` in
`platform/raycast-scripts/local/`, `es`/`pp` in `.../cloudpc/`, per `docs/audit/cmux.md`
recommendations I1/I3/I5 and `scripts/audit-projects.sh`'s existing orphan detection).

## Context
- Extends: `scripts/generate-raycast.sh`, `scripts/cmux-workspaces.sh`
- Related: `scripts/audit-projects.sh` (existing orphan detection — FAILs the audit on an
  orphan, by design, precisely because neither generator prunes; this proposal reuses its
  registry-diff logic rather than fixing the audit itself), `docs/audit/cmux.md` (source of
  I1/I3/I5 recommendations)
- touches: `scripts/generate-raycast.sh`, `scripts/cmux-workspaces.sh`, `scripts/lib/registry.sh`

## Motivation
`home/projects.toml` is the single source of truth for both generators (I1 already landed), but
neither generator cleans up its own output when a project code is removed from the registry —
`scripts/audit-projects.sh`'s section 3 (`raycast-sync`) already FAILs the audit on the resulting
orphans (5 known today: `es`/`gd`/`pp`/`sj` local, `es`/`pp` cloudpc) but never fixes them. Every
registry removal currently requires a manual `rm` pass across 3 output directories per generator.

## Requirements

### Requirement: Raycast generator prunes orphaned scripts on every run
`scripts/generate-raycast.sh` SHALL, after writing the current registry's scripts, delete any
`{code}.sh` file in `platform/raycast-scripts/`, `platform/raycast-scripts/local/`, or
`platform/raycast-scripts/cloudpc/` whose `{code}` is not a key in the current `projects.toml`
`[projects]` table. `--dry-run` SHALL list what would be deleted without deleting.

### Requirement: cmux-workspaces generator prunes orphaned registry-derived state
`scripts/cmux-workspaces.sh` SHALL apply the same prune step to any of its own generated
per-project artifacts that are keyed by project code and derived solely from `projects.toml`
(scoped during implementation to whatever `cmux-workspaces.sh` actually writes per-code today —
if it turns out to generate nothing prunable beyond the raycast scripts already covered above,
this requirement is satisfied by a no-op with that finding documented in a code comment, not by
inventing new per-code output to prune).

### Requirement: Prune runs before the picker/dashboard-sensitive reorder step
Pruning MUST happen before any step that reads the output directory listing (e.g. Raycast's own
directory-scan for its script picker), so a stale entry never appears in a live picker even
transiently mid-run.

## Scope
- **IN**: prune logic in both generators; reusing `audit-projects.sh`'s registry-diff pattern
  (never re-deriving it); `--dry-run` prune preview.
- **OUT**: changing `audit-projects.sh` itself (it stays a read-only auditor); pruning anything
  not derived from `projects.toml` project codes; the other `docs/audit/cmux.md` recommendations
  (I3 polling, I4 preflight checks, I5 `--kill` flag) — separate, unrelated scope.

## Done Means
- Removing a project's entry from `projects.toml` and re-running `generate-raycast.sh` deletes
  that project's `.sh` files from all 3 output directories on the next run, with no manual `rm`.
- `generate-raycast.sh --dry-run` after a registry removal prints exactly which files it would
  delete, without deleting them.
- `scripts/audit-projects.sh` reports zero orphan raycast scripts after a normal generator run
  following a registry change (proving the generator's own prune, not the auditor, resolved it).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `generate-raycast.sh` prune logic | N/A — bash script, no unit harness in this repo | `[4.1]` |
| `cmux-workspaces.sh` prune logic | N/A — bash script | `[4.2]` |
| `audit-projects.sh` zero-orphan confirmation | N/A | `[4.3]` |

## Impact
| Area | Change |
|------|--------|
| `scripts/generate-raycast.sh` | Add post-generation prune pass over 3 output dirs |
| `scripts/cmux-workspaces.sh` | Add equivalent prune pass (scope TBD by [2.1]'s finding) |

## Risks
| Risk | Mitigation |
|------|-----------|
| Prune deletes a file a human hand-edited outside the generator | Generators already fully own their output dirs (regenerated every run) — no hand-edit convention exists to break; document this invariant in a comment at the prune call site |
| A bug in the registry diff prunes a still-valid code | Reuse `audit-projects.sh`'s already-battle-tested diff logic verbatim rather than reimplementing; `--dry-run` lets a human preview before trusting a fresh implementation |
