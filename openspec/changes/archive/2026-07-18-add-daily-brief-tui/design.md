# Design: add-daily-brief-tui

## Shape

```
6:00 timer (systemd user / launchd)
  └─ bun run apps/daily-brief/src/index.tsx collect --open-widget
       ├─ sources/mx.ts ──────── GET /briefing /triage /sources  (127.0.0.1:8799, 3s, fail-open)
       ├─ sources/openItems.ts ─ loop projects.toml repos with .beads/ (skip **/archive/**),
       │                          run ~/.claude/scripts/bin/open-items per repo
       ├─ sources/docsState.ts ─ read ~/.local/state/docs-hygiene-daily/results.jsonl
       │                          + ~/.claude/state/docs-sweep-last-run.json
       ├─ write ~/.local/state/daily-brief/<date>.json  (.tmp + atomic rename) + latest.json
       └─ widgetOpen.ts ──────── zellij --session <s> run --floating --pinned true -- daily-brief view
                                  └─ fallback: tmux new-window -b -t "<sess>:^"  (mux transition)
                                  └─ fallback: skip, exit 0 (no mux running)

daily-brief view  ── ink (React) app over latest.json (or --date); view --plain = static ANSI
```

## Decisions

**TypeScript + ink under Bun (2026-07-18, Leo — supersedes the Python/curses v1 design).**
Aligns with the preferred native language; ink gives componentized sections (React reconciler
over the terminal) instead of hand-managed curses geometry. Bun is the runtime: already a repo
dependency precedent (`nexus-listener.ts`), runs TS/JSX natively with no build step, ships
`bun test`. node/mise is NOT a fallback runtime — one runtime keeps the units simple.

**Persistent widget = pinned floating pane, NOT a Zellij WASM plugin.** Zellij plugins compile
to wasm32-wasip1 (Rust/Go/C/AssemblyScript); ink/React cannot target that. `zellij run
--floating --pinned true` (0.42+; 0.44.3 installed) gives the actual "persistent widget"
behavior — always-on-top across tabs, dismissable, re-openable. Session pick: newest-activity
session from `zellij list-sessions`; the timer runs outside any session so every action uses the
explicit `--session <name>` flag.

**tmux fallback during the mux transition.** Current daily driver is tmux (cc-tmux stack); if
6am finds no Zellij session, the brief would silently never appear. `widgetOpen.ts` falls back
to the previous design's tmux first-tab semantics (`tmux new-window -b -t "<sess>:^"`,
most-recently-active attached session, `base-index` 1 respected). Neither mux running → skip,
exit 0. The injector never fails the unit.

**Aggregation lives client-side, not in mx.** mx stays the comms/calendar authority; open-items
and doc-sweep state are per-machine files mx should not know about. The collector composes.

**Snapshot-then-render, not live TUI.** The 6am artifact is a dated JSON snapshot; the ink app
is a pure renderer over it. Free re-opens all day, browsable history dir, testable seam
(components take a snapshot object; collector takes a stubbed fetch).

**Radar actions: unauthenticated first, token fallback.** Leo's call 2026-07-18: mx should trust
localhost writes (`if-ammk`). Until that lands, POSTs hitting 401 retry once with
`Authorization: Bearer` from `~/.mx/gateway.env` (already on disk, 0600). Failures render inline.

**Installer guard instead of a runtime purity rule.** The v1 design avoided bun to dodge the
`if-vit.1` brick class; the stack pivot makes bun load-bearing, so the guard moves to the
installer: the scheduler template enables `daily-brief.timer` only when `command -v bun`
succeeds, warning and continuing otherwise — `chezmoi apply` can never brick on this unit, and
the unit itself wraps collection in fail-soft so a bad morning never marks the service failed.

**Timer at 06:00, docs sweep at 04:15.** Ordering is deliberate: the doc-cleanup results the
brief reads are at most ~1h45m old at collection time. `Persistent=true` / launchd default
behavior replays a missed 6am on wake.

## Snapshot schema (v1)

```json
{
  "schemaVersion": 1,
  "generated_at": "ISO",
  "mx":        {"available": true, "briefing": {...}, "triage_mine": [...], "sources": [...]},
  "meetings":  {"events": [...], "source_health": [{"id","last_sync_at","item_count","stale"}]},
  "open_items":{"repos": [{"code","root","summary","bucket_counts","top_items"}], "errors": [...]},
  "docs":      {"hygiene": {...,"stale"}, "sweep": {...,"stale"}}
}
```

Per the scripts-as-data-producers convention: `schemaVersion` from day one (the UI consumes it),
sections carry `available`/`stale` coverage attestation, collector exits 0 always.

## Widget layout (ink)

```
┌ Daily Brief — Fri Jul 18 ────────────────────────────────┐
│ MEETINGS  (gcal ok · outlook-cal NEVER SYNCED)           │
│   09:00  Standup (Teams link)          14:00  Gym        │
│ RADAR  12 MINE (9 open · 3 waiting)                      │
│ > [ado] Reorganize Bicep folder taxonomy      2d  [s][t] │
│ OPEN ITEMS  if:30 (9 human) · cc:12 · nx:4 · brown:7     │
│ DOCS  hygiene: 3 flagged · sweep: 8 flagged (fresh)      │
└ j/k move · Enter open url · s snooze · t status · q quit ┘
```

Components: `<App>` → `<Meetings>` `<Radar>` (the one interactive list, `useInput`) `<OpenItems>`
`<Docs>` — each a pure function of its snapshot slice. `view --plain` reuses the same section
formatters through a static string renderer (no ink mount) for piping/screenshots.
