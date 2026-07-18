---
order: 0718a
---

# Proposal: Daily Brief TUI — 6am mx + open-items + docs composite, persistent Zellij widget

## Change ID
`add-daily-brief-tui`

## Summary
Add `apps/daily-brief/`, a TypeScript (Bun + ink/React) package that composes a once-daily 6am
snapshot — mx-gateway briefing/triage/sources (meetings from Google + Outlook calendars, radar
items), fleet-wide open-items across every registry repo with `.beads/`, and the scheduled
doc-cleanup state — renders it as an ink TUI with act-on-item keys, and delivers it as a
persistent pinned floating pane in the most-recently-active Zellij session (tmux first-tab
fallback during the mux transition). Scheduled by a systemd-user-timer + launchd plist pair
registered in the chezmoi scheduler installer with a bun-absence guard.

## Context
- Extends: the scheduled-task pattern of `scripts/docs-hygiene-daily.py` +
  `home/dot_config/systemd/user/docs-hygiene-daily.{timer,service}`; consumes mx-gateway
  (`127.0.0.1:8799`) and `~/.claude/scripts/bin/open-items` as-is; Bun runtime precedent:
  `home/dot_local/share/nexus-listener.ts`
- Related: Zellij 0.44 (`zellij run --floating --pinned`), `if-ammk` (cross-repo mx work:
  outlook-calendar first sync + localhost-trusted writes; this spec ships a 401 token fallback
  so it does not block), `if-vit.1` (bun-less-Linux installer brick — this spec's installer
  registration adds the guard that class demands)
- touches: `apps/daily-brief/package.json`, `apps/daily-brief/src/index.tsx`, `apps/daily-brief/src/collect.ts`, `apps/daily-brief/src/sources/mx.ts`, `apps/daily-brief/src/sources/openItems.ts`, `apps/daily-brief/src/sources/docsState.ts`, `apps/daily-brief/src/ui/App.tsx`, `apps/daily-brief/src/ui/sections.tsx`, `apps/daily-brief/src/plainRender.ts`, `apps/daily-brief/src/widgetOpen.ts`, `apps/daily-brief/test/collect.test.ts`, `apps/daily-brief/test/openItems.test.ts`, `apps/daily-brief/test/mxActions.test.ts`, `apps/daily-brief/README.md`, `home/dot_config/systemd/user/daily-brief.service`, `home/dot_config/systemd/user/daily-brief.timer`, `home/Library/LaunchAgents/com.leonardoacosta.daily-brief.plist`, `home/run_onchange_after_install-user-schedulers.sh.tmpl`

## Motivation
Morning state is scattered across five surfaces (mx radar, per-repo beads/openspec, two doc-sweep
state files, two calendars), each needing an explicit visit; mx-gateway already composes
`/briefing` (queue + calendar + meds) but nothing merges it with local dev state or delivers it
without being asked. A generated-once-daily widget that is simply *there* — pinned in the focused
Zellij session — follows the ambient-surfacing doctrine that replaced `/next` and `/open`.
Stack decision 2026-07-18 (Leo): TypeScript + ink over Python/curses, aligning with the preferred
native language; Zellij as the delivery surface. A Zellij WASM plugin was considered and ruled
out — plugins compile to wasm32-wasip1 (Rust/Go/C), which ink/React cannot target — so the
persistent widget is a pinned floating pane running the ink app.

## Requirements

### Requirement: Daily snapshot collection composes all brief sources fail-open
See `specs/daily-brief/spec.md`.

### Requirement: Meetings section merges both calendars with staleness honesty
See `specs/daily-brief/spec.md`.

### Requirement: Radar section surfaces MINE triage items with act-on-item keys
See `specs/daily-brief/spec.md`.

### Requirement: Open-items section aggregates every registry repo with beads
See `specs/daily-brief/spec.md`.

### Requirement: Docs section reads the scheduled doc-cleanup state
See `specs/daily-brief/spec.md`.

### Requirement: Brief renders as an ink TUI delivered as a persistent Zellij widget
See `specs/daily-brief/spec.md`.

### Requirement: Dual-platform 6am scheduling registered in the chezmoi installer without brick risk
See `specs/daily-brief/spec.md`.

## Non-goals
- A Zellij WASM plugin (would force Rust; ink cannot compile to wasm32-wasip1 — the pinned-pane
  widget is the deliberate substitute)
- Migrating the wider tmux/cc-tmux workflow to Zellij (this spec only delivers into Zellij when
  present, with a tmux fallback; the mux migration is its own decision)
- Meds and finance panels (mx `/briefing` carries them; not requested — trivially addable later)
- Fixing mx's outlook-calendar sync or its auth model (cross-repo, tracked as `if-ammk`)
- Email/Teams inbox summarization (mx `/triage` already covers the ball-in-court subset)

## Testing
- Collector fail-open per source, atomic snapshot writes, registry loop with archive exclusion,
  calendar staleness classification, 401 token fallback: `bun test` suites in
  `apps/daily-brief/test/` (stubbed HTTP server + fixture projects.toml) — tasks 4.1–4.3
- Widget injection: runtime-verified against a live disposable Zellij session (pinned floating
  pane appears, existing panes untouched) and the tmux fallback + no-mux exit-0 paths — task 4.4
- Scheduler wiring: `chezmoi apply --dry-run` shows unit deployment; timer/plist dry-run
  verification per platform, plus the bun-absence guard exercised on a stripped PATH — task 4.5
- Interactive ink UI: ink-testing-library render assertions where feasible; full-screen behavior
  covered by the runtime evidence demanded in task 3.4 (real snapshot rendered in a pty with all
  four sections present)
