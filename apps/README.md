# apps/

Standalone tools, first-party and vendored side by side. Each first-party app
has its own README with build/test/entry-point details:

| App | What | Docs |
| --- | --- | --- |
| `wavetui/` | Go/bubbletea TUI dashboard | `wavetui/README.md`, `openspec/specs/wavetui/spec.md` |
| `ctx-scan/` | Bun/TS fleet context scanner | `ctx-scan/README.md`, `openspec/specs/ctx-scan/spec.md` |
| `daily-brief/` | Bun/TS + ink daily briefing widget | `daily-brief/README.md`, `openspec/specs/daily-brief/spec.md` |
| `cc-tmux/` | Python tmux plugin for Claude Code sessions | `cc-tmux/README.md` |

## Vendored submodules

`kontroll/` and `zsa-voyager-keymap/` are pinned-upstream git submodules
(`.gitmodules` at the repo root) — **submodules are pinned upstream, bump
don't edit**. Do not commit local edits inside either directory; update the
pin (`git submodule update --remote`) instead. Do not move or restructure
either submodule's path.
