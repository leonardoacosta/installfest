# cc-tmux Specification

## ADDED Requirements

### Requirement: Prompt text is delivered to a claude pane as data, never as literal keystrokes
The conductor SHALL deliver prompt text into a claude tmux pane via `tmux load-buffer` (prompt
bytes on stdin) followed by `tmux paste-buffer -p` (bracketed paste) and `tmux delete-buffer`,
then submit with exactly one `send-keys Enter` — NEVER via `send-keys -l` with prompt text
containing caller-supplied or otherwise untrusted substrings. This MUST hold for every seeding
site in the conductor (initial dispatch and window-open alike). As defense-in-depth against a
target REPL that does not honor bracketed paste, internal newlines in the prompt MUST additionally
be stripped (replaced with spaces) before delivery, with a logged warning — this guard is
secondary to the load-buffer sequence, not a replacement for it.

#### Scenario: an embedded-newline prompt submits as one block
- Given: a prompt string containing an embedded newline followed by further text (e.g.
  `"line1\n/quit\nline3"`)
- When: the conductor seeds this prompt into a claude pane
- Then: the pane receives the entire prompt as a single block and submits exactly once — the
  text after the embedded newline is never typed or executed as a separate command

#### Scenario: a stubbed tmux runner proves load-buffer stdin is used, not send-keys -l
- Given: a stubbed tmux runner that records every invoked argv and any stdin passed to it
- When: the conductor seeds a prompt through the stubbed runner
- Then: the recorded invocations show the prompt bytes delivered via `load-buffer ... -` stdin,
  followed by a `paste-buffer -p` and a `delete-buffer`, and exactly one `send-keys ... Enter` —
  at no point does a `send-keys -l` invocation carry the raw prompt text

#### Scenario: every conductor seeding site uses the same sequence
- Given: the conductor's initial-dispatch seeding site and its `_open_window` seeding site
- When: either site seeds a prompt
- Then: both use the identical load-buffer → paste-buffer → delete-buffer → send-keys Enter
  sequence — no seeding site independently issues `send-keys -l` with variable prompt text
