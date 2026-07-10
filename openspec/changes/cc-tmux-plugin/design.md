# Design: cc-tmux plugin

## Why a first-party plugin (not three third-party installs)

Two recons (`docs/recon/tmux-claude-plugins.md`, `docs/recon/unsafe9-claude-tmux-hop.md`) surveyed
the space. The decision is one owned plugin because:
- The richest surveyed source (`unsafe9/claude-tmux-hop`) declares MIT but ships **no LICENSE
  file** — unsafe to fork verbatim. Clean-room reimplementation is required anyway, so owning it is
  free.
- Three separate installs fragment the status bar, keybindings, and Claude-hook wiring across
  unrelated repos. One plugin consolidates them and deploys via chezmoi to all machines.
- A Claude Code plugin manifest self-registers hooks on `claude plugin install` — no edit to cc's
  governed `settings.json`. This is the decisive reason the cross-repo hook concern (flagged twice
  in recon) evaporates.

## Architecture (mirrors the surveyed design, original code)

```
apps/cc-tmux/
  pyproject.toml              # requires-python >=3.10, no deps
  LICENSE                     # MIT, Leo (clean-room)
  bin/cc-tmux                 # PYTHONPATH shim → python3 -m cc_tmux
  cc-tmux.tmux                # tmux entrypoint: keybindings + status format + discover
  .claude-plugin/plugin.json  # Claude Code plugin manifest (self-registers hooks)
  hooks/hooks.json            # event → state mapping (10s timeouts)
  skills/
    cc-status/  cc-config/  cc-dispatch/
  src/cc_tmux/
    __init__.py  __main__.py
    cli.py          # cmd_<name>() handlers
    parser.py       # argparse subcommands
    tmux.py         # tmux ops, PaneInfo dataclass, get_hop_panes(), set_pane_state()
    priority.py     # STATE_PRIORITY, PENDING_STATES, sort
    paths.py        # tmux.conf + plugin-path detection
    notify/         # __init__.py (registry) + macos.py + linux.py + windows.py (Strategy)
    usage.py        # nexus-agent query → status segment (replaces tmux-nexus-creds)
    conductor.py    # persistent session, dispatch modes, context injection
    install.py      # conductor instructions, env checks
    testing.py      # self-test (priority sort, transitions, path detection)
    log.py
```

## Key invariants (carry these into the code)

1. **Pane options are the ONLY state store.** Every view derives from one `get_hop_panes()` read.
   Never add a parallel store. State dies with the pane (auto-cleanup).
2. **Views are not stores.** Inbox/cycle/status are derived; inbox-dismiss is a view filter (a
   global cleared-at stamp), never a state mutation, so status counts are untouched.
3. **Real-transition guard.** `set_pane_state()` returns whether `@cc-state` actually changed.
   Auto-hop and app-focus fire ONLY on a real change; a re-asserted state must not re-yank focus.
   Only the OS notification (its own fingerprint+cooldown dedup) runs on a re-register.
4. **Hot path skips git identity.** Only `waiting`/`idle` resolve project/branch; `active` (the
   most frequent register) does not. The inbox backfills missing identity once on open.
5. **Fail open everywhere.** No `$TMUX` / no tmux binary → exit 0. A hook error never blocks Claude.

## The `prefix + Space` collision (Req-11)

`tmux-which-key` (shipped this session) binds `prefix + Space` to its display-menu. `claude-tmux-hop`
also defaults cycle to `prefix + Space`. They cannot both own it.

Resolution (chosen): **cc-tmux cycle moves off `Space`** to `prefix + o` (mnemonic: "hop"), leaving
which-key on `prefix + Space`. Rationale: which-key is the more general, discoverability-first
surface and its `Space` binding is muscle-memory-standard (matches its upstream default and the
vscode/emacs which-key convention); the Claude-pane cycle is a narrower, newer action. `@cc-cycle-key`
still lets Leo reassign. design decision recorded here so the implementer does not re-derive it.

## Usage segment replacement (Req-8)

`home/dot_local/bin/executable_tmux-nexus-creds` is a working sh script querying nexus-agent. Its
logic moves verbatim-in-behavior into `cc_tmux/usage.py` (`cc-tmux usage`), and `status-right`
switches from `#(tmux-nexus-creds)` to `#(... cc-tmux usage)`. The standalone script is REMOVED in
the same change (no dead duplicate). This is a clean replacement, not a compat shim — the output
format is preserved byte-for-byte so the status bar looks identical (Verification Batch asserts the
diff).

## Conductor (Req-9) — in-scope, same batch

Per Leo's decision, Conductor ships with the core rather than as a follow-on. It is still
**disabled by default** (`@cc-conductor-enabled off`) — off means no keybinding registers and
`conductor --popup` refuses — so shipping it same-batch adds code but not default surface area. The
dispatch primitives (`spawn-task`, `send-prompt`, `list --json`, worktree spawn) are general and
usable via the `cc-dispatch` skill regardless of the popup.

## What is explicitly OUT

- `tmux-which-key` absorption — it is a general action menu, a separate concern; folding it in is
  scope creep (Leo confirmed: not in scope).
- TPM — this repo installs tmux plugins by manual clone + `run-shell` (see the which-key precedent);
  no TPM dependency is added.
