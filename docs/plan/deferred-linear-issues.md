# Deferred Specs — Linear Filing Fallback

No `.claude/linear-config.json` present in this repo, so deferred specs are recorded here
instead (per `/apply:all` Phase 6 check 3.5's markdown fallback).

## add-cmux-sidebar-widgets

- **Status**: 11/15 tasks complete (DB/API/UI batches fully done). E2E batch (4 tasks) held.
- **Epic**: if-bqw · **Feature**: if-g2mg
- **Why held**: all 4 remaining tasks require live SSH + cmux manipulation on the operator's
  Mac. Open P0 bead `if-vit.7` documents an unresolved incident where `cmux close-others`
  accidentally closed 3+ live Mac workspaces during this exact kind of verification. Proceeding
  autonomously would repeat that risk before the operator has decided how to do it safely.
- **Remaining tasks** (`openspec/changes/add-cmux-sidebar-widgets/tasks.md` § E2E Batch):
  - `[4.1]` Live-verify cc-tmux's dual-write on a real state transition (beads:if-35jf)
  - `[4.2]` Live-verify the full left sidebar in a real cmux session (beads:if-g4u2)
  - `[4.3]` Live-verify the git-tree panel, local + SSH-backed (beads:if-tf9q)
  - `[4.4]` Live-verify the usage dashboard panel against real nexus-agent data (beads:if-kg51)
- **Next step**: resolve `if-vit.7` (decide on a safe live-cmux verification protocol), then
  `/apply add-cmux-sidebar-widgets --continue` to finish the E2E batch and archive.
