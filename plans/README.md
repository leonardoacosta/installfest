# plans/ — Advisor Plan Ledger (installfest)

Executor-ready remediation plans from read-only `/improve:*` audits. Each plan is
self-contained for a zero-context executor. Executing a plan is a separate,
approved step — plans are input, not work-in-progress.

Ledger rule: when a row moves to DONE/BLOCKED/REJECTED, append a
`spec-impact: <slug>[, ...]` token (or `spec-impact: none` for direct commits).

## Plan Index

| # | Plan | Priority | Effort | Status | Depends on | Beads |
| - | ---- | -------- | ------ | ------ | ---------- | ----- |
| 001 | [cc-tmux liveness + doctor truthfulness + reconcile revival](001-cc-tmux-liveness-and-doctor.md) | P1 | M | DONE | — | if-clqv |
| 002 | [transition matrix: approval hole, transition-only timestamps, SessionStart matcher](002-cc-tmux-transition-matrix.md) | P1 | S | OPEN | — | if-r78i |
| 003 | [TTL-cache 4MB credentials fetch + session-context ts cutoff + glyph cleanup/docs](003-cc-tmux-usage-cache-and-staleness.md) | P1 | M | DONE | — | if-xtox |
| 004 | [git dirty/ahead via session-context.json (cross-repo nx write + cc-tmux read)](004-session-context-git-status.md) | P2 | M | OPEN | 003 | if-vnrv |
| 005 | [render consolidation: one spawn per tick + trace-write gating](005-cc-tmux-render-consolidation.md) | P2 | M | OPEN | 001, 003 | if-pw0g |
| 006 | [beads/roadmap row staleness marker](006-cc-tmux-beads-row-staleness.md) | P3 | S | OPEN | — | if-mx9w |
| 007 | [conductor dispatch hardening (disabled-by-default feature)](007-cc-tmux-conductor-hardening.md) | P3 | M | OPEN | — | if-x0qg |

## Wave 1 — 2026-07-11 `/improve:code deep apps/cc-tmux/` (HEAD 60a1441)

Audit: 9 seams, 49 agents, 40 adversarially-verified findings (31 confirmed,
6 refuted, 2 corrected, 1 already-settled). Evidence bundles:
`/tmp/installfest-code-audit/plan-NNN.json` (ephemeral — plans are self-contained).

### Execution order

1. **002** (S, hooks.json only, no code conflicts) -> **001** -> **003**
2. **004** after 003 (shared `_read_session_context` edits; nx-repo side is a
   separate commit/push under that repo's rules — operator approval)
3. **005** strictly after 001 + 003 (absorbs their cli.py logic into render-all)
4. **006**, **007** any time (no conflicts)

### Cross-plan file conflicts

- `cli.py`: 001 (cmd_tabs_row, cmd_doctor) x 003 (_read_session_context,
  _active_usage) x 005 (render-all absorbs both) — sequence as above.
- `tmux.py`: 002 (set_pane_state timestamp gate) x 001 (no overlap, different
  functions) x 003 (delete session_count_glyph) — low conflict, still sequence.
- `hooks/hooks.json`: 002 only.

### Operator gates / follow-ups

- **Plugin snapshot propagation**: Claude-side hooks run the cached snapshot at
  `~/.claude/plugins/cache/cc-tmux/cc-tmux/<version>/`. Every plan touching
  hooks.json or register-path code is DEAD on the Claude side until a plugin
  version bump + `claude plugin update`. Verify with a real hook fire.
- **Plugin was silently disabled** by the 0.1.1 update (root cause of the
  2026-07-11 outage: frozen states, no notifications). Re-enabled by operator
  2026-07-11. Plan 001 adds the doctor rows that would have caught this.
- **nx-repo**: nexus-agent `/credentials` payload has accumulated 2,709
  credential rows (4.03MB) — junk/duplicate hygiene issue, nx-side fix, not
  addressable from this repo. Also: nexus-agent never polls usage
  (`usagePolledAt` null on every row) so 5H/7D render `--` by design.
- **Plan 004 nx side**: separate repo/remote — its commit/push needs explicit
  approval per cross-repo git rule.

### Rejected / refuted findings (do not re-report)

| ID | Why rejected |
| -- | ------------ |
| BEADS-02 (cc-tmux should spawn roadmap-pulse revalidation) | Settled-by-spec: `2026-07-11-cc-tmux-session-usage-bars` design invariant "no new background process"; refresh ownership is nexus-statusline's. Headless-project staleness is a documented tradeoff — changing it is a design-change proposal, Leo's call. |
| USE-2 (zero-isActive should render a "no active acct" marker) | Documented Invariant-5 fail-open contract in usage.py; empty segment is by design. |
| USE-3 / MLP-2 (representative-pane choice wrong) | waiting>idle>active representative selection is by design. |
| GIT-FRESH-1 (git identity should refresh on render) | waiting/idle-only resolution is documented invariant 4 (hot-path cost). |
| CD-2 (send-keys -l newline submits) | Not reproducible as a live defect. |
| USE-4 (as stated: "indefinite" stale SES) | Overstated — writer-side 6h GC bounds it; the real, bounded defect is carried by plan 003 (MLP-1 ts cutoff). |
| USE-5 (divergent segment markup copies) | Known accepted duplication. |

### Not audited

- `notify/` platform backends (linux/macos/windows) — no findings seeded, not swept.
- `parser.py`, `paths.py`, `__main__.py` — trivial glue, skipped.
- chezmoi deployment scripts / tmux theme confs beyond status-format wiring.
- Repo-wide (non-cc-tmux) code: audit-scan gave the repo 99% (A); single finding
  (B12 workspace package.json) predates this audit and is not cc-tmux's.
