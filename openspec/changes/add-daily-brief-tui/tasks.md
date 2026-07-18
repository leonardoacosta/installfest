---
stack: t3
---
<!-- beads:epic:if-2dvu -->
<!-- beads:feature:if-8iix -->

# Implementation Tasks

## DB Batch

- [x] 1.1 Scaffold `apps/daily-brief/` Bun package (package.json with ink + react + committed lockfile, `src/index.tsx` CLI entry with `collect`/`view` subcommands, tsconfig, bin shim) — Bun-only runtime, no node/build step [beads:if-25im]
- [x] 1.2 Implement `src/sources/mx.ts`: fail-open fetch client for `http://127.0.0.1:8799` `/briefing` `/triage` `/sources` (3s timeout, `{available: false, error}` on any failure) and MINE-filter + disposition grouping for triage items [beads:if-4ktt]
- [x] 1.3 Implement `src/sources/openItems.ts`: parse `home/projects.toml` via `$DOTFILES`, loop entries whose resolved path exists, contains `.beads/`, and does not match `**/archive/**`, run `~/.claude/scripts/bin/open-items` from each repo, capture summary/bucket_counts/top items, record per-repo errors without aborting the loop [beads:if-zrys]
- [x] 1.4 Implement `src/sources/docsState.ts`: read `~/.local/state/docs-hygiene-daily/results.jsonl` and `~/.claude/state/docs-sweep-last-run.json`, classify each `stale` when missing or older than 48h [beads:if-qlr8]
- [x] 1.5 Implement `src/collect.ts`: compose the schemaVersion-1 snapshot (mx, meetings with per-source `source_health` staleness from `/sources`, open_items, docs), write atomically to `~/.local/state/daily-brief/<YYYY-MM-DD>.json` plus `latest.json`, exit 0 on all failures [beads:if-zb0z]

## API Batch

- [x] 2.1 Implement radar action client in `src/sources/mx.ts`: `POST /triage/{id}/snooze` and `POST /triage/{id}/status` unauthenticated first, on 401 retry once with `Authorization: Bearer` read from `~/.mx/gateway.env`, return structured result (never throw) for inline UI rendering [beads:if-oq5o]
- [x] 2.2 Add `home/dot_config/systemd/user/daily-brief.service` (Type=oneshot, ExecStart running `bun run` on the collect entrypoint with `--open-widget`, fail-soft wrapper so a bad morning never marks the unit failed) and `daily-brief.timer` (`OnCalendar=*-*-* 06:00:00`, `Persistent=true`, WantedBy=timers.target) [beads:if-5rsx]
- [x] 2.3 Add `home/Library/LaunchAgents/com.leonardoacosta.daily-brief.plist` (StartCalendarInterval 06:00 running the same collect --open-widget entrypoint via bun) [beads:if-iyz4]
- [x] 2.4 Register both units in `home/run_onchange_after_install-user-schedulers.sh.tmpl` with a bun-absence guard: sha256sum include lines at top, timer enabled in the Linux loop ONLY when `command -v bun` succeeds (warn + skip otherwise, chezmoi apply still exits 0 — the if-vit.1 brick class), plist in the macOS bootstrap list [beads:if-epc5]

## UI Batch

- [x] 3.1 Implement ink section components (`src/ui/App.tsx`, `src/ui/sections.tsx`) for MEETINGS / RADAR / OPEN ITEMS / DOCS as pure functions of snapshot slices, with staleness banners (never-synced calendar, stale docs state) and per-repo open-item count line, plus `src/plainRender.ts` reusing the section formatters for `view --plain` static ANSI output [beads:if-rs8c]
- [x] 3.2 Implement the interactive ink app: `useInput` j/k selection on the radar list, Enter opens item url via the platform opener, s=snooze prompt, t=triage-status prompt, q quit, resize-safe, all action results rendered inline [beads:if-jyqb]
- [x] 3.3 Implement `src/widgetOpen.ts`: pick the newest-activity Zellij session from `zellij list-sessions` and spawn `zellij --session <name> run --floating --pinned true -- daily-brief view`; when no Zellij session exists fall back to tmux first-tab (`tmux new-window -b -t "<sess>:^" -n brief`, most-recently-active attached session); neither mux running → skip; always exit 0 [beads:if-bfes]
- [x] 3.4 Wire `collect --open-widget` end-to-end and capture runtime evidence: real 6am-equivalent run producing a snapshot, ink app rendered against it in a pty with all four sections present [beads:if-sebb]

## E2E Batch

- [x] 4.1 Add `bun test` suite `test/collect.test.ts`: collector fail-open cases (mx down, one repo erroring, missing docs state) via stubbed fetch/HTTP server, atomic-write interruption, snapshot schema assertions [beads:if-mbm6]
- [x] 4.2 Add `test/openItems.test.ts`: fixture projects.toml with an `archive/`-path entry, a beads-less repo, and a normal repo — assert exclusion, silent skip, and inclusion respectively [beads:if-2wo6]
- [x] 4.3 Add `test/mxActions.test.ts`: stub server returning 401 then 200 — assert unauthenticated-first, single token retry from a fixture gateway.env, structured error on double failure [beads:if-77uy]
- [x] 4.4 Runtime-verify widget injection: disposable Zellij session gets the pinned floating pane with existing panes untouched; tmux-only fallback lands the window before the lowest index without stealing focus; exit 0 with neither mux running [beads:if-gox5]
- [x] 4.5 Runtime-verify scheduling: `chezmoi apply --dry-run` shows both units + installer registration; `systemctl --user daemon-reload` + `systemctl --user list-timers daily-brief.timer` (Linux) and `plutil -lint` on the plist (macOS) pass; installer guard exercised on a PATH without bun (warn + skip, exit 0) [beads:if-8gyl]
