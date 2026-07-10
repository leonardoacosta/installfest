---
name: cc-status
description: Summarize all tracked Claude Code sessions running in tmux and their live states (waiting / idle / active) via the cc-tmux plugin. Use when the user asks "what are my Claude sessions doing", "which panes are waiting", "anything blocked on me", "show session status", or otherwise wants an overview of parallel Claude panes tracked by cc-tmux.
---

# cc-status

Report the live state of every Claude Code pane cc-tmux is tracking in the current
tmux server. State lives ONLY in tmux pane options (the single source of truth), so
this skill just reads it out — it never mutates anything.

## When to use

- "What are my Claude sessions doing right now?"
- "Which panes are waiting on me / blocked on a permission prompt?"
- "Give me an overview of the parallel Claude work."

## How to gather the data

Run the cc-tmux status commands (invoke `cc-tmux` on PATH, or the plugin's
`bin/cc-tmux` by absolute path if it is not linked):

```bash
# Pending panes as clickable badges (waiting first, then idle):
cc-tmux status-inbox

# Aggregate counts per state, formatted for the status bar:
cc-tmux status

# Every tracked pane, one per line — label<TAB>pane_id (attention-ordered):
cc-tmux inbox

# Every tracked pane including active ones (unfiltered):
cc-tmux picker-data
```

`cc-tmux inbox` / `picker-data` emit `label<TAB>pane_id` rows. The label carries the
state icon, `session:window`, project, branch, time-in-state, wait reason, and task
summary. Parse the label column for the human summary; keep `pane_id` only if the
user then wants to jump or dispatch (see the `cc-dispatch` skill).

## States

| State   | Meaning                                                          |
| ------- | --------------------------------------------------------------- |
| waiting | Blocked on the user — permission prompt, question, plan, or elicitation (see the wait reason). Highest attention. |
| idle    | Finished its turn, awaiting the next prompt.                    |
| active  | Currently working. Shown for overview; never a cycle/hop target. |

## How to report

Lead with what needs attention: list `waiting` panes first (with their wait reason),
then `idle`, then a one-line count of `active`. Keep it scannable — the user is
triaging parallel work, not reading prose. If `cc-tmux` produces no output, there are
no tracked Claude panes (or tmux is unavailable); say so plainly.
