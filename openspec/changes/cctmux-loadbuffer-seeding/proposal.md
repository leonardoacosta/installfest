---
order: 0722k
---

# Proposal: cctmux-loadbuffer-seeding — seed claude panes via load-buffer, not send-keys -l

## Change ID
`cctmux-loadbuffer-seeding`

## Summary
Replace `cc-tmux` conductor's two prompt-seeding sites (`send-keys -l prompt` + a separate
`Enter`) with the `load-buffer` (stdin) → `paste-buffer -p` (bracketed) → `delete-buffer` →
`send-keys Enter` sequence wavetui's Go dispatch package already proves correct, add a
newline-strip belt-and-suspenders guard in case the target REPL doesn't honor bracketed paste,
and add self-test coverage that proves an embedded newline in the prompt never submits early or
injects a following line as a separate command.

## Why
`cc-tmux`'s conductor seeds a prompt into a claude tmux pane with `send-keys -t <target> -l
<prompt>` followed by a separate `send-keys -t <target> Enter`
(`apps/cc-tmux/src/cc_tmux/conductor.py:392-396` and the identical pattern in `_open_window` at
`:479-480`). `-l` sends the text literally — a newline embedded in the prompt is delivered as a
literal keystroke, the pane's REPL submits right there, and everything after that newline is
typed as a **new** prompt or slash-command. When the seeded text carries untrusted substrings
(task/title/branch text supplied by a caller), later lines become injected commands the caller
never intended to submit. The sibling app wavetui already solved this exact problem in
`apps/wavetui/internal/dispatch/tmux.go:260-284`: it delivers prompt text through `tmux
load-buffer` (stdin) + `paste-buffer -p` (bracketed paste), never as raw keystrokes, then submits
once via a separate `send-keys Enter`. This change ports that proven pattern into cc-tmux's
Python conductor.

## What Changes
- `apps/cc_tmux/src/cc_tmux/tmux.py`: add a `load_buffer`/`paste_buffer` (and `delete_buffer`)
  helper pair — grep first for an existing stdin-capable path on `_run_tmux` before adding a new
  one, per the plan's own note that one may already exist. The load-buffer call pipes the prompt
  as stdin (`tmux load-buffer -b <unique-name> -`); the buffer name is unique per seed (e.g.
  pid + monotonic clock) to avoid collisions between concurrent seeds.
- `apps/cc-tmux/src/cc_tmux/conductor.py`: replace both seeding sites (`:392-396` and the
  `_open_window` site at `:479-480`) with load-buffer(prompt) → paste-buffer(`-p`, `-t target`)
  → delete-buffer → send-keys Enter, preserving the existing `sent`/`entered is None` failure
  shape (extended to check every step) and leaving the surrounding readiness/grace logic
  (`_READY_TIMEOUT`, `_READY_GRACE`) untouched.
- `apps/cc-tmux/src/cc_tmux/conductor.py` (or `tmux.py`, wherever the seeding sequence lives):
  add a newline-strip guard as defense-in-depth — if bracketed paste isn't honored by the target
  REPL, internal newlines in the prompt are replaced with spaces and a warning is logged, marked
  with a `# ponytail:` comment noting it is belt-and-suspenders, not the primary defense.
- `apps/cc-tmux/src/cc_tmux/testing.py`: add a `test_prompt_seeding_*` self-test that feeds a
  prompt containing an embedded newline (`"line1\n/quit\nline3"`) through the seeding path with a
  stubbed tmux runner (captures argv + stdin), and asserts the prompt bytes go through
  `load-buffer` stdin (never `send-keys -l` with the raw prompt) and exactly one `send-keys
  Enter` follows.

## Context
- depends on: `add-repo-app-test-gate`
- touches: `apps/cc-tmux/src/cc_tmux/conductor.py`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/testing.py`

## Testing
| Affected seam | Task |
|----------------|------|
| `load_buffer`/`paste_buffer` helper correctness | `[1.1]`, unit-callable, verified via `[2.1]` |
| Both conductor seeding sites use the new sequence | `[1.2]`, verified via `[2.1]`'s self-test and a `rg 'send-keys.*-l.*prompt'` no-hits check |
| Newline-strip defense-in-depth guard | `[1.3]`, verified via `[2.1]` |
| Embedded-newline prompt does not submit early or inject a following line | `[2.1]` new `test_prompt_seeding_*` self-test with a stubbed tmux runner |
| Full self-test suite + repo gate stay green | `[2.2]` `cc-tmux self-test` then `scripts/check.sh`, both exit 0 |

## Done Means
- Seeding a prompt containing an embedded newline (e.g. `"line1\n/quit\nline3"`) into a claude
  pane does not submit early at the first newline, and does not inject the text after the
  newline as a separate command.
- `rg 'send-keys.*-l.*prompt' apps/cc-tmux/src/cc_tmux/conductor.py` has no hits — the literal
  seeding path is gone from both call sites.
- `rg 'load-buffer' apps/cc-tmux/src/cc_tmux/tmux.py` shows the new helper is present and used.
- `cc-tmux self-test` includes and passes a prompt-seeding regression test (name matches
  `prompt_seed`), proving the embedded-newline prompt reaches `load-buffer` stdin, never
  `send-keys -l` with the raw prompt, and exactly one `send-keys Enter` follows.
- `scripts/check.sh` exits 0 with the updated `cc-tmux self-test` suite included in the gate
  (per `add-repo-app-test-gate`).
