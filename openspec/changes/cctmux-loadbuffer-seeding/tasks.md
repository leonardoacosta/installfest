---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [x] [1.1] Add a `load_buffer`/`paste_buffer` (and `delete_buffer`) helper to
  `apps/cc-tmux/src/cc_tmux/tmux.py`. Grep first for an existing stdin-capable path
  (`rg "input=|stdin" apps/cc-tmux/src/cc_tmux/tmux.py`) before adding a new one, per
  `plans/005-cctmux-loadbuffer-seeding.md` step 1. The helper runs
  `tmux load-buffer -b <buf> -` with the prompt on stdin, using a unique buffer name per seed
  (e.g. pid + monotonic clock) to avoid collisions; `_run_tmux` (the existing argv-list, no-shell
  subprocess wrapper) stays the only way this app calls tmux. [type:security]
- [x] [1.2] Replace both seeding sites in `apps/cc-tmux/src/cc_tmux/conductor.py`
  (`:392-396` and the `_open_window` site at `:479-480`) with the sequence: load-buffer(prompt)
  → paste-buffer (`-p` bracketed, `-t target`) → delete-buffer → send-keys Enter. Keep the
  existing `sent`/`entered is None` failure-handling shape, extended to check each step; leave
  the surrounding readiness/grace logic (`_READY_TIMEOUT`, `_READY_GRACE`) untouched, per
  `plans/005-cctmux-loadbuffer-seeding.md` step 2.
  - depends on: 1.1
  - [type:security]
- [x] [1.3] Add the newline-strip guard as belt-and-suspenders defense-in-depth: if
  `"\n" in prompt`, log a warning and replace internal newlines with spaces before delivery,
  marked with a `# ponytail:` comment noting it is secondary to the load-buffer/bracketed-paste
  sequence, per `plans/005-cctmux-loadbuffer-seeding.md` step 3.
  - depends on: 1.2
  - [type:security]

## E2E Batch

- [x] [2.1] Add a `test_prompt_seeding_*` self-test to `apps/cc-tmux/src/cc_tmux/testing.py`
  following an existing stubbed-runner test's pattern (`rg "def test_"
  apps/cc-tmux/src/cc_tmux/testing.py | head`). Feed a prompt containing `"line1\n/quit\nline3"`
  through the seeding path with a stubbed tmux runner that captures argv + stdin, and assert:
  (a) the prompt bytes go through `load-buffer` stdin, not `send-keys -l`; (b) exactly one
  `send-keys Enter` follows; (c) no `send-keys -l` invocation carries the raw prompt — per
  `plans/005-cctmux-loadbuffer-seeding.md` step 4 and this proposal's spec delta. [type:test]
- [x] [2.2] Run `cd apps/cc-tmux && cc-tmux self-test` — all pass, including the new
  prompt-seeding test — then `scripts/check.sh` from the repo root, exit 0 (per
  `add-repo-app-test-gate`, this suite is in the gate). Paste terminal output as evidence.
  Escape hatch per the plan: if `cc-tmux self-test` is already red on a clean checkout before
  this change, STOP and report rather than building on a red suite — do not silently absorb a
  pre-existing failure into this change's scope. [type:test]
  - depends on: 2.1
