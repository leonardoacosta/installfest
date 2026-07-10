# cc-tmux

A first-party Claude Code + tmux plugin that makes parallel Claude Code sessions
visible and navigable inside tmux.

Every Claude pane's state (`waiting` / `idle` / `active`) is tracked in tmux pane
options — the single source of truth — via Claude Code hooks. On top of that it
provides priority-based pane cycling, jump-back, an fzf notification inbox, OS
notifications with terminal auto-focus, status-bar integration, a folded-in
multi-account usage segment, and an opt-in dispatch Conductor.

Clean-room implementation (architecture adapted from `unsafe9/claude-tmux-hop`,
all code original). Python 3.10+, stdlib only, no runtime dependencies.

## CLI

```
cc-tmux <command>
```

Run `cc-tmux --help` for the full subcommand list, and `cc-tmux self-test` to run
the built-in pure-function test suite.

## Layout

```
apps/cc-tmux/
  bin/cc-tmux            # PYTHONPATH shim -> python3 -m cc_tmux
  src/cc_tmux/
    tmux.py             # pane-option state store (the only state store)
    priority.py         # attention-priority sort + cycle selection
    paths.py            # tmux.conf + plugin-dir detection
    cli.py / parser.py  # argparse subcommands -> cmd_<name>() handlers
    testing.py          # cc-tmux self-test
```
