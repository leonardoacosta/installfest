# cc-tmux Specification Delta

## MODIFIED Requirements

### Requirement: Opt-in window rename supports a project-code + session-title format
When `@cc-window-rename` is on and `@cc-window-rename-format` is `title`, the plugin SHALL rename
the pane's window to `<project-code>·<session-title>`, hard-truncated to 20 characters combined.
The project code SHALL resolve from the dotfiles project registry (`home/projects.toml`) by the
pane's current directory; the session title SHALL be captured from the `SessionStart` hook
payload's `session_title` field (the custom title if set via `/rename` or `-n`, else Claude's own
default) and persisted in `@cc-title`. Either half MAY be absent; the plugin MUST fall back to
whichever half resolved rather than leaving the window unnamed. The renamed text does NOT include
a state icon — see "Animated tab icon" below for how the icon is rendered instead. The
`rename-window` command's actual success or failure SHALL be observed and reported (not assumed
true once issued) — a failed rename MUST NOT be recorded as having renamed the window.

#### Scenario: registered project gets a code-prefixed title
- Given: `@cc-window-rename-format` is `title`, the pane's cwd is inside a project registered in
  `home/projects.toml` with code `if`, and `@cc-title` holds `"Fix ssh mesh auth flow"`
- When: the window is renamed
- Then: the window name is `if·Fix ssh mesh auth` (20 characters, code + title truncated
  together)

#### Scenario: unregistered project falls back to title alone
- Given: the pane's cwd is not covered by any registry entry, and `@cc-title` holds a title
- When: the window is renamed
- Then: the window name is the title alone, truncated to 20 characters

#### Scenario: no session title yet falls back to the resolved project name
- Given: `@cc-title` is unset (no `SessionStart` hook has fired yet) and the registry has no code
  for the pane's cwd
- When: the window is renamed
- Then: the window name falls back to `@cc-project` (git toplevel basename or dir name)

#### Scenario: a failed rename is reported as not fired
- Given: `tmux rename-window` fails (non-zero exit, e.g. a stale pane id or a race with the
  window closing)
- When: `_maybe_rename_window` runs
- Then: it returns `False` and the diagnostic trace records the attempt as failed, not succeeded
