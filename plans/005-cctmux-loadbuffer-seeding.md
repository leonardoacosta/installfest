# Plan 005 — Seed cc-tmux prompts via load-buffer instead of `send-keys -l`

**Written against commit:** `d441448` — if the excerpt no longer matches, STOP and report drift.
**Finding:** cc-tmux #5 — `send-keys -l prompt` + separate `Enter` mangles multi-line prompts and lets a newline in untrusted prompt text submit early and inject later lines as new REPL commands (MED confidence).
**Priority:** 5 of 8. Depends on plan 001 being live (cc-tmux self-test must run in the gate to protect this change). Land 001 first.

## Why this matters

`cc-tmux`'s conductor seeds a prompt into a claude tmux pane with `send-keys -l` (literal)
followed by a separate `Enter`. A newline embedded in the prompt is delivered literally,
the terminal submits at that point, and text after the newline is typed as a **new** prompt
or slash-command. When the prompt carries untrusted substrings (task/title/branch text from
a caller — e.g. wavetui's `RenderSpawnPrompt`), later lines become injected commands. The
sibling app wavetui already solved exactly this: it delivers prompt text through
`tmux load-buffer` (stdin) + `paste-buffer`, never as keystrokes.

## Current state (verified excerpt)

`apps/cc-tmux/src/cc_tmux/conductor.py:392-396`:

```python
    # -l sends the text literally, then a separate Enter submits it.
    sent = tmux._run_tmux(["send-keys", "-t", target, "-l", prompt])
    entered = tmux._run_tmux(["send-keys", "-t", target, "Enter"])
    if sent is None or entered is None:
        sys.stderr.write(f"cc-tmux conductor: send-keys to pane {target} failed.\n")
```

Same pattern in `_open_window` at `:479-480`:

```python
        sent = tmux._run_tmux(["send-keys", "-t", new_pane, "-l", prompt])
        entered = tmux._run_tmux(["send-keys", "-t", new_pane, "Enter"])
```

## The exemplar to copy (wavetui, Go)

`apps/wavetui/internal/dispatch/tmux.go:260-284` — the proven pattern:

```go
func (execTmuxRunner) LoadBuffer(ctx context.Context, bufName string, data []byte) error {
	cmd := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", bufName, "-")
	cmd.Stdin = bytes.NewReader(data)
	...
}
func (execTmuxRunner) PasteBuffer(ctx context.Context, bufName, paneID string) error {
	_, err := runOut(ctx, "tmux", "paste-buffer", "-b", bufName, "-p", "-t", paneID)
	...
}
func (execTmuxRunner) SendKeysEnter(ctx context.Context, paneID string) error {
	_, err := runOut(ctx, "tmux", "send-keys", "-t", paneID, "Enter")
	...
}
```

The sequence is: `load-buffer -b <name> -` (prompt via stdin) → `paste-buffer -b <name> -p -t <pane>`
(the `-p` requests bracketed paste, which prevents the REPL from acting on embedded newlines
as submissions) → `delete-buffer -b <name>` → `send-keys Enter` to submit once.

## Conventions to match (cc-tmux)

- Read `apps/cc-tmux/src/cc_tmux/tmux.py` first — `_run_tmux` (the subprocess wrapper) is
  the only way this app calls tmux, and it takes an argv list (no shell). Add a helper there
  that pipes stdin, mirroring `_run_tmux` but using `subprocess.run(..., input=prompt.encode())`.
  There may already be a stdin-capable path — grep `rg "input=|stdin" apps/cc-tmux/src/cc_tmux/tmux.py`.
- cc-tmux is stdlib-only (`pyproject.toml` `dependencies = []`) — do not add a dependency.
- Tests live in `apps/cc-tmux/src/cc_tmux/testing.py` (run via `cc-tmux self-test`). Add
  pure-function coverage there following its existing `_AssertError`/`test_*` style.

## Steps

1. **Add a `load_buffer`/`paste_buffer` helper** to `tmux.py` (or extend `_run_tmux` to
   accept an optional `input_bytes=`). It must run `tmux load-buffer -b <buf> -` with the
   prompt on stdin. Pick a unique buffer name per seed (e.g. `f"cc-tmux-seed-{os.getpid()}-{monotonic}"`)
   to avoid collisions. Verify: unit-callable, returns non-None on success.

2. **Replace both seeding sites.** In `conductor.py` at `:392-396` and `:479-480`, replace
   the `send-keys -l prompt` call with the sequence: load-buffer (prompt) → paste-buffer
   (`-p` bracketed, `-t target`) → delete-buffer → send-keys Enter. Keep the existing
   `sent/entered is None` failure handling shape, extended to check each step. Preserve the
   surrounding readiness/grace logic (`_READY_TIMEOUT`, `_READY_GRACE`) untouched.

3. **Guard against the residual `Enter`-splits-a-paste case.** With bracketed paste (`-p`),
   the pane receives the multi-line text as one block and the single `send-keys Enter`
   submits it once. If cc-tmux's target REPL (claude) doesn't honor bracketed paste, a
   multi-line prompt would still submit at the first newline — in that case, additionally
   strip/reject bare newlines: if `"\n" in prompt`, log a warning and replace internal
   newlines with spaces (a prompt is a single instruction line). Implement the bracketed
   paste first; add the newline-strip as a belt-and-suspenders guard with a `# ponytail:`
   comment noting it's defense-in-depth.

4. **Add a self-test.** In `testing.py`, add a `test_prompt_seeding_*` that feeds a prompt
   containing `"line1\n/quit\nline3"` through the seeding path with a stubbed tmux runner
   (capture the argv + stdin the runner receives) and asserts: (a) the prompt bytes go
   through `load-buffer` stdin, not `send-keys -l`; (b) exactly one `send-keys Enter`
   follows; (c) no `send-keys -l` with the raw prompt is issued. Follow an existing
   stubbed-runner test in testing.py as the pattern (grep `rg "def test_" apps/cc-tmux/src/cc_tmux/testing.py | head`).

5. **Run tests:** `cd apps/cc-tmux && cc-tmux self-test` → all pass including the new one.
   Then `scripts/check.sh` → exit 0 (with plan 001 live, this suite is in the gate).

## Boundaries

- **In scope:** `apps/cc-tmux/src/cc_tmux/conductor.py` (the two seeding sites), `apps/cc-tmux/src/cc_tmux/tmux.py` (new helper), `apps/cc-tmux/src/cc_tmux/testing.py` (new test).
- **Out of scope:** wavetui (it's the exemplar, already correct — do not touch), any other cc-tmux file, the readiness/reconcile logic, cc-tmux's tmux-status-bar code.

## Done criteria (machine-checkable)

- `rg 'send-keys.*-l.*prompt' apps/cc-tmux/src/cc_tmux/conductor.py` → no hits (literal seeding gone).
- `rg 'load-buffer' apps/cc-tmux/src/cc_tmux/tmux.py` → present.
- `cc-tmux self-test` → passes, and includes a test whose name matches `prompt_seed` (grep the self-test output or source).
- The new test proves a `\n/quit\n`-containing prompt does not reach `send-keys -l`.
- `scripts/check.sh` → exit 0.

## Test plan

The self-test in step 4 is the required new coverage. It must use a stubbed tmux runner
(no real tmux) and assert on the argv/stdin sequence — this is a pure-function test in the
testing.py style, no live tmux pane needed.

## Maintenance note

Any future code that puts text into a claude pane must use the load-buffer helper, never
`send-keys -l` with variable text. This is the Python twin of plan 003 (AppleScript) and
the same principle wavetui's dispatch package enforces. If cc-tmux ever gains more seed
sites, they route through the new helper.

## Escape hatch

- If `cc-tmux self-test` is failing on a clean checkout at `d441448` before your change: STOP and report — plan 001 will surface pre-existing failures, and you shouldn't build on a red suite.
- If claude's REPL turns out not to honor bracketed paste (test the multi-line submit manually if you have a live pane): keep the newline-strip guard from step 3 as the primary defense and note it prominently in your report.
