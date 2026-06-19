# Design: `view` terminal file-rendering command

## Locked decisions

| Decision | Resolution | Why |
| --- | --- | --- |
| Session detection | `[[ -n "$TMUX" ]]` + inherited `$TMUX_PANE` | A command run inside a pane (incl. Claude Code's Bash tool) inherits the pane id; `tmux split-window` targets it with no client querying. Detection is free. |
| Split direction | `split-window -v` (pane below) | Horizontal divider — matches the `Cmd+Shift+D` muscle memory in `tmux.conf`. |
| Close mechanism | "Everything ends in a pager; `q` closes the pane" | tmux closes a pane when its command exits. `glow -p` / `bat --paging=always` page even short files, so `q` quits the renderer -> command exits -> split vanishes. No signal handling or cleanup code. |
| Markdown | `glow -p` | Rendered markdown (vs `bat`'s raw syntax highlight). `glow` is the one new dependency. |
| Code / text / JSON | `bat --paging=always --color=always` | Already installed and aliased; `--paging=always` forces the pager so the pane persists until `q`. |
| HTML | `mac-open` (real Mac browser), no split | Terminal HTML rendering is always a fidelity compromise (zero CSS/JS for text browsers; blocky for screenshot pipelines). `mac-open` already routes a file to the real browser over Tailscale — the true fidelity ceiling — so HTML hands off rather than degrading in-terminal. |
| Pane reuse | `@view_pane` tag + `respawn-pane -k` | Without it, repeated `view` calls stack panes. Tag the viewer pane once, respawn into it thereafter. |
| Focus | Viewer-follows by default; `-d` keeps caller focus | "View this" implies you want to read it; `-d` is the escape for the Claude-Code-driven case. |

## Dispatch table (v1)

| Input | Renderer (in split) | Closes on |
| --- | --- | --- |
| `.md` / `.markdown` / `.mdx` | `glow -p <file>` | `q` |
| source / json / yaml / toml / extensionless text | `bat --paging=always --color=always <file>` | `q` |
| `.html` / `.htm` | `mac-open <file>` (no split, opens on Mac) | n/a |

Type detection is extension-first (fast, predictable) with a `file --mime-type` fallback for extensionless files.

## Execution contexts

| Context | `$TMUX` | stdout TTY | Behavior |
| --- | --- | --- | --- |
| Interactive in tmux | set | yes | split below, paged renderer, focus -> viewer |
| Claude Code Bash (in tmux) | set | no | split below (own TTY); Bash call returns instantly, Leo reads the pane |
| Bare shell, interactive | unset | yes | inline paged render in current pane |
| Bare / piped | unset | no | inline plain render to stdout |

`tmux split-window "cmd"` returns immediately; the renderer runs in the new pane's own TTY independent of the caller. That is what lets Claude Code invoke `view` and get an instant exit while the split persists.

## v2 — Kitty raster + images (deferred, decision locked)

Image rendering ships in a v2 follow-up together with the only setting change it needs. The rendering strategy is **locked now** so v2 does not relitigate it:

- Images render via the **Kitty graphics protocol**, not half-block, for true raster fidelity: `chafa -f kitty --passthrough tmux <img>`.
- This requires `set -g allow-passthrough on` in `home/dot_config/tmux/tmux.conf` (currently off). The flip + the image renderer land together in v2 so the change is testable.
- Half-block (`chafa --fit-width`) is the no-passthrough fallback.
- PDFs/binaries -> `mac-open`; directories -> `eza --tree` piped to a pager in the split.

This strategy is constrained by a hard environmental fact chain (verified 2026-06-19): Ghostty refuses Sixel permanently and only decodes the Kitty protocol; tmux 3.6a passthrough is off by default; the local `img2sixel` build is `libpng: no`. So Kitty-via-passthrough is the only in-terminal raster path, and `chafa` is the producer (already installed). Full rationale and sources live in the `reference-terminal-image-rendering` memory; do not wire `view` around `img2sixel`.

## Out of scope

- Interactive terminal browsers (carbonyl, browsh) — rejected: abandoned / Firefox-tax / no fidelity gain over `mac-open`.
- Inline HTML rendering of any kind — superseded by the HTML -> `mac-open` decision.
- `fzf`-driven directory file-picker — possible later, not in v1/v2.
