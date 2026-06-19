# Proposal: Add `view` terminal file-rendering command

## Change ID
`view-command`

## Summary
Add a `view <file>` command that renders a file optimally for its type inside a horizontal tmux split, inferring the current terminal session automatically. v1 dispatches Markdown to `glow`, source/text to `bat`, and HTML straight to the existing `mac-open` (real browser on the Mac). Every in-terminal renderer runs in a `split-window -v` pane that closes on `q`, and a viewer pane opened by a previous `view` call is reused instead of stacked. Images, PDFs, directories, and a Kitty true-raster tier are deferred to a v2 follow-up.

## Context
- Extends: `scripts/` (new `view.sh`, mirrors `mac-open.sh` placement), `home/dot_local/bin/` (chezmoi symlink, mirrors `symlink_mac-open.tmpl`), `platform/homebrew/Brewfile`, `scripts/install-arch.sh`
- Related: `mac-open` (`scripts/mac-open.sh` -> `~/.local/bin/mac-open`) is the HTML + high-fidelity escape hatch; tmux 3.6a config (`home/dot_config/tmux/tmux.conf`); `bat`/`eza` aliases (`home/dot_zsh/functions/load-tools.zsh`)
- depends on: none
- touches: `scripts/view.sh`, `home/dot_local/bin/symlink_view.tmpl`, `platform/homebrew/Brewfile`, `scripts/install-arch.sh`

## Motivation
Reading a rendered file from a terminal session today means either guessing the right tool (`glow` for markdown, `bat` for code, a browser for HTML) or dumping raw text through the default pager. There is no single front door that picks the optimal renderer and shows it in a dedicated pane without disrupting the working pane.

The session-inference half is effectively free: a command run from inside a tmux pane (including one spawned by Claude Code's Bash tool) inherits `$TMUX`/`$TMUX_PANE`, so `tmux split-window` targets the current pane with no client querying. The renderer half is a small dispatch table over tools already standardized in this repo (`bat` installed; `glow` is the one new dependency). HTML is the deliberate exception: terminal HTML rendering is always a fidelity compromise, and `mac-open` already routes a file to the real browser on the Mac over Tailscale — so `view` hands HTML straight to it rather than reinventing a degraded in-terminal renderer.

## Requirements

### Req-1: Create the `view` dispatcher script
Create `scripts/view.sh` (deploys to `~/.local/bin/view`). Given a file path (relative or absolute), the script:
- Resolves the argument to an absolute path; a missing file prints an error and exits non-zero.
- Detects the file type by extension, falling back to `file --mime-type` for extensionless files.
- Routes by type:
  - `.md` / `.markdown` / `.mdx` -> `glow -p <file>`
  - `.html` / `.htm` -> `mac-open <file>` (hands off to the Mac browser; no split)
  - everything else (source code, JSON, YAML, TOML, plain text) -> `bat --paging=always --color=always <file>`
- Runs each in-terminal renderer (`glow`, `bat`) so that pressing `q` exits the renderer and therefore closes the pane (the "everything ends in a pager" invariant). `glow -p` and `bat --paging=always` both enter a pager even for short files.

### Req-2: Infer the session and open a horizontal split
The script must adapt to three execution contexts using `$TMUX` and a stdout-TTY check:
- Inside tmux (`$TMUX` set): open the renderer in a horizontal split below the current pane via `tmux split-window -v -l 60% -c "#{pane_current_path}"`. This is the primary path and works whether `view` is run interactively or by Claude Code's Bash tool.
- Not in tmux but stdout is a TTY: render inline in the current pane, paged.
- Not in tmux and stdout is not a TTY (piped / non-interactive): render inline, non-paged, to stdout.
- HTML always routes to `mac-open` regardless of context (never a split).

### Req-3: Reuse the viewer pane instead of stacking
The first `view` call tags its spawned pane with a tmux user option (`@view_pane 1`). Subsequent `view` calls check the current window for an existing tagged pane; if one exists, they `respawn-pane -k` the new renderer into it instead of creating another split. This keeps repeated `view` calls from accumulating stacked panes.

### Req-4: Deploy via chezmoi symlink
Create `home/dot_local/bin/symlink_view.tmpl` containing `{{ .chezmoi.sourceDir }}/../scripts/view.sh`, mirroring `symlink_mac-open.tmpl`. After `chezmoi apply`, `~/.local/bin/view` resolves to `scripts/view.sh`.

### Req-5: Add `glow` to package manifests
`glow` is the only new dependency (markdown renderer; `bat` and `mac-open` already present). Add it to both manifests for the unified Mac+Arch toolchain:
- `platform/homebrew/Brewfile`: `brew "glow"`
- `scripts/install-arch.sh`: add `glow` to the pacman package list in `install_arch_packages()`

### Req-6: Graceful degradation
The script must fail open:
- Missing file -> error message on stderr, exit 1.
- `glow` not installed -> fall back to `bat` for markdown (warn once), so the command still works before `chezmoi apply` adds the dependency.
- Optional `-d` flag -> keep focus on the calling pane (pass `-d` to `split-window`) instead of switching to the viewer; default is to focus the viewer.
