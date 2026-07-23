## MODIFIED Requirements

### Requirement: A claimed item is linked to its session via an /apply reference or cwd+timestamp proximity
`TranscriptSource` SHALL link a Claude Code session to a claimed beads/openspec item using, in
order: (1) an exact `/apply <id>` reference found in a `user`-type line's message text, or (2) a
fallback match on the transcript's `cwd` field against the item's known repo path AND
claim-timestamp proximity within a configurable window (default 10 minutes). A subagent sidechain
transcript (`isSidechain: true`) SHALL inherit its parent session's item linkage via `parentUuid`
rather than being matched independently. The transcript's own `cwd` field MUST be trusted over any
inference from directory-name flattening. The resolved `cwd` used in this matching SHALL also be
published onto the linked item's `store.SessionLink` so a downstream pane can render it —
matching without publishing it leaves the operator no way to inspect the comparison the algorithm
made.

#### Scenario: an exact /apply reference links immediately
- Given: a `user`-type transcript line contains the text `/apply if-abc12`
- When: `TranscriptSource` processes that line
- Then: the session is linked to item `if-abc12` with no further matching needed

#### Scenario: cwd+timestamp fallback links when no exact reference exists
- Given: no transcript line contains an `/apply <id>` reference
- When: the transcript's `cwd` matches a claimed item's repo path and the transcript's earliest
  timestamp falls within the configured window of that item's claim timestamp
- Then: the session is linked to that item via the fallback path

#### Scenario: cwd match alone, without timestamp proximity, does not link
- Given: a transcript's `cwd` matches a claimed item's repo path
- When: the transcript's earliest timestamp is outside the configured proximity window
- Then: no link is made — cwd alone is not sufficient

#### Scenario: a sidechain file inherits its parent's linkage
- Given: a transcript line has `isSidechain: true` and a `parentUuid`
- When: `TranscriptSource` processes it
- Then: it is attributed to whatever item the parent session is linked to, never matched
  independently

#### Scenario: directory-name flattening is never trusted over the transcript's own cwd field
- Given: a transcript file's flattened directory name could plausibly map to more than one real
  project path
- When: `TranscriptSource` determines the session's working directory
- Then: it uses the `cwd` field recorded inside the transcript lines, never a path reconstructed
  from the flattened directory name

#### Scenario: the matched cwd is available on the linked item's SessionLink
- Given: `TranscriptSource` has resolved a session's linkage (via either the exact-reference or
  cwd+timestamp path)
- When: it publishes the resulting `store.SessionLinkEvent`
- Then: the published `SessionLink.CWD` carries the same `cwd` value the linkage decision itself
  was made against

### Requirement: SessionsPane renders the pane map, context gauges, and zombie badges as a focus-ring pane
`SessionsPane` SHALL implement `wavetui-core`'s `Pane` interface (`Update(Snapshot) Pane`,
`View() string`, `Focusable() bool`) and render, per linked session: its pane identity (when
known), the linked session's own reported working directory, context-percent gauge, and zombie
badge (when applicable). It SHALL attach to `wavetui-core`'s existing focus ring without requiring
any change to the root model. The pane's header SHALL state that its contents are scoped to
sessions linked to the currently selected item, not every live Claude Code session in the repo.

#### Scenario: SessionsPane implements the shared Pane interface
- Given: `wavetui-core`'s root model's pane collection
- When: `SessionsPane` is added to it
- Then: it satisfies the same `Pane` interface as `QueuePane` and `DetailPane`, requiring no root
  model changes

#### Scenario: a linked session's context gauge is visible
- Given: an item has a linked session with a context-percent estimate
- When: `SessionsPane` renders that item's row
- Then: the context-percent gauge is displayed and reflects the current estimate

#### Scenario: a linked session's cwd is visible on its row
- Given: an item has a linked session whose `SessionLink.CWD` is non-empty
- When: `SessionsPane` renders that item's row
- Then: the row displays that cwd value alongside the existing pane/context%/zombie fields

#### Scenario: the header states the pane's per-item scope
- Given: an operator has any item selected, linked or not
- When: `SessionsPane` renders its header
- Then: the header text makes clear the list below is scoped to the selected item's linked
  sessions, not a repo-wide list of every live Claude Code session
