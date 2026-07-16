<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-tuq3 -->

# Tasks: cc-tmux-title-folder-fallback

> Literal `## API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no DB or UI
> batch this time (no new constants, no `cli.py` call-site changes beyond the one function this
> touches). Owner: general-purpose engineer agents (no dedicated api/ui roles for this Python
> tmux plugin).

## API Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/cli.py`: in `compose_title_name` (or `_maybe_rename_window`, [beads:if-ay2b]
  whichever owns the title/code composition — read both before editing), change the fallback
  order so that when `@cc-title` is unset or empty, the window name is the raw current-directory
  basename (`os.path.basename(pane_current_path)`) alone — regardless of whether a project code
  resolves from the registry. The project-code prefix (`<code>·<title>`) is applied ONLY when a
  session title is present. Reuse whatever basename-resolution helper `"state"`-mode renaming
  already calls (`cli.py:756-759` per this session's `/openspec:explore` findings) rather than
  reimplementing path-basename logic a second time. [owner:general-purpose] [type:api]

## E2E Batch

- [ ] [2.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test covering title-absent + [beads:if-pe3p]
  project-code-resolves — asserts the window name is the folder basename alone, not the code.
  Extend or add a second case for title-absent + no-registered-project, asserting the same
  folder-basename fallback (unchanged from today's behavior in that specific case, but now
  reached via the same code path as the code-resolves case rather than a separate branch).
  Existing tests for title-present composition (code-prefixed and title-alone) must keep
  passing unmodified. [owner:general-purpose] [type:api]
- [ ] [2.2] Run `python3 -m py_compile` on the touched file and the full self-test suite [beads:if-o1fb]
  (`cd apps/cc-tmux && python3 -c "import sys; sys.path.insert(0,'src'); from cc_tmux.testing
  import run_self_test; sys.exit(run_self_test())"`) — confirm the total pass count increases
  (new tests added) and zero failures. Run `./scripts/check.sh` at the repo root and confirm it
  passes. [owner:general-purpose] [type:api]
