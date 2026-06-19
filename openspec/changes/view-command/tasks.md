# Implementation Tasks

<!-- beads:epic:if-911 -->
<!-- beads:feature:if-1hq -->

## Script Batch

- [x] [1.1] [P-1] Create `scripts/view.sh` — resolve path to absolute, detect type by extension (`.md/.markdown/.mdx`, `.html/.htm`, else) with `file --mime-type` fallback for extensionless files; route md->`glow -p`, html->`mac-open`, else->`bat --paging=always --color=always` [owner:general-purpose] [beads:if-cbb]
- [x] [1.2] [P-1] Implement session-context tiers in `view.sh` — `$TMUX` set: `tmux split-window -v -l 60% -c "#{pane_current_path}"`; no tmux + stdout TTY: inline paged; no tmux + non-TTY: plain stdout; html always -> `mac-open` (no split) [owner:general-purpose] [beads:if-940]
- [x] [1.3] [P-2] Implement viewer-pane reuse in `view.sh` — tag spawned pane `@view_pane 1`; on later calls `respawn-pane -k` into an existing tagged pane in the current window instead of a new split [owner:general-purpose] [beads:if-hvo]
- [x] [1.4] [P-2] Add error handling + `-d` flag to `view.sh` — missing file -> stderr + exit 1; missing `glow` -> `bat` fallback for markdown with one-time warn; `-d` passes `-d` to `split-window` to keep focus on the caller pane [owner:general-purpose] [beads:if-318]

## Config Batch

- [x] [2.1] [P-1] Create `home/dot_local/bin/symlink_view.tmpl` containing `{{ .chezmoi.sourceDir }}/../scripts/view.sh` (mirrors `symlink_mac-open.tmpl`; deploys `~/.local/bin/view`) [owner:general-purpose] [beads:if-9s7]
- [x] [2.2] [P-1] Add `brew "glow"` to `platform/homebrew/Brewfile` (near the other CLI tools) [owner:general-purpose] [beads:if-bl4]
- [x] [2.3] [P-1] Add `glow` to the pacman package list in `install_arch_packages()` in `scripts/install-arch.sh` [owner:general-purpose] [beads:if-g4j]

## Verification Batch

- [x] [3.1] [P-1] Run `chezmoi apply`; verify `~/.local/bin/view` symlink resolves to `scripts/view.sh` and is executable [owner:general-purpose] [beads:if-s9z]
- [x] [3.2] [P-1] Verify markdown: `view README.md` opens a horizontal split rendering glow output; `q` closes the pane [owner:general-purpose] [beads:if-q63]
- [x] [3.3] [P-1] Verify code: `view scripts/view.sh` opens a split with bat syntax highlighting; `q` closes the pane [owner:general-purpose] [beads:if-amp]
- [x] [3.4] [P-1] Verify HTML handoff: `view <file>.html` routes to `mac-open` and opens on the Mac with no tmux split created [owner:general-purpose] [beads:if-6ta]
- [x] [3.5] [P-2] Verify pane reuse: two consecutive `view` calls render in one pane, not two stacked panes [owner:general-purpose] [beads:if-o65]
- [x] [3.6] [P-2] Verify fallback tiers: `view` run outside tmux renders inline; `view missing.md` exits 1 with a stderr message [owner:general-purpose] [beads:if-a00]

## Future (Blocked)

- [ ] [4.1] [P-3] [deferred] v2 image rendering — `view <img>` -> `chafa -f kitty --passthrough tmux` (true raster) AND add `set -g allow-passthrough on` to `home/dot_config/tmux/tmux.conf`; half-block (`chafa --fit-width`) as the no-passthrough fallback. See `design.md` "v2 — Kitty raster + images" [owner:general-purpose]
- [ ] [4.2] [P-3] [deferred] v2 PDF + directory — `.pdf`/binary -> `mac-open`; directory -> `eza --tree --level=2 --icons --color=always` piped to a pager in the split [owner:general-purpose]
- [ ] [4.3] [P-3] [deferred] v2 multi-file — `view a.md b.md` renders all files in one pane (bat native concat for code; sequential `glow` for markdown) [owner:general-purpose]
