---
stack: cc-meta
---
<!-- beads:epic:if-oxks -->
<!-- beads:feature:if-ok17 -->

# Implementation Tasks

## API Batch

- [x] [1.1] [user:pre] DECISION: cc's `hn` (harness) registry entry has `deploy = "homelab"` (a bare string) while every other one of the 37 migrated projects uses a `{ type = "...", ... }` table (`at`/`hl`/`if` all use `{ type = "docker", host = "homelab" }` for the identical docker-on-homelab shape) — searched: cc's full `projects.json` (36 of 37 other entries table-shaped), `rules/CORE.md` Breaking Changes Policy (no silent reshaping of existing data without confirming intent); no documented pattern covers whether to preserve the literal string or normalize `hn` to match its siblings. [type:config] [beads:if-sj0t]
  - RESOLVED (Leo, 2026-07-19): normalize `hn` to `{ type = "docker", host = "homelab" }`, matching its siblings. Task 1.4 (below) implements this.
- [x] [1.2] Extend `home/projects.toml`'s header comment to document the 11 new optional fields (`devPort`, `port`, `deploy`, `monitors`, `personas`, `seed_command`, `stack`, `has_beads`, `has_openspec`, `prod_url`, `legacy_codes`) per `design.md` § New optional fields, including the `deploy` per-type field vocabulary and the `monitors` nested-table convention. [beads:if-oxwf]
- [x] [1.3] Migrate new-field data onto the 30 cc-registry projects whose code already matches an existing `home/projects.toml` entry 1:1 (`cc`, `ap`, `oo`, `tc`, `tl`, `mv`, `la`, `ct`, `cs`, `nx`, `nova`, `mesh`, `lv`, `hl`, `if`, `ba`, `bo`, `dc`, `es`, `ew`, `fb`, `ic`, `lu`, `pp`, `sc`, `se`, `ws`, `ss` — plus `ws-topo` handled separately in [1.6] and the 7 code-mismatches in [1.4] and the `tb` collision in [1.5]), per `design.md`'s per-project source data. [beads:if-8mk2]
  - depends on: 1.2
- [ ] [1.4] Migrate new-field data onto the 7 identity-matched (not code-matched) existing entries: cc's `pc`→`priceless-config`, `pa`→`priceless-app`, `sj`→`seth-jones`, `at`→`atlas`, `tm`→`terraform-modules`, `gd`→`guardian`, `hn`→`harness` (per [1.1]'s resolution) — each gets its cc-registry fields attached to the existing toml entry matched by `path` per `design.md` § Legacy code aliasing, plus `legacy_codes = ["<cc-code>"]`. [beads:if-tkio]
  - depends on: 1.1, 1.2
- [ ] [1.5] Migrate cc's `tb` (The Bridge, `~/dev/brown/thebridge`) new-field data onto if-toml's existing `thebridge` entry (NOT its existing `tb` entry, which is a distinct, unrelated project at `dev/tb`) — do NOT add `"tb"` to `thebridge`'s `legacy_codes` per `design.md`'s collision note. [beads:if-265z]
  - depends on: 1.2
- [ ] [1.6] Create a new `[[projects]]` entry for `ws-topo` (`~/dev/ws-topo`, no existing if-toml match by code or path) with `stack = "t3-turbo"`, `devPort = 3198`, `has_beads = false`, `has_openspec = false`, and category/icon/tiers chosen consistent with sibling personal-category entries. [beads:if-tw1x]
  - depends on: 1.2

## E2E Batch

- [ ] [2.1] Runtime-verify: run `python3 -c "import tomllib; d=tomllib.load(open('home/projects.toml','rb')); print(len(d['projects']))"` against the fully-migrated file — confirm it succeeds with no exception and reports exactly 94 entries. Paste the stdout. [beads:if-v0ck]
  - depends on: 1.3, 1.4, 1.5, 1.6
- [ ] [2.2] Runtime-verify: run `scripts/generate-raycast.sh --dry-run` before and after the migration, diff the two outputs restricted to a sample of untouched if-only entries (e.g. `brown`, `ds`, `priceless`, `personal`) — confirm byte-identical output for those codes. Paste the diff (empty). [beads:if-bcrz]
  - depends on: 2.1
- [ ] [2.3] Runtime-verify: source `scripts/cmux-workspaces.sh`'s `load_projects` function (or invoke `mux --list`) before and after the migration — confirm the same sample codes from [2.2] produce identical `PROJECTS`/`CATEGORIES`/`FULL_NAMES` entries. Paste the comparison output. [beads:if-rigc]
  - depends on: 2.1
- [ ] [2.4] `bash -n` on `scripts/generate-raycast.sh`, `scripts/cmux-workspaces.sh`, and `scripts/mux-remote.sh` — confirm zero syntax errors (documents that no UI Batch edits were needed, per `design.md` § Consumer impact). [beads:if-31a9]
  - depends on: 2.1
