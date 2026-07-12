---
name: cc-dispatch
description: Route a task to another Claude Code pane tracked by cc-tmux — switch focus, send a prompt into a pane, spawn a new task window, or spawn into a fresh git worktree. The single authoritative home of the `cc-tmux conductor dispatch` CLI shape, used by both the Conductor session and ad-hoc Claude sessions. Use when the user (or the Conductor) wants to hand work to a specific pane, "send this to the other session", "open a new pane for X", "spin up a worktree and work on Y".
---

# cc-dispatch

The authoritative reference for the cc-tmux dispatch CLI. Both the Conductor and any
ordinary Claude session dispatch work through the SAME command, so its flag shape is
documented here once — do not hardcode it elsewhere.

## List dispatchable panes first

Dispatch targets a pane id. Enumerate the tracked panes (the Conductor's own session
is excluded automatically):

```bash
cc-tmux conductor list --json
```

Each row: `{id, session, window, state, project, branch, task, wait_reason, timestamp}`.
Pick the target `id` for `switch` / `send-prompt`; use a directory path for the spawn
modes.

## The dispatch command

```bash
cc-tmux conductor dispatch --mode <mode> --target <target> [--prompt <text>] [--force]
```

| Mode | `--target` | Other flags | Effect |
| ---- | ---------- | ----------- | ------ |
| `switch` | pane id | — | Move focus to that pane. Sends nothing. Use when the user only wants to look. |
| `send-prompt` | pane id | `--prompt` (required), `--force` | Type `--prompt` into the pane and submit it. **Refused when the pane is `active` (busy)** unless `--force`. |
| `spawn-task` | project directory | `--prompt` (optional) | Open a NEW window running `claude` in that project root, then seed `--prompt`. Falls back to the current pane's directory if `--target` is omitted (refused inside the conductor session — pass an explicit `--target` there; an explicit `--target` that is not a directory is a misuse error, never a fallback). |
| `spawn-worktree` | git repo directory | `--prompt` (optional) | Create a fresh git worktree (`.worktrees/conductor-<ts>` on a new `conductor/<ts>` branch), open a `claude` window there, then seed `--prompt`. Keeps the main checkout untouched. Falls back to the current pane's directory if `--target` is omitted (refused inside the conductor session — pass an explicit `--target` there; an explicit `--target` that is not a directory is a misuse error, never a fallback). Worktrees/branches are NOT auto-removed — clean up with `git worktree remove` + `git branch -D`, or a stale-worktree reaper such as `wt reap`. |

## Examples

```bash
# Look at the pane running the api work:
cc-tmux conductor dispatch --mode switch --target %12

# Hand a follow-up to an idle pane that already has the right repo loaded:
cc-tmux conductor dispatch --mode send-prompt --target %8 --prompt "run the e2e suite"

# Force a prompt into a busy pane (use sparingly — interrupts active work):
cc-tmux conductor dispatch --mode send-prompt --target %8 --prompt "stop and summarize" --force

# Spin up a fresh task pane in a project:
cc-tmux conductor dispatch --mode spawn-task --target ~/dev/oo --prompt "add a health check route"

# Isolated parallel work in a new worktree:
cc-tmux conductor dispatch --mode spawn-worktree --target ~/dev/oo --prompt "refactor the billing module"
```

## Exit codes

- `0` — dispatched (or a read succeeded).
- `1` — refused or failed: target pane is `active` without `--force`, the target pane is
  not a tracked Claude pane (unknown/stale — see `--force`), no `claude` binary for a
  spawn, git worktree failure, the tmux dispatch action itself failed, or the conductor
  is disabled for `--popup`.
- `2` — misuse: missing `--mode`, `--target`, or a required `--prompt`; an explicit
  spawn `--target` that is not a directory; or a spawn from the conductor session
  without an explicit `--target`.

## Choosing a mode

- Prefer `send-prompt` to an `idle`/`waiting` pane that already holds the context over
  spawning a new one — spawning is for genuinely new, parallel work.
- Never `send-prompt` to an `active` pane unless the user is explicit; surface that it
  is busy and confirm before using `--force`.
- Use `spawn-worktree` when the work must not touch the current checkout (parallel
  branch, risky change); `spawn-task` when a fresh pane in the existing checkout is fine.
