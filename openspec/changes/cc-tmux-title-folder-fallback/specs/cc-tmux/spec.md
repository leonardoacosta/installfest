## MODIFIED Requirements

### Requirement: Opt-in window rename supports a project-code + session-title format
When `@cc-window-rename` is on and `@cc-window-rename-format` is `title`, the plugin SHALL rename
the pane's window to `<project-code>·<session-title>`, hard-truncated to 20 characters combined,
WHENEVER a session title is present. The project code SHALL resolve from the dotfiles project
registry (`home/projects.toml`) by the pane's current directory; the session title SHALL be
captured from the `SessionStart` hook payload's `session_title` field (the custom title if set via
`/rename` or `-n`, else Claude's own default) and persisted in `@cc-title`. When no session title
is present (`@cc-title` unset or empty), the plugin SHALL fall back to the raw current-directory
basename (`os.path.basename(pane_current_path)`) alone — the project-code prefix is used ONLY
when a title is present, never as a title-absent fallback on its own. The renamed text does NOT
include a state icon — see "Animated tab icon" below for how the icon is rendered instead. The
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

#### Scenario: no session title falls back to the folder name, even inside a registered project
- Given: `@cc-title` is unset or empty (no `SessionStart` hook has fired yet, or Claude never set
  a title), and the pane's cwd IS inside a project registered in `home/projects.toml` with code
  `if`
- When: the window is renamed
- Then: the window name is the raw current-directory basename alone (e.g. `new-service`), NOT
  `if` — the project-code prefix is not applied when there is no title to prefix

#### Scenario: no session title and no registered project both fall back to the same folder name
- Given: `@cc-title` is unset or empty, and the registry has no code for the pane's cwd
- When: the window is renamed
- Then: the window name is the raw current-directory basename alone — identical fallback whether
  or not the project happens to be registered

#### Scenario: a failed rename is reported as not fired
- Given: `tmux rename-window` fails (non-zero exit, e.g. a stale pane id or a race with the
  window closing)
- When: `_maybe_rename_window` runs
- Then: it returns `False` and the diagnostic trace records the attempt as failed, not succeeded
