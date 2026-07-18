---
model: opus
name: cmux:evolve
description: cmux upstream currency audit -- diffs manaflow-ai/cmux releases against installfest's cmux pipeline (bridge relay/notify, workspace launcher polling, nx/mx integration seams) and recommends which bespoke shims a native cmux feature could replace.
argument-hint: [--re-research <id>] [--json] [--no-record]
allowed-tools: Bash, Read, WebFetch, Agent
effort: high
---

# cmux:evolve -- cmux Upstream Currency Audit

Thin orchestrator, modeled on cc's `/workflow:evolve`. cmux (manaflow-ai/cmux, the
Ghostty-based multiplexer this repo drives via `scripts/cmux-workspaces.sh` /
`scripts/mux-remote.sh` / `scripts/cmux-bridge.py` / the Rust `ssh-mesh/scripts/remote/cmux-bridge/`
HTTP relay) is young and ships fast (releases every 2-7 days plus nightlies, per
`docs/audit/cmux.md`). This command pulls its release feed, identifies releases we
haven't evaluated yet, fans out research on whether each one lets us delete or shrink a
piece of our homegrown pipeline, and gates on Leo's decision before anything changes.

**No auto-implement.** This command only researches and records verdicts. Turning an
`implement` verdict into an actual code change is separate follow-through (a bead, or a
`/feature` + apply pass for anything multi-file).

## Argument Parsing

```bash
RE_RESEARCH=""
JSON_OUTPUT=false
NO_RECORD=false

for arg in $ARGUMENTS; do
  case "$arg" in
    --re-research=*) RE_RESEARCH="${arg#--re-research=}" ;;
    --json) JSON_OUTPUT=true ;;
    --no-record) NO_RECORD=true ;;
    *) echo "Unknown argument: $arg" >&2 ;;
  esac
done
```

## Phase 1 -- Refresh the cmux release catalog

```!
~/dev/personal/installfest/scripts/cmux-evolve-refresh.sh --json
```

The script polls `https://github.com/manaflow-ai/cmux/releases.atom` (a stable, pollable
feed -- no auth, no rate-limit risk), diffs entries against
`.claude/cmux-evolve/state/last-checked.json`, and emits `{"changed": bool,
"new_releases": [{"tag", "title", "published", "notes_excerpt"}], "latest_tag": "..."}`.
Exit 0 always (per the scripts-as-data-producers convention this repo already follows in
`scripts/check.sh`/`scripts/audit-projects.sh`: JSON `error` key on failure, never a
nonzero exit that would abort this command's render).

If `changed` is `false` and `--re-research` was not passed, skip to Phase 7 with a
"no new releases" report.

## Phase 2 -- Pipeline snapshot (ground truth)

Read `docs/audit/cmux.md` (Cross-Domain Interactions table, section 3) as the current
pipeline model rather than re-deriving it. As of this writing that's:

| Surface | File(s) | What it does today |
|---|---|---|
| Socket client | `scripts/cmux-bridge.py` | Speaks cmux's own Unix-socket protocol (`browser.open_split`/`set_status`/`notification.create`); called by `scripts/mac-open.sh` |
| HTTP relay | `ssh-mesh/scripts/remote/cmux-bridge/` (Rust, port 10998) | Proxies remote Claude Code hook callbacks (`/cmux/hook`, `/cmux/attention`, `/cmux/notify`) back to the Mac's cmux; deployed via LaunchAgent (`if-j8g`) |
| Workspace launcher | `scripts/cmux-workspaces.sh` | Sleep-based polling (`wait_for_cmux`, `wait_for_surface`, 7 fixed `sleep` calls) to sequence workspace/pane creation over the cmux CLI |
| Remote trigger | `scripts/mux-remote.sh` | AppleScript picker invoking the launcher from Shortcuts/NFC |
| Persistent session | `ws-claude` wrapper (zellij) | Keeps the Claude Code pane alive across SSH disconnect -- a workaround, not a cmux-native feature |

Before Phase 4, re-check whether any of these files changed since `docs/audit/cmux.md`
was written (`git log --oneline -- scripts/cmux-workspaces.sh scripts/cmux-bridge.py
scripts/mux-remote.sh ssh-mesh/scripts/remote/cmux-bridge/ | head -5`) -- if so, treat the
table above as a starting point, not gospel, and read the current file.

## Phase 3 -- Identify undecided releases

```bash
DECISIONS=.claude/cmux-evolve/state/decisions.json
[ -f "$DECISIONS" ] || echo '{"decisions":{}}' > "$DECISIONS"

# UNDECIDED = new_releases from Phase 1 whose tag has no entry in decisions.json,
# OR the single tag named by --re-research.
```

Print the undecided list. If empty (every new release was already decided, e.g. via
`--re-research`), skip to Phase 7.

## Phase 4 -- Parallel research fan-out

For each undecided release, spawn one agent in parallel (cap at 4 concurrent -- this
feed moves fast but rarely ships more than a handful of releases between runs). No
`cc-feature-analyst` equivalent exists in this repo, so use `general-purpose`:

```
Agent({
  subagent_type: "general-purpose",
  description: "Research cmux <tag>",
  prompt: "cmux (manaflow-ai/cmux) shipped release <tag> (<published>): <notes_excerpt>.
Fetch the full release notes at https://github.com/manaflow-ai/cmux/releases/tag/<tag> if
the excerpt is insufficient. installfest's current cmux pipeline (ground truth):
<Phase 2 table, inlined>. Judge: does this release ship a feature that could replace or
shrink one of these homegrown pieces (readiness/sync signals replacing the sleep-based
polling in cmux-workspaces.sh, a notify/hooks API replacing cmux-bridge.py or the Rust
relay, session persistence replacing the ws-claude zellij wrapper)? Return JSON:
{tag, relevant_surface: one of [bridge-client, http-relay, workspace-launcher,
remote-trigger, persistent-session, none], recommendation: implement|defer|skip,
rationale, confidence: high|medium|low}."
})
```

A release with nothing relevant to our pipeline returns `relevant_surface: none,
recommendation: skip` -- that is an expected, common outcome, not a failed research pass.

## Phase 5 -- Decision gate (USER-GATED)

Group research records by `relevant_surface`. Present each non-empty group:

```
## <Surface> (currently: <file(s)>)

### cmux <tag> (<published>)
- What shipped: <notes_excerpt>
- Could replace: <rationale>
- Recommendation: <recommendation> (confidence: <confidence>)

──────────────────────────────────────
Decision? [a] Accept all here  [d] Defer all  [s] Skip all  [c] Custom per item
```

**No auto-apply.** Pause here for Leo's verdict, one per surface group (or `c` for
per-release). An accepted `implement` verdict becomes a bead in Phase 8 -- it does NOT
trigger any code change in this command.

