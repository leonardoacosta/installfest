# Implementation Plans — /improve audit 2026-07-22

Advisor-authored plans from a read-only audit of `installfest` at commit `d441448`.
Each plan is self-contained for a fresh-context executor. **Never let an executor edit
outside its plan's declared scope.** Plans stamp the commit they were written against; if
HEAD has moved and an excerpt no longer matches, the plan says STOP and report drift.

> Note: this `plans/` dir was previously retired (see git history / the older note that
> lived here — new work had moved to beads + openspec). It is revived here solely to hold
> this audit's executable plans. Runtime work tracking still belongs in beads/openspec;
> these files are execution specs, not a parallel tracker.

## Execution order & dependencies

```
001 app-test-gate  ──┬─► 005 cctmux-loadbuffer   (needs cc-tmux self-test in the gate)
 (do first)          ├─► 006 ctx-scan-boundaries (needs ctx-scan bun test in the gate)
                     └─► 007 daily-brief          (needs daily-brief bun test in the gate)

002 mesh-file-server ─ independent (shell layer already shellcheck-gated)
003 applescript      ─ independent
004 nfs-export       ─ independent, HAS A [user] DECISION — do not guess it
008 front-door-docs  ─ independent, pure docs, zero risk
```

**Land 001 first.** It wires the app test suites into `scripts/check.sh`, which is the
regression net that plans 005/006/007 rely on to prove their changes. 002/003/004/008 touch
the shell/docs layers and don't strictly need it, but there's no reason to delay 001.

## Status table

| # | Plan | Category | Effort | Risk | Depends on | Status |
|---|------|----------|--------|------|------------|--------|
| 001 | [App test gate in check.sh](001-app-test-gate.md) | tests/dx | S | low | — | TODO |
| 002 | [Mesh file-server hardening](002-mesh-file-server-hardening.md) | security | M | med | — | TODO |
| 003 | [AppleScript injection fix](003-applescript-injection.md) | security | S | low | — | TODO |
| 004 | [NFS export scoping](004-nfs-export-scoping.md) | security | S | med | `[user]` decision | TODO |
| 005 | [cc-tmux load-buffer seeding](005-cctmux-loadbuffer-seeding.md) | correctness/security | M | med | 001 | TODO |
| 006 | [ctx-scan scan boundaries](006-ctx-scan-boundaries.md) | security | S–M | low | 001 | TODO |
| 007 | [daily-brief hardening + tests](007-daily-brief-hardening-and-tests.md) | security/tests | M | low | 001 | TODO |
| 008 | [Front-door docs pass](008-front-door-docs.md) | docs | S–M | low | — | TODO |

Executors: update your row's Status (TODO → IN PROGRESS → DONE / BLOCKED) and note blockers inline.

## What was audited

`standard` effort, 4 parallel read-only subagents across all 9 categories, over: `apps/`
(wavetui Go, ctx-scan + daily-brief Bun/TS, cc-tmux Python, kontroll Rust light pass),
`scripts/`, `home/` (chezmoi), `platform/`, `infra/`, `shared/`.

**Not audited / skipped:** vendored `apps/zsa-voyager-keymap/qmk_firmware` and `apps/kontroll`
internals (pinned upstream submodules — light pass only, one real bug found: #009 below);
`node_modules`, `.beads/`, `openspec/changes/archive/`. Heavy git history analysis was skipped
(slow network mount). This audit did not deeply exercise the Terraform (`infra/`) or the
Windows/CloudPC platform paths.

## Findings NOT turned into plans (recorded so they aren't re-audited)

- **kontroll `hex_to_rgb` panics on short/non-ASCII hex input** (`apps/kontroll/src/utils.rs:4-6`,
  callers `cli.rs:159,178`) — HIGH confidence, real: `kontroll set-rgb --color "#abc"` panics
  before the intended "not a valid hex color" error path runs. Not planned because kontroll is
  a **vendored upstream submodule** (`github.com/zsa/kontroll`) — fix belongs upstream, not in
  this repo. If a local patch is wanted, it's ~3 lines (length+ASCII check before slicing).
- **wavetui headless children SIGKILLed on clean quit** (`internal/daemon/headless_dispatcher.go:102`
  bound to run-scoped ctx) — LOW confidence, likely **by design** (the app documents stopping
  sources + Program together on quit). Investigate-only; not a plan.
- **wavetui unbounded per-session accumulation** (`transcript.go:699` userMessages, `:622`
  remainder) — LOW confidence, minor memory in a single-user long-lived session. Investigate-only.
- **Bleeding-edge Go pins** — `go 1.26.5`, untagged pseudo-version for `charmbracelet/ultraviolet`
  (`apps/wavetui/go.mod:16`). Minor reproducibility note; bump when ultraviolet cuts a tag. Not a plan.
- **`zsa-firmware-check.sh` sources `~/.env`** vs harden.sh's parse-don't-source pattern — nil
  impact today (`~/.env` is 0600/user-owned); latent inconsistency, not worth a plan.

## Considered and rejected (do not re-report)

- Findings already dispositioned in the retired `docs/plans/README.md` reconciliation
  (2026-07-18) and the 2026-03 `docs/audit/` CRITICALs — all resolved/historical. Re-verified
  not-regressed in this pass.
- if-7cce.2 doc-drift bundle (CLAUDE.md dirs, undocumented front-door tools, stale mx-broker
  status) — already tracked; plan 008 covers only NEW doc drift and defers to that bead on overlap.
- wavetui's hand-rolled TOML subset parser (`internal/config/config.go`) — correctly single-sourced
  and tested; not duplicated anywhere. No action.
- No cross-app dependency debt (each language uses its native JSON; no redundant parsers). No action.

## Direction options (maintainer's call — not ranked against the fixes above)

- **A. Shared Claude-Code session-state reader** — wavetui sources, cc-tmux, and the three
  recon targets all re-implement "make parallel CC sessions visible." A single shared
  session-state source both frontends consume kills duplicate discovery logic while keeping
  wavetui's full TUI and cc-tmux's status-bar mode as distinct valid UX. Effort M.
- **B. Ratify the ctx-scan↔wavetui shell-out boundary** — the in-flight `wavetui-context-pane`
  change already chose shell-out over porting; document the `schemaVersion` JSON contract now
  so future tabs shelling out the same way don't drift. Effort S (mostly in flight).
- **C. (now plan 001)** app test+lint in the gate — promoted to a plan.
- **D. Decide daily-brief's trajectory** — it's pulling Ink/React and growing a `view` surface
  that overlaps wavetui's tab model. Decide now whether it becomes a wavetui source/tab or
  stays a standalone renderer, before there are two Ink apps to unwind. Effort M (decision).
- **E. (partly plan 008)** mark vendored submodules — doc half is plan 008; the structural
  `apps/vendor/` split is a separate maintainer decision.
