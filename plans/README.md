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
| 002 | [transition matrix: approval hole, transition-only timestamps, SessionStart matcher](002-cc-tmux-transition-matrix.md) | P1 | S | DONE | — | if-r78i |
| 003 | [TTL-cache 4MB credentials fetch + session-context ts cutoff + glyph cleanup/docs](003-cc-tmux-usage-cache-and-staleness.md) | P1 | M | DONE | — | if-xtox |
| 004 | [git dirty/ahead via session-context.json (cross-repo nx write + cc-tmux read)](004-session-context-git-status.md) | P2 | M | DONE | 003 | if-vnrv |
| 005 | [render consolidation: one spawn per tick + trace-write gating](005-cc-tmux-render-consolidation.md) | P2 | M | DONE | 001, 003 | if-pw0g |
| 006 | [beads/roadmap row staleness marker](006-cc-tmux-beads-row-staleness.md) | P3 | S | DONE | — | if-mx9w |
| 007 | [conductor dispatch hardening (disabled-by-default feature)](007-cc-tmux-conductor-hardening.md) | P3 | M | DONE | — | if-x0qg |
| 008 | [dead scripts/ sweep: cmux-debug, ani-cli, dbpro, youtube-transcript, setup-az-wrapper](008-dead-scripts-sweep.md) | P2 | S | OPEN | — | if-qmrl |
| 009 | [open-family: ropen ~/dev/if fallback fix (live defect) + basename-dispatch consolidation](009-open-family-consolidation.md) | P1 | M | DONE | — | if-fq6t |
| 010 | [workspace pkg: wk-doctor gate, check.sh coverage, ado-ready stdin-loss bugfix, README drift](010-workspace-pkg-hygiene.md) | P2 | M | DONE (spec-impact: none) | — | if-js82 |
| 011 | [zsh + dot_local: dead aliases, broken proxied trio, editor-wrapper dupes, chezmoiignore gap](011-shell-and-dotlocal-hygiene.md) | P2 | M | OPEN | 008 | if-zzl4 |
| 012 | [deploy-hook integrity: post-commit dead under beads hooksPath + IF-POSTMERGE drift](012-deploy-hook-integrity.md) | P1 | S | DONE | — | if-th0d |
| 013 | [ssh-mesh + platform: superseded playbooks, config drift, stale published key, wezterm](013-ssh-mesh-platform-staleness.md) | P2 | M | OPEN | — | if-zf42 |
| 014 | [cc-tmux dead-code sweep: session-context reader, ANSI bar, paths.py, legacy segments](014-cc-tmux-dead-code-sweep.md) | P2 | M | OPEN | — | if-z44k |

## Wave 2 — 2026-07-14 `/improve:entropy deep` (HEAD 9399b92)

Audit: 10 seams, 50 agents, 40 adversarially-verified findings (0 refuted at
verify; 22 lower-impact tail items carried into plans), 7 plans drafted with
0 dropped claims. Evidence bundles: `/tmp/installfest-entropy-audit/plan-NNN.json`
(ephemeral — plans are self-contained). Grades: scripts-root B, open-family B,
workspace B, zsh B, dot-config B, chezmoi-run B, platform B, ssh-mesh C,
cc-tmux C, dot-local B.

### Execution order

1. **012** (P1, S — revives silently-dead commit-time auto-deploy) and
   **009 step 1** (P1 ropen fix, S) first — both live defects.
2. **008** before **011** (both touch scripts/install-arch.sh).
3. **010** any time (sole owner of scripts/check.sh; carries the ado-ready
   stdin-loss bugfix — SC2259, reproduced: b-and-b ADO tracker always emits []).
4. **009 step 2** (consolidation), **013**, **014** any time after their
   operator gates clear.

### Cross-plan file conflicts

- `scripts/install-arch.sh`: 008 (header line 3) x 011 (packages[] zellij) —
  sequence 008 -> 011 or merge the header fix into 011.
- `.claude/workflows/project-mgmt-audit.js`: 008 (cmux-debug ref) x 013
  (ssh-mesh configs prompt repoint) — small independent edits, either order.
- All other plans are sole owners of their files.

### Operator gates (decisions plans will surface, not pre-decided)

- 008: delete-vs-promote dbpro.sh / youtube-transcript.sh / setup-az-wrapper.sh.
- 009: >5-file consolidation approval (gopen/sopen/mopen/iopen -> viewopen.sh);
  OPEN-05 Tailscale-IP dedup DEFERRED (MagicDNS behavior regression risk).
- 010: wk-doctor deploy (one-line symlink, recommended) vs delete.
- 011: proxied-trio platform intent; vercel-trim wire-vs-delete; cspell dict.
- 013: playbook-lane delete vs chezmoi-era pointers; fetch-all twins;
  .terraform.lock.hcl tracking (policy change).
- 014: notify/windows.py keep-vs-delete; plugin version bump 0.1.2 -> 0.1.3
  (snapshot propagation gate); pre-existing self-test failure
  cli.beads_pane_fallback (105/106 baseline, NOT caused by this work).

### Known pre-existing defects surfaced (escalated as beads, not plan-created)

- Commit-triggered chezmoi auto-deploy dead on beads machines (no post-commit
  in .beads/hooks; core.hooksPath=.beads/hooks) — plan 012.
- ropen-server silently serves empty registry under systemd (~/dev/if fallback
  + unexported DOTFILES) — plan 009.
- ado-ready normalizer never receives input (SC2259 heredoc-overrides-pipe);
  b-and-b ADO tracker output always [] — plan 010.
- cc-tmux self-test baseline 105/106 (cli.beads_pane_fallback fails with live
  tmux server; monkeypatch gap) — noted in plan 014, fix unowned.

### Not audited (wave 2)

- notify/ backends' internals (only liveness/wiring checked).
- docs/ content accuracy beyond claims plans touch.
- infra/ terraform module internals (only wiring + lock policy).
- ~/.agents installed skills, cc-side plugin cache content.

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