## Phase 6 -- Persist decisions

Simple jq-based append (installfest has no PG/cc-decisions infra -- unlike cc's evolve,
this file IS the system of record, not a mirror):

```bash
TODAY=$(date -I)
jq --arg tag "$TAG" --arg date "$TODAY" --arg verdict "$VERDICT" \
   --arg rationale "$RATIONALE" --arg surface "$SURFACE" \
   '.decisions[$tag] = (.decisions[$tag] // {first_seen: $date, surface: $surface}) +
    {last_verdict: $verdict, last_rationale: $rationale, decided: $date}' \
   "$DECISIONS" > "${DECISIONS}.tmp" && mv "${DECISIONS}.tmp" "$DECISIONS"
```

Then update `last-checked.json`'s cursor to the newest release tag seen this run, so the
next invocation's Phase 1 diff starts from here.

## Phase 7 -- Report

```markdown
# cmux Evolve Report

Generated: {timestamp}
Latest cmux release: {latest_tag}
Releases reviewed this run: {count}

## Verdicts

| Release | Surface | Recommendation | Leo's verdict | Ref |
|---|---|---|---|---|

## Sources

- Releases feed: https://github.com/manaflow-ai/cmux/releases.atom
- Pipeline ground truth: docs/audit/cmux.md
```

No fabricated savings/impact numbers -- report only counted artifacts (releases
reviewed, verdicts recorded, beads filed), same honesty boundary as cc's evolve.

## Phase 8 -- Bead creation for accepted `implement` verdicts

installfest has `.beads/` -- for each `implement` verdict, mint through `bd create`
directly (no `beads-helpers.sh` shim exists in this repo yet), following this repo's
existing hygiene: imperative title <=72 chars, parent under `if-vit` (workspace-resilience,
the capability epic that already owns the cmux pipeline's other follow-ups --
`if-vit.1`/`if-vit.2`/`if-vit.3`), `type:config` label, priority 2 for anything that
removes a live shim (bridge relay, sleep-polling), priority 3 for smaller ergonomics
wins. Stamp the resulting bead ID back into `decisions.json`'s `ref` field for that tag.

```bash
bd create "<imperative title>" -t task -p <2|3> --parent if-vit \
  --labels "type:config,owner:leonardoacosta" \
  --description "cmux <tag> ships <feature> -- replaces/shrinks <surface>. <rationale>" \
  --json | jq -r '.id // .[0].id // empty'
```

## Phase 9 -- Notification

Match this repo's existing graceful-degrade idiom (see `scripts/mesh-heartbeat.sh`,
`scripts/az-reauth.sh`):

```bash
NX_SEND="$HOME/.claude/scripts/lib/nx-send.sh"
if [ -f "$NX_SEND" ]; then
  . "$NX_SEND"
  nx_notify "cmux evolve: <count> release(s) reviewed, <n> beads filed." 2>/dev/null || true
fi
```

## Resume Strategy

Idempotent on `decisions.json`: re-running with no new releases (Phase 1 `changed:
false`) and no `--re-research` is a no-op beyond reporting the last state. A release
already decided is never re-researched except via `--re-research=<tag>`.

## JSON Output Format (`--json`)

```json
{
  "latest_tag": "v0.64.19",
  "reviewed": [
    {"tag": "v0.64.18", "surface": "workspace-launcher", "recommendation": "implement",
     "verdict": "implement", "ref": "bd:if-vit.4"}
  ]
}
```
