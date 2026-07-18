# daily-brief Specification

## Purpose
TBD - created by archiving change add-daily-brief-tui. Update Purpose after archive.
## Requirements
### Requirement: Daily snapshot collection composes all brief sources fail-open
The system SHALL provide a collector (`daily-brief collect`) — TypeScript executed by Bun from `apps/daily-brief/` — that composes a dated snapshot JSON at `~/.local/state/daily-brief/<YYYY-MM-DD>.json` (plus a `latest.json` pointer) from four source families: mx-gateway (`GET /briefing`, `GET /triage`, `GET /sources` at `http://127.0.0.1:8799`, 3s timeout each), fleet open-items, doc-hygiene state, and generation metadata. Every source MUST degrade independently — a failed source records `{"available": false, "error": "..."}` in its section and never aborts the run or a sibling source. Dependencies are limited to ink (+ its React peer) with a committed lockfile; the collector path MUST NOT require node — Bun is the sole runtime.

#### Scenario: mx-gateway down at collection time
- **WHEN** `daily-brief collect` runs while nothing listens on 127.0.0.1:8799
- **THEN** the snapshot is still written, its `mx` section carries `available: false` with the error, and the open-items and docs sections populate normally

#### Scenario: snapshot written atomically
- **WHEN** a collection run is interrupted mid-write
- **THEN** the previous day's snapshot and `latest.json` remain intact (writes go to a `.tmp` file then atomic rename)

### Requirement: Meetings section merges both calendars with staleness honesty
The snapshot's meetings section SHALL contain today's calendar events from mx-gateway (`/briefing.calendar`, which aggregates the `gcal` and `outlook-calendar` sources), and SHALL record each calendar source's `last_sync_at` and `item_count` from `/sources`. The widget's meetings panel MUST render a per-source staleness banner (e.g. `outlook-calendar: never synced`) whenever a calendar source is `NOT_SERVING`, has `last_sync_at` null, or last synced more than 24h ago — absence of Outlook events MUST be distinguishable from an empty Outlook calendar.

#### Scenario: Outlook calendar registered but never synced
- **WHEN** `/sources` reports `outlook-calendar` with `item_count: 0` and `last_sync_at: null`
- **THEN** the meetings panel shows Google events normally and a warning line that Outlook calendar has never synced, rather than silently implying a meeting-free day

### Requirement: Radar section surfaces MINE triage items with act-on-item keys
The snapshot's radar section SHALL contain mx `/triage` items filtered to `core.ballInCourt == "MINE"`, grouped by `verdict.disposition` (OPEN before WAITING), each carrying title, source, url, `lastActivityAt`, and prior `triage` decision if present. The widget SHALL bind action keys on the selected radar item — snooze (`POST /triage/{id}/snooze`), triage-status (`POST /triage/{id}/status`), and open-URL — issuing POSTs without an Authorization header first (localhost-trust model, cross-repo dep `if-ammk`); on HTTP 401 it MUST retry once with `Authorization: Bearer` read from `~/.mx/gateway.env` if that file exists, and render the failure inline (never crash) if both attempts fail.

#### Scenario: snooze before mx localhost trust ships
- **WHEN** the user presses snooze on an item and mx still enforces bearer auth
- **THEN** the unauthenticated POST gets 401, the client retries with the token from `~/.mx/gateway.env`, and the item renders as snoozed on success

#### Scenario: action on a stale item
- **WHEN** an action POST returns a non-2xx status on both attempts
- **THEN** the widget shows the status inline on that item and remains usable

### Requirement: Open-items section aggregates every registry repo with beads
The collector SHALL iterate `home/projects.toml` project entries, resolve each path, and for every existing repo directory that contains `.beads/` and whose path does not match `**/archive/**`, run `~/.claude/scripts/bin/open-items` from that repo (capturing its JSON contract: exit 0, error-key on failure). The snapshot stores per-repo `summary` counts plus `bucket_counts` and the top items (blocked and human_only buckets first). A repo whose scan fails records its error and is skipped, never aborting the fleet loop.

