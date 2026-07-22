# Plan 007 — daily-brief: strip control chars from rendered titles, wire tests, cover untested modules

**Written against commit:** `d441448` — if excerpts no longer match, STOP and report drift.
**Findings:** apps #4 (ANSI/control-sequence injection via untrusted item titles in `--plain`, MED), tests #3 (over half of src untested, no `test` script, HIGH).
**Priority:** 7 of 8. Depends on plan 001 (daily-brief `bun test` in the gate) — and plan 001 step 5 already adds the `test` script; if 001 landed, skip step 2 here and just verify.

## Why this matters

`daily-brief` aggregates external sources (email subjects, GitHub titles, calendar events)
and renders them. In `--plain` mode it writes those titles raw to the terminal with no
control-char stripping, so a crafted title containing ANSI escape sequences can manipulate
the user's terminal on render (cursor moves, line clears, output spoofing). Separately, 4 of
7 source modules have no tests and the app's own `package.json` has no `test` script, so its
3 existing test files aren't even discoverable via `npm test`.

## Current state (verified excerpt)

`apps/daily-brief/src/plainRender.ts:48-54`:

```ts
    if (group.rows.length === 0) continue;
    lines.push(`  ${group.label}`);
    for (const row of group.rows) {
      lines.push(`    - ${row.title} [${row.source}] (${relativeAgo(row.lastActivityAt)})`);
    }
```

Titles also flow raw at `plainRender.ts:33` (meeting title/location) and `:67` (item title).
`row.title` originates from the mx-gateway aggregation (`src/sources/mx.ts`: `TriageCore.title`,
`CalendarEvent.title`) — external, untrusted.

Untested modules (have logic, no test in `test/`): `src/plainRender.ts`, `src/widgetOpen.ts`,
`src/ui/format.ts`, `src/sources/docsState.ts`, `src/index.tsx`. Tested: `openItems.test.ts`,
`collect.test.ts`, `mxActions.test.ts`.

## Conventions to match

- Read `apps/daily-brief/src/ui/format.ts` first — shared formatting helpers likely live
  there; the sanitizer belongs beside them (single shared function, called by both the plain
  and Ink render paths so neither can forget it).
- Tests are Bun (`bun test`), in `apps/daily-brief/test/`, following `openItems.test.ts` as
  the pattern (import the module, assert on pure output). No test framework beyond Bun's.
- The app uses Ink/React for its rich mode; the `--plain` path is separate. Sanitize at the
  data boundary (once, on the title string) so both modes are covered.

## Steps

1. **Add a `stripControlChars(s: string): string` helper** in `src/ui/format.ts` (or
   wherever the shared string helpers live). Strip C0 (`\x00-\x1F` except keep `\t`? — no,
   drop `\t` too for single-line rows) and C1 (`\x80-\x9F`) control characters, i.e.
   `s.replace(/[\x00-\x1F\x7F-\x9F]/g, "")`. Keep it a pure function.
   Verify: `stripControlChars("a\x1b[31mb")` → `"a[31mb"` (ESC removed).

2. **Apply it at the render boundary.** In `plainRender.ts`, wrap every external title/
   location before interpolation: `:33` (meeting title, location), `:51` (`row.title`),
   `:67` (item title). Also apply in the Ink render path if titles are interpolated there
   (grep `rg "\.title" apps/daily-brief/src/ | grep -v test`). Prefer sanitizing once in the
   source/collect layer (`src/sources/mx.ts` or `collect.ts`) so every renderer gets clean
   data — if that's a clean single choke point, do it there instead of per-render-site and
   note the choice. Verify: a fixture item with `title: "x\x1b[2Jy"` renders as `xy`.

3. **Add the `test` script** to `apps/daily-brief/package.json`: `"test": "bun test"`.
   (If plan 001 already added it, confirm it's present and skip.) Verify: `bun run test` runs.

4. **Add tests for the sanitizer and the untested modules.** Priority order:
   - `test/plainRender.test.ts` — covers the render output AND the control-char stripping
     (a title with ANSI escapes produces clean output). This is the finding's regression test.
   - `test/format.test.ts` — `stripControlChars` unit cases (empty, plain, ANSI, C1, tab).
   - `test/docsState.test.ts` — `src/sources/docsState.ts` has source logic; cover its main
     transform following `collect.test.ts`'s style.
   - `widgetOpen.ts` and `index.tsx` are wiring/entry — cover only if they hold pure logic;
     if they're thin glue, note that in your report and skip rather than writing brittle
     render tests.
   Verify: `bun test` → all pass, new files included.

5. **Run the gate:** `cd apps/daily-brief && bun test` → pass; `scripts/check.sh` → exit 0.

## Boundaries

- **In scope:** `apps/daily-brief/src/ui/format.ts` (or the shared-helper file), `src/plainRender.ts`, possibly `src/sources/mx.ts`/`collect.ts` (single sanitize choke point), `apps/daily-brief/package.json` (scripts), new files under `apps/daily-brief/test/`.
- **Out of scope:** the mx-gateway itself (the source of the titles — you sanitize on consume, not at the gateway), ctx-scan (sibling Bun app, untouched), wavetui, the Ink component tree beyond title interpolation.

## Done criteria (machine-checkable)

- `rg "stripControlChars" apps/daily-brief/src` → defined once, called at every external-title render site (or one choke point).
- `apps/daily-brief/package.json` has `"test": "bun test"`.
- `cd apps/daily-brief && bun test` → passes; new test files `plainRender.test.ts` + `format.test.ts` (at minimum) present and green.
- A title fixture containing `\x1b[` renders with the ESC removed (asserted in a test).
- `scripts/check.sh` → exit 0.

## Test plan

The `plainRender.test.ts` ANSI case is the required regression test for the security finding
— it must assert that an escape sequence in a title does not appear in rendered output.
`format.test.ts` unit-tests the helper directly. Both follow `openItems.test.ts`'s
import-and-assert shape.

## Maintenance note

Any new render surface (a third output mode, a new source with titles) must run titles
through `stripControlChars` — sanitizing at a single collect-layer choke point (step 2's
preferred option) makes that automatic and is the more future-proof choice. If daily-brief
becomes a wavetui source (direction item D), this sanitization moves with the data.

## Escape hatch

- If titles turn out to already be sanitized somewhere upstream (check `collect.ts`/`mx.ts` before adding): report that and add only the tests, not a redundant second strip.
- If `bun test` is red on clean `d441448`: STOP and report (plan 001 dependency).
