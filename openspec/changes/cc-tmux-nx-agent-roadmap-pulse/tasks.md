---
stack: cc-meta
---
<!-- beads:feature:if-fz7h -->

# Tasks: cc-tmux-nx-agent-roadmap-pulse

<!-- beads:epic:if-bqw -->

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no UI batch
> (no `tmux.conf`/theme-file changes; everything lives in `nx_agent.py`/`cli.py`). `stack:
> cc-meta` used by functional analogy (installfest has no `project.toml [stack]` and is a
> personal tooling/config repo, same shape as `~/dev/cc` itself though not literally it — see
> `cc-45l4w`, filed against cc's own `/feature` to add a real stack value for this case).
> Owner: general-purpose engineer agents (no dedicated api/ui roles for this Python tmux plugin),
> same convention prior cc-tmux specs in this repo used.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/nx_agent.py`: add `roadmap_pulse(code, ttl=CACHE_TTL_SECS, [beads:if-ap2j]
  cache_path=None, now=None)` following the exact `project_git_status`/`session_context` pattern
  — `GET {_BASE_URL}/projects/{code}/pulse`, cached via `_fetch_cached`, fail-open `None` on any
  failure (unreachable host, non-2xx, malformed body). Docstring notes this is the client half of
  the companion nx-side `/projects/:code/pulse` endpoint (separate repo, separate spec — see
  proposal.md `## Context`).

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/cli.py`: `_read_roadmap_pulse` tries [beads:if-1r2b]
  `nx_agent.roadmap_pulse(code)` first. On a non-`None` dict result, build the `(content,
  age_sec)` return tuple from it (age derived from the response's own freshness signal, not file
  mtime). On `None`, fall back to the existing `.line` file read verbatim — today's code path,
  unchanged, same return shape.
  - depends on: 1.1

## E2E Batch

- [x] [3.1] `apps/cc-tmux/src/cc_tmux/testing.py`: add self-tests — `nx_agent.roadmap_pulse` [beads:if-y2jg]
  cache hit/miss/negative-cache (mirror `_test_nx_agent_session_context_cache` at
  `testing.py:3426`), `_read_roadmap_pulse` nx-agent-success path (monkeypatch
  `nx_agent.roadmap_pulse` per the save/restore pattern at `testing.py:1610`), and
  `_read_roadmap_pulse` nx-agent-down falls back to the `.line` file unchanged.
  - depends on: 2.1