#### Scenario: archived project path in the registry
- **WHEN** a `projects.toml` entry resolves under an `archive/` directory
- **THEN** it is excluded from the loop and absent from the snapshot

#### Scenario: repo without beads
- **WHEN** a registry repo has no `.beads/` directory
- **THEN** it is skipped silently (not an error) and does not appear in the open-items section

### Requirement: Docs section reads the scheduled doc-cleanup state
The snapshot's docs section SHALL read `~/.local/state/docs-hygiene-daily/results.jsonl` (the 04:15 docs-hygiene-daily timer output) and `~/.claude/state/docs-sweep-last-run.json` (cc docs-sweep state), recording per-file flagged/error findings, each file's generation timestamp, and a `stale: true` marker when a state file is older than 48h or missing.

#### Scenario: docs-hygiene has not run recently
- **WHEN** `results.jsonl` is older than 48h at collection time
- **THEN** the docs panel renders its findings with an explicit stale banner instead of presenting them as fresh

### Requirement: Brief renders as an ink TUI delivered as a persistent Zellij widget
The brief UI (`daily-brief view`) SHALL be an ink (React) terminal app rendering the snapshot's MEETINGS / RADAR / OPEN ITEMS / DOCS sections, with keyboard selection on the radar list, inline action results, and a non-interactive `view --plain` static render. After a successful 6am collection, the injector SHALL deliver it as a persistent widget in the most-recently-active Zellij session: a pinned floating pane spawned via `zellij --session <name> run --floating --pinned true -- daily-brief view` (Zellij >= 0.43; a Zellij WASM plugin is explicitly NOT the mechanism — plugins target wasm32-wasip1, which ink cannot compile to). When no Zellij session exists but a tmux server is running (mux-transition fallback), the injector SHALL instead open it as a tmux window inserted before the lowest-index window of the most-recently-active tmux session. With neither mux running, injection is skipped silently — the snapshot persists and `daily-brief view` renders it on demand. The injector always exits 0.

#### Scenario: zellij session present at 6am
- **WHEN** the timer fires while a Zellij session is running
- **THEN** the brief appears as a pinned floating pane in the most-recently-active session, on top of the current tab, without destroying or replacing any existing pane

#### Scenario: tmux-only morning during the mux transition
- **WHEN** the timer fires with no Zellij session but an attached tmux session whose lowest window index is 1
- **THEN** the brief opens as a tmux window at index 1 without stealing focus

#### Scenario: no mux at 6am
- **WHEN** the timer fires on a machine with neither a Zellij nor tmux server running
- **THEN** collection succeeds, no widget is created, exit code is 0, and `daily-brief view` later renders the morning snapshot

### Requirement: Dual-platform 6am scheduling registered in the chezmoi installer without brick risk
The system SHALL ship a systemd user timer/service pair (`daily-brief.timer` `OnCalendar=*-*-* 06:00:00`, `Persistent=true`; oneshot service running `bun run` on the collect entrypoint with `--open-widget`) in `home/dot_config/systemd/user/` and a launchd plist (`com.leonardoacosta.daily-brief.plist`, `StartCalendarInterval` 06:00) in `home/Library/LaunchAgents/`, both registered in `home/run_onchange_after_install-user-schedulers.sh.tmpl` (enable loop + sha256 include lines). Because the payload requires Bun, the installer MUST guard this unit: when `bun` is absent, it skips the enable with a warning and `chezmoi apply` still succeeds (never reproducing the `if-vit.1` bun-less brick class); the unit itself also fails soft (non-zero collection stays inside the unit, never surfacing to chezmoi).

#### Scenario: fresh Linux machine without bun
- **WHEN** `chezmoi apply` provisions a bun-less Linux box
- **THEN** the installer prints a skip warning for `daily-brief.timer`, enables everything else, and exits successfully

#### Scenario: machine asleep at 6am
- **WHEN** a laptop wakes at 9am having missed the 6am window
- **THEN** the persistent timer fires the missed run on wake and the widget appears then

