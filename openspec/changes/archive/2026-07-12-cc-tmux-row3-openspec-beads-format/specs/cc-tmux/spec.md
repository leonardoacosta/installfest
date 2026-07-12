# cc-tmux Specification Delta

## MODIFIED Requirements

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse counts, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`. The row SHALL render in the form
`openspec: {open} open {unarchived} unarchived ({age}) | beads: {ready} ready {blocked} blocked
({age})`, where the beads half counts only "standalone" beads — issues that are NOT a transitive
descendant, via a `parent-child` dependency, of any issue whose title starts with `[SPEC]` or
`[CAPABILITY]` — so the two halves are additive rather than double-counting OpenSpec-tracked
work. Any line in the cache starting with `next:` or `radar:` SHALL NOT be rendered on this row —
only the openspec/beads counts render, regardless of what else the cache file contains (defense
against a stale or rolled-back cache carrying either token). Each half's numeric values SHALL be
coloured by semantic threshold (DIM for a healthy zero/low count, YELLOW when `unarchived > 0` or
`standalone_blocked > 0`, RED above a documented high-count threshold). No new data production
mechanism SHALL be introduced for this row — it reads the cache nexus-statusline's own
`getRoadmapPulse()` already maintains, extended upstream to carry the beads fields.

#### Scenario: row 3 renders both halves with independent staleness ages
- Given: a cached roadmap-pulse file whose counts are `2 open, 1 unarchived` (openspec) and `3
  ready, 0 blocked` (standalone beads)
- When: the beads-bar row renders
- Then: it shows `openspec: 2 open 1 unarchived (<age>) | beads: 3 ready 0 blocked (<age>)` with
  `1 unarchived` coloured YELLOW and the rest DIM/CYAN

#### Scenario: a stray next: or radar: line never renders
- Given: a cached roadmap-pulse file containing a `next: …` line, a `radar:stale` line (stale
  pre-fix content), and a counts line
- When: the beads-bar row renders
- Then: only the openspec/beads counts render — neither the `next:` nor the `radar:` line
  appears anywhere on the row

#### Scenario: standalone beads exclude OpenSpec-tracked work
- Given: 5 open beads total, 3 of which are tasks under a `[SPEC] some-proposal` feature (itself
  under a `[CAPABILITY]` epic), and 2 of which have no epic ancestor at all
- When: the standalone-beads count is computed
- Then: only the 2 unparented beads count toward `beads: {ready}/{blocked}` — the 3
  OpenSpec-tracked tasks do not, since they're already represented by the openspec half

#### Scenario: counts-only cache renders as-is
- Given: a cached roadmap-pulse file containing only the openspec/beads counts (no `next:` or
  `radar:` line)
- When: the beads-bar row renders
- Then: it shows both halves, unchanged

#### Scenario: no cache yet renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text
