---
order: 0722m
---

# Proposal: harden-daily-brief-titles-and-tests — strip control chars from rendered titles, wire up tests

## Change ID
`harden-daily-brief-titles-and-tests`

## Why
`daily-brief` aggregates external sources (email subjects via mx-gateway, GitHub titles,
calendar events) and renders them. In `--plain` mode it writes those titles raw to the
terminal with no control-char stripping — `plainRender.ts:33` (meeting title/location),
`:51` (radar `row.title`), and `:67` (open-items `item.title`) all interpolate untrusted
strings directly. A crafted title containing an ANSI escape sequence (e.g. from a hostile
email subject or GitHub issue title reaching mx-gateway) can manipulate the user's terminal
on render — cursor moves, line clears, output spoofing. `row.title` originates from
`src/sources/mx.ts` (`TriageCore.title`, `CalendarEvent.title`) — external and untrusted by
construction.

Separately, 4 of 7 source modules have zero test coverage (`src/plainRender.ts`,
`src/widgetOpen.ts`, `src/ui/format.ts`, `src/sources/docsState.ts`, `src/index.tsx`) and
`apps/daily-brief/package.json` has no `test` script, so even the 3 existing test files
(`openItems.test.ts`, `collect.test.ts`, `mxActions.test.ts`) are only runnable via a direct
`bun test` invocation, not `npm test`/`bun run test`.

## What Changes
- Add a `stripControlChars(s: string): string` helper — strips C0 (`\x00-\x1F`) and C1
  (`\x7F-\x9F`) control characters, including tab — as a pure function, placed beside the
  other shared formatting helpers in `src/ui/format.ts`.
- Apply it at a single choke point rather than three separate call sites: `src/ui/format.ts`
  is already the shared, framework-free layer both `plainRender.ts` and the ink render path
  (`ui/sections.tsx`) consume for every title (`formatMeetingsSummary`'s event lines,
  `toRadarRows`/`formatRadarGroups`, `formatOpenItemsRepos`) — sanitizing titles/locations
  inside those row-building functions covers both render paths automatically and can never be
  forgotten at a new render site. (If a source/collect-layer choke point in `sources/mx.ts` or
  `collect.ts` turns out cleaner during implementation, that is an acceptable alternative per
  the source plan — the requirement is "sanitized once, before either renderer sees it," not a
  specific file.)
- Add/confirm the `apps/daily-brief/package.json` `"test": "bun test"` script — the sibling
  proposal `add-repo-app-test-gate` may already add this; if so, this proposal treats it as a
  confirm-only no-op rather than re-adding it.
- Add regression tests: `test/format.test.ts` (unit cases for `stripControlChars`) and
  `test/plainRender.test.ts` (a title fixture containing an ANSI escape renders clean in
  `--plain` output) — the required regression coverage for the security finding.

## Context
- depends on: `add-repo-app-test-gate`
- touches: `apps/daily-brief/src/ui/format.ts`, `apps/daily-brief/src/plainRender.ts`, `apps/daily-brief/package.json`

## Testing
| Affected seam | Task |
|----------------|------|
| `stripControlChars` unit cases (empty, plain, ANSI, C1, tab) | `[2.1]` `test/format.test.ts` |
| ANSI-escape title renders clean in `--plain` mode (security regression) | `[2.1]` `test/plainRender.test.ts` |
| `src/sources/docsState.ts` main transform | `[2.2]` `test/docsState.test.ts` |
| `apps/daily-brief/package.json` test script present | `[1.3]`, verified in `[2.3]` via `bun run test` |
| Full suite still green | `[2.3]` `bun test` + `scripts/check.sh` |

## Done Means
- A title containing an ANSI escape sequence (`\x1b[`) renders with the escape stripped in
  `--plain` mode.
- `bun test` in `apps/daily-brief` passes, including new `plainRender.test.ts` and
  `format.test.ts`.
- `bun run test` is a valid command in `apps/daily-brief` (via the `test` script).
- `stripControlChars` is defined once and reached by every external-title render path (or a
  single documented choke point upstream of both renderers) — no render site left interpolating
  a raw untrusted title.
