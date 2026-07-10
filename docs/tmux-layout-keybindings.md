# tmux Layout Anatomy & Keybindings

> Reference for the tmux setup in `home/dot_config/tmux/tmux.conf.tmpl` + per-theme `.conf` files.
> Not an audit (see `docs/audit/tmux.md` for the dated 2026-03-25 audit) — this is the current-state
> cheat sheet: hierarchy, pane layouts, every custom keybinding, and how to customize.

## Anatomy: the four-level hierarchy

```
Server (background process, one per machine)
 └─ Session (e.g. "main") — a workspace, survives disconnect
     └─ Window (a "tab") — base-index 1, browser-like (tmux.conf.tmpl:17)
         └─ Pane — a split within a window; pane-base-index 1 too
```

- **Server** — starts automatically on first `tmux` call, dies when the last session ends.
- **Session** — what you attach/detach from (`tmux attach`). Panes keep running while detached.
- **Window** — numbered tabs across the status bar. Renumbered on close (`renumber-windows on`,
  line 21) so gaps don't linger.
- **Pane** — a rectangular split inside a window, arranged by a **layout**.

## Pane layouts (tmux's built-in layout engine)

tmux ships 5 layouts, cycled with `prefix + space` (default binding, not overridden here):

| Layout | Shape |
|---|---|
| `even-horizontal` | panes side by side, equal width |
| `even-vertical` | panes stacked, equal height |
| `main-horizontal` | one big pane on top, rest stacked below |
| `main-vertical` | one big pane on left, rest stacked right |
| `tiled` | grid, auto-balanced |

This config doesn't pin a default layout or `main-pane-width`/`main-pane-height` — panes are
arranged manually via splits (see below), not via a fixed layout preset.

## Custom keybindings (`tmux.conf.tmpl`)

**Splits / panes**

| Key | Action | Line |
|---|---|---|
| `Cmd+D` | split vertical (new pane right) | 100-102 |
| `Cmd+Shift+D` | split horizontal (new pane below) | 104-106 |
| `prefix + \|` / `prefix + -` | same splits, traditional-style backup | 155-156 |
| `Cmd+W` | smart close: kills pane if >1 exists, else kills window | 60-62 |
| `prefix + h/j/k/l` | vim-style pane navigation | 159-162 |
| `prefix + H/J/K/L` (repeatable) | resize pane 5 cells in that direction | 165-168 |

**Windows ("tabs")**

| Key | Action | Line |
|---|---|---|
| `Cmd+T` | new window | 56-58 |
| `Cmd+1`…`Cmd+9` | jump to window N | 64-90 |
| `Cmd+Shift+[` / `]` | prev/next window | 92-98 |
| `prefix + C-h` / `C-l` (repeatable) | prev/next window, backup form | 171-172 |
| `prefix + <` / `>` (repeatable) | swap window with neighbor + follow it | 178-179 |
| `prefix + c` | new window, traditional backup | 175 |

**Copy mode / scrollback**

| Key | Action | Line |
|---|---|---|
| `prefix + [` or `PageUp` | enter copy-mode (vi keys) | 185, 191-192 |
| `v` (in copy-mode) | begin selection | 186 |
| `y` | copy selection → `pbcopy`, exits copy-mode | 187 |
| mouse drag release | same copy-to-clipboard | 188 |
| `Cmd+Shift+Left/Right` | select to line start/end | 131-137 |
| `Option+Shift+Left/Right` | select word back/forward | 139-145 |
| `prefix + C-p` | **TRIAL** — dumps 50k lines of scrollback to `less` in a new window; works around Claude Code's TUI corrupting scrollback over SSH. Started 2026-04-23; review after ~2 weeks of use to promote or drop | 113-129 |

**Misc**

| Key | Action | Line |
|---|---|---|
| `prefix + r` | reload config in-place | 152 |
| `Cmd+K` | clear scrollback history | 108-110 |

**cc-tmux (Claude pane tracker)** — bound by `apps/cc-tmux/cc-tmux.tmux` (loaded via `run-shell`),
all overridable via `@cc-*-key` options.

| Key | Action |
|---|---|
| `prefix + o` | cycle to next pending Claude pane (priority: waiting → idle), newest-first |
| `prefix + C-f` | pane picker popup (fzf, or `display-menu` fallback) |
| `prefix + i` | notification inbox — every tracked pane, attention-first; `enter` switches, `ctrl-x` dismisses |
| `C-Space` (root, no prefix) | jump back to the previous pane across sessions/windows |
| `prefix + y` | open the Conductor popup (only when `@cc-conductor-enabled`) |
| `prefix + Y` | kill + respawn the Conductor (destructive; only when `@cc-conductor-enabled`) |

Cycle moved off `prefix + Space` to `prefix + o` to avoid colliding with tmux-which-key's menu
(which keeps `Space`); see `openspec/changes/cc-tmux-plugin/design.md` § collision.

## Customization points

1. **Theme** — swapped via chezmoi templating, not tmux itself. `home/.chezmoi.toml.tmpl:2` sets
   `$theme := "vercel"`, injected into `tmux.conf.tmpl:213` as
   `source-file ~/.config/tmux/{{ .theme }}-theme.conf`. Theme files live alongside the main config
   in `home/dot_config/tmux/`: `vercel-theme.conf` (current), `one-hunter-vercel-theme.conf`,
   `nord-theme.conf`, `tokyo-night-abyss-theme.conf`. Switching = edit the chezmoi var + `chezmoi
   apply`.
2. **Status bar** — position/interval/justify set at lines 202-209; colors/segments (status-left,
   status-right, window-status-format) live in the per-theme `.conf` file.
3. **User-keys block** (lines 52-145) — the mechanism that gets Cmd/Option combos from
   WezTerm/Ghostty into tmux at all (macOS intercepts raw Cmd key events; the terminal emulator
   translates them into custom escape sequences tmux can bind). Each key needs a
   `set -s user-keys[N]` line plus a `bind-key -n UserN` line. Adding a new Cmd+key shortcut means
   adding both — and matching the escape sequence in the terminal emulator config too (see the
   escape-sequence-triplication risk documented in `docs/audit/tmux.md` § 9).
4. **Pane layout defaults** — not currently pinned. Add `set -g main-pane-height 70%` etc. for a
   fixed `main-horizontal` ratio, or bind a key to `select-layout tiled` for a one-key grid reset.

## Plugins

Installed by manual clone/symlink (no TPM — see `docs/audit/tmux.md` N5), each guarded by an
`if-shell` presence check in `tmux.conf.tmpl` so a fresh machine still loads the rest of the config:

- **tmux-which-key** — popup action menu on `prefix + Space` (third-party, `run_onchange_after_install-tmux-which-key.sh.tmpl`).
- **cc-tmux** — first-party Claude Code + tmux plugin (`apps/cc-tmux/`): tracks parallel Claude
  pane state (waiting/idle/active), priority cycling/jump-back, an fzf inbox, the multi-account
  usage segment in `status-right` (replaces the retired `tmux-nexus-creds`), and an opt-in
  Conductor. Symlinked into `~/.tmux/plugins/cc-tmux` and Claude-hook-registered by
  `run_onchange_after_install-cc-tmux.sh.tmpl`. Keybindings above.

## Related

- `docs/audit/tmux.md` — dated adversarial audit (2026-03-25); several findings there are already
  fixed in the current config (OSC 52 clipboard via `set-clipboard on`, theme is now `vercel` not
  `one-hunter-vercel`) — treat that doc as historical, not current state.
