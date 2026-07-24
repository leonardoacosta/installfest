# Repo Context: Davidcreador/herdr-token-dashboard

> Source: https://github.com/Davidcreador/herdr-token-dashboard
> Context: project (installfest)   ·   Stars: 9   ·   Last push: 2026-07-15   ·   License: MIT
> Ask: none — general sweep (no rider supplied; findings weighted toward the live motivating
> question from this session: "how do we show custom data on herdr's agent tabs")
> Profile: external-repos   ·   Trust tier: unrated — never vetted (not yet in the recon registry)

## Research Plan
- Target map: `cmd/token-dashboard/{main.go (37KB), claude_test.go (8.5KB)}`, `docs/dashboard-preview.svg`, `scripts/{build.sh,capture-dashboard-preview.py,package-release.sh}`, `herdr-plugin.toml`, `README.md`
- Effort-scaling decision: 1 Explore agent (default) — single cohesive Go binary, not decomposable into independent boundaries
- Chosen split (if fan-out): n/a
- Expected key files/pages: `cmd/token-dashboard/main.go` (plugin entrypoint, TUI, all three agent-data-source readers, pricing table, notification logic), `cmd/token-dashboard/claude_test.go` (Claude Code transcript parsing tests), `herdr-plugin.toml` (manifest shape)

## Ask
No rider supplied — general adoption sweep. The sweep was naturally shaped by this session's
open design question (raised before this recon): whether cc-tmux/herdr integration should show
custom per-agent data via herdr's `pane.report_metadata` sidebar-token mechanism, or some other
route. This repo is directly relevant: it's a real, shipping herdr plugin that shows exactly this
class of custom per-agent data (live token spend/cost), and it answers the question by *not*
using `report_metadata` at all — see Card A below.

## Purpose
A Go-based herdr plugin that polls `herdr pane list` every 3 seconds, reads each tracked agent
pane's own session data (Pi JSONL, OpenCode's local HTTP API + on-disk fallback, or Claude Code's
transcript JSONL under `~/.claude/projects/`), and renders a live cost/token dashboard in its own
dedicated herdr tab (Bubble Tea + Lip Gloss TUI), plus fires a `herdr notification show` toast
when an agent finishes with a nonzero cost.

## Architecture & Key Patterns

```
herdr pane list  (CLI subprocess, polled every 3s from inside the TUI)
    |
    for each agent pane, dispatch on AgentSession.Source:
      "herdr:pi"           -> read Pi session JSONL
      "herdr:opencode"     -> query http://127.0.0.1:4096/session/{id}, fall back to
                               ~/.local/share/opencode/storage/message/{id}/*.json on disk
      "herdr:claude"/"claude" -> resolve + read ~/.claude/projects/{munged-cwd}/{sessionID}.jsonl
    |
    normalize into one tokenStats struct (Cost, InputT, OutputT, CacheR/W, Model, Provider,
    Messages, Tools map, Duration, ...)
    |
    Bubble Tea + Lip Gloss TUI: summary table + per-agent detail cards + workspace totals
    |
    poll-loop diffs prevStatus -> curr; on any -> "done" with cost > 0: herdr notification show
```

All herdr interaction is CLI-subprocess-only (`os/exec.Command("herdr", ...)`, overridable via
`HERDR_BIN_PATH`) — confirmed by an explicit grep for `report_metadata|socket|net.Dial` across
the full `main.go`, which returned no matches. Three call sites: `herdr pane list`,
`herdr notification show <title> --body <body> --position top-right --sound done`, and
`herdr plugin pane open --plugin <id> --entrypoint dashboard --placement tab`.

## Findings

### STEAL — herdr integration patterns: Claude transcript reading, plugin manifest, and the poll-primary gotcha

