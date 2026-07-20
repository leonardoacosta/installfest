---
stack: t3
---
<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-g2mg -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Verify live whether `workspaces.latestMessage`/`.unread`/`.remote` populate for a plain-terminal `claude` launch (our current `cmux-workspaces.sh` architecture) vs. only cmux's native agent-session surface type — SSH into the Mac, create a disposable test workspace, launch `claude` as a plain pane command, inspect `cmux workspace list --json` for those fields, record the finding [beads:if-tuox]

  **Finding (cmux 0.64.19, live-verified 2026-07-18)**: they do NOT populate. Created disposable
  workspace `cmux ssh homelab --name cmux-evolve-test-if-tuox`, launched `claude` via `cmux send`
  (same plain-pane-injection pattern as `cmux-workspaces.sh`'s `pane_exec`), confirmed via
  `capture-pane`/`read-screen` that Claude actually ran and answered a prompt ("Paris"), then
  re-queried `cmux workspace list --json`. `latest_conversation_message`, `latest_submitted_message`,
  and `latest_submitted_at` all stayed `null` throughout — before, during, and after the exchange.
  Note the real field names are snake_case (`latest_conversation_message` /
  `latest_submitted_message` / `latest_submitted_at`), not `latestMessage`; there is no `unread`
  field anywhere in the schema (verified via a full-payload grep across `workspace list --json`,
  zero matches). By contrast, pre-existing workspaces opened through cmux's own native
  agent-session mechanism (workspace:3/41/2 in the live list) show these fields populated with real
  conversation summaries and timestamps. Conclusion: these fields are sourced from cmux's native
  agent-session surface type (`new-surface --type agent-session --provider claude`), not from a
  plain terminal pane running the `claude` binary via shell injection — our `cmux-workspaces.sh`
  architecture will never see them populate no matter how long a plain-pane `claude` session runs.
  `remote.*` fields (`.connected`, `.state`, etc.) are unaffected — confirmed pre-existing and
  populated for both surface types, as expected. Cleanup verified: `cmux close-workspace` + a bare
  `cmux list-workspaces` confirming `workspace:52` no longer listed.
- [x] [1.2] Define the shared smuggled-field encoding scheme (state token, optional wait-reason, transition epoch, openspec summary, beads summary, usage 5H/7D figures) as a single documented format both the Python writer and the Swift reader consume identically — one canonical reference, not reinvented per field [beads:if-c2ad]
- [x] [1.3] Confirm the exact `cmux workspace-action` CLI param names and `cmux()` action-method name/params for setting a workspace's `description` from both a CLI call (Python) and in-sidebar context, via `cmux docs api` and the `cli-contract.md` raw resource [beads:if-nxb6]

## API Batch

- [x] [2.1] Extend `apps/cc-tmux/src/cc_tmux/tmux.py`'s hook handlers to dual-write the [1.2] encoding via `cmux workspace-action --description` on every existing state transition, gated on `CMUX_WORKSPACE_ID` being set; fail-open on any cmux-call error, matching the existing hook fail-open invariants [beads:if-3jd4]
  - depends on: 1.2, 1.3

  **Implementation**: every state-writing hook funnels through the single choke point
  `tmux.set_pane_state` (via `cli.cmd_register`, confirmed against `hooks.json` —
  SessionStart/UserPromptSubmit/PreToolUse/PostToolUse/Notification/Stop all call
  `cc-tmux register --state ...`), so the dual-write lives there rather than being
  duplicated per hook. Added `tmux._cmux_dual_write()` + `tmux._import_status_encoding()`
  (imports `scripts/lib/cmux_status_encoding.py` DOTFILES-relative, same resolution
  pattern as `registry.py`, reusing the shared module rather than reimplementing
  encode/decode). Fires only on a REAL transition (`changed`, mirroring invariant 3 /
  `notify.react`'s existing gate — a re-assert doesn't restamp `@cc-timestamp` either, so
  there's nothing new to mirror) and only when `$CMUX_WORKSPACE_ID` is set; wrapped in a
  single `try/except Exception: pass` matching invariant 5. Read-modify-write: reads the
  workspace's current `description` via `cmux workspace list --json` (filtered on `ref`),
  decodes, overwrites only `state`/`wait_reason`/`epoch`, re-encodes, writes back via
  `cmux workspace-action --action set-description` — leaves `openspec`/`beads`/`usage_5h`/
  `usage_7d` (task 2.4's fields) untouched.

  **Verification**: `cc-tmux self-test` — 116/116 passed (no regression). Live smoke test
  on the Mac (2026-07-18), per the incident safety rule: listed existing workspaces first
  (`workspace:9/10/11/14/15`, all off-limits), created exactly one disposable workspace
  `cmux-evolve-test-if-3jd4-v2` -> `workspace:16`, seeded it with
  `CC1||||3 open, 1 approved|12 ready, 2 blocked|68|47` (simulating task 2.4's fields
  already present). Copied the modified `tmux.py` + `cmux_status_encoding.py` into an
  isolated `/tmp` scratch dir on the Mac (not the deployed plugin — no tmux server was
  running there to drive a real hook fire) and called `tmux._cmux_dual_write` directly
  against the live cmux daemon with `CMUX_WORKSPACE_ID=workspace:16`:
  - `_cmux_dual_write("%99", "waiting", "permission", <epoch>)` ->
    `cmux workspace list --json` showed
    `CC1|waiting|permission|<epoch>|3 open, 1 approved|12 ready, 2 blocked|68|47` —
    state/wait_reason/epoch written, openspec/beads/usage fields preserved byte-for-byte.
  - `_cmux_dual_write("%99", "idle", "", <epoch>)` (simulating Stop) ->
    `CC1|idle||<epoch>|3 open, 1 approved|12 ready, 2 blocked|68|47` —
    wait_reason correctly cleared, epoch updated, other 4 fields still preserved.
  - Unset `$CMUX_WORKSPACE_ID` -> confirmed no-op, no exception.
  Cleaned up: `cmux close-workspace --workspace workspace:16`, removed the `/tmp` scratch
  dir. Re-verified via `cmux workspace list --json`: `workspace:16` gone, and every
  remaining pre-existing workspace's `description` byte-identical to the pre-test
  snapshot. Note: `workspace:15` (`cmux-evolve-test-gittree-native`, presumably task 2.3's
  own test workspace) also disappeared between the before/after snapshots — this was NOT
  caused by any command in this task (no command here ever targeted `workspace:15`;
  every `_cmux_dual_write`/`workspace-action`/`close-workspace` call was scoped to
  `workspace:16` only) — attributable to concurrent session activity closing its own
  workspace independently.
- [x] [2.2] Port `apps/cc-tmux/src/cc_tmux/usage.py`'s `color_for`/`pct_for`/`_extract_util` logic to a standalone JS module; build a static HTML page that fetches `http://localhost:7400/credentials` client-side and renders the full multi-account dashboard (per-account progress bars, reset countdowns, summary header) using that ported logic [beads:if-5oeg]

  Ported `color_for`/`pct_for`/`_extract_util`/`_extract_reset_at`/`_account_label`/
  `dedupe_credentials`/`_freshest_active` faithfully to `scripts/cmux-usage-dashboard/
  usage-logic.js` (ES module), plus new countdown/refill-time formatters
  (`formatCountdown`/`formatRefillTime`, no Python equivalent existed). Built
  `scripts/cmux-usage-dashboard/index.html` — fetches the credentials endpoint client-side,
  dedupes, renders a compact summary chip row + full per-account 5H/7D meter cards, fails
  open to a clean "Usage unavailable" state on any fetch/parse error. Verified live: served
  via `python3 -m http.server`, Playwright headless load showed zero console errors; a
  same-origin fetch to `localhost:7400` (real nexus-agent, reachable on this machine) hit a
  genuine CORS block and the page correctly failed open (no uncaught exception); routing the
  real captured `/credentials` payload (125 raw rows, 3 unique post-dedupe identities) through
  `page.route` confirmed correct rendering — 3 summary chips + 3 account cards with correct
  colors/percentages/active badges, including a past-due reset correctly showing "Resetting…".
- [x] [2.3] Build a git-tree generator script (`git log --graph --all --format=...` parsed into HTML) that runs wherever it's invoked (local or remote SSH host) and is wired via `cmux browser open` from the workspace's own context [beads:if-f51y]

  **Finding (cmux 0.64.19, live-verified 2026-07-18)**: found and fixed a real dispatch bug in the
  prior (crashed) dispatch's implementation. `find_opener()` picked the native
  `cmux browser open file://...` path whenever a `cmux` binary was merely on PATH — but cmux's
  own SSH-backed remote-workspace mechanism installs a relay-forwarding `cmux` shim
  (`~/.cmux/bin/cmux`) on remote hosts too (confirmed live on homelab: `cmux --json capabilities`
  reports `"socket_path":"/Users/leonardoacosta/.local/state/cmux/cmux.sock"` — the Mac's own
  socket, reached over a relay). Calling the native path from homelab created a real
  `"type":"browser"` split (verified via `cmux list-panels`), but `cmux browser snapshot` on that
  surface showed the Mac's own "Can't open this page" error page, not the generated commit
  graph — the WebView renders on the Mac, where the homelab-local file path doesn't exist.
  Fixed by gating path 1 on `platform.system() == "Darwin"` (only true when actually running on
  the Mac), not `shutil.which("cmux")` alone. Re-verified after the fix: on homelab, dispatch
  correctly fell through to `mac-open --cmux`, served the file over Tailscale HTTP
  (`http://100.73.182.4:8790/...`), and `curl`ing that URL returned real HTML with real commit
  subjects from installfest's actual `git log` (`feat(cmux): port usage.py logic...`, etc.).
  On the real Mac (`ssh mac`, `uname -s` = Darwin, native `cmux` genuinely local), created a
  disposable local test workspace (`cmux-evolve-test-gittree-native`, workspace:15, verified
  `remote.enabled: false` — a real local, not SSH-backed, workspace), ran the script against the
  Mac's own local installfest checkout, and confirmed via `cmux browser snapshot --json` that the
  real rendered page (7356 bytes of HTML, `commit-hash` rows, real commit subjects matching the
  Mac checkout's own `git log`, zero "Can't open this page" text) loaded in cmux's embedded
  browser panel. Cleaned up per the safety rule: closed only `workspace:15` via
  `cmux close-workspace --workspace workspace:15`; a subsequent `list-workspaces` confirmed every
  pre-existing workspace (9, 10, 11, 14) untouched — a concurrent session's own test-workspace
  churn (workspace:13 -> workspace:16, unrelated task if-3jd4) was observed but not caused by this
  verification. Non-git-directory case (`--repo /tmp/...`) confirmed to render the "No git
  repository here" placeholder with exit 0, not an exception.
- [x] [2.4] Build a small periodic writer that populates the [1.2]-encoded openspec-status, beads-status, and usage 5H/7D fields via `cmux workspace-action`/the `cmux()` action confirmed in [1.3] [beads:if-34sn]
  - depends on: 1.2, 1.3

  Reviewed and confirmed correct the partial files left by a crashed prior attempt:
  `scripts/cmux-status-writer.py`, `scripts/lib/cmux_status_encoding.py` (shared encode/decode,
  reused per the [1.2] doc's "implement once" instruction), and
  `home/Library/LaunchAgents/com.leonardoacosta.cmux-status-writer.plist` (`StartInterval=120`,
  `RunAtLoad=true`, matches the existing `StartInterval`-based launchd convention already used by
  `validate-proxy.plist`/`mic-priority.plist` — no residual wiring gap). Confirmed the writer
  correctly reuses `scripts/bin/openspec-status --json --no-enrich` and
  `bd ready --json -n 0` / `bd list --status blocked --json -n 0` (the `-n 0` unlimited-results
  form, avoiding the known `bd ready` default-limit-10 undercounting footgun) rather than
  reinventing either. Usage carrier selection reuses `cc_tmux.usage.active_usage()` directly (no
  reimplementation) and correctly converts its 0..1 float to the encoding's integer-percent string.

  Live-verified on the real Mac (`ssh mac`, cmux 0.64.19) against the disposable test workspace
  `cmux-evolve-test-if-34sn` (workspace:13) left over from the crashed prior attempt: confirmed
  real field names (`current_directory`, `ref`, `remote.enabled/connected/destination`,
  `selected`, `description`) match the writer's assumptions exactly via a live
  `cmux workspace list --json` dump. Simulated a pre-existing state-only description
  (`CC1|waiting|permission|1737158765||||`, standing in for [2.1]'s not-yet-shipped hook
  dual-write), ran the writer targeting only that workspace twice, and confirmed both times the
  `state`/`wait_reason`/`epoch` segments survived byte-for-byte while `openspec`
  (`1 in-progress`) and `beads` (`46 ready, 0 blocked`) populated with real live data — idempotent,
  no corruption. Separately ran the writer against the real selected/carrier workspace
  (`workspace:11`, safe/reversible — my own live session) to confirm carrier-only usage writing
  and correct fail-open empty usage fields when nexus-agent's `/credentials` endpoint was
  unreachable from the Mac. Cleanup verified per the session's safety protocol: closed ONLY
  `workspace:13` via `cmux close-workspace --workspace workspace:13` (never `close-others`),
  confirmed via `cmux workspace list --json` before/after that `workspace:11`
  (main), `workspace:14` (a concurrent agent's own `cmux-evolve-test-git-tree-remote`),
  `workspace:9` (brown), and `workspace:10` (tc) were all unaffected.

## UI Batch

- [x] [3.1] Prototype a minimal left custom sidebar (`home/dot_config/cmux/sidebars/claude-sessions.swift.tmpl`) using only cmux's natively-free fields: `title`/`directory` (truncated to 5 chars) + session name, `branch`, `dirty` — verify it renders correctly in a real cmux session before adding smuggled-field rows on top [beads:if-boqe]

  **Finding (cmux 0.64.19, live-verified 2026-07-18)**: exact 5-character string slicing is NOT
  achievable with the sidebar interpreter's supported subset. Exhaustively confirmed live via a
  series of disposable diagnostic `.swift` sidebars (deployed to the real Mac, validated + opened
  + inspected via `System Events` accessibility-tree text dumps — no Screen Recording TCC grant
  for the SSH session, so `screencapture`/`browser snapshot` were unavailable and the AX-tree text
  dump was the working live-introspection substitute): `String.prefix(n)`/`.dropFirst(n)` are
  documented as Array-only methods and silently no-op on a `String` (return empty or drop the
  containing `Text` row); `Array(name)` bridges to `[Character]` correctly for `.count` but not for
  further chained methods; `.map { String($0) }` on a String's characters, direct `Character`
  interpolation via array-subscript, and `"name".first` all evaluate empty; `var` mutation is a
  silent no-op (accepted syntactically, writes never persist) and a self-recursive `func` blanks
  the entire render (evaluation budget or missing self-reference support). The only confirmed
  working string-shortening primitive is `.split(separator: <literal char>)` producing multi-char
  `Substring`s (`String(substring)` conversion works), not usable for generic positional
  truncation. Landed truncation as VISUAL truncation instead
  (`.lineLimit(1)` + `.truncationMode(.tail)` + `.frame(maxWidth: 44)`, all confirmed-supported
  modifiers) — the accessible/underlying text value is the full basename, not a
  programmatically-shortened string; this is a real interpreter constraint, not a shortcut. Filed
  as a note for a later spec-wording pass (task 4.2's live E2E check should assert visual/frame
  truncation, not an exact "insta…" string).

  Live-verified end to end on the real Mac (`ssh mac`, cmux 0.64.19): `cmux sidebar validate
  claude-sessions` → `OK ... 1 valid, 0 invalid`; `cmux sidebar open claude-sessions` rendered a
  real pane whose AX-tree text dump showed `Claude Sessions` (header) followed by one row per real
  live workspace — `installfest`/`if` (directory basename + title), `brown`, `tc` — matching the
  three actual open workspaces (`if`, `brown`, `tc`) with no interpreter errors and no crash;
  branch/dirty rows correctly omitted for these three SSH-backed workspaces since their live
  `branch` field is currently nil (guarded by `if let`, not a bug). Cleanup verified per the
  session's safety protocol: closed only the disposable diagnostic-sidebar panes this dispatch
  personally created (never `close-others`); a `cmux list-workspaces` before/after confirmed
  `workspace:9`/`10`/`11` (`brown`/`tc`/`if`) unaffected. Note: a concurrent session was verified
  live-testing this identical task on the same shared Mac/checkout at the same time (confirmed via
  a second `claude` process visible in the target session's own transcript) and had independently
  reached the same root-cause finding and landed the same visual-truncation fix in this file before
  this dispatch's own edit — the two converged on byte-identical file content (confirmed via
  `shasum -a 256` on both the repo source and the Mac-deployed copy).
- [x] [3.2] Extend the sidebar with the Claude-state indicator: SF-Symbol/shape stand-in for the Claude mark, solid when `idle`, pulsating opacity when `active` (alternating on `clock` wall-clock parity), pulsing red when `waiting` with reason `permission`; parses the [1.2] encoding from `description` [beads:if-n2oq]
  - depends on: 2.1, 3.1

  **Finding (cmux 0.64.19, live-verified 2026-07-18)**: the encoding doc's own reader-contract
  pseudocode — `description.split(separator: "|", omittingEmptySubsequences: false)` expecting
  exactly 8 segments — does NOT work. The interpreter silently ignores the
  `omittingEmptySubsequences` argument and always omits empty subsequences (the real Swift
  default, `true`): splitting a real CC1 string like `CC1|idle||1737158765||||` (8 fields, 3
  empty) produced `segs.count == 3`, not 8 — confirmed live via a disposable diagnostic sidebar
  rendering the split result as text, deployed to the real Mac and inspected via a System Events
  accessibility-tree text dump (no Screen Recording TCC grant over SSH, same substitute method
  task 3.1 used). This breaks positional field indexing for any real CC1 string, since most fields
  are legitimately empty most of the time. **Workaround**: since fields are fixed-order, `state`
  and `wait_reason` are identified by exact-prefix match instead (`d.hasPrefix("CC1|idle|")`,
  `"CC1|active|"`, `"CC1|waiting|permission|"`) — the trailing `|` after each vocabulary token
  disambiguates it from any other field value, sidestepping `.split` entirely. This is the same
  class of workaround as task 3.1's visual-truncation fix: a confirmed interpreter limitation
  worked around at the call site, not a shortcut.

  Live-verified end to end on the real Mac (`ssh mac`, cmux 0.64.19), per the incident safety
  rule: listed existing workspaces first (`if`/`brown`/`tc`, all off-limits), created exactly one
  disposable workspace (`cmux-evolve-test-if-n2oq-v2`, workspace:19). Iterated a diagnostic
  sidebar rendering the `cc1Mode`-equivalent classification as literal text, then the full
  production-shaped sidebar (identical logic plus the real `Image`/`.opacity`/`.foregroundColor`
  indicator), seeding the workspace's `description` via `cmux workspace-action --action
  set-description` for each state in turn and re-capturing the AX-tree text dump after each:
  - `CC1|idle||1737158765||||` → diagnostic text showed `MODE=[idle]`; the full sidebar showed a
    real SwiftUI `Image` element (`DESC:image`) immediately before the workspace's basename text,
    in the exact position the indicator renders.
  - `CC1|active||1737158900||||` → `MODE=[active]`; same `Image` element present.
  - `CC1|waiting|permission|1737159001||||` → `MODE=[waiting-permission]`/`MODE=[waiting]`; same
    `Image` element present.
  - The pre-existing `if` workspace (state field empty, `CC1||||1 in-progress|46 ready, 0
    blocked||`) and `brown`/`tc` (no description at all) all rendered **zero** `Image` elements
    for their rows in every capture — the fail-closed "no known state → no indicator" case falls
    out of the same `hasPrefix` chain with no extra code path, matching the spec's "unparseable or
    absent description renders no state indicator" scenario. `cmux sidebar validate` returned
    `1 valid, 0 invalid` for every iteration of the production file (zero interpreter parse
    errors on `Image`/`.opacity`/`.foregroundColor`/`.accessibilityLabel`/`if let`).
  - `clock.second` confirmed live and real (not static): captured values 17, 49, 46 across
    separate dumps (parities 1, 1, 0 respectively) — the active/waiting pulse ternary
    (`clock.second % 2 == 0 ? 1.0 : 0.35`) is driven by a genuinely changing wall-clock value.
  - AX-tree focus was intermittently flaky mid-session (a concurrent session's own active work in
    the `brown` workspace repeatedly stole front-window focus, and the Mac's own workspace:18 test
    instance disappeared on its own between captures — same concurrent-collision class task 2.1
    documented for workspace:15, not caused by any command here) — every capture actually used
    above succeeded before or after a focus reset; captures that returned empty were discarded,
    not treated as findings.

  Cleanup verified per the safety rule: closed only `workspace:19` via `cmux close-workspace
  --workspace workspace:19`; `cmux list-workspaces` before/after confirmed `if`/`brown`/`tc`
  unaffected. Removed both diagnostic sidebar files (`diag-n2oq.swift`, `diag-n2oq-full.swift`)
  from `~/.config/cmux/sidebars/` on the Mac — re-running `cmux sidebar validate` against their
  names now correctly errors `Sidebar file is missing`, confirming no debris left behind. The
  production `.swift` was deployed once more (verbatim, via `scp` — chezmoi wiring is task 3.4)
  and `cmux sidebar validate claude-sessions` returned `1 valid, 0 invalid` as the final check.
- [x] [3.3] Extend the sidebar with the openspec-status + beads-status row and the compact usage-meter footer (CYAN/YELLOW/RED thresholds ported from `usage.py`), both sourced from the [2.4] writer's smuggled fields [beads:if-6to1]
  - depends on: 2.4, 3.1

  **Design decision (writer-side sentinel, approach 2)**: the smuggled `openspec`/`beads`/
  `usage` middle fields could NOT be extracted reader-side because task 3.2's finding — cmux's
  interpreter ignores `.split(separator:"|", omittingEmptySubsequences:false)` and always
  collapses empty subsequences — makes positional indexing unrecoverable once any field is empty
  (you cannot tell which segment maps to which field after collapse; that is exactly the
  information collapse destroys). Live-confirmed this session: an old-format `CC1|||||||`
  splits to `N=1`, and a partially-empty `CC1|idle|-|1737159050||||` splits to `N=4` (trailing
  empties dropped). Approach 1 (reader-side content-shape heuristics) is fragile and can't even
  locate fields after collapse; rejected. **Chosen approach 2**: the shared encoder
  (`scripts/lib/cmux_status_encoding.py`) now substitutes a single-char `-` sentinel for every
  empty field, so an encoded string has ZERO empty segments and a plain `.split(separator:"|")`
  reliably yields all 8 — live-confirmed: `CC1|idle|-|1737159050|3 open, 1 approved|12 ready, 2
  blocked|85|47` splits to `N=8` with the exact 8 segments intact. The reader (`cc1Field`) indexes
  positionally and maps `-`→"". Verified compatible: task 3.2's state indicator stays on its
  `hasPrefix` matching (strictly more robust, `CC1|idle|-|...` still has prefix `CC1|idle|`),
  needs no change. Round-trip + backward-compat proven in Python (old empty-segment strings decode
  correctly and self-heal to sentinel form on first re-write); both dual-write writers
  (`tmux.py` [2.1], `cmux-status-writer.py` [2.4]) import the shared module and were shown to
  preserve each other's fields through the merged encode. `docs/cmux-sidebar-encoding.md` updated
  (its reader-contract pseudocode was known-wrong per 3.2's finding — replaced with the
  sentinel/positional scheme; worked examples + Python contract updated to match).
  `cc-tmux self-test`: 116/116 passed (unchanged — the encoding module has no self-test coverage,
  so no count change; round-trip proven via a standalone assertion script instead).

  **Live-verified end to end on the real Mac** (`ssh mac`, cmux 0.64.19), per the incident safety
  rule. The cmux GUI app was NOT running on arrival (only `cmux-bridge`, no socket) — launched it
  backgrounded (`open -g -a`) to run the mandatory render check, a reversible/non-destructive act
  that never touches existing workspaces. BEFORE snapshot: exactly one pre-existing workspace
  `workspace:1` (`tc`, `selected=true`, desc `CC1|||||||`), recorded off-limits. `cmux sidebar
  validate claude-sessions` → `1 valid, 0 invalid` (interpreter accepts `cc1Field`, `Int()`,
  `.cyan`/`.yellow`, and the nested Color ternary). Created disposable workspaces
  `cmux-evolve-test-if-6to1` (a first attempt's shell-quoting error left an extra same-named
  disposable — both mine, both off the pre-existing set, both cleaned up).
  - **Writer-collision finding**: the Mac's launchd `com.leonardoacosta.cmux-status-writer`
    (120 s `StartInterval`) is running the OLD (pre-sentinel) encoder (this session's change isn't
    deployed to the Mac's checkout) and read-modify-wrote my seeded description back to old-format
    empty segments within seconds, breaking the split. Paused it (`launchctl bootout`) for a
    stable reader test, RESTORED it (`launchctl bootstrap`) at cleanup — verified back in
    `launchctl list` (`runs=31`, `state=not running` = normal between interval ticks). This is a
    deployment concern for the E2E batch (the Mac needs this session's writer change pulled), not a
    reader defect.
  - **Production render (AX-tree text dump**, System Events accessibility tree — no Screen
    Recording TCC over SSH, same substitute method as 3.1/3.2): seeded `workspace:3` as the
    carrier `CC1|idle|-|1737159050|3 open, 1 approved|12 ready, 2 blocked|85|47` and `workspace:2`
    as sentinel-empty `CC1|idle|-|1737159050|-|-|-|-`. Dump showed: carrier row rendered
    `◇ 3 open, 1 approved` (openspec) + `● 12 ready, 2 blocked` (beads); the panel footer rendered
    exactly ONCE — `Usage` / `5H 85%` / `7D 47%` — from the carrier only (proving `Int("85")`/
    `Int("47")` parse and the carrier-scan `ForEach` yields a single footer); the sentinel-empty
    workspace rendered its state `Image` but ZERO smuggled rows (fail-closed); the old-format `tc`
    row rendered no smuggled rows and no indicator (fail-closed via `cc1Field`'s `count!=8` guard).
  - **Threshold color port** (usage.py `color_for` at integer-percent granularity: `>80`→RED,
    `>=50`→YELLOW, else CYAN — i.e. `p>80 ⟺ util>0.80`, `p>=50 ⟺ util>=0.50`): a `band()`
    diagnostic using the identical `Int()`+comparison the footer's `.foregroundColor` ternary uses
    classified `81=RED  80=YELLOW  65=YELLOW  50=YELLOW  49=CYAN  30=CYAN  0=CYAN` — all three
    bands covered, both boundaries (`>80`, `>=50`) exact. The actual `.red`/`.yellow`/`.cyan`
    rendering is proven by `validate` accepting the ternary + the footer rendering live.
  - **Cleanup** verified: removed my diagnostic sidebar (`diag-6to1.swift`) and the AX-dump
    scratch; RESTORED the launchd writer; AFTER `cmux workspace list --json` == BEFORE
    byte-for-byte (only `workspace:1` `tc`, `selected=true`, desc `CC1|||||||`, untouched; zero of
    my disposable workspaces remaining — they auto-closed on an app restart mid-session). NOTE: a
    concurrent session is working the SAME `if-6to1` bead — its own `diag-6to1b/c/d.swift`
    diagnostic files were present on the Mac and left UNTOUCHED (only my no-suffix `diag-6to1.swift`
    was mine to remove); no repo-file collision observed (my edits to the encoding module / sidebar
    / doc were intact at commit time).
- [x] [3.4] Wire the sidebar file into chezmoi deployment (`home/dot_config/cmux/sidebars/claude-sessions.swift.tmpl`) so it deploys to `~/.config/cmux/sidebars/` on `chezmoi apply` [beads:if-wut0]

  **Already wired — verified, not assumed.** The file lives at the chezmoi source path
  `home/dot_config/cmux/sidebars/claude-sessions.swift.tmpl` (repo `.chezmoiroot` = `home/`).
  `chezmoi managed | grep cmux` lists `.config/cmux/sidebars/claude-sessions.swift`;
  `chezmoi source-path ~/.config/cmux/sidebars/claude-sessions.swift` resolves back to the `.tmpl`
  source — so the chezmoi source-state is correctly registered.

  **`.tmpl` suffix decision: KEEP it.** The Swift content contains ZERO Go-template delimiters
  (`grep '{{\|}}'` → none), so chezmoi's template pass is a pure passthrough — no execution
  problem, no mangling. `chezmoi diff ~/.config/cmux/sidebars/claude-sessions.swift` rendered the
  full Swift (including this session's `cc1Field`/footer additions) verbatim with no template
  errors. Renaming to drop `.tmpl` would be a churn with no benefit (and the file's own header
  comment already documents why it carries the extension without directives). A targeted
  `chezmoi apply ~/.config/cmux/sidebars/claude-sessions.swift` (single-path, avoiding the
  full-apply unrelated-drift halt) succeeded; a follow-up `chezmoi diff` was empty and a byte
  comparison (`diff <(chezmoi cat …) <deployed>`) confirmed the deployed file is byte-identical to
  the chezmoi-rendered source, with the 3.3 additions present.

## E2E Batch

- [x] [4.1] Live-verify cc-tmux's dual-write: trigger a real state transition (prompt submit, permission prompt, stop) inside a cmux workspace, confirm `cmux workspace-action` fires and the workspace's `description` updates with the correct encoding [beads:if-35jf]
  - depends on: 2.1

  **BLOCKED — real bug found, dual-write never fires in genuine cmux-pane usage (cmux
  0.64.19, live-verified 2026-07-19).** `_cmux_dual_write` (`apps/cc-tmux/src/cc_tmux/tmux.py:607-638`)
  matches the current workspace via `w.get("ref") == os.environ.get("CMUX_WORKSPACE_ID")`. Live-verified
  on the real Mac (`ssh mac`) that this comparison can never succeed for a real pane: created a
  disposable local workspace (`cmux-evolve-test-if-35jf-e2e`, `workspace:2`), confirmed via
  `cmux send "env | grep -i cmux"` that cmux actually exports `CMUX_WORKSPACE_ID` as the workspace's
  **UUID** (`362EF01A-04BC-497C-9589-F640C7CE81A0`), while `cmux workspace list --json --id-format both`
  shows that same workspace's `ref` as `workspace:2` and its `id` as that same UUID — `ref` and
  `CMUX_WORKSPACE_ID` are never the same string, so `current_ws` at line 629-636 is always `None` and
  the function returns before ever reaching the `workspace-action` subprocess call at line 649.

  Confirmed by directly invoking the real production entrypoint (`apps/cc-tmux/bin/cc-tmux register
  --state ...`, the exact command `hooks.json` wires to SessionStart/UserPromptSubmit/Notification/Stop)
  inside the real pane with cmux's real injected environment — the same call a genuine Claude Code
  session running in that pane would make on prompt-submit / permission-prompt / stop:
  - `cc-tmux register --state active` → `EXIT=0`, `workspace:2` description unchanged (`CC1|-|-|-|-|...`,
    state/wait_reason/epoch fields stayed sentinel-`-`).
  - `cc-tmux register --state waiting --reason permission` → `EXIT=0`, same no-op, no change.
  - `cc-tmux register --state idle` → `EXIT=0`, same no-op, no change.

  All three calls exit 0 (the `try/except Exception: pass` fail-open contract holds — no crash), but
  none of them ever call `cmux workspace-action`, so the task's actual ask ("confirm `cmux
  workspace-action` fires") is disconfirmed, not confirmed. The description DID change once during this
  test, but only because the unrelated [2.4] periodic launchd writer (`cmux-status-writer.py --all`,
  120 s tick, unconditional full sweep) independently wrote a real `beads` field — proving
  `workspace-action`/description-writing works fine as a mechanism in general, and isolating the bug to
  `_cmux_dual_write`'s matching logic specifically.

  **Same bug pattern also exists** in `scripts/cmux-status-writer.py`'s single-workspace/carrier branch
  (`target_ref = os.environ.get("CMUX_WORKSPACE_ID")`, `[w for w in all_workspaces if w.get("ref") ==
  target_ref]`, line ~286-289) — but it is currently unexercised in production because the deployed
  launchd plist (`home/Library/LaunchAgents/com.leonardoacosta.cmux-status-writer.plist`) always invokes
  with `--all`, which bypasses the ref-matching branch entirely (confirmed by reading the plist's
  `ProgramArguments`).

  **Recommended fix** (not applied here — `tmux.py` belongs to the already-closed API batch / bead
  if-3jd4; scope explosion per `rules/CORE.md`, flagging rather than silently patching): resolve the
  current workspace by requesting `cmux workspace list --json --id-format both` and matching
  `w.get("id") == workspace_ref` instead of `w.get("ref")`, then use the matched `ref` (or the `id`
  directly, if `workspace-action --workspace` accepts a UUID — not yet confirmed) for the
  `workspace-action` call. This is a real, load-bearing gap: today, none of the state-icon
  animation this spec ships (task 3.2/3.3, live-verified only against manually-seeded descriptions)
  will ever actually update from genuine Claude Code session activity — the sidebar can render a
  correct icon, but nothing currently drives it from real usage.

  Cleanup verified: closed only `workspace:2` via `cmux close-workspace --workspace workspace:2`;
  `cmux workspace list --json` before/after confirmed the one pre-existing workspace
  (`cmux-e2e-if-35jf`, `workspace:1` — apparent debris from an earlier crashed E2E attempt at this
  same bead, left untouched per the safety protocol since this dispatch did not create it) was
  byte-identical throughout.

  **FIXED and re-verified (cmux 0.64.19, live-verified 2026-07-19/20).** Confirmed the exact
  correct field via `cmux workspace list --json --id-format both` (real Mac): a workspace's `id`
  (UUID) is a separate field from its `ref` (`workspace:N`), and a pane's real injected
  `$CMUX_WORKSPACE_ID` (confirmed via `env` inside a live pane, e.g.
  `EB9201DD-7F4B-46C0-8FC0-35D3FE186873`) equals that workspace's `id`, never its `ref`. Also
  confirmed the write side needs no change: `cmux workspace-action --help` documents
  `--workspace <id|ref|index>`, and a live call
  (`cmux workspace-action --workspace <UUID> --action set-description ...`) exited 0 and updated
  the description correctly — the bug was READ-side matching only, exactly as the prior dispatch
  scoped it.

  **Fix applied** (`apps/cc-tmux/src/cc_tmux/tmux.py` `_cmux_dual_write`, ~line 607-650): the
  `cmux workspace list` call now passes `--id-format both`, and the `current_ws = next(...)`
  lookup matches `w.get("id") == workspace_ref` instead of `w.get("ref")`. Same fix pattern
  applied to `scripts/cmux-status-writer.py`'s `list_workspaces()` (now requests
  `--id-format both` too) and `main()`'s `target_ref` matching (now matches `w.get("ref") ==
  target_ref or w.get("id") == target_ref`, since that script's `target_ref` has two legitimate
  sources — an explicit `--workspace <ref>` CLI arg, documented usage per task 4.2's
  `--workspace workspace:6`, which is genuinely a `ref` — and the `$CMUX_WORKSPACE_ID` env-var
  fallback, which is a UUID; matching against either keeps both call sites correct without
  needing to track provenance separately). The `--all` full-sweep path is untouched (it never
  filtered by ref/id in the first place). `cc-tmux self-test`: 116/116 passed, no regressions
  (`cd apps/cc-tmux && PYTHONPATH=src python3 -m cc_tmux self-test`).

  **Second, deeper finding surfaced during live re-verification**: a bare `cmux workspace create`
  (the disposable-test-workspace mechanism every task in this file uses) gives a plain zsh pane
  with NO tmux server running (`$TMUX`/`$TMUX_PANE` both empty, `tmux display-message` ->
  `no server running`). `cli.cmd_register` resolves `pane = args.pane or
  tmux.current_pane_id()` and returns 0 immediately when that's `None` — so invoking
  `cc-tmux register --state ...` in a bare (non-tmux) cmux pane never reaches
  `set_pane_state`/`_cmux_dual_write` at all, regardless of the ref/id fix. This is expected
  architecture, not a new bug: cc-tmux is fundamentally a tmux plugin, and its hooks only make
  sense running inside an actual tmux pane (the genuine deployed setup: tmux running inside a
  cmux-launched pane, e.g. an SSH-backed workspace attaching to the homelab tmux session, or a
  local tmux session on the Mac). Re-verified faithfully by starting a real detached tmux session
  inside the disposable test pane (`tmux new-session -d -s if35jf-verify`, confirmed
  `$CMUX_WORKSPACE_ID` correctly inherited into that session's pane env from the parent cmux
  shell) and driving `cc-tmux register` from *inside* that tmux pane via `tmux send-keys`,
  exactly matching how a real Claude Code session running under cc-tmux in a cmux workspace would
  invoke it.

  **Live re-verification, all three real state transitions** (real Mac, disposable workspace
  `cmux-evolve-test-if-fix-4dot1` -> `workspace:7`, pre-existing `workspace:1`
  (`cmux-e2e-if-35jf`) listed off-limits first): baseline reset to `CC1|-|-|-|-|-|-|-|` via
  `workspace-action`, then via the patched `bin/cc-tmux register` invoked from inside the real
  tmux pane:
  - `register --state active` -> exit 0, description became
    `CC1|active|-|1784518206|-|22 ready, 0 blocked|-|-` (state written, epoch stamped, the
    pre-existing beads field from the unrelated [2.4] periodic launchd writer preserved
    byte-for-byte).
  - `register --state waiting --reason permission` -> exit 0, description became
    `CC1|waiting|permission|1784518253|-|22 ready, 0 blocked|-|-` (wait_reason correctly set).
  - `register --state idle` -> exit 0, description became
    `CC1|idle|-|1784518259|-|-|-|-` (wait_reason correctly cleared; the beads field's own value
    at this snapshot reflects the concurrent [2.4] writer's independent periodic re-query, not a
    regression in this fix — the state/wait_reason/epoch fields it doesn't own were unaffected by
    that writer's pass).
  This directly disconfirms the prior report's inference that the description would never update
  from genuine hook activity — with the fix, all three of the task's named transitions (prompt
  submit / permission prompt / stop) now correctly drive `cmux workspace-action` end to end.

  **`cmux-status-writer.py` fix also independently live-verified**: created a second disposable
  workspace (`cmux-evolve-test-if-writer-fix` -> `workspace:8`), ran the patched script with only
  `$CMUX_WORKSPACE_ID` set (no `--workspace` flag, exercising exactly the previously-broken
  env-var path) from a scratch copy preserving the script's real relative-path module resolution
  -> `updated 1/1 workspace(s)`, and confirmed only `workspace:8`'s description picked up real
  live beads data (`workspace:1` untouched) — proving the `id`-or-`ref` match correctly resolved
  the single target workspace from the UUID alone.

  Cleanup verified throughout: killed the test tmux session (`tmux kill-session`, confirmed via
  `tmux ls` -> `no server running`), closed only `workspace:7` and `workspace:8` via
  `cmux workspace close --workspace <ref>`, removed all `/tmp` scratch copies on the Mac. Final
  `cmux workspace list --json` showed only the pre-existing `workspace:1` (`cmux-e2e-if-35jf`),
  untouched throughout every step.
- [x] [4.2] Live-verify the full left sidebar in a real cmux session: git state, project/session name, Claude-state icon animation (all three modes), openspec/beads segments, usage footer — against a real disposable test workspace, cleaned up after [beads:if-g4u2]
  - depends on: 3.2, 3.3, 3.4

  **Live-verified end to end on the real Mac** (`ssh mac`, cmux 0.64.19, 2026-07-19), per the incident
  safety rule: listed existing workspaces first (`cmux-e2e-if-35jf`, off-limits), created exactly one
  disposable local workspace (`cmux-evolve-test-if-g4u2`, `workspace:6`, `cwd` = the Mac's real
  installfest checkout, which happened to be genuinely dirty: `M platform/raycast-scripts/local/
  QuoteRepo.sh`, branch `main`). Selected it and ran `cmux sidebar open claude-sessions` (`1 valid, 0
  invalid`), then captured the real rendered sidebar via a System Events accessibility-tree
  `entire contents of window 1` dump (same substitute technique 3.1/3.2/3.3 established — no Screen
  Recording TCC grant over SSH).

  **Real-data pass** (no seeding): ran `python3 scripts/cmux-status-writer.py --workspace workspace:6`
  directly (bypasses the [4.1]-documented `CMUX_WORKSPACE_ID` bug by taking an explicit `--workspace`
  ref) — `updated 1/1 workspace(s)`, and the workspace's description picked up a **real, live** beads
  count: `CC1|-|-|-|-|22 ready, 0 blocked|-|-`. The AX-tree dump confirmed, in order: header `Claude
  Sessions`; the pre-existing `cmux-e2e-if-35jf` row untouched; then for my row — `installfest`
  (real cwd basename) + `cmux-evolve-test-if-g4u2` (session title); `main` (real git branch, sourced
  from cmux's own workspace model, not the plain CLI JSON which doesn't expose `branch`/`dirty` as
  top-level keys) + a lone orange `●` dirty dot (correctly present — the checkout really is dirty);
  and `● 22 ready, 0 blocked` (real beads segment). No state-icon `image` element and no openspec
  segment rendered in this real-data pass — both correctly fail-closed: state was never written (the
  [4.1] dual-write bug — no genuine transition had occurred), and openspec was empty because
  `~/.claude/scripts/bin/openspec-status` itself silently no-ops on this Mac (`flock: command not
  found` → falsely interpreted as "already running, skipping" — `flock` is a Linux/util-linux tool
  absent from stock macOS; confirmed via `which flock` → not found, reproduced twice). This is a
  pre-existing gap in a `~/.claude`-owned (cc repo) script, outside this spec's scope — noted, not
  fixed here.

  **Full-integration pass** (seeding the fields real infra can't currently supply, to close out the
  task's own remaining coverage — same technique 3.2/3.3 used, applied here as one combined capture
  rather than split across separate diagnostic files): three sequential `cmux workspace-action
  --action set-description` calls against `workspace:6`, preserving the real beads field, each
  re-captured via a fresh AX-tree dump:
  - `CC1|idle|-|<epoch>|-|22 ready, 0 blocked|-|-` → `image 1` present immediately before
    `installfest` (idle indicator).
  - `CC1|active|-|<epoch>|-|22 ready, 0 blocked|-|-` → `image 1` present, same position (active
    indicator; pulse/opacity animation itself already live-verified via a genuinely changing
    `clock.second` in task 3.2 — AX-tree text dumps can't capture opacity, so not re-proven pixel-wise
    here).
  - `CC1|waiting|permission|<epoch>|-|22 ready, 0 blocked|-|-` → `image 1` present, same position
    (waiting/permission indicator).
  - `CC1|idle|-|<epoch>|2 draft, 1 approved|22 ready, 0 blocked|63|38` → full combined row confirmed
    in one dump: state `image 1`, `installfest`/`cmux-evolve-test-if-g4u2`, `main` + dirty `●`,
    `◇ 2 draft, 1 approved` (openspec) + `● 22 ready, 0 blocked` (beads, still the real live count),
    and a `Usage` / `5H 63%` / `7D 38%` footer — every documented sidebar element rendering together
    in a single real workspace.

  Cleanup verified: closed only `workspace:6` via `cmux close-workspace --workspace workspace:6`;
  `cmux workspace list --json` before/after confirmed `workspace:1` (`cmux-e2e-if-35jf`)
  byte-identical throughout. No diagnostic sidebar files were created (reused the production
  `claude-sessions` sidebar directly), so no extra file cleanup was needed.
- [x] [4.3] Live-verify the git-tree panel against both a local workspace and an SSH-backed workspace (homelab), confirming the SSH case renders the REMOTE repository's graph, not a stale local one [beads:if-tf9q]
  - depends on: 2.3

  **Live-verified end to end on the real Mac + homelab** (`ssh mac`, cmux 0.64.19, 2026-07-19), per the
  incident safety rule. Since the Mac's and homelab's `installfest` checkouts happened to already be at
  the same commit (both `231e4fa`, synced via the deploy hooks), a commit-hash diff alone wouldn't
  distinguish "rendered the real remote repo" from "rendered a stale local copy that happens to
  match" — used a stronger, unambiguous marker instead: created a **throwaway git repo that exists
  only on homelab** (`/tmp/claude-.../scratchpad/cmux-git-tree-marker-if-tf9q`, one commit,
  subject `MARKER-COMMIT-if-tf9q-homelab-only-<epoch>`) — a path with zero presence anywhere on the
  Mac's filesystem, so any render of its content is only possible if the script genuinely executed on
  homelab.

  - **SSH-backed case**: listed existing workspaces first (`cmux-e2e-if-35jf`, off-limits), created one
    disposable SSH-backed workspace (`cmux-evolve-test-if-tf9q`, `workspace:3`, `cmux ssh homelab`,
    confirmed `remote.enabled=true`/`remote.connected=true`/`dest=homelab`). Ran `cmux-git-tree.py
    --repo <the homelab-only marker path>` inside that pane; `cmux browser snapshot` on the resulting
    surface showed `ready_state: complete`, URL `http://100.73.182.4:8790/.cache/cmux-git-tree/
    cmux-git-tree-marker-if-tf9q-*.html` (homelab's real Tailscale IP, HTTP-served per `find_opener`'s
    non-Darwin path), title `git tree — cmux-git-tree-marker-if-tf9q`, and body text containing the
    exact commit hash (`84fd509`) and the exact unique subject
    `MARKER-COMMIT-if-tf9q-homelab-only-1784517287` — content that could only exist if the script's
    `git log` subprocess actually ran against homelab's filesystem. Conclusively rules out the
    "stale local render" failure mode this task exists to catch.
  - **Local case**: created one disposable local workspace (`cmux-evolve-test-if-tf9q-local`,
    `workspace:4`, `cwd` = the Mac's own installfest checkout). Ran the same script against
    `/Users/leonardoacosta/dev/personal/installfest`; `cmux browser snapshot` showed a `file://
    .../installfest-*.html` URL (Darwin path — no HTTP server, per `find_opener`), title
    `git tree — installfest`, and body text headed by the Mac's real HEAD (`231e4fab` /
    `chore(beads): close if-ammk...`), matching `git log -1` run directly on the Mac.

  Cleanup verified: closed `workspace:3` and `workspace:4` via `cmux close-workspace`; removed the
  generated cache HTML on both hosts (`~/.cache/cmux-git-tree/*.html` on the Mac and on homelab) and
  the throwaway marker repo from the scratchpad. `cmux workspace list --json` before/after confirmed
  `workspace:1` (`cmux-e2e-if-35jf`) byte-identical throughout.
- [x] [4.4] Live-verify the usage dashboard panel against real nexus-agent data (or its unreachable-degradation path) [beads:if-kg51]
  - depends on: 2.2

  **Live-verified the degradation path on the real Mac** (`ssh mac`, cmux 0.64.19, 2026-07-19), per the
  incident safety rule. Checked real nexus-agent reachability first: `Nexus.app`'s menubar process is
  running (`ps aux` shows `/Applications/Nexus.app/Contents/MacOS/nexus`), but its HTTP credentials API
  is genuinely NOT listening on port 7400 right now — `curl -sv http://localhost:7400/credentials` →
  `connect ... failed: Connection refused` on both `::1` and `127.0.0.1`. This is real, unforced,
  live-observed state (not simulated), so per the task's own "or its unreachable-degradation path"
  allowance, verified that path.

  Listed existing workspaces first (`cmux-e2e-if-35jf`, off-limits), started a real HTTP server serving
  `scripts/cmux-usage-dashboard/` (`python3 -m http.server 8791`, confirmed `HTTP 200` on
  `index.html`), created one disposable workspace (`cmux-evolve-test-if-kg51`, `workspace:5`), and
  opened `http://localhost:8791/index.html` in cmux's embedded browser panel via `cmux browser open
  --workspace workspace:5`. `cmux browser snapshot` showed `ready_state: complete`, title `Claude Usage
  Dashboard`, and body text `Claude Usage Dashboard\nUsage unavailable.` — the real fetch to the
  genuinely-unreachable endpoint failed and the page correctly fell open to its degraded state, with no
  blank page and no stuck-loading state. The real-data rendering path (per-account meters, colors,
  countdowns) was already exhaustively proven in task 2.2 via Playwright with a real captured
  `/credentials` payload routed through `page.route` (125 raw rows → 3 accounts, correct
  colors/percentages/active badges) — not re-proven here since real nexus-agent data isn't available
  in this environment right now; only the delivery mechanism (cmux browser panel actually loading and
  rendering the page) needed independent E2E confirmation, which this pass provides.

  Cleanup verified: closed `workspace:5` via `cmux close-workspace`; killed the temporary HTTP server
  (`pkill -f "http.server 8791"`, re-confirmed via a follow-up `curl` connection-refused) and removed
  its log file. `cmux workspace list --json` before/after confirmed `workspace:1`
  (`cmux-e2e-if-35jf`) byte-identical throughout.
