# Design: wavetui-flair

## Architecture

```
  wavetui-core's Store  ───────────────────►  Snapshot (immutable, unchanged)
                                                    │
                                                    ▼
                                          root model receives SnapshotMsg
                                                    │
                              ┌─────────────────────┴─────────────────────┐
                              ▼                                           ▼
                   root model keeps prevSnapshot            FlairManager.Diff(prev, next)
                   and renders QueuePane/DetailPane/                      │
                   sibling panes from `next` — UNCHANGED         []FlairEvent (pure value)
                   whether flair is enabled or not                        │
                                                                           ▼
                                                              FlairManager starts/stops
                                                              per-event animations (spring/
                                                              particle/fade state, keyed by
                                                              item ID) — owns its OWN tick
                                                              loop via tea.Tick, idle when
                                                              no animation is live
                                                                           │
                              ┌────────────────────────────────────────────┤
                              ▼                                           ▼
                  row-scoped highlight map                    lipgloss v2 Layer/Canvas
                  (map[itemID]HighlightState)                 overlay (toast, celebration)
                  passed into QueuePane.SetHighlights          composited over root View()
                  — nil/empty when disabled, QueuePane          output at the ROOT level only
                  renders identically either way                — absent when disabled
```

`FlairManager` never touches the Store, never touches the bus, and never mutates a `Snapshot`.
It is a pure consumer of two consecutive `Snapshot` values, producing view-only animation state
that the root model optionally overlays on top of rendering that already happened correctly
without it. This is the direct implementation of the hard constraint: if `FlairManager` panicked,
was `nil`, or was compiled out, `QueuePane`/`DetailPane`/every existing pane renders identically —
minus the overlay layer, which is additive, not load-bearing.

## Snapshot diffing (the only place flair reads Store data)

```go
package flair

type EventKind string

const (
    EventItemClosed      EventKind = "item_closed"       // present in prev, absent in next
    EventItemAppeared    EventKind = "item_appeared"      // absent in prev, present in next
    EventBlockerResolved EventKind = "blocker_resolved"   // Item.Blocker: non-nil -> nil
    EventNegative        EventKind = "negative"           // Item.Stale: false -> true (zombie-adjacent)
)

type FlairEvent struct {
    Kind   EventKind
    ItemID string
}

// Diff is a pure function: same inputs always produce the same output, no side effects,
// no I/O. This is what the disabled-equals-identical test in tasks.md [4.6] exercises —
// calling Diff (or not) never changes what QueuePane renders from `next` itself.
func Diff(prev, next core.Snapshot) []FlairEvent
```

`EventItemAppeared`/`EventItemClosed` are derived by ID-set comparison
(`next.Items` IDs minus `prev.Items` IDs, and vice versa) — no assumption about *why* an item
disappeared (closed vs. archived vs. a transient source error) beyond what `Item.Kind` already
distinguishes (`KindBead` vs `KindProposal`), which is enough to pick "row flash green" vs "toast
banner springs in" per the event->effect map below. `EventBlockerResolved` and `EventNegative`
are per-item field-transition comparisons on items present in both snapshots. **A "proposal
archived" event is a `KindProposal` item disappearing** — `wavetui-flair` does not need a
dedicated archive-detection mechanism; it reuses the same appear/disappear diff, just keyed by
`Item.Kind` to pick the right effect.

`wavetui-core`'s `Snapshot.Errors` field (`[]SourceError`) is read but never triggers an
animation on its own — a source-level error is `wavetui-core`'s own badge concern, not a flair
event. This keeps the negative-effect reservation honest: `EventNegative` is scoped specifically
to `Item.Stale` transitioning true (a claim going zombie-adjacent), the one case the pre-loaded
design brief calls out as the exclusive negative-attention trigger, not a general "anything looks
wrong" catch-all.

## Tick-loop lifecycle (the zero-idle-cost invariant)

```go
type FlairManager struct {
    active map[string]animState // keyed by item ID; empty map == no tick needed
    cfg    config.FlairConfig
}

// Tick is only scheduled via tea.Tick(frameInterval, ...) from the root model's Update,
// and ONLY when len(active) > 0 after processing the current message. The root model's
// Update never issues an unconditional tea.Tick on every pass — that would be the banned
// permanent 30fps loop.
func (m *FlairManager) NeedsTick() bool { return len(m.active) > 0 }
```