- **Coverage**: NONE — `rg -n "\.claude/projects|input_tokens|output_tokens|cache_read|transcript" apps/cc-tmux/src` and `rg -il herdr apps/cc-tmux/src apps/cc-tmux/*.md apps/cc-tmux/openspec` both returned zero matches. cc-tmux's only existing "usage" surface (`apps/cc-tmux/src/cc_tmux/usage.py`) reads nexus-agent's `/credentials` HTTP endpoint for account-level 5h/7d rate-limit utilization %, not per-pane token/cost data from Claude Code's own transcript — a different domain and granularity entirely. cc-tmux has zero herdr awareness today.
- **Source credit**: `cmd/token-dashboard/main.go` (`mungeClaudePath`, `claudeSessionPath`, `readClaudeSession`, the `turns` map, the poll-loop comment in `(m *model) refresh()`), `cmd/token-dashboard/claude_test.go` (glob-fallback and dedup test cases), `herdr-plugin.toml` (manifest shape), and the README's "for future compatibility" line describing the event hook's rationale (the *mechanism-doesn't-fire* claim is verified verbatim in `main.go`'s own comment; the *why it's kept anyway* framing is the README's, not code — corrected from the initial draft, which mis-attributed both to code).
- **Before**: no reference exists in installfest for how a herdr plugin talks to the herdr binary, how to locate/parse a Claude Code session transcript, or which event hooks are trustworthy on herdr 0.7.x.
- **After**: `docs/reference/herdr-integration-patterns.md` documents four things ready to reuse verbatim when this becomes a real feature:
  1. **Transcript path resolution**: munge the pane's cwd (every char outside `[A-Za-z0-9-]` -> `-`) to get `~/.claude/projects/<munged-cwd>/<sessionID>.jsonl`; if that misses (pane cwd changed after launch), fall back to `glob(~/.claude/projects/*/​<sessionID>.jsonl)` — session UUIDs are globally unique so the first glob match is safe.
  2. **Turn-level dedup**: streamed/retried assistant-message records collapse into a map keyed on `messageID + "\x00" + requestID`; each new record for the same key OVERWRITES the prior one (last-write-wins), so a streamed-then-finalized pair correctly lands on the final usage numbers, and message count is `len(map)` not raw line count.
  3. **Manifest shape reference**: `[[panes]] placement = "tab"`, `[[events]] on = "pane.agent_status_changed"`, `[[actions]]`, `[[keys.command]] type = "plugin_action"` — confirms and extends what was already reverse-engineered from herdr-file-viewer's manifest.
  4. **Critical gotcha**: the `[[events]] on = "pane.agent_status_changed"` manifest hook does **not** reliably fire — `main.go`'s own comment states it "does not fire for pane.agent_status_changed in Herdr 0.7.0," and the plugin's real detection mechanism is its 3-second poll loop diffing previous vs. current status. The README separately explains the event hook is kept "for future compatibility when Herdr adds manifest event dispatch for pane events" — i.e. it's a forward-compat stub, not a working mechanism today. Any new herdr plugin (installfest's herdr 0.7.5 is the same minor line as 0.7.0) MUST default to poll-and-diff as primary, never trust the event hook alone.
  5. **Decision input, not yet a decision**: this plugin answers our open "how do we show custom per-agent data" question by NOT using `pane.report_metadata`/`[ui.sidebar.agents]` tokens at all — it owns a full dedicated tab (`placement = "tab"`) with a self-built Bubble Tea TUI instead. Reasons this fits *its* use case: richer layout (summary table + detail cards) than 16 short (32-char) sidebar tokens could hold, and full-screen real estate for per-tool breakdowns. cc-tmux's candidate data (state, wait-reason, task summary, project, branch) is comparably rich to what this plugin tracks — this precedent is a real data point *for* the owned-tab approach, not just the sidebar-token approach assumed earlier this session, and should inform rather than pre-decide the eventual `/feature` spec.
  6. **Refined design (Leo's ask, this session)**: the owned tab should mirror cc-tmux's EXISTING floating accounts panel rather than invent a new layout. `apps/cc-tmux/src/cc_tmux/render.py`'s `render_accounts_popup` (invoked via `cli.cmd_accounts_popup`) already renders exactly the requested top section — one block per deduped account: a leading `*` for the active account (the sole active-indicator, per that function's own docstring), a neutral braille usage glyph (`render_usage_glyph_2metric`, `n=20`), and green-uniform `5H:xx% 7D:xx%` text, followed by an indented identity line (email + 8-char org id) and up to two reset-countdown lines. The bottom section (per-session token usage) should add a **SES** (session context-window usage) column driven by `render_session_bar`'s existing `_context_color_pair` 6-tier severity ramp (dim ≤100k → green >100k → yellow >200k → orange >300k → red >500k → red/bright-red pulsing >600k → dark-red/red pulsing >750k) plus the `_SES_HANDOFF_THRESHOLD` (63% of context window) `!handoff:/workflow:handoff` suffix already shown in the tmux status bar — both ported as-is, not redesigned. `@cc-state` (`waiting|idle|active`) and `@cc-wait-reason` (`question|plan|permission|elicitation`, only set while waiting) are the real tmux pane options this whole system already tracks (`apps/cc-tmux/src/cc_tmux/tmux.py:70-74`) — any sidebar-token variant would carry these same values across, not invent new ones.
