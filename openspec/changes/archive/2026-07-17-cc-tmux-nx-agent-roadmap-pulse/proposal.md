---
order: 0717a
---

## Change ID
cc-tmux-nx-agent-roadmap-pulse

## Summary
cc-tmux row3's `op:`/`bd:` counts currently read a local file
(`~/.claude/scripts/state/roadmap-pulse.<code>.line`) that nothing refreshes anymore — the
nexus-statusline SWR spawn that used to keep it warm was deleted in nx's strip-UI batch
(`b8361e8c`) and its CC-statusline invocation was separately removed (`2a6eda0c`, "Leo asked to
remove the CLI statusline entirely"). This proposal makes cc-tmux fetch fresh counts from a new
nx-agent HTTP endpoint instead, following the same cached-fetch client pattern `nx_agent.py`
already uses for `session_context`/`project_git_status`, falling back to the (possibly stale)
local file when nx-agent is unreachable.

## Context
- depends on (cross-repo, not a same-repo openspec slug — `wave-plan-build`'s `- depends on:`
  parser is repo-local, so this is prose-only, not a parsed line): a companion proposal in
  `~/dev/personal/nexus` must land a new `GET /projects/:code/pulse` endpoint, computed natively
  via nx-agent's existing bead-watcher (`cached-bead-source.ts`) + spec-watcher infra, before
  this spec's client code has a real endpoint to call. Author that spec separately via `/feature`
  in `~/dev/personal/nexus`. Until it lands, this spec's tasks still ship correctly — they just
  exercise the fail-open fallback path (nx-agent 404/unreachable -> `.line` file), which is
  today's status quo.
- touches: `apps/cc-tmux/src/cc_tmux/nx_agent.py`, `apps/cc-tmux/src/cc_tmux/cli.py`,
  `apps/cc-tmux/src/cc_tmux/testing.py`

## Motivation
if-bqw.7 (bead) recorded row3 rendering 16h+-stale counts since 2026-07-16 15:30-15:39 because
the only refresh path died. Leo's direction (2026-07-17, worked through in the `/open` session
that spawned this spec): do not restore the dead statusline-coupled spawn, and do not couple
refresh to cc-tmux's own render loop either — source it from the same nx-agent host cc-tmux
already queries for 5H/7D usage and session-context (port 7400), computed natively in nx rather
than nx shelling out cross-repo to cc's `roadmap-pulse` script.

## Requirements
- cc-tmux SHALL add a `roadmap_pulse(code, ttl=CACHE_TTL_SECS, cache_path=None, now=None)`
  client function to `nx_agent.py`, matching the existing cached-fetch pattern (`_fetch_cached`,
  on-disk TTL cache, negative caching, fail-open `None` on any error — same invariant as
  `session_context`/`project_git_status`).
- `_read_roadmap_pulse` in `cli.py` SHALL try `nx_agent.roadmap_pulse(code)` first. On a
  non-`None` result, build row3's `(content, age_sec)` return tuple from that JSON. On `None`
  (nx-agent unreachable, negative-cached, or malformed), fall back to the existing `.line` file
  read — byte-identical to today's behavior.
- No changes to the nx-agent HTTP contract are made BY THIS spec — it assumes the companion
  nx-side `/projects/:code/pulse` endpoint exists and returns a JSON shape compatible with
  `_parse_roadmap_pulse_counts`'s existing `op:`/`bd:`/`next:` semantics. The exact response
  shape is finalized in the nx-side spec; this spec's `cli.py` integration accepts the documented
  shape as the working contract, subject to a follow-up adjustment if the nx-side spec's actual
  shape differs once authored.

## Scope
In: cc-tmux client-side fetch + fallback wiring only.
Out: the nx-agent endpoint implementation itself (separate spec, separate repo); retiring or
deprecating cc's `~/dev/cc/scripts/bin/roadmap-pulse` script or the `.line` file (stays as the
fallback data source, unchanged); restoring the old nexus-statusline SWR spawn (explicitly
rejected by Leo in favor of this design).

## Testing
- Unit: `testing.py` self-tests for `nx_agent.roadmap_pulse` cache hit/miss/negative-cache
  (mirrors `_test_nx_agent_session_context_cache` at `testing.py:3426`), and for
  `cli._read_roadmap_pulse`'s nx-agent-success path plus its nx-agent-down-fallback-to-file path
  (monkeypatch `nx_agent.roadmap_pulse` following the existing `session_context` save/restore
  pattern at `testing.py:1610`).
- E2E: N/A — cc-tmux is a terminal status-bar plugin with no browser/HTTP user flow. Verified via
  this repo's existing self-test harness (`python -m cc_tmux` test entrypoint) plus manual tmux
  render inspection, per this repo's established convention (no Playwright surface here).

## Impact
Row 3's `op:`/`bd:` counts become fresh again once the nx-side endpoint ships and this lands;
until then, zero regression — `cli.py` falls back to the current (dead-refresh, stale-since-
2026-07-16) file-read behavior, identical to today.

## Risks
- The nx-side endpoint's response shape is not yet locked (separate spec, not yet authored) —
  this spec's parsing logic may need a follow-up adjustment once that shape is finalized. This is
  a normal iteration on a documented placeholder contract, not an unshipped feature or a deferral.
- Fail-open-to-file semantics mean a permanently-down nx-agent silently regresses to today's
  stale-file behavior with no error surfaced. Accepted per Leo's explicit "fall back to file"
  decision; worth a follow-up bead if silent staleness recurs after this ships.
