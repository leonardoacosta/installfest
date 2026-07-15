---
name: cc-config
description: Inspect and persistently edit the cc-tmux plugin's @cc-* tmux options (notifications, focus, cycle mode, window rename, conductor toggle, icons, keybindings) and regenerate the conductor instructions. Use when the user wants to configure, tune, enable, or disable cc-tmux behavior — "turn on notifications for waiting panes", "enable the conductor", "change the cycle key", "make idle panes notify me", "update conductor instructions".
---

# cc-config

Read and write the cc-tmux plugin's configuration, which lives entirely in tmux
`@cc-*` GLOBAL options (no config file for behavior toggles). Changes take effect
immediately; persist them into the user's `tmux.conf` when they should survive a
tmux server restart.

## Inspect current config

```bash
# Show every cc-tmux option currently set on the server:
tmux show-options -g | grep '@cc-'

# Read one option (empty output = unset / default):
tmux show-options -gv @cc-notify
```

## Change an option

```bash
# Set for the running server (immediate, non-persistent):
tmux set-option -g @cc-notify "waiting,idle"

# Unset -> back to default:
tmux set-option -gu @cc-notify
```

To persist, also add the `set -g @cc-... "..."` line to the user's tmux.conf
(`~/.config/tmux/tmux.conf` or `~/.tmux.conf`) — confirm the target file first.

## Option reference

| Option | Default | Purpose |
| ------ | ------- | ------- |
| `@cc-notify` | (empty = off) | Comma-separated states that fire an OS notification (`waiting,idle,active`). |
| `@cc-focus-app` | (empty = off) | Comma-separated states that bring the terminal to the foreground. |
| `@cc-notify-cooldown` | `30` | Per-pane notification dedup window, seconds. |
| `@cc-cycle-mode` | `priority` | `priority` (cycle the top non-empty group only) or `flat` (all pending panes). |
| `@cc-window-rename` | off | When on, renames a window per `@cc-window-rename-format`. |
| `@cc-window-rename-format` | `state` | `state` -> `<dir>` alone. `title` -> `<project-code>·<session-title>`, truncated to 10 chars combined. The state icon is NOT part of the renamed text in either format — see the animated tab icon below. |
| tab icon (always on, no option) | animated | Rendered by the render-all tabs row (status-format[0]), not baked into the window name — needed for real animation since hooks fire irregularly. `waiting` pulses `░▒▓█▓▒░`, `active` rotates `▁▏▔▕`, `idle` is a static glyph, untracked windows show nothing. |
| `@cc-icon-waiting` / `-idle` / `-active` | built-in | Per-state icon overrides. |
| `@cc-cycle-key` / `-picker-key` / `-inbox-key` / `-back-key` | `o` / `C-f` / `i` / `C-Space` | Keybinding overrides (re-source the plugin to apply). |
| `@cc-conductor-enabled` | off | Enable the Conductor session + its keybindings. |
| `@cc-conductor-session` | `conductor` | Conductor session name. |
| `@cc-conductor-key` / `-respawn-key` | `y` / `Y` | Conductor popup / respawn keys. |

## Conductor instructions

The Conductor's mode-selection playbook is generated from cc-tmux's built-in canon.
To (re)write the on-disk instruction file the Conductor reads:

```bash
cc-tmux conductor --update-instructions
```

This regenerates `~/.local/state/cc-tmux/conductor-instructions.md` (or
`$XDG_STATE_HOME/cc-tmux/…`) from the canonical instructions and prints the path.
Edit that file for a custom playbook; run `--update-instructions` again to reset it.
A running conductor picks up changes on its next prompt, or immediately on respawn
(`cc-tmux conductor --popup --respawn`).
