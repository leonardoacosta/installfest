# Spec: File Viewer

## ADDED Requirements

### Requirement: `view` command renders a file by type
The `view <file>` command SHALL resolve a relative or absolute path and render it with the optimal tool for its file type. Markdown SHALL render with `glow`, source/text/JSON SHALL render with `bat`, and HTML SHALL hand off to `mac-open`.

#### Scenario: render a Markdown file
- Given: `view` is on PATH and the session is inside tmux
- When: user runs `view README.md`
- Then: a horizontal tmux split opens below the current pane
- And: the split shows `glow`-rendered Markdown
- And: pressing `q` closes the split

#### Scenario: render a source file
- Given: the session is inside tmux
- When: user runs `view scripts/view.sh`
- Then: a horizontal split opens showing `bat` syntax-highlighted output
- And: pressing `q` closes the split

#### Scenario: HTML hands off to the Mac browser
- Given: `mac-open` is on PATH
- When: user runs `view report.html`
- Then: the file opens in the real browser on the Mac via `mac-open`
- And: no tmux split is created

#### Scenario: missing file errors cleanly
- When: user runs `view does-not-exist.md`
- Then: an error is printed to stderr
- And: the command exits non-zero

### Requirement: `view` infers the terminal session
The command SHALL detect whether it is running inside tmux via `$TMUX` and SHALL adapt its rendering target without any explicit session argument.

#### Scenario: inside tmux opens a split
- Given: `$TMUX` is set
- When: user runs `view file.md`
- Then: the renderer runs in a `split-window -v` pane targeting the current pane

#### Scenario: bare interactive shell renders inline
- Given: `$TMUX` is unset and stdout is a TTY
- When: user runs `view file.md`
- Then: the file renders inline in the current pane through a pager

#### Scenario: non-interactive invocation renders plain
- Given: `$TMUX` is unset and stdout is not a TTY
- When: `view file.md` output is piped
- Then: the file renders non-paged to stdout

### Requirement: `view` reuses its viewer pane
Repeated `view` invocations SHALL render into a single reused pane rather than accumulating stacked panes.

#### Scenario: second view reuses the pane
- Given: a previous `view` call opened and tagged a viewer pane in the current window
- When: user runs `view another.md`
- Then: the new renderer replaces the content of the existing tagged pane
- And: no additional split is created

### Requirement: `view` deploys via chezmoi
The command SHALL be deployed as a chezmoi symlink that resolves to the repo source script.

#### Scenario: chezmoi apply installs view
- Given: `home/dot_local/bin/symlink_view.tmpl` points at `scripts/view.sh`
- When: user runs `chezmoi apply`
- Then: `~/.local/bin/view` is a symlink to `scripts/view.sh`
- And: it is executable

### Requirement: `view` degrades gracefully
The command SHALL fail open when an optional dependency is absent.

#### Scenario: glow missing falls back to bat
- Given: `glow` is not installed
- When: user runs `view notes.md`
- Then: the file renders with `bat` as a fallback
- And: a one-time warning is emitted
