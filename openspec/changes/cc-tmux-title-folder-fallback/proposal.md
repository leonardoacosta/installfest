---
order: 0716a
---

# Proposal: cc-tmux-title-folder-fallback

## Why

`cc-tmux`'s `"title"`-mode window renaming (`@cc-window-rename-format title`) composes
`<project-code>·<session-title>`. Today, when the session has no title yet
(`@cc-title` unset — no `SessionStart` hook has fired, or Claude never set one), the window falls
back to whichever half resolved: if a project code resolves from the registry, the window shows
just that code (e.g. `if`) with no further context; only when NEITHER half resolves does it fall
back to `@cc-project` (the git-toplevel/dir basename).

Leo's ask (`/openspec:explore`, this session): when a session has no title, the tab should show
the folder name — not the bare project code alone. Clarified during this proposal's discovery:
"folder name" means the raw current-directory basename (the same source `"state"`-mode renaming
already uses via `os.path.basename(pane_current_path)`), and it REPLACES the whole window name
when title is absent — not appended as `code·folder` — regardless of whether a project code
resolves. This makes the fallback behavior consistent whether or not the pane's cwd happens to be
inside a registered project.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/cli.py`**: `compose_title_name` (or its caller,
  `_maybe_rename_window`) changes its fallback order. Today: `title` present -> `code·title` (or
  `title` alone if no code); `title` absent -> `code` alone (or `@cc-project` if no code either).
  New: `title` present -> unchanged (`code·title` or `title` alone); `title` absent -> the raw
  folder basename (`os.path.basename(pane_current_path)`) alone, regardless of whether a project
  code resolves. The project-code-prefixing behavior is ONLY ever applied when a real session
  title exists.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED delta on "Opt-in window rename supports a
  project-code + session-title format" — updates the "no session title yet falls back to the
  resolved project name" scenario to fall back to the folder basename unconditionally (not
  `@cc-project`, and not gated on the registry lookup failing), and clarifies that the
  code-prefix composition only applies when a title is present.

## Non-Goals

- No change to `"state"`-mode renaming (`@cc-window-rename-format state`) — it already uses the
  folder basename as its primary naming source; this proposal only touches `"title"`-mode's
  fallback path.
- No change to how the project code itself resolves (`home/projects.toml` registry lookup) — only
  to when it's used (title-present case only, going forward).
- No change to the 20-character combined truncation rule, the "no state icon in the renamed
  text" behavior, or the rename-success/failure reporting contract — all unchanged, restated
  verbatim in the spec delta.

## Context
- touches: `apps/cc-tmux/src/cc_tmux/cli.py`
- Related: `openspec/changes/archive/2026-07-11-cc-tmux-tabs-and-rename-fix/` and
  `openspec/changes/archive/2026-07-12-cc-tmux-rename-fix-and-truncate/` — prior work on the same
  rename/title requirement, restated (not touched) by this delta.

## Testing

- `apps/cc-tmux/src/cc_tmux/testing.py`: extend or add a self-test covering `compose_title_name`
  (or `_maybe_rename_window`) with title absent + project code resolving — asserts the window
  name is the folder basename alone, not the code. Existing tests for title-present composition
  and the fully-unresolved (`@cc-project`) case are restated/kept passing.
