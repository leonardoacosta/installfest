## MODIFIED Requirements

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse content, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`, PLUS a third independent segment carrying
the active nexus-agent credential's identity (email + 8-character org id, e.g.
`leo@priceless.dev·bc7da511` — the same format used by the accounts popup's identity rows).

**Left-side content cycles between two phases on a wall-clock timer, prefixed by a countdown
glyph** — reversing the prior version's "next: SHALL NOT render on this row" exclusion. Phase is
`int(now / 8.0) % 2`, `now` the caller-supplied wall-clock time at render (the same daemon-free,
`status-interval`-driven cadence `animated_icon` already uses for the tabs row — no timer
process, no new tmux hook):

- **Phase 0** (counts): the openspec/beads portion renders in cc's abbreviated form `op:
  {open}o {in_progress}ip {ua}ua ({age}) | bd: {open}o {ready}r {blocked}b ({age})` (if-bqw.1, cc
  commit `b6b9a234` / cc-w83ov.4), where `ua` is the closure-debt count — specs that are done but
  not yet archived — and the `bd:` half counts only "standalone" beads — issues that are NOT a
  transitive descendant, via a `parent-child` dependency, of any issue whose title starts with
  `[SPEC]` or `[CAPABILITY]` — so the two halves are additive rather than double-counting
  OpenSpec-tracked work. The `bd:` half's `open` count is the total standalone beads currently
  open/in_progress/blocked, alongside the pre-existing `ready`/`blocked` counts. Each half's
  numeric values SHALL be coloured by semantic threshold (DIM for a healthy zero/low count on
  `open`/`in_progress`/`ready`, YELLOW when `ua > 0` or `standalone_blocked > 0`, RED above a
  documented high-count threshold).
- **Phase 1** (next): the row instead renders the cache's `next:` line verbatim (already
  pre-truncated by the producer script) in place of the `op:`/`bd:` segments — the two never
  render simultaneously.
- A `radar:` line SHALL NOT be rendered in either phase (unchanged from the prior requirement
  version — defense against a stale or rolled-back cache carrying that token).
- When no `next:` line is available in the cache, phase 1 falls back to rendering phase 0's
  content instead (never a blank left side when counts ARE available).
- No new data production mechanism SHALL be introduced for the openspec/beads/next portion — it
  reads the same cache nexus-statusline's own `getRoadmapPulse()` already maintains; this row
  parses it a second, independent time.

**Account identity segment**: the plugin SHALL append the active credential's identity as a
third segment, independent of the openspec/beads/next cycle — present whenever an active
nexus-agent credential resolves, regardless of cycle phase or whether the roadmap-pulse cache
exists at all. The segment SHALL be clickable, bound to `cc-tmux accounts-popup`, via the same
`#[range=user|accounts]` mouse-range marker mechanism, in both phases.

#### Scenario: phase 0 renders counts with the countdown glyph, plus the account identity
- Given: a cached roadmap-pulse file whose counts are `1o 0ip 0ua` (openspec) and `1o 1r 0b`
  (standalone beads), an active nexus-agent credential `leo@priceless.dev` / org `bc7da511-...`,
  and a render `now` that resolves to phase 0
- When: the beads-bar row renders
- Then: it shows `[countdown-glyph] op: 1o 0ip 0ua (<age>) | bd: 1o 1r 0b (<age>) |
  leo@priceless.dev·bc7da511` with all counts coloured DIM, and the account segment clickable via
  the same mouse-range marker

#### Scenario: phase 1 renders the next-action line instead of counts
- Given: the same cached roadmap-pulse file additionally carries a `next: [WORKSPACE-CMDCENTER]
  Wor...` line, and a render `now` that resolves to phase 1
- When: the beads-bar row renders
- Then: the left side shows `[countdown-glyph] next: [WORKSPACE-CMDCENTER] Wor...` — the `op:`/
  `bd:` segments do NOT appear — and the account segment still renders on the right, unchanged

#### Scenario: no next: line available falls back to phase 0's content in phase 1
- Given: a cached roadmap-pulse file with counts but no `next:` line, and a render `now` that
  resolves to phase 1
- When: the beads-bar row renders
- Then: the left side shows the phase-0 `op:`/`bd:` content (with countdown glyph) instead of a
  blank phase-1 slot — the row never goes empty just because the cycle landed on an unavailable
  phase

#### Scenario: no roadmap-pulse cache, but an active account resolves
- Given: no roadmap-pulse cache file exists yet for the current project, and an active
  nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row shows ONLY the account identity segment (`leo@priceless.dev·bc7da511`) — not an
  empty row, in either phase, since the account segment is independent of the cycle

#### Scenario: openspec/beads/next cache present, no active account resolves
- Given: a cached roadmap-pulse file with real counts and a `next:` line, and nexus-agent is
  unreachable (no active credential resolves)
- When: the beads-bar row renders
- Then: the row shows only the cycling left-side content (counts or next, per phase) — no empty
  account segment, no error

#### Scenario: a stray radar: line never renders, in either phase
- Given: a cached roadmap-pulse file containing a `next: …` line, a `radar:stale` line (stale
  pre-fix content), and a counts line
- When: the beads-bar row renders, in both phase 0 and phase 1
- Then: the `radar:` line never appears on the row in either phase — only the phase-appropriate
  content (counts or next) and, if applicable, the account segment

#### Scenario: standalone beads exclude OpenSpec-tracked work
- Given: 5 open beads total, 3 of which are tasks under a `[SPEC] some-proposal` feature (itself
  under a `[CAPABILITY]` epic), and 2 of which have no epic ancestor at all
- When: the standalone-beads count is computed
- Then: only the 2 unparented beads count toward `bd: {open}o {ready}r {blocked}b` — the 3
  OpenSpec-tracked tasks do not, since they're already represented by the `op:` half

#### Scenario: nothing available renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project, and no active
  nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text, no countdown glyph with nothing to
  prefix (unchanged from the prior requirement version's "no cache yet" contract, now also gated
  on the account segment's own absence)
