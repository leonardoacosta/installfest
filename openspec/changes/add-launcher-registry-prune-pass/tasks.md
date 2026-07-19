---
stack: cc-meta
---
<!-- beads:epic:if-oxks -->
<!-- beads:feature:if-nusd -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Add a shared `registry_orphan_codes()` helper to `scripts/lib/registry.sh`: given an [beads:if-6yss]
  output directory + tier, return the list of `{code}.sh` basenames present on disk whose code
  is NOT a `projects.toml` key with that tier — same diff logic `scripts/audit-projects.sh`
  section 3 already implements, extracted so both the auditor and the two generators share one
  implementation instead of two independent copies.

## API Batch

- [x] [2.1] Extend `scripts/generate-raycast.sh`: after writing all current-registry scripts, [beads:if-wre5]
  call `registry_orphan_codes()` for each of `platform/raycast-scripts/`,
  `platform/raycast-scripts/local/`, `platform/raycast-scripts/cloudpc/` (skipping `root.sh`/
  `open-project.sh` as intentional infra, matching the auditor's existing exclusion), delete
  every returned orphan, and print what was deleted. Under `--dry-run`, print the same list
  without deleting.
  - depends on: 1.1
- [x] [2.2] Investigate `scripts/cmux-workspaces.sh`'s actual per-project-code generated output [beads:if-g2bg]
  (if any beyond what [2.1] already covers) and either wire the same prune helper against it, or
  — if nothing per-code is generated there today — document that finding in a code comment at
  the top of the script (no-op prune, nothing to do) rather than inventing new prunable output.
  - depends on: 1.1

## UI Batch

- [ ] [3.1] Update `scripts/generate-raycast.sh`'s own usage/header comment to document the new [beads:if-5i47]
  prune behavior and `--dry-run`'s prune-preview output, so a reader sees it without diffing the
  script.

## E2E Batch

- [ ] [4.1] Runtime-verify: seed a disposable extra `{code}.sh` file in each of the 3 raycast [beads:if-xuu0]
  output directories (simulating a stale orphan), run `generate-raycast.sh --dry-run`, confirm
  it lists all 3 as pending deletion without removing them; run without `--dry-run`, confirm all
  3 are actually gone and every real registry-backed script is untouched.
  - depends on: 2.1
- [ ] [4.2] Runtime-verify [2.2]'s finding: either confirm the prune pass correctly removes a [beads:if-ucod]
  seeded orphan from `cmux-workspaces.sh`'s own output, or confirm the documented no-op case by
  running the script normally and showing no prunable output exists.
  - depends on: 2.2
- [ ] [4.3] Runtime-verify: run `scripts/audit-projects.sh` before and after a full registry [beads:if-sbh3]
  removal + generator re-run; confirm the pre-run shows the orphan FAIL and the post-run shows
  `PASS: raycast-sync` with zero orphans reported.
  - depends on: 2.1, 2.2
- [ ] [4.4] `bash -n` on both modified generator scripts; confirm no syntax errors. [beads:if-7wle]
  - depends on: 2.1, 2.2, 3.1
