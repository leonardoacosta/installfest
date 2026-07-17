## MODIFIED Requirements

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse content, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`, PLUS a third independent segment carrying
the active nexus-agent credential's identity (email + 8-character org id, e.g.
`leo@priceless.dev·bc7da511` — the same format used by the accounts popup's identity rows).

**The `op:`/`bd:` counts render permanently, every tick — there is no cycle.** (Reverses
`cc-tmux-row3-next-cycle`'s wall-clock swap between counts and a `next:` line; that requirement
version is superseded.) The left side renders cc's abbreviated form `op: {open}o {in_progress}ip
{ua}ua ({age}) | bd: {open}o {ready}r {blocked}b ({age})` (if-bqw.1, cc commit `b6b9a234` /
cc-w83ov.4), where `ua` is the closure-debt count — specs that are done but not yet archived —
and the `bd:` half counts only "standalone" beads — issues that are NOT a transitive descendant,
via a `parent-child` dependency, of any issue whose title starts with `[SPEC]` or `[CAPABILITY]`
— so the two halves are additive rather than double-counting OpenSpec-tracked work. The `bd:`
half's `open` count is the total standalone beads currently open/in_progress/blocked, alongside
the pre-existing `ready`/`blocked` counts. A `next:` line SHALL NOT be rendered on this row (a
stray `next:` in the cache is ignored, same treatment as a stray `radar:` line below).

**Every count number is independently colored by a 4-tier threshold scheme**, per its own label
(`op:` and `bd:` use different threshold sets — `bd:` runs roughly 2x `op:`'s scale at every
tier, matching real observed volume): default (DIM, healthy/low), YELLOW (moderate), a pulsating
YELLOW<->DIM tier (elevated, alternating on the same wall-clock cadence the tabs row's animated
icons already use — no new timer), and RED (high). This applies to ALL THREE numbers in each
segment (`open`/`in_progress`/`ua` for `op:`; `open`/`ready`/`blocked` for `bd:`) independently —
not only the closure-debt number, which is how the prior requirement version colored it.

- `op:` thresholds: `<=5` default, `6-10` YELLOW, `11-20` pulsating YELLOW<->DIM, `>=21` RED.
- `bd:` thresholds: `<=10` default, `11-20` YELLOW, `21-40` pulsating YELLOW<->DIM, `>=41` RED.

A `radar:` line SHALL NOT be rendered (unchanged from the prior requirement version — defense
against a stale or rolled-back cache carrying that token).

**Account identity segment**: the plugin SHALL append the active credential's identity as a
third segment, independent of the `op:`/`bd:` content — present whenever an active nexus-agent
credential resolves, regardless of whether the roadmap-pulse cache exists at all. The segment
SHALL be clickable, bound to `cc-tmux accounts-popup`, via the same `#[range=user|accounts]`
mouse-range marker mechanism.

#### Scenario: counts render every tick, colored by tier, plus the account identity
- Given: a cached roadmap-pulse file whose counts are `1o 0ip 0ua` (openspec) and `1o 1r 0b`
  (standalone beads), and an active nexus-agent credential `leo@priceless.dev` / org
  `bc7da511-...`
- When: the beads-bar row renders, at any wall-clock tick
- Then: it shows `op: 1o 0ip 0ua (<age>) | bd: 1o 1r 0b (<age>) | leo@priceless.dev·bc7da511`
  with every count in the default (DIM) tier, and the account segment clickable via the same
  mouse-range marker — never a `next:` line, at any tick

#### Scenario: a number in the pulsating tier alternates color across ticks
- Given: a cached roadmap-pulse file whose `op:` counts include `in_progress = 14` (within
  `op:`'s pulsating range, `11-20`)
- When: the beads-bar row is captured at two different wall-clock seconds one second apart
- Then: the `14` renders YELLOW at one capture and DIM at the other, alternating by wall-clock
  tick parity — every other count in that render stays at its own steady tier color

#### Scenario: a number above the red threshold renders steady RED, not pulsing
- Given: a cached roadmap-pulse file whose `bd:` counts include `open = 45` (at/above `bd:`'s red
  threshold, `41`)
- When: the beads-bar row is captured at two different wall-clock seconds one second apart
- Then: the `45` renders RED at both captures — RED never pulses, only the intermediate tier does

#### Scenario: op: and bd: use independent thresholds for the same raw count
- Given: a count of `15` appears once as `op:`'s `in_progress` (within `op:`'s `11-20` pulsating
  range) and once as `bd:`'s `ready` (within `bd:`'s `11-20` YELLOW range, not yet pulsating)
- When: the beads-bar row renders
- Then: the `op:` occurrence pulses YELLOW<->DIM while the `bd:` occurrence renders steady
  YELLOW — the same raw number resolves to a different tier depending on which label it belongs
  to

#### Scenario: a stray next: or radar: line never renders
- Given: a cached roadmap-pulse file containing a `next: …` line, a `radar:stale` line (stale
  pre-fix content), and a counts line
- When: the beads-bar row renders
- Then: neither the `next:` nor the `radar:` line ever appears on the row — only the `op:`/`bd:`
  counts and, if applicable, the account segment

#### Scenario: no roadmap-pulse cache, but an active account resolves
- Given: no roadmap-pulse cache file exists yet for the current project, and an active
  nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row shows ONLY the account identity segment (`leo@priceless.dev·bc7da511`) — not an
  empty row

#### Scenario: roadmap-pulse cache present, no active account resolves
- Given: a cached roadmap-pulse file with real counts, and nexus-agent is unreachable (no active
  credential resolves)
- When: the beads-bar row renders
- Then: the row shows only the `op:`/`bd:` content — no empty account segment, no error

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
- Then: the row is empty — no error, no placeholder text