Every animation (spring, fade, particle) carries its own expiry (harmonica settling threshold or
a fixed duration). The frame handler removes an entry from `active` once it settles; the root
model checks `NeedsTick()` after every `Update()` call and only re-issues `tea.Tick` when it is
still `true`. This is the concrete mechanism behind "idle at zero CPU cost when nothing is
animating" — no polling, no fixed-rate loop, a tick is scheduled if and only if there is
something left to animate.

## Config + calm-mode + truecolor gating

`config.FlairConfig{ Enabled bool; CalmMode bool }` (additive fields on `wavetui-core`'s existing
`internal/config` package, alongside the `SessionLink` proximity-window field `wavetui-sessions`
already adds there). Gating order, checked once per incoming `Snapshot` before any `Diff()` call:

1. `!cfg.Enabled` → `Diff()` is never called, `active` map stays empty, overlay compositor is
   never invoked. This is the literal disabled-equals-identical path — flair code does not run
   at all, not merely "runs but suppresses output."
2. `cfg.CalmMode` (implies `Enabled == true`) → `Diff()` still runs (state signals like "blocked
   on you" still need to update), but every effect resolves to its static-glyph fallback instead
   of an animated one — e.g. the presence sprite renders its current state's single glyph with no
   frame cycling, a row that would flash instead gets a one-shot static color swap with no fade.
3. Truecolor detection (`lipgloss.HasDarkBackground`-adjacent capability probe via
   `github.com/charmbracelet/lipgloss/v2`'s color-profile detection, `termenv`-backed) — on a
   non-truecolor terminal, every `go-colorful` lerp step is skipped in favor of the nearest
   16-color ANSI equivalent (`go-colorful` provides `.Clamped()`/distance-based nearest-color
   helpers for exactly this), so a "flash green -> fade" sequence becomes a plain 2-3 step ANSI
   color change instead of a smooth gradient, rather than rendering broken truecolor escape codes
   the terminal cannot interpret.

## Verified dependency versions (Go module proxy, queried live during authoring)

| Module | Latest tag | Status |
|---|---|---|
| `github.com/charmbracelet/bubbletea` (v1 import path) | `v1.3.10` | Stable |
| `github.com/charmbracelet/bubbletea/v2` | `v2.0.8` | **Stable** (preceded by alpha/beta/rc tags, all superseded) |
| `github.com/charmbracelet/lipgloss` (v1 import path) | `v1.1.0` | Stable |
| `github.com/charmbracelet/lipgloss/v2` | `v2.0.5` | **Stable** (preceded by alpha/beta tags, all superseded) |
| `github.com/charmbracelet/harmonica` | `v0.2.0` | Stable (small, focused spring-physics library) |
| `github.com/charmbracelet/bubbles` (v1 import path, `progress`/`spinner`) | `v1.0.0` | Stable |
| `github.com/lucasb-eyer/go-colorful` | `v1.4.0` | Stable |
| `github.com/NimbleMarkets/ntcharts` | `v0.5.1` | Stable, but **not adopted** — see below |

Commands run: `curl https://proxy.golang.org/<module>/@v/list` and `/@latest` per module (module
paths with uppercase segments require Go's proxy escaping, e.g. `!nimble!markets`). This is the
authoritative release-tag source — no reliance on a changelog, blog post, or training-data
recollection of "v2 is still beta," which the exploration session's premise turned out to be
stale on.

## Alternatives

**Wholesale bubbletea v1 -> v2 module upgrade, rejected for this proposal.** v2's "Cursed
Renderer" (damage-tracked, per-frame-optimized) is real and now stable, but `wavetui-core`'s root
model already implements `tea.Model` against v1, and three sibling proposals
(`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`) are authored assuming that
root model's shape. `FlairManager`'s actual workload — occasional reactive effects gated by real
state changes, explicitly NOT a sustained 30fps loop — does not need the Cursed Renderer's
damage-tracking benefit; v1's renderer redraws the full frame on each `tea.Tick`, which is fine at
the low, bursty tick rate this design produces. Re-litigating three landed/in-flight sibling
proposals' root-model tech choice for a performance benefit this proposal's own workload does not
require would be scope creep past what was asked. If a future proposal's workload genuinely needs
sustained high-frequency rendering, that is its own decision to make, informed by this
proposal's finding that v2 is stable whenever that day comes.

**Lipgloss v2 adopted, scoped to this proposal's own package only.** Unlike bubbletea, lipgloss
v1 has no layered/z-indexed compositing primitive at all — v1's `JoinHorizontal`/`JoinVertical`
can arrange strings side by side but cannot overlay a toast banner on top of existing content
without manual string-splicing (the exact anti-pattern the originating design brief calls out to
avoid). `lipgloss/v2`'s `Layer`/`Canvas` types are a genuinely new capability, not a performance
upgrade, so adopting v2 here is solving an actual capability gap rather than chasing a newer
major version. Because Go treats `github.com/charmbracelet/lipgloss` and
`.../lipgloss/v2` as distinct module paths, this proposal's `overlay.go` can import v2 while every
existing/sibling pane (`queuepane.go`, `detailpane.go`, `sessionspane.go`, etc.) keeps importing
v1 untouched — zero migration forced onto any other proposal's code.

**Fallback path if lipgloss v2 had still been beta** (documented per the originating instruction,
even though the live check above shows it is not needed): full-screen overlays would degrade to
a much simpler mechanism — a dedicated blank line reserved at the top of the root view for a
plain-text toast banner (no spring-in, no true overlay), and the full-screen celebration would
become a temporary full-screen replace-the-view state instead of a composited overlay. This
fallback trades away the "springs in from the top edge" motion detail but keeps every other
event->effect mapping intact.

**ntcharts considered, not adopted.** Confirmed real and available (`v0.5.1`), and would be a
natural fit for a KPI-history sparkline — but `wavetui-sessions`' `KPIBar` (verified by reading its
proposal.md/design.md during authoring) is text-only (continue-count, rate-limit-incident counter,
stale-claim minutes) with no historical-trend rendering claimed. Adding ntcharts now with no
current consumer would be exactly the premature-dependency pattern the Reader Gate's reuse-before-
reinvention ordering warns against (new dependency is the last resort, not a "might be useful"
reach). If a future proposal's victory-recap wants a trend sparkline, it can add this dependency
then, informed by this note that it is available and already vetted.

## Event -> effect map (implementation reference for tasks.md)

| Trigger (from `Diff()`) | Effect | Primitive |
|---|---|---|
| `EventItemClosed` (KindBead) | Row flash green -> decayed fade, small particle burst, tally ticks up | `go-colorful` lerp + `harmonica` decay spring |
| `EventItemAppeared` (KindProposal, i.e. "archived" in reverse — item leaving is the archive case above; a genuinely NEW proposal item appearing) | Toast banner springs in from top edge, auto-dismisses | `lipgloss/v2` `Layer`/`Canvas` + `harmonica` spring (Y-offset) |
| `EventItemAppeared` (KindBead) | Row slides in (spring x-offset), subtle | `harmonica` spring (X-offset) |
| `EventBlockerResolved` | Glyph morph, color pulse | `go-colorful` lerp, no particle |
| `EventNegative` | Horizontal shake + red pulse — reserved exclusively for this event kind, never reused for any other trigger | `harmonica` spring (X-offset, high damping) + `go-colorful` red pulse |
| Wave complete (future — see Scope OUT) | Full-screen celebration overlay + recap card | Deferred: no `wavetui-dispatch` progress/complete event exists yet (verified by reading its design.md during authoring — no such event is emitted) |
| Zoom in/out (future — see Scope OUT) | Harmonica-interpolated pane geometry | Deferred: `wavetui-core` has no scope/zoom model yet (verified by reading its design.md during authoring) |

## Victory-recap data sourcing (anti-drift invariant)

When a wave-complete trigger eventually exists (a future proposal's concern), its recap numbers
(items closed, duration) MUST be computed by the SAME `Diff()` mechanism this proposal ships —
counting `EventItemClosed` events accumulated since the wave started — never a second query against
`bd`/`openspec` directly. This is the concrete implementation of the "same Store rollups, never a
separate accounting path" hard constraint: there is exactly one code path (snapshot diffing) that
can ever produce a "how many items closed" number in this package.

## Presence sprites (Clawd) — conditional, additive

`sprite.go`'s task is written to check, at implementation time, whether `wavetui-core`'s `Item`
struct (as extended by `wavetui-sessions`, IF that proposal has landed by then) exposes a session
state accessor. If present, render a small 2-4 frame cycle sprite (working/thinking/
blocked-on-you/zombie/done/swarm) mapped directly to that one state field — never a second state
machine. If absent (sessions hasn't landed yet when this task executes), the task is skipped and
a follow-up bead is filed rather than blocking this proposal's other tasks or inventing a
placeholder state source. Art direction note for whoever implements this: Clawd is Anthropic's
mascot — render an original homage (a generic terminal crab-friend silhouette, not the actual
mascot's likeness), since this is a personal tool, not an Anthropic-branded surface.