- **Gain**: zero re-research when this becomes a real build; the poll-primary gotcha alone prevents shipping a plugin silently broken on event delivery. **Effort**: small (doc only). **Files**: `docs/reference/herdr-integration-patterns.md` (**create**).
- **Evidence**: 5/6 citations SUPPORTED, 1/6 PARTIAL (corrected above — real claim, mis-attributed source, now fixed) → 6/6 citations verified once corrected.

**Placement Verdict**
| # | Row | Verdict |
|---|-----|---------|
| 1 | Layer | Reference doc — not yet a script/skill/agent/command, since no working caller exists. A script or skill would be premature (Act-now-bias rule: no named caller caps the artifact at a docs note) |
| 2 | Landing path | `docs/reference/herdr-integration-patterns.md` |
| 3 | Extend-before-create | No existing installfest doc owns "reading Claude Code's own transcript" or "herdr plugin authoring." `apps/cc-tmux/skills/cc-status` reads cc-tmux's OWN tmux-pane-option state (not raw transcripts); `usage.py` reads nexus-agent's account-level HTTP API (not transcripts, not herdr). Genuinely new domain — create. The companion memory note `reference_herdr_sidebar_metadata_mechanism.md` (written earlier this session) should also get a follow-up line recording that the one real precedent found uses the owned-tab approach, not sidebar tokens |
| 4 | Standalone vs facet | Standalone doc for now — no canonical installfest skill yet owns the herdr-plugin-authoring domain to attach a `references/` dir to |
| 5 | Scope | installfest-local — cc-tmux is the fleet's own tmux/herdr plugin (per the prior `ogulcancelik-herdr` recon), not duplicated in any other repo |
| 6 | Tracked medium | New file — will be a plain tracked file under `docs/` (installfest's `docs/` tree has no gitignore carve-outs); no vendor-first precondition needed |
| 7 | Gitignore hazard | None — `docs/` is fully tracked |
| 8 | Description class | n/a — plain reference doc, not a skill (no caller yet to declare auto-trigger vs. explicit-only) |
| 9 | Wiring sites | None yet — pre-caller. A future `/feature` for the actual cc-tmux/herdr integration would cite this doc directly |
| 10 | Caller + cadence | **No current caller** — this is the explicit docs-note ceiling per the Act-now-bias rule; do not build a skill or harness around it until a real feature is spec'd |
| 11 | Fleet propagation | n/a — installfest-local |

### ADAPT — Cost-estimation pricing table (longest-substring model match + cache multipliers) -> cc's `claude-api` skill

- **Coverage**: PARTIAL — cc's `claude-api` skill already owns "model ids, pricing, params" per its own description, but (per that description) has no documented recipe for *estimating* cost when a transcript has token counts but no embedded dollar figure — exactly Claude Code's own transcript shape.
- **Source credit**: `cmd/token-dashboard/main.go` (`claudePricing` table, `claudeRates`, `claudeCost`), `cmd/token-dashboard/claude_test.go` (`TestClaudeRates`, the "unknown model has zero cost but tokens are kept" case).
- **Before**: `claude-api` has raw pricing figures but no "derive cost from a token-only transcript" pattern.
- **After**: skill gains a short recipe — match model-name substrings **longest-first** (`len(p.substr) > best`, not first-match-in-table-order), so a specific entry like `sonnet-4-5` correctly outranks a broader `sonnet-4` regardless of table ordering; bill cache-read tokens at 0.1x the matched input rate and cache-write tokens at 1.25x the input rate; an unmatched model contributes $0 cost but keeps its token counts (never silently drops data).
- **Gain**: reusable estimation recipe for any future local cost tooling (a herdr plugin, cc-tmux, or elsewhere) without re-deriving the cache multipliers or hitting the longest-match-not-first-match footgun from scratch.
- **Effort**: small. **Files**: `~/dev/cc/skills/claude-api/SKILL.md` (**update** — cc repo, out of scope for this installfest session's edits; named as the target for a follow-up).
- **Evidence**: 4/4 citations SUPPORTED.

**Placement Verdict**
| # | Row | Verdict |
|---|-----|---------|
| 1 | Layer | Skill update — reference content, extending cc's existing `claude-api` skill |
| 2 | Landing path | `~/dev/cc/skills/claude-api/SKILL.md` (or its `references/` subdir, if one exists — check on landing) |
| 3 | Extend-before-create | `claude-api` explicitly covers "model ids, pricing, params" already — extend, never create a sibling pricing doc |
| 4 | Standalone vs facet | Facet of `claude-api`'s existing pricing coverage |
| 5 | Scope | **Global (cc)** — per the project-context inversion rule: cost-estimation-from-tokens is fleet-wide Anthropic-API knowledge, not installfest-specific, so the landing path is explicitly cc rather than a project-local copy |
| 6 | Tracked medium | n/a this session — cc-repo edit deferred, not made during this installfest-context run |
| 7 | Gitignore hazard | None expected — skills are tracked in cc |
| 8 | Description class | Auto-trigger — already exists on `claude-api` (triggers on "pricing", "cost", model IDs, etc. per its listed description) |
| 9 | Wiring sites | None new — extends the existing skill body in place, no new routing table entry needed |
| 10 | Caller + cadence | Any future cost-estimation work in any repo; `claude-api` is a general-purpose, on-demand reference |
| 11 | Fleet propagation | n/a — a cc skill is already fleet-visible by definition |

### MONITOR (2)

- **Cross-agent adapter struct** (`tokenStats` unifying Pi/OpenCode/Claude readers into one shape) — no caller: cc-tmux tracks only Claude Code panes today, no Pi or OpenCode instances in the fleet. Revisit if a second agent CLI (Pi, OpenCode) ever joins the fleet's tmux/herdr setup.
- **OpenCode dual-path (live server + disk fallback)** — same no-caller reason; see its own Novelty Record below, since the resilience pattern (prefer live API, degrade to on-disk artifact) is independently reusable even without OpenCode itself.

### SKIP (0)

None — no duplicate-coverage findings this run; cc-tmux had zero prior surface in any of these domains.

## Novel Patterns

**A. Munged-cwd + session-UUID-glob-fallback transcript resolution**
- **Domain**: locating a Claude Code session transcript file from a pane's tracked cwd + session ID.
- **Mechanism**: `~/.claude/projects/<cwd-with-every-non-alnum-dash-char-replaced-by-dash>/<sessionID>.jsonl` is tried first; on miss, `glob(~/.claude/projects/*/​<sessionID>.jsonl)` — safe because session UUIDs are globally unique, so no disambiguation is needed across the glob's matches. `cmd/token-dashboard/main.go` (`mungeClaudePath`, `claudeSessionPath`), test-verified in `claude_test.go`'s "glob fallback when munged cwd path misses" case.
- **Why not now**: no caller — captured in Card A's doc as ready-to-lift reference content instead of being implemented speculatively.
- **Revisit trigger**: any feature that needs to read a specific Claude Code pane's own transcript (a herdr status panel, a cc-tmux cost row, a session-recovery tool).

**B. Turn dedup via composite-key map, last-write-wins**
- **Domain**: aggregating usage/cost from a JSONL transcript that can contain streamed and retried duplicate records for the same logical turn.
- **Mechanism**: `map[messageID + "\x00" + requestID]claudeTurn{...}`, each new line for an existing key overwrites rather than accumulates — `main.go`'s `readClaudeSession`, test-verified by `claude_test.go`'s "dedupe repeated message id and requestId" case (asserts the LATER of two conflicting usage values wins).
- **Why not now**: same as A — no caller yet.
- **Revisit trigger**: same as A.

**C. Poll-loop-as-primary, event-hook-as-future-compat-stub**
- **Domain**: detecting agent-pane state transitions in a herdr plugin.
- **Mechanism**: the manifest declares `[[events]] on = "pane.agent_status_changed"`, but `main.go`'s own comment states this "does not fire for pane.agent_status_changed in Herdr 0.7.0"; the actual working mechanism is the TUI's 3-second poll loop diffing `prevStatus[paneID]` against the current status. The README separately frames the event hook as kept "for future compatibility when Herdr adds manifest event dispatch for pane events."
- **Why not now**: no herdr plugin exists in installfest yet to apply this gotcha to.
- **Revisit trigger**: the moment any herdr plugin build starts in this repo — this becomes a hard requirement, not optional guidance, since installfest runs herdr 0.7.5 (same minor line as the confirmed-broken 0.7.0).

**D. OpenCode live-API-with-disk-fallback**
- **Domain**: reading an external agent tool's session data that may or may not have a live server running.
- **Mechanism**: try `http://127.0.0.1:4096/session/{id}` first (live, richer data); on non-200/unreachable, fall back to reading persisted JSON under `~/.local/share/opencode/storage/message/{id}/*.json` (`main.go`, `readOpenCodeLive`/`readOpenCodeDisk`).
- **Why not now**: no OpenCode usage in the fleet.
- **Revisit trigger**: OpenCode (or any agent CLI with a similar live-server + on-disk-persistence shape) joins the fleet.

**E. ANSI-capture-to-SVG doc-preview generation**
- **Domain**: generating an accurate screenshot of a TUI for a README, without hand-drawing or a real screenshot tool.
- **Mechanism**: `herdr pane read <pane-id> --source visible --ansi > frame.ansi`, then `scripts/capture-dashboard-preview.py` parses SGR escape codes (`\x1b\[([0-9;]*)m`) into styled spans and renders an SVG.
- **Why not now**: no herdr plugin/TUI exists in installfest yet to document this way.
- **Revisit trigger**: any future herdr (or tmux) plugin README needs an accurate TUI preview image.

## Prior Coverage
None found for this repo: no `docs/recon/*herdr-token-dashboard*` artifact, no registry entry, no openspec change, no beads match. Adjacent prior coverage: `docs/recon/ogulcancelik-herdr.md` (cc, 2026-07-22) recon'd herdr itself and flagged `pane.report_metadata`/token statusline as "coverage FULL" — but that verdict was scoped to *tmux status bar* parity (nexus-statusline + cc-tmux own that surface); herdr's own native sidebar was correctly left open by that prior run, and this session's earlier investigation (captured in memory as `reference_herdr_sidebar_metadata_mechanism.md`) confirmed the mechanism exists. This recon adds the missing real-world data point: the one concrete herdr plugin found that displays custom per-agent data does NOT use that mechanism.
